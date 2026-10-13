package store

import "context"

// DeleteDomain removes configuration and credentials atomically. Historical traffic
// and audit records retain their original IDs and are not erased.
func (s *Store) DeleteDomain(ctx context.Context, id, hostname string, expected int64, actor string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = publishLock(ctx, tx); err != nil {
		return err
	}
	var host string
	var revision int64
	if err = tx.QueryRow(ctx, "SELECT hostname,desired_revision FROM domains WHERE id=$1 FOR UPDATE", id).Scan(&host, &revision); err != nil {
		return err
	}
	if host != hostname || revision != expected {
		return ErrConflict
	}
	statements := []string{
		"DELETE FROM agent_manage_sessions WHERE domain_id=$1",
		"DELETE FROM agent_manage_keys WHERE domain_id=$1",
		"DELETE FROM agent_rotations WHERE agent_id IN (SELECT id FROM agents WHERE domain_id=$1)",
		"DELETE FROM agent_control_acks WHERE agent_id IN (SELECT id FROM agents WHERE domain_id=$1)",
		"DELETE FROM api_decisions WHERE agent_id IN (SELECT id FROM agents WHERE domain_id=$1)",
		"DELETE FROM event_keys WHERE agent_id IN (SELECT id FROM agents WHERE domain_id=$1)",
		"DELETE FROM agents WHERE domain_id=$1",
		"DELETE FROM install_tokens WHERE domain_id=$1",
		"DELETE FROM snapshots WHERE domain_id=$1",
		"DELETE FROM verification_challenges WHERE domain_id=$1",
		"DELETE FROM rule_history WHERE rule_set_id IN (SELECT id FROM rule_sets WHERE domain_id=$1)",
		"DELETE FROM rule_sets WHERE domain_id=$1",
		"DELETE FROM link_history WHERE link_id IN (SELECT id FROM links WHERE domain_id=$1)",
		"DELETE FROM links WHERE domain_id=$1",
		"DELETE FROM domains WHERE id=$1",
	}
	for _, statement := range statements {
		if _, err = tx.Exec(ctx, statement, id); err != nil {
			return err
		}
	}
	if err = audit(ctx, tx, actor, "domain_deleted", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
