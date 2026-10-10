package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS
var ErrOwnerExists = errors.New("owner already initialized")

type Store struct {
	DB           *pgxpool.Pool
	routingCache decisionCache
}
type Owner struct {
	ID, Email, PasswordHash string
	MFAEnabled              bool
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_URL")
	}
	cfg.MaxConns = 10
	cfg.MinConns = 0
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	s := &Store{}
	s.DB, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot create database pool")
	}
	if err = s.DB.Ping(ctx); err != nil {
		s.DB.Close()
		return nil, fmt.Errorf("database unavailable")
	}
	return s, nil
}
func (s *Store) Close() { s.DB.Close() }
func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(718204001)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		data, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		h := sha256.Sum256(data)
		checksum := hex.EncodeToString(h[:])
		var existing string
		err = tx.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=$1", entry.Name()).Scan(&existing)
		if err == nil {
			if checksum != existing {
				return fmt.Errorf("migration checksum changed: %s", entry.Name())
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err = tx.Exec(ctx, string(data)); err != nil {
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)", entry.Name(), checksum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) Bootstrap(ctx context.Context, id, email, hash string) error {
	// The singleton constraint enforces the one-owner invariant across processes.
	tag, err := s.DB.Exec(ctx, "INSERT INTO owners(id,email,password_hash) VALUES($1,$2,$3) ON CONFLICT(singleton) DO NOTHING", id, email, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOwnerExists
	}
	return nil
}
func (s *Store) OwnerByEmail(ctx context.Context, email string) (Owner, error) {
	var o Owner
	err := s.DB.QueryRow(ctx, "SELECT id,email,password_hash,mfa_enabled FROM owners WHERE email=$1", email).Scan(&o.ID, &o.Email, &o.PasswordHash, &o.MFAEnabled)
	return o, err
}
func (s *Store) CreateSession(ctx context.Context, hash, id string, ttl time.Duration) error {
	_, err := s.DB.Exec(ctx, "INSERT INTO sessions(token_hash,owner_id,expires_at) VALUES($1,$2,now()+$3::interval)", hash, id, fmt.Sprintf("%f seconds", ttl.Seconds()))
	return err
}
func (s *Store) Session(ctx context.Context, hash string) (Owner, error) {
	var o Owner
	err := s.DB.QueryRow(ctx, `SELECT o.id,o.email,o.mfa_enabled FROM sessions s JOIN owners o ON o.id=s.owner_id WHERE s.token_hash=$1 AND s.expires_at>now() AND (NOT o.mfa_enabled OR s.mfa_verified)`, hash).Scan(&o.ID, &o.Email, &o.MFAEnabled)
	return o, err
}
func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.DB.Exec(ctx, "DELETE FROM sessions WHERE token_hash=$1", hash)
	return err
}
func (s *Store) AllowLogin(ctx context.Context, key string, limit int) (bool, error) {
	var n int
	err := s.DB.QueryRow(ctx, `INSERT INTO auth_buckets(key_hash,window_started,attempts) VALUES($1,now(),1)
 ON CONFLICT(key_hash) DO UPDATE SET
 attempts=CASE WHEN auth_buckets.window_started<=now()-interval '15 minutes' THEN 1 ELSE auth_buckets.attempts+1 END,
 window_started=CASE WHEN auth_buckets.window_started<=now()-interval '15 minutes' THEN now() ELSE auth_buckets.window_started END
 RETURNING attempts`, key).Scan(&n)
	return n <= limit, err
}
func (s *Store) Ready(ctx context.Context) error {
	var ok bool
	if err := s.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM owners) AND EXISTS(SELECT 1 FROM schema_migrations WHERE version='010_security.sql')").Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return errors.New("bootstrap or migration incomplete")
	}
	return nil
}
func (s *Store) RotateSession(ctx context.Context, hash, id, oldHash string, ttl time.Duration) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if err = tx.QueryRow(ctx, "SELECT mfa_enabled FROM owners WHERE id=$1 FOR UPDATE", id).Scan(&enabled); err != nil {
		return err
	}
	if enabled {
		return ErrMFARequired
	}
	if _, err = tx.Exec(ctx, "INSERT INTO sessions(token_hash,owner_id,expires_at) VALUES($1,$2,now()+$3::interval)", hash, id, fmt.Sprintf("%f seconds", ttl.Seconds())); err != nil {
		return err
	}
	if oldHash != "" {
		if _, err = tx.Exec(ctx, "DELETE FROM sessions WHERE token_hash=$1", oldHash); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) Cleanup(ctx context.Context) error {
	if _, err := s.DB.Exec(ctx, "DELETE FROM mfa_challenges WHERE expires_at<=now(); DELETE FROM mfa_pending WHERE expires_at<=now()"); err != nil {
		return err
	}
	_, err := s.DB.Exec(ctx, "DELETE FROM sessions WHERE expires_at<=now()")
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, "DELETE FROM auth_buckets WHERE window_started<=now()-interval '30 minutes'")
	return err
}
