package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"shortlink.local/control/internal/security"
	"shortlink.local/policy"
	"time"
)

var ErrPolicy = errors.New("invalid policy")
var ErrCapability = errors.New("capability unavailable")

type RuleSet struct {
	ID                string        `json:"id"`
	Scope             string        `json:"scope"`
	ScopeID           *string       `json:"scopeId"`
	Revision          int64         `json:"revision"`
	PublishedRevision *int64        `json:"publishedRevision"`
	State             string        `json:"state"`
	Rules             []policy.Rule `json:"rules"`
}

const ruleColumns = "id,scope,scope_id,revision,published_revision,draft,draft=published"

func scanRule(row pgx.Row) (RuleSet, error) {
	var r RuleSet
	var raw []byte
	var same bool
	err := row.Scan(&r.ID, &r.Scope, &r.ScopeID, &r.Revision, &r.PublishedRevision, &raw, &same)
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(raw, &r.Rules)
	r.State = "draft"
	if r.PublishedRevision != nil && same {
		r.State = "published"
	}
	return r, err
}
func (s *Store) RuleSet(ctx context.Context, id string) (RuleSet, error) {
	return scanRule(s.DB.QueryRow(ctx, "SELECT "+ruleColumns+" FROM rule_sets WHERE id=$1", id))
}
func (s *Store) RuleSets(ctx context.Context, after string, limit int) ([]RuleSet, error) {
	rows, err := s.DB.Query(ctx, "SELECT "+ruleColumns+" FROM rule_sets WHERE id>$1 ORDER BY id LIMIT $2", after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RuleSet{}
	for rows.Next() {
		r, e := scanRule(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) CreateRuleSet(ctx context.Context, scope string, binding *string, actor string) (RuleSet, error) {
	if scope != "global" && scope != "domain" && scope != "link" || (scope == "global") != (binding == nil) {
		return RuleSet{}, ErrPolicy
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return RuleSet{}, err
	}
	defer tx.Rollback(ctx)
	if err = publishLock(ctx, tx); err != nil {
		return RuleSet{}, err
	}
	var domain, link *string
	if scope == "domain" {
		var id string
		if err = tx.QueryRow(ctx, "SELECT id FROM domains WHERE id=$1", binding).Scan(&id); err != nil {
			return RuleSet{}, err
		}
		domain = &id
	}
	if scope == "link" {
		var d string
		if err = tx.QueryRow(ctx, "SELECT domain_id FROM links WHERE id=$1", binding).Scan(&d); err != nil {
			return RuleSet{}, err
		}
		domain = &d
		link = binding
	}
	r, err := scanRule(tx.QueryRow(ctx, "INSERT INTO rule_sets(id,scope,scope_id,domain_id,link_id) VALUES($1,$2,$3,$4,$5) RETURNING "+ruleColumns, security.Token(), scope, binding, domain, link))
	if err != nil {
		return r, resourceError(err)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_logs(actor_id,action,domain_id) VALUES($1,'rule_set_created',$2)", actor, domain); err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}
func (s *Store) UpdateRuleSet(ctx context.Context, id string, expected int64, rules []policy.Rule, actor string) (RuleSet, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return RuleSet{}, err
	}
	defer tx.Rollback(ctx)
	old, err := scanRule(tx.QueryRow(ctx, "SELECT "+ruleColumns+" FROM rule_sets WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return old, err
	}
	if old.Revision != expected {
		return old, ErrConflict
	}
	rules = policy.Normalize(rules)
	if err = policy.Validate(old.Scope, rules); err != nil {
		return old, fmt.Errorf("%w: %s", ErrPolicy, err)
	}
	raw, _ := json.Marshal(rules)
	r, err := scanRule(tx.QueryRow(ctx, "UPDATE rule_sets SET draft=$2,revision=revision+1 WHERE id=$1 RETURNING "+ruleColumns, id, raw))
	if err != nil {
		return r, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_logs(actor_id,action,domain_id) SELECT $1,'rule_draft_updated',domain_id FROM rule_sets WHERE id=$2", actor, id); err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}

type DomainRevision struct {
	DomainID string `json:"domainId"`
	Revision int64  `json:"revision"`
}
type PublishResult struct {
	Revision        int64            `json:"revision"`
	DomainRevisions []DomainRevision `json:"domainRevisions"`
	PendingAgentIDs []string         `json:"pendingAgentIds"`
	PublishedAt     time.Time        `json:"publishedAt"`
}
type History struct {
	Revision    int64     `json:"revision"`
	PublishedAt time.Time `json:"publishedAt"`
}

func (s *Store) History(ctx context.Context, id string, link bool) ([]History, error) {
	table, key := "rule_history", "rule_set_id"
	if link {
		table, key = "link_history", "link_id"
	}
	rows, err := s.DB.Query(ctx, "SELECT revision,published_at FROM "+table+" WHERE "+key+"=$1 ORDER BY revision DESC LIMIT 100", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []History{}
	for rows.Next() {
		var h History
		if err = rows.Scan(&h.Revision, &h.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Publishing uses one transaction lock, then domains in ID order. Enroll and renew lock domains too.
func publishLock(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(718204008)")
	return err
}
func affected(ctx context.Context, tx pgx.Tx, scope string, binding *string) ([]string, error) {
	rows, err := tx.Query(ctx, "SELECT id FROM domains WHERE ($1='global' OR ($1='domain' AND id=$2) OR ($1='link' AND id=(SELECT domain_id FROM links WHERE id=$2))) ORDER BY id FOR UPDATE", scope, binding)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func refresh(ctx context.Context, tx pgx.Tx, ids []string, seed []byte, result *PublishResult) error {
	for _, id := range ids {
		var revision int64
		if err := tx.QueryRow(ctx, "UPDATE domains SET desired_revision=desired_revision+1 WHERE id=$1 RETURNING desired_revision", id).Scan(&revision); err != nil {
			return err
		}
		result.DomainRevisions = append(result.DomainRevisions, DomainRevision{id, revision})
		// Validate every local domain, including domains awaiting enrollment.
		var mode string
		if err := tx.QueryRow(ctx, "SELECT mode FROM domains WHERE id=$1", id).Scan(&mode); err != nil {
			return err
		}
		if mode == "local" || mode == "api" {
			if err := saveSnapshot(ctx, tx, id, seed); err != nil {
				return fmt.Errorf("%w: %s", ErrPolicy, err)
			}
		}
		var agent, method string
		err := tx.QueryRow(ctx, "SELECT id,method FROM agents WHERE domain_id=$1 AND revoked_at IS NULL", id).Scan(&agent, &method)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if method != "cli" && method != "php_zip" {
			return ErrCapability
		}
		if method == "cli" {
			result.PendingAgentIDs = append(result.PendingAgentIDs, agent)
		}
	}
	return nil
}
func (s *Store) PublishRules(ctx context.Context, id string, expected, target int64, actor string) (PublishResult, error) {
	result := PublishResult{DomainRevisions: []DomainRevision{}, PendingAgentIDs: []string{}, PublishedAt: time.Now().UTC()}
	seed, err := s.Seed(ctx)
	if err != nil {
		return result, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if err = publishLock(ctx, tx); err != nil {
		return result, err
	}
	// Read binding before domain locks; lock rule set afterward, allowing draft editors to finish.
	var scope string
	var binding *string
	if err = tx.QueryRow(ctx, "SELECT scope,scope_id FROM rule_sets WHERE id=$1", id).Scan(&scope, &binding); err != nil {
		return result, err
	}
	ids, err := affected(ctx, tx, scope, binding)
	if err != nil {
		return result, err
	}
	r, err := scanRule(tx.QueryRow(ctx, "SELECT "+ruleColumns+" FROM rule_sets WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return result, err
	}
	if r.Revision != expected {
		return result, ErrConflict
	}
	rules := r.Rules
	if target > 0 {
		var raw []byte
		if err = tx.QueryRow(ctx, "SELECT rules FROM rule_history WHERE rule_set_id=$1 AND revision=$2", id, target).Scan(&raw); err != nil {
			return result, err
		}
		rules = nil
		if err = json.Unmarshal(raw, &rules); err != nil {
			return result, err
		}
	}
	if err = policy.Validate(scope, rules); err != nil {
		return result, ErrPolicy
	}
	for _, rule := range rules {
		if rule.Enabled && rule.Risk != "*" {
			for _, domain := range ids {
				if err = protectionCapability(ctx, tx, domain); err != nil {
					return result, err
				}
			}
		}
		if rule.DestinationSource == "custom" {
			rows, e := tx.Query(ctx, "SELECT hostname FROM domains")
			if e != nil {
				return result, e
			}
			for rows.Next() {
				var host string
				if e = rows.Scan(&host); e != nil {
					rows.Close()
					return result, e
				}
				if !policy.URL(rule.Destination, host) {
					rows.Close()
					return result, ErrPolicy
				}
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return result, e
			}
		}
	}
	raw, _ := json.Marshal(rules)
	result.Revision = r.Revision + 1
	if _, err = tx.Exec(ctx, "UPDATE rule_sets SET revision=$2,published_revision=$2,draft=$3,published=$3 WHERE id=$1", id, result.Revision, raw); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO rule_history(rule_set_id,revision,rules) VALUES($1,$2,$3)", id, result.Revision, raw); err != nil {
		return result, err
	}
	if err = refresh(ctx, tx, ids, seed, &result); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_logs(actor_id,action,domain_id) SELECT $1,$3,domain_id FROM rule_sets WHERE id=$2", actor, id, map[bool]string{true: "rules_rolled_back", false: "rules_published"}[target > 0]); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func (s *Store) PublishLink(ctx context.Context, id string, expected, target int64, actor string) (PublishResult, error) {
	result := PublishResult{DomainRevisions: []DomainRevision{}, PendingAgentIDs: []string{}, PublishedAt: time.Now().UTC()}
	seed, err := s.Seed(ctx)
	if err != nil {
		return result, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if err = publishLock(ctx, tx); err != nil {
		return result, err
	}
	var binding *string
	binding = &id
	ids, err := affected(ctx, tx, "link", binding)
	if err != nil {
		return result, err
	}
	l, err := scanLink(tx.QueryRow(ctx, "SELECT "+linkColumns+" FROM links WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return result, err
	}
	if l.Revision != expected {
		return result, ErrConflict
	}
	var allowed bool
	if err = tx.QueryRow(ctx, "SELECT verified_at IS NOT NULL AND EXISTS(SELECT 1 FROM agents WHERE domain_id=$1 AND ((mode='local' AND method='cli') OR (mode='api' AND method='php_zip')) AND revoked_at IS NULL) FROM domains WHERE id=$1", l.DomainID).Scan(&allowed); err != nil {
		return result, err
	}
	if !allowed {
		return result, ErrPrerequisite
	}
	routing := l.Routing
	if l.DraftRouting != nil {
		routing = l.DraftRouting
	}
	destination := l.Destination
	if l.DraftDestination != nil {
		destination = *l.DraftDestination
	}
	if target > 0 {
		if err = tx.QueryRow(ctx, "SELECT destination,routing FROM link_history WHERE link_id=$1 AND revision=$2", id, target).Scan(&destination, &routing); err != nil {
			return result, err
		}
	}
	var host string
	if err = tx.QueryRow(ctx, "SELECT hostname FROM domains WHERE id=$1", l.DomainID).Scan(&host); err != nil {
		return result, err
	}
	if !policy.URL(destination, host) || policy.ValidateRouting(routing, host) != nil {
		return result, ErrPolicy
	}
	if routing != nil {
		var compatible bool
		if err = tx.QueryRow(ctx, "SELECT mode='api' OR EXISTS(SELECT 1 FROM agents WHERE domain_id=$1 AND revoked_at IS NULL AND routing_version >= $2) FROM domains WHERE id=$1", l.DomainID, requiredRoutingVersion(routing)).Scan(&compatible); err != nil {
			return result, err
		}
		if !compatible {
			return result, ErrRoutingCapability
		}
	}
	result.Revision = l.Revision + 1
	if _, err = tx.Exec(ctx, "UPDATE links SET state='published',destination=$2,draft_destination=NULL,routing=$4,draft_routing=NULL,revision=$3 WHERE id=$1", id, destination, result.Revision, routing); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO link_history(link_id,revision,destination,routing) VALUES($1,$2,$3,$4)", id, result.Revision, destination, routing); err != nil {
		return result, err
	}
	if err = refresh(ctx, tx, ids, seed, &result); err != nil {
		return result, err
	}
	action := "link_published"
	if target > 0 {
		action = "link_rolled_back"
	}
	if err = audit(ctx, tx, actor, action, l.DomainID); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func effective(ctx context.Context, tx pgx.Tx, domain string, preview *RuleSet) ([]policy.ScopedRule, error) {
	rows, err := tx.Query(ctx, "SELECT id,scope,scope_id,published FROM rule_sets WHERE scope='global' OR domain_id=$1 ORDER BY id", domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []policy.ScopedRule{}
	for rows.Next() {
		var id, scope string
		var binding *string
		var raw []byte
		if err = rows.Scan(&id, &scope, &binding, &raw); err != nil {
			return nil, err
		}
		var rules []policy.Rule
		if preview != nil && preview.ID == id {
			rules = preview.Rules
		} else if err = json.Unmarshal(raw, &rules); err != nil {
			return nil, err
		}
		for _, r := range rules {
			out = append(out, policy.ScopedRule{Rule: r, Scope: scope, ScopeID: binding})
		}
	}
	return out, rows.Err()
}
func (s *Store) Simulate(ctx context.Context, link string, signals policy.Signals, previewID string, expected *int64) (policy.Decision, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return policy.Decision{}, err
	}
	defer tx.Rollback(ctx)
	l, err := scanLink(tx.QueryRow(ctx, "SELECT "+linkColumns+" FROM links WHERE id=$1", link))
	if err != nil {
		return policy.Decision{}, err
	}
	d, err := scanDomain(tx.QueryRow(ctx, "SELECT "+domainColumns+" FROM domains WHERE id=$1", l.DomainID))
	if err != nil {
		return policy.Decision{}, err
	}
	var preview *RuleSet
	if previewID != "" {
		r, e := scanRule(tx.QueryRow(ctx, "SELECT "+ruleColumns+" FROM rule_sets WHERE id=$1", previewID))
		if e != nil {
			return policy.Decision{}, e
		}
		if expected == nil || *expected != r.Revision {
			return policy.Decision{}, ErrConflict
		}
		if r.Scope == "domain" && *r.ScopeID != d.ID || r.Scope == "link" && *r.ScopeID != link {
			return policy.Decision{}, ErrBinding
		}
		preview = &r
	}
	rules, err := effective(ctx, tx, d.ID, preview)
	if err != nil {
		return policy.Decision{}, err
	}
	// Restrict to the selected link; unrelated link rules must not affect this simulation.
	filtered := []policy.ScopedRule{}
	for _, r := range rules {
		if r.Scope != "link" || *r.ScopeID == link {
			filtered = append(filtered, r)
		}
	}
	engine, err := policy.Compile(filtered, d.ID, d.Hostname, map[string]bool{link: true})
	if err != nil {
		return policy.Decision{}, fmt.Errorf("%w: %s", ErrPolicy, err)
	}
	destination := l.Destination
	if preview != nil && l.DraftDestination != nil {
		destination = *l.DraftDestination
	}
	var protectionRaw []byte
	var protection policy.Protection
	if err = tx.QueryRow(ctx, "SELECT protection FROM domains WHERE id=$1", d.ID).Scan(&protectionRaw); err != nil {
		return policy.Decision{}, err
	}
	if err = json.Unmarshal(protectionRaw, &protection); err != nil {
		return policy.Decision{}, err
	}
	routing := l.Routing
	if preview != nil && l.DraftRouting != nil {
		routing = l.DraftRouting
	}
	result := engine.EvaluateRouting(link, destination, routing, signals, protection)
	result.EventID = security.Token()
	result.ConfigRevision = d.DesiredRevision
	return result, tx.Commit(ctx)
}
