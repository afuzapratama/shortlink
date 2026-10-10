package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"shortlink.local/control/internal/security"
	"time"
)

var ErrFactor = errors.New("invalid authentication factor")
var ErrMFARequired = errors.New("MFA required")

func (s *Store) BeginMFA(ctx context.Context, owner string) (string, error) {
	secret := security.MFASecret()
	seed, err := s.Seed(ctx)
	if err != nil {
		return "", err
	}
	sealed, err := security.Seal(seed, "mfa:"+owner, []byte(secret))
	if err != nil {
		return "", err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if err = tx.QueryRow(ctx, "SELECT mfa_enabled FROM owners WHERE id=$1 FOR UPDATE", owner).Scan(&enabled); err != nil {
		return "", err
	}
	if enabled {
		return "", ErrUsed
	}
	if _, err = tx.Exec(ctx, "INSERT INTO mfa_pending(owner_id,secret,expires_at) VALUES($1,$2,now()+interval '10 minutes') ON CONFLICT(owner_id) DO UPDATE SET secret=$2,expires_at=now()+interval '10 minutes'", owner, sealed); err != nil {
		return "", err
	}
	if err = audit(ctx, tx, owner, "mfa_setup_started", owner); err != nil {
		return "", err
	}
	return secret, tx.Commit(ctx)
}
func recoveryCodes(ctx context.Context, tx pgx.Tx, owner string) ([]string, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM mfa_recovery WHERE owner_id=$1", owner); err != nil {
		return nil, err
	}
	codes := make([]string, 8)
	for i := range codes {
		codes[i] = security.Token()
		if _, err := tx.Exec(ctx, "INSERT INTO mfa_recovery(owner_id,code_hash) VALUES($1,$2)", owner, security.Digest(codes[i])); err != nil {
			return nil, err
		}
	}
	return codes, nil
}
func (s *Store) ConfirmMFA(ctx context.Context, owner, code, sessionHash string) ([]string, error) {
	seed, err := s.Seed(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if err = tx.QueryRow(ctx, "SELECT mfa_enabled FROM owners WHERE id=$1 FOR UPDATE", owner).Scan(&enabled); err != nil {
		return nil, err
	}
	if enabled {
		return nil, ErrUsed
	}
	var sealed []byte
	if err = tx.QueryRow(ctx, "SELECT secret FROM mfa_pending WHERE owner_id=$1 AND expires_at>now() FOR UPDATE", owner).Scan(&sealed); err != nil {
		return nil, ErrFactor
	}
	raw, err := security.OpenSecret(seed, "mfa:"+owner, sealed)
	if err != nil {
		return nil, err
	}
	step, ok := security.MatchTOTP(string(raw), code, -1, time.Now())
	if !ok {
		return nil, ErrFactor
	}
	if _, err = tx.Exec(ctx, "UPDATE owners SET mfa_enabled=true,mfa_secret=$2,mfa_last_step=$3 WHERE id=$1", owner, sealed, step); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM sessions WHERE owner_id=$1 AND token_hash<>$2", owner, sessionHash); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM mfa_pending WHERE owner_id=$1", owner); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "UPDATE sessions SET mfa_verified=true WHERE token_hash=$1 AND owner_id=$2", sessionHash, owner); err != nil {
		return nil, err
	}
	codes, err := recoveryCodes(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, owner, "mfa_enabled", owner); err != nil {
		return nil, err
	}
	return codes, tx.Commit(ctx)
}
func (s *Store) MFAChallenge(ctx context.Context, owner, hash, oldHash string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if err = tx.QueryRow(ctx, "SELECT mfa_enabled FROM owners WHERE id=$1 FOR UPDATE", owner).Scan(&enabled); err != nil {
		return err
	}
	if !enabled {
		return ErrFactor
	}
	if _, err = tx.Exec(ctx, "INSERT INTO mfa_challenges(challenge_hash,owner_id,expires_at) VALUES($1,$2,now()+interval '5 minutes')", hash, owner); err != nil {
		return err
	}
	if oldHash != "" {
		if _, err = tx.Exec(ctx, "DELETE FROM sessions WHERE token_hash=$1", oldHash); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) factor(ctx context.Context, tx pgx.Tx, seed []byte, owner, code string) error {
	var enabled bool
	var sealed []byte
	var last int64
	if err := tx.QueryRow(ctx, "SELECT mfa_enabled,mfa_secret,mfa_last_step FROM owners WHERE id=$1 FOR UPDATE", owner).Scan(&enabled, &sealed, &last); err != nil {
		return err
	}
	if !enabled {
		return ErrFactor
	}
	if security.ValidToken(code) {
		tag, err := tx.Exec(ctx, "DELETE FROM mfa_recovery WHERE owner_id=$1 AND code_hash=$2", owner, security.Digest(code))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrFactor
		}
		return nil
	}
	raw, err := security.OpenSecret(seed, "mfa:"+owner, sealed)
	if err != nil {
		return err
	}
	step, ok := security.MatchTOTP(string(raw), code, last, time.Now())
	if !ok {
		return ErrFactor
	}
	_, err = tx.Exec(ctx, "UPDATE owners SET mfa_last_step=$2 WHERE id=$1", owner, step)
	return err
}
func (s *Store) VerifyMFAChallenge(ctx context.Context, challengeHash, code, sessionHash string, ttl time.Duration) error {
	seed, err := s.Seed(ctx)
	if err != nil {
		return err
	}
	var owner string
	if err = s.DB.QueryRow(ctx, "SELECT owner_id FROM mfa_challenges WHERE challenge_hash=$1", challengeHash).Scan(&owner); err != nil {
		return ErrFactor
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked string
	if err = tx.QueryRow(ctx, "SELECT id FROM owners WHERE id=$1 FOR UPDATE", owner).Scan(&locked); err != nil {
		return err
	}
	var attempts int
	if err = tx.QueryRow(ctx, "SELECT attempts FROM mfa_challenges WHERE challenge_hash=$1 AND expires_at>now() FOR UPDATE", challengeHash).Scan(&attempts); err != nil || attempts >= 5 {
		return ErrFactor
	}
	if _, err = tx.Exec(ctx, "UPDATE mfa_challenges SET attempts=attempts+1 WHERE challenge_hash=$1", challengeHash); err != nil {
		return err
	}
	if err = s.factor(ctx, tx, seed, owner, code); err != nil {
		if errors.Is(err, ErrFactor) {
			if err = tx.Commit(ctx); err != nil {
				return err
			}
			return ErrFactor
		}
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM mfa_challenges WHERE challenge_hash=$1", challengeHash); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO sessions(token_hash,owner_id,expires_at,mfa_verified) VALUES($1,$2,now()+$3::interval,true)", sessionHash, owner, fmt.Sprintf("%f seconds", ttl.Seconds())); err != nil {
		return err
	}
	if err = audit(ctx, tx, owner, "mfa_login", owner); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) ChangeMFA(ctx context.Context, owner, code, sessionHash string, disable bool) ([]string, error) {
	seed, err := s.Seed(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = s.factor(ctx, tx, seed, owner, code); err != nil {
		return nil, err
	}
	var codes []string
	action := "mfa_recovery_regenerated"
	if disable {
		action = "mfa_disabled"
		if _, err = tx.Exec(ctx, "UPDATE owners SET mfa_enabled=false,mfa_secret=NULL,mfa_last_step=-1 WHERE id=$1", owner); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM mfa_recovery WHERE owner_id=$1", owner); err != nil {
			return nil, err
		}
	} else {
		codes, err = recoveryCodes(ctx, tx, owner)
		if err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, "DELETE FROM sessions WHERE owner_id=$1 AND token_hash<>$2", owner, sessionHash); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM mfa_challenges WHERE owner_id=$1", owner); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, owner, action, owner); err != nil {
		return nil, err
	}
	return codes, tx.Commit(ctx)
}
