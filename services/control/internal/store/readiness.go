package store

import "context"

func (s *Store) RecordProbe(ctx context.Context, domain, agent string, revision int64, ready bool, result []byte, actor string) (Domain, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Domain{}, e
	}
	defer tx.Rollback(ctx)
	if e = publishLock(ctx, tx); e != nil {
		return Domain{}, e
	}
	d, e := scanDomain(tx.QueryRow(ctx, "SELECT "+domainColumns+" FROM domains WHERE id=$1 FOR UPDATE", domain))
	if e != nil {
		return d, e
	}
	if d.DesiredRevision != revision {
		return d, ErrConflict
	}
	var active bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agents WHERE id=$1 AND domain_id=$2 AND revoked_at IS NULL)", agent, domain).Scan(&active); e != nil {
		return d, e
	}
	if agent != "" && !active {
		return d, ErrConflict
	}
	state := d.Readiness
	if active {
		state = "attention"
		if ready && d.AppliedRevision != nil && *d.AppliedRevision == revision && (d.Connection == "online" || d.Mode == "api") {
			state = "ready"
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE domains SET readiness=$2,probe_checked_at=now(),probe_result=$3::jsonb WHERE id=$1", domain, state, string(result)); e != nil {
		return d, e
	}
	if e = audit(ctx, tx, actor, "readiness_probed", domain); e != nil {
		return d, e
	}
	d, e = scanDomain(tx.QueryRow(ctx, "SELECT "+domainColumns+" FROM domains WHERE id=$1", domain))
	if e != nil {
		return d, e
	}
	e = tx.Commit(ctx)
	return d, e
}
