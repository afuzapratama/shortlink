package store

import (
	"context"
	"errors"
	"shortlink.local/control/internal/security"
	"shortlink.local/policy"
	"strings"
	"time"
)

var ErrManageAccess = errors.New("agent manage access invalid or expired")

type ManageKey struct {
	Key       string    `json:"key"`
	ExpiresAt time.Time `json:"expiresAt"`
	URL       string    `json:"url"`
}
type ManageSession struct {
	Session   string    `json:"session"`
	CSRF      string    `json:"csrf"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *Store) UpdateOpening(ctx context.Context, domain, target string, expected int64, actor string, manageSession ...string) (Domain, error) {
	seed, err := s.Seed(ctx)
	if err != nil {
		return Domain{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Domain{}, err
	}
	defer tx.Rollback(ctx)
	if err = publishLock(ctx, tx); err != nil {
		return Domain{}, err
	}
	d, err := scanDomain(tx.QueryRow(ctx, "SELECT "+domainColumns+" FROM domains WHERE id=$1 FOR UPDATE", domain))
	if err != nil {
		return d, err
	}
	if len(manageSession) > 0 {
		var permitted bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agent_manage_sessions m JOIN agents a ON a.id=m.agent_id WHERE m.session_hash=$1 AND m.agent_id=$2 AND m.domain_id=$3 AND m.expires_at>now() AND a.revoked_at IS NULL)", security.Digest(manageSession[0]), actor, domain).Scan(&permitted); err != nil {
			return d, err
		}
		if !permitted {
			return d, ErrManageAccess
		}
	}
	if d.DesiredRevision != expected {
		return d, ErrConflict
	}
	if target != "" && (!policy.URL(target, d.Hostname) || strings.ContainsAny(target, " \t\r\n")) {
		return d, ErrPolicy
	}
	if d.Mode == "local" && target != "" {
		var incompatible bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agents WHERE domain_id=$1 AND revoked_at IS NULL AND routing_version<3)", domain).Scan(&incompatible); err != nil {
			return d, err
		}
		if incompatible {
			return d, ErrRoutingCapability
		}
	}
	d, err = scanDomain(tx.QueryRow(ctx, "UPDATE domains SET opening_url=$2,desired_revision=desired_revision+1 WHERE id=$1 RETURNING "+domainColumns, domain, target))
	if err != nil {
		return d, err
	}
	if err = saveSnapshot(ctx, tx, domain, seed); err != nil {
		return d, err
	}
	if err = audit(ctx, tx, actor, "domain_opening_updated", domain); err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}
func (s *Store) IssueManageKey(ctx context.Context, domain, actor string) (ManageKey, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return ManageKey{}, err
	}
	defer tx.Rollback(ctx)
	d, err := scanDomain(tx.QueryRow(ctx, "SELECT "+domainColumns+" FROM domains WHERE id=$1 FOR UPDATE", domain))
	if err != nil {
		return ManageKey{}, err
	}
	var agent string
	if err = tx.QueryRow(ctx, "SELECT id FROM agents WHERE domain_id=$1 AND revoked_at IS NULL", domain).Scan(&agent); err != nil {
		return ManageKey{}, err
	}
	key := ManageKey{Key: security.Token(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute), URL: "https://" + d.Hostname + "/__agent/manage"}
	if _, err = tx.Exec(ctx, "DELETE FROM agent_manage_keys WHERE domain_id=$1 OR expires_at<now()", domain); err != nil {
		return key, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO agent_manage_keys(key_hash,agent_id,domain_id,expires_at) VALUES($1,$2,$3,$4)", security.Digest(key.Key), agent, domain, key.ExpiresAt); err != nil {
		return key, err
	}
	if err = audit(ctx, tx, actor, "agent_manage_key_created", domain); err != nil {
		return key, err
	}
	return key, tx.Commit(ctx)
}
func (s *Store) ExchangeManageKey(ctx context.Context, a Agent, key string) (ManageSession, error) {
	if !security.ValidToken(key) {
		return ManageSession{}, ErrManageAccess
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return ManageSession{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, "UPDATE agent_manage_keys SET used_at=now() WHERE key_hash=$1 AND agent_id=$2 AND domain_id=$3 AND used_at IS NULL AND expires_at>now()", security.Digest(key), a.ID, a.DomainID)
	if err != nil {
		return ManageSession{}, err
	}
	if tag.RowsAffected() != 1 {
		return ManageSession{}, ErrManageAccess
	}
	session := ManageSession{Session: security.Token(), ExpiresAt: time.Now().UTC().Add(15 * time.Minute)}
	session.CSRF = security.CSRF(session.Session)
	if _, err = tx.Exec(ctx, "DELETE FROM agent_manage_sessions WHERE expires_at<now()"); err != nil {
		return session, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO agent_manage_sessions(session_hash,agent_id,domain_id,expires_at) VALUES($1,$2,$3,$4)", security.Digest(session.Session), a.ID, a.DomainID, session.ExpiresAt); err != nil {
		return session, err
	}
	if err = audit(ctx, tx, a.ID, "agent_manage_login", a.DomainID); err != nil {
		return session, err
	}
	return session, tx.Commit(ctx)
}
func (s *Store) AuthorizeManage(ctx context.Context, a Agent, session string) error {
	if !security.ValidToken(session) {
		return ErrManageAccess
	}
	var active bool
	err := s.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agent_manage_sessions WHERE session_hash=$1 AND agent_id=$2 AND domain_id=$3 AND expires_at>now())", security.Digest(session), a.ID, a.DomainID).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return ErrManageAccess
	}
	return nil
}
func (s *Store) LogoutManage(ctx context.Context, a Agent, session string) error {
	_, err := s.DB.Exec(ctx, "DELETE FROM agent_manage_sessions WHERE session_hash=$1 AND agent_id=$2", security.Digest(session), a.ID)
	return err
}
func (s *Store) Opening(ctx context.Context, a Agent, slug string) (string, bool, error) {
	d, err := s.Domain(ctx, a.DomainID)
	if err != nil {
		return "", false, err
	}
	if d.OpeningURL != "" && !policy.URL(d.OpeningURL, d.Hostname) {
		return "", false, ErrPolicy
	}
	var published bool
	if slug != "" {
		err = s.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM links WHERE domain_id=$1 AND slug=$2 AND state='published' AND NOT disabled AND deleted_at IS NULL)", a.DomainID, slug).Scan(&published)
	}
	return d.OpeningURL, published, err
}
