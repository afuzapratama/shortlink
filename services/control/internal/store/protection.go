package store

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"shortlink.local/policy"
	"time"
)

type ProtectionStatus struct {
	Settings        policy.Protection `json:"settings"`
	Revision        int64             `json:"revision"`
	AppliedRevision *int64            `json:"appliedRevision"`
	Supported       bool              `json:"supported"`
	LastSeenAt      *time.Time        `json:"lastSeenAt"`
	FeedVersion     *string           `json:"feedVersion"`
}

func (s *Store) Protection(ctx context.Context, domain string) (ProtectionStatus, error) {
	var status ProtectionStatus
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT protection,desired_revision,applied_revision,last_seen_at,EXISTS(SELECT 1 FROM agents WHERE domain_id=$1 AND revoked_at IS NULL AND protection_version=1) OR mode='api',(SELECT feed_version FROM agents WHERE domain_id=$1 AND revoked_at IS NULL) FROM domains WHERE id=$1`, domain).Scan(&raw, &status.Revision, &status.AppliedRevision, &status.LastSeenAt, &status.Supported, &status.FeedVersion)
	if err != nil {
		return status, err
	}
	err = json.Unmarshal(raw, &status.Settings)
	return status, err
}
func protectionCapability(ctx context.Context, tx pgx.Tx, domain string) error {
	var supported bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM domains d WHERE d.id=$1 AND (d.mode='api' OR EXISTS(SELECT 1 FROM agents a WHERE a.domain_id=d.id AND a.revoked_at IS NULL AND a.method='cli' AND a.protection_version=1 AND d.last_seen_at>now()-interval '90 seconds')))`, domain).Scan(&supported); err != nil {
		return err
	}
	if !supported {
		return ErrCapability
	}
	return nil
}
func (s *Store) SetProtection(ctx context.Context, domain string, expected int64, p policy.Protection, actor string) (ProtectionStatus, error) {
	if err := policy.ValidateProtection(p); err != nil {
		return ProtectionStatus{}, ErrPolicy
	}
	seed, err := s.Seed(ctx)
	if err != nil {
		return ProtectionStatus{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return ProtectionStatus{}, err
	}
	defer tx.Rollback(ctx)
	if err = publishLock(ctx, tx); err != nil {
		return ProtectionStatus{}, err
	}
	var revision int64
	var mode string
	if err = tx.QueryRow(ctx, "SELECT desired_revision,mode FROM domains WHERE id=$1 FOR UPDATE", domain).Scan(&revision, &mode); err != nil {
		return ProtectionStatus{}, err
	}
	if revision != expected {
		return ProtectionStatus{}, ErrConflict
	}
	if mode != "local" && mode != "api" {
		return ProtectionStatus{}, ErrCapability
	}
	if p.Mode == "enforce" {
		if err = protectionCapability(ctx, tx, domain); err != nil {
			return ProtectionStatus{}, err
		}
	}
	raw, _ := json.Marshal(p)
	if _, err = tx.Exec(ctx, "UPDATE domains SET protection=$2,desired_revision=desired_revision+1 WHERE id=$1", domain, raw); err != nil {
		return ProtectionStatus{}, err
	}
	if err = saveSnapshot(ctx, tx, domain, seed); err != nil {
		return ProtectionStatus{}, err
	}
	if err = audit(ctx, tx, actor, "protection_published", domain); err != nil {
		return ProtectionStatus{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ProtectionStatus{}, err
	}
	return s.Protection(ctx, domain)
}

type RiskSummary struct {
	Signal          string `json:"signal"`
	Mode            string `json:"mode"`
	CandidateAction string `json:"candidateAction"`
	Reason          string `json:"reasonCode"`
	RuleID          string `json:"ruleId"`
	Events          int64  `json:"events"`
}

func (s *Store) RiskSummary(ctx context.Context, domain string) ([]RiskSummary, error) {
	if _, err := s.Domain(ctx, domain); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT signal,mode,candidate_action,candidate_reason,candidate_rule_id,sum(events) FROM risk_observations WHERE domain_id=$1 AND bucket>now()-interval '24 hours' GROUP BY 1,2,3,4,5 ORDER BY 6 DESC LIMIT 100`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RiskSummary{}
	for rows.Next() {
		var item RiskSummary
		if err = rows.Scan(&item.Signal, &item.Mode, &item.CandidateAction, &item.Reason, &item.RuleID, &item.Events); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
