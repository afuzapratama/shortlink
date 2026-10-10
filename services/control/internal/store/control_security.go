package store

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"shortlink.local/control/internal/security"
	"time"
)

type Migration struct {
	Purpose   string    `json:"purpose"`
	ID        string    `json:"id"`
	Origin    string    `json:"origin"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type SignedMigration struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}
type ControlStatus struct {
	AgentID         string     `json:"agentId"`
	DomainID        string     `json:"domainId"`
	Hostname        string     `json:"hostname"`
	Credential      string     `json:"credential,omitempty"`
	RotationPending bool       `json:"rotationPending"`
	MigrationID     string     `json:"migrationId,omitempty"`
	LastSeenAt      *time.Time `json:"lastSeenAt"`
}

func (s *Store) securityTransaction(ctx context.Context, owner, code string) (pgx.Tx, error) {
	seed, err := s.Seed(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var enabled bool
	err = tx.QueryRow(ctx, "SELECT mfa_enabled FROM owners WHERE id=$1 FOR UPDATE", owner).Scan(&enabled)
	if err == nil && enabled {
		err = s.factor(ctx, tx, seed, owner, code)
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}
func (s *Store) StageRotation(ctx context.Context, owner, code, agent string) error {
	seed, err := s.Seed(ctx)
	if err != nil {
		return err
	}
	tx, err := s.securityTransaction(ctx, owner, code)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var domain string
	if err = tx.QueryRow(ctx, "SELECT domain_id FROM agents WHERE id=$1 AND revoked_at IS NULL", agent).Scan(&domain); err != nil {
		return err
	}
	var locked string
	if err = tx.QueryRow(ctx, "SELECT id FROM domains WHERE id=$1 FOR UPDATE", domain).Scan(&locked); err != nil {
		return err
	}
	var old string
	if err = tx.QueryRow(ctx, "SELECT credential_hash FROM agents WHERE id=$1 AND revoked_at IS NULL FOR UPDATE", agent).Scan(&old); err != nil {
		return err
	}
	var pending bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agent_rotations WHERE agent_id=$1 AND acked_at IS NULL AND expires_at>now())", agent).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return ErrUsed
	}
	raw := security.Token()
	sealed, err := security.Seal(seed, "rotation:"+agent, []byte(raw))
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO agent_rotations(agent_id,old_hash,new_hash,secret,expires_at) VALUES($1,$2,$3,$4,now()+interval '24 hours') ON CONFLICT(agent_id) DO UPDATE SET old_hash=$2,new_hash=$3,secret=$4,expires_at=now()+interval '24 hours',created_at=now(),acked_at=NULL`, agent, old, security.Digest(raw), sealed); err != nil {
		return err
	}
	if err = audit(ctx, tx, owner, "agent_rotation_staged", agent); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) StageMigration(ctx context.Context, owner, code, origin string) (SignedMigration, error) {
	tx, err := s.securityTransaction(ctx, owner, code)
	if err != nil {
		return SignedMigration{}, err
	}
	defer tx.Rollback(ctx)
	m := Migration{"shortlink-control-migration-v1", security.Token(), origin, time.Now().UTC().Add(7 * 24 * time.Hour)}
	if _, err = tx.Exec(ctx, "INSERT INTO control_migrations(id,origin,expires_at) VALUES($1,$2,$3)", m.ID, m.Origin, m.ExpiresAt); err != nil {
		return SignedMigration{}, err
	}
	if err = audit(ctx, tx, owner, "control_migration_staged", m.ID); err != nil {
		return SignedMigration{}, err
	}
	seed, err := s.Seed(ctx)
	if err != nil {
		return SignedMigration{}, err
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return SignedMigration{}, err
	}
	doc := SignedMigration{base64.RawURLEncoding.EncodeToString(payload), base64.RawURLEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), payload))}
	return doc, tx.Commit(ctx)
}
func (s *Store) ControlStates(ctx context.Context) ([]ControlStatus, error) {
	rows, err := s.DB.Query(ctx, `SELECT a.id,a.domain_id,d.hostname,EXISTS(SELECT 1 FROM agent_rotations r WHERE r.agent_id=a.id AND r.acked_at IS NULL AND r.expires_at>now()),COALESCE(c.migration_id,''),d.last_seen_at FROM agents a JOIN domains d ON d.id=a.domain_id LEFT JOIN agent_control_acks c ON c.agent_id=a.id WHERE a.revoked_at IS NULL ORDER BY d.hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ControlStatus{}
	for rows.Next() {
		var v ControlStatus
		if err = rows.Scan(&v.AgentID, &v.DomainID, &v.Hostname, &v.RotationPending, &v.MigrationID, &v.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) AgentControl(ctx context.Context, a Agent) (ControlStatus, error) {
	var v ControlStatus
	v.AgentID = a.ID
	v.DomainID = a.DomainID
	if err := s.DB.QueryRow(ctx, "SELECT hostname FROM domains WHERE id=$1", a.DomainID).Scan(&v.Hostname); err != nil {
		return v, err
	}
	var sealed []byte
	err := s.DB.QueryRow(ctx, "SELECT secret FROM agent_rotations WHERE agent_id=$1 AND acked_at IS NULL AND expires_at>now()", a.ID).Scan(&sealed)
	if err == pgx.ErrNoRows {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	seed, err := s.Seed(ctx)
	if err != nil {
		return v, err
	}
	raw, err := security.OpenSecret(seed, "rotation:"+a.ID, sealed)
	if err != nil {
		return v, err
	}
	v.Credential = string(raw)
	v.RotationPending = true
	return v, nil
}
func (s *Store) AckControl(ctx context.Context, a Agent, secret, migration, origin string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked string
	if err = tx.QueryRow(ctx, "SELECT id FROM domains WHERE id=$1 FOR UPDATE", a.DomainID).Scan(&locked); err != nil {
		return err
	}
	var current string
	if err = tx.QueryRow(ctx, "SELECT credential_hash FROM agents WHERE id=$1 AND revoked_at IS NULL FOR UPDATE", a.ID).Scan(&current); err != nil {
		return err
	}
	var next string
	err = tx.QueryRow(ctx, "SELECT new_hash FROM agent_rotations WHERE agent_id=$1 AND acked_at IS NULL AND expires_at>now() FOR UPDATE", a.ID).Scan(&next)
	hash := security.Digest(secret)
	if err == nil {
		if hash != next {
			return ErrFactor
		}
		if _, err = tx.Exec(ctx, "UPDATE agents SET credential_hash=$2 WHERE id=$1", a.ID, next); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE agent_rotations SET acked_at=now(),secret=''::bytea WHERE agent_id=$1", a.ID); err != nil {
			return err
		}
		if err = audit(ctx, tx, a.ID, "agent_rotation_acked", a.ID); err != nil {
			return err
		}
	} else if err != pgx.ErrNoRows {
		return err
	} else if hash != current {
		return ErrFactor
	}
	if migration != "" {
		var target string
		if err = tx.QueryRow(ctx, "SELECT origin FROM control_migrations WHERE id=$1 AND expires_at>now()", migration).Scan(&target); err != nil {
			return err
		}
		if target != origin {
			return ErrBinding
		}
		if _, err = tx.Exec(ctx, "INSERT INTO agent_control_acks(agent_id,migration_id) VALUES($1,$2) ON CONFLICT(agent_id) DO UPDATE SET migration_id=$2,acked_at=now()", a.ID, migration); err != nil {
			return err
		}
		if err = audit(ctx, tx, a.ID, "control_migration_acked", migration); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
