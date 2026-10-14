package store

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"shortlink.local/control/internal/security"
	"shortlink.local/policy"
)

var ErrPrerequisite = errors.New("domain verification required")
var ErrExpired = errors.New("enrollment expired")
var ErrUsed = errors.New("enrollment used")
var ErrBinding = errors.New("domain or method mismatch")

// InstallContext resolves the server-bound installation ID without consuming a token or issuing a credential.
func (s *Store) InstallContext(ctx context.Context, token string) (string, Domain, error) {
	var installation, domain, method string
	var expires time.Time
	var used *time.Time
	var revoked bool
	err := s.DB.QueryRow(ctx, "SELECT installation_id,domain_id,method,expires_at,used_at,revoked FROM install_tokens WHERE token_hash=$1", security.Digest(token)).Scan(&installation, &domain, &method, &expires, &used, &revoked)
	if err != nil {
		return "", Domain{}, err
	}
	if revoked {
		return "", Domain{}, ErrUsed
	}
	if method != "cli" && method != "php_zip" {
		return "", Domain{}, ErrBinding
	}
	if used != nil {
		if time.Now().After(used.Add(10 * time.Minute)) {
			return "", Domain{}, ErrUsed
		}
	} else if time.Now().After(expires) {
		return "", Domain{}, ErrExpired
	}
	d, err := s.Domain(ctx, domain)
	return installation, d, err
}

type Challenge struct {
	Name      string    `json:"name"`
	Value     string    `json:"value"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type InstallToken struct {
	Token           string    `json:"token"`
	ExpiresAt       time.Time `json:"expiresAt"`
	DomainID        string    `json:"domainId"`
	ControlPlaneURL string    `json:"controlPlaneUrl"`
	InstallationID  string    `json:"installationId"`
}
type Agent struct{ ID, DomainID, Method, Version string }
type Snapshot struct {
	Revision       int64               `json:"revision"`
	LeaseExpiresAt time.Time           `json:"leaseExpiresAt"`
	Domain         Domain              `json:"domain"`
	Links          []Link              `json:"links"`
	Rules          []policy.ScopedRule `json:"rules"`
	GeoRelease     *string             `json:"geoRelease"`
	FeedVersion    *string             `json:"feedVersion"`
	Protection     policy.Protection   `json:"protection"`
}

func (s *Store) Seed(ctx context.Context) ([]byte, error) {
	candidate, _ := base64.RawURLEncoding.DecodeString(security.Token())
	if _, err := s.DB.Exec(ctx, "INSERT INTO control_keys(seed) VALUES($1) ON CONFLICT(singleton) DO NOTHING", candidate); err != nil {
		return nil, err
	}
	var seed []byte
	err := s.DB.QueryRow(ctx, "SELECT seed FROM control_keys").Scan(&seed)
	return seed, err
}
func audit(ctx context.Context, tx pgx.Tx, actor, action, domain string) error {
	_, err := tx.Exec(ctx, "INSERT INTO audit_logs(actor_id,action,domain_id) VALUES($1,$2,$3)", actor, action, domain)
	return err
}
func (s *Store) Challenge(ctx context.Context, domain, actor string) (Challenge, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Challenge{}, err
	}
	defer tx.Rollback(ctx)
	d, err := scanDomain(tx.QueryRow(ctx, "SELECT "+domainColumns+" FROM domains WHERE id=$1 FOR UPDATE", domain))
	if err != nil {
		return Challenge{}, err
	}
	c := Challenge{Name: "_shortlink." + d.Hostname}
	err = tx.QueryRow(ctx, "SELECT value,expires_at FROM verification_challenges WHERE domain_id=$1 AND expires_at>now()", domain).Scan(&c.Value, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		c.Value = "shortlink-verification=" + security.Token()
		c.ExpiresAt = time.Now().UTC().Add(24 * time.Hour)
		if _, err = tx.Exec(ctx, "INSERT INTO verification_challenges(domain_id,value,expires_at) VALUES($1,$2,$3) ON CONFLICT(domain_id) DO UPDATE SET value=excluded.value,expires_at=excluded.expires_at", domain, c.Value, c.ExpiresAt); err != nil {
			return c, err
		}
		if _, err = tx.Exec(ctx, "UPDATE domains SET readiness=CASE WHEN verified_at IS NULL THEN 'verification_pending' ELSE readiness END WHERE id=$1", domain); err != nil {
			return c, err
		}
		if err = audit(ctx, tx, actor, "verification_challenge", domain); err != nil {
			return c, err
		}
	} else if err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (s *Store) Verify(ctx context.Context, domain, value, actor string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var matched bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM verification_challenges WHERE domain_id=$1 AND value=$2 AND expires_at>now())", domain, value).Scan(&matched)
	if err != nil {
		return err
	}
	if !matched {
		return ErrExpired
	}
	if _, err = tx.Exec(ctx, "UPDATE domains SET verified_at=now(),readiness=CASE WHEN readiness IN ('draft','verification_pending') THEN 'installation_pending' ELSE readiness END WHERE id=$1", domain); err != nil {
		return err
	}
	if err = audit(ctx, tx, actor, "domain_verified", domain); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) IssueToken(ctx context.Context, domain, method, actor string) (InstallToken, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return InstallToken{}, err
	}
	defer tx.Rollback(ctx)
	d, err := scanDomain(tx.QueryRow(ctx, "SELECT "+domainColumns+" FROM domains WHERE id=$1 FOR UPDATE", domain))
	if err != nil {
		return InstallToken{}, err
	}
	if (d.Mode == "local" && method != "cli") || (d.Mode == "api" && method != "php_zip") {
		return InstallToken{}, ErrBinding
	}
	var allowed bool
	if err = tx.QueryRow(ctx, "SELECT verified_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM agents WHERE domain_id=$1 AND revoked_at IS NULL) FROM domains WHERE id=$1", domain).Scan(&allowed); err != nil {
		return InstallToken{}, err
	}
	if !allowed {
		return InstallToken{}, ErrPrerequisite
	}
	t := InstallToken{Token: security.Token(), InstallationID: security.Token(), DomainID: domain, ExpiresAt: time.Now().UTC().Add(30 * time.Minute)}
	if _, err = tx.Exec(ctx, "UPDATE install_tokens SET revoked=true WHERE domain_id=$1 AND used_at IS NULL", domain); err != nil {
		return t, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO install_tokens(token_hash,domain_id,installation_id,method,expires_at) VALUES($1,$2,$3,$4,$5)", security.Digest(t.Token), domain, t.InstallationID, method, t.ExpiresAt); err != nil {
		return t, err
	}
	if err = audit(ctx, tx, actor, "install_token_created", domain); err != nil {
		return t, err
	}
	return t, tx.Commit(ctx)
}
func credential(seed []byte, tokenHash, installation, proofHash string) string {
	h := hmac.New(sha256.New, seed)
	fmt.Fprintf(h, "shortlink-agent-v1:%s:%s:%s", tokenHash, installation, proofHash)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (s *Store) Enroll(ctx context.Context, token, installation, proof, hostname, method, version string) (Agent, string, error) {
	return s.EnrollRouting(ctx, token, installation, proof, hostname, method, version, 0)
}
func (s *Store) EnrollRouting(ctx context.Context, token, installation, proof, hostname, method, version string, routingVersion int) (Agent, string, error) {
	seed, err := s.Seed(ctx)
	if err != nil {
		return Agent{}, "", err
	}
	hash := security.Digest(token)
	var domain string
	err = s.DB.QueryRow(ctx, "SELECT domain_id FROM install_tokens WHERE token_hash=$1", hash).Scan(&domain)
	if err != nil {
		return Agent{}, "", err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Agent{}, "", err
	}
	defer tx.Rollback(ctx)
	if err = publishLock(ctx, tx); err != nil {
		return Agent{}, "", err
	}
	d, err := scanDomain(tx.QueryRow(ctx, "SELECT "+domainColumns+" FROM domains WHERE id=$1 FOR UPDATE", domain))
	if err != nil {
		return Agent{}, "", err
	}
	var boundInstallation, boundMethod string
	var expires time.Time
	var used *time.Time
	var storedProof, agentID *string
	var revoked bool
	err = tx.QueryRow(ctx, "SELECT installation_id,method,expires_at,used_at,proof_hash,agent_id,revoked FROM install_tokens WHERE token_hash=$1 FOR UPDATE", hash).Scan(&boundInstallation, &boundMethod, &expires, &used, &storedProof, &agentID, &revoked)
	if err != nil {
		return Agent{}, "", err
	}
	if revoked {
		return Agent{}, "", ErrUsed
	}
	if hostname != d.Hostname || method != boundMethod || installation != boundInstallation {
		return Agent{}, "", ErrBinding
	}
	proofHash := security.Digest(proof)
	secret := credential(seed, hash, installation, proofHash)
	if used != nil {
		if storedProof == nil || !security.Equal(*storedProof, proofHash) || time.Now().After(used.Add(10*time.Minute)) {
			return Agent{}, "", ErrUsed
		}
		a := Agent{}
		err = tx.QueryRow(ctx, "SELECT id,domain_id,method,version FROM agents WHERE id=$1 AND revoked_at IS NULL AND credential_hash=$2", agentID, security.Digest(secret)).Scan(&a.ID, &a.DomainID, &a.Method, &a.Version)
		if err != nil {
			return a, "", ErrUsed
		}
		return a, secret, tx.Commit(ctx)
	}
	if time.Now().After(expires) {
		return Agent{}, "", ErrExpired
	}
	a := Agent{security.Token(), domain, method, version}
	if _, err = tx.Exec(ctx, "INSERT INTO agents(id,domain_id,installation_id,credential_hash,method,version,routing_version) VALUES($1,$2,$3,$4,$5,$6,$7)", a.ID, domain, installation, security.Digest(secret), method, version, routingVersion); err != nil {
		return a, "", resourceError(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE install_tokens SET used_at=now(),proof_hash=$2,agent_id=$3 WHERE token_hash=$1", hash, proofHash, a.ID); err != nil {
		return a, "", err
	}
	if _, err = tx.Exec(ctx, "UPDATE domains SET readiness='preparing',desired_revision=desired_revision+1,connection='unknown',applied_revision=NULL WHERE id=$1", domain); err != nil {
		return a, "", err
	}
	if err = saveSnapshot(ctx, tx, domain, seed); err != nil {
		return a, "", err
	}
	if err = audit(ctx, tx, a.ID, "agent_enrolled", domain); err != nil {
		return a, "", err
	}
	return a, secret, tx.Commit(ctx)
}
func saveSnapshot(ctx context.Context, tx pgx.Tx, domain string, seed []byte) error {
	d, err := scanDomain(tx.QueryRow(ctx, "SELECT "+domainColumns+" FROM domains WHERE id=$1", domain))
	if err != nil {
		return err
	}
	if d.OpeningURL != "" {
		if !policy.URL(d.OpeningURL, d.Hostname) {
			return ErrPolicy
		}
		if d.Mode == "local" {
			var incompatible bool
			if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agents WHERE domain_id=$1 AND revoked_at IS NULL AND routing_version<3)", domain).Scan(&incompatible); err != nil {
				return err
			}
			if incompatible {
				return ErrRoutingCapability
			}
		}
	}
	snapshot := Snapshot{Revision: d.DesiredRevision, LeaseExpiresAt: time.Now().UTC().Add(24 * time.Hour), Domain: d, Links: []Link{}, Rules: []policy.ScopedRule{}}
	rows, err := tx.Query(ctx, "SELECT "+linkColumns+" FROM links WHERE domain_id=$1 AND state='published' AND NOT disabled AND deleted_at IS NULL ORDER BY id", domain)
	if err != nil {
		return err
	}
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			rows.Close()
			return err
		}
		l.DraftDestination = nil
		l.DraftRouting = nil
		if err = policy.ValidateRouting(l.Routing, d.Hostname); err != nil {
			rows.Close()
			return err
		}
		snapshot.Links = append(snapshot.Links, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var protectionRaw []byte
	if err = tx.QueryRow(ctx, "SELECT protection FROM domains WHERE id=$1", domain).Scan(&protectionRaw); err != nil {
		return err
	}
	if err = json.Unmarshal(protectionRaw, &snapshot.Protection); err != nil {
		return err
	}
	snapshot.Rules, err = effective(ctx, tx, domain, nil)
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, l := range snapshot.Links {
		ids[l.ID] = true
	}
	filtered := []policy.ScopedRule{}
	for _, r := range snapshot.Rules {
		if r.Scope != "link" || ids[*r.ScopeID] {
			filtered = append(filtered, r)
		}
	}
	for _, link := range snapshot.Links {
		if link.Routing != nil && d.Mode == "local" {
			var compatible bool
			if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agents WHERE domain_id=$1 AND revoked_at IS NULL AND routing_version >= $2)", domain, requiredRoutingVersion(link.Routing)).Scan(&compatible); err != nil {
				return err
			}
			if !compatible {
				return ErrRoutingCapability
			}
		}
	}
	snapshot.Rules = filtered
	if _, err = policy.Compile(snapshot.Rules, d.ID, d.Hostname, ids); err != nil {
		return err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if len(payload) > 3<<20 {
		return fmt.Errorf("%w: snapshot exceeds 3 MiB payload limit", ErrPolicy)
	}
	signature := base64.RawURLEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), payload))
	etag := "\"" + security.Digest(string(payload)) + "\""
	_, err = tx.Exec(ctx, "INSERT INTO snapshots(domain_id,revision,lease_expires_at,payload,signature,etag) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(domain_id) DO UPDATE SET revision=excluded.revision,lease_expires_at=excluded.lease_expires_at,payload=excluded.payload,signature=excluded.signature,etag=excluded.etag", domain, snapshot.Revision, snapshot.LeaseExpiresAt, payload, signature, etag)
	return err
}
func (s *Store) AuthenticateAgent(ctx context.Context, secret string) (Agent, error) {
	var a Agent
	err := s.DB.QueryRow(ctx, "SELECT a.id,a.domain_id,a.method,a.version FROM agents a WHERE a.revoked_at IS NULL AND (a.credential_hash=$1 OR EXISTS(SELECT 1 FROM agent_rotations r WHERE r.agent_id=a.id AND r.new_hash=$1 AND r.acked_at IS NULL AND r.expires_at>now()))", security.Digest(secret)).Scan(&a.ID, &a.DomainID, &a.Method, &a.Version)
	return a, err
}
func (s *Store) AgentSnapshot(ctx context.Context, a Agent) ([]byte, string, string, error) {
	seed, err := s.Seed(ctx)
	if err != nil {
		return nil, "", "", err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, "", "", err
	}
	defer tx.Rollback(ctx)
	var id string
	if err = tx.QueryRow(ctx, "SELECT id FROM domains WHERE id=$1 FOR UPDATE", a.DomainID).Scan(&id); err != nil {
		return nil, "", "", err
	}
	var active bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agents WHERE id=$1 AND revoked_at IS NULL)", a.ID).Scan(&active); err != nil {
		return nil, "", "", err
	}
	if !active {
		return nil, "", "", pgx.ErrNoRows
	}
	var expires time.Time
	if err = tx.QueryRow(ctx, "SELECT lease_expires_at FROM snapshots WHERE domain_id=$1", a.DomainID).Scan(&expires); err != nil {
		return nil, "", "", err
	}
	if time.Until(expires) < 12*time.Hour {
		if err = saveSnapshot(ctx, tx, a.DomainID, seed); err != nil {
			return nil, "", "", err
		}
	}
	var payload []byte
	var signature, etag string
	err = tx.QueryRow(ctx, "SELECT payload,signature,etag FROM snapshots WHERE domain_id=$1", a.DomainID).Scan(&payload, &signature, &etag)
	if err != nil {
		return nil, "", "", err
	}
	return payload, signature, etag, tx.Commit(ctx)
}

type QueueHealth struct {
	Buffered, WriteErrors int64
	RoutingVersion        int
	ProtectionVersion     int
}

func (s *Store) Heartbeat(ctx context.Context, a Agent, revision int64, version string, depth, bytes, dropped int64, geo, feed *string, health ...QueueHealth) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var desired int64
	var previous *int64
	if err = tx.QueryRow(ctx, "SELECT desired_revision,applied_revision FROM domains WHERE id=$1 FOR UPDATE", a.DomainID).Scan(&desired, &previous); err != nil {
		return err
	}
	var active bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agents WHERE id=$1 AND revoked_at IS NULL)", a.ID).Scan(&active); err != nil {
		return err
	}
	if !active {
		return pgx.ErrNoRows
	}
	if a.Method != "cli" || revision > desired {
		return ErrBinding
	}
	tag, err := tx.Exec(ctx, "UPDATE domains SET applied_revision=$2,connection='online',last_seen_at=now() WHERE id=$1 AND (applied_revision IS NULL OR applied_revision<=$2)", a.DomainID, revision)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	var q QueueHealth
	if len(health) > 0 {
		q = health[0]
	}
	if q.RoutingVersion < 4 {
		var requires bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM links WHERE domain_id=$1 AND state='published' AND NOT disabled AND deleted_at IS NULL AND routing->>'proxyCheck'='true' AND routing ? 'ipQualityBlock')", a.DomainID).Scan(&requires); err != nil {
			return err
		}
		if requires {
			return ErrRoutingCapability
		}
	}
	if q.RoutingVersion < 3 {
		var root bool
		if err = tx.QueryRow(ctx, "SELECT opening_url<>'' FROM domains WHERE id=$1", a.DomainID).Scan(&root); err != nil {
			return err
		}
		if root {
			return ErrRoutingCapability
		}
	}
	if q.RoutingVersion < 2 {
		var requires bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM links WHERE domain_id=$1 AND state='published' AND NOT disabled AND deleted_at IS NULL AND routing IS NOT NULL AND ($2=0 OR routing->>'proxyCheck'='true'))", a.DomainID, q.RoutingVersion).Scan(&requires); err != nil {
			return err
		}
		if requires {
			return ErrRoutingCapability
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE agents SET version=$2,queue_depth=$3,queue_bytes=$4,dropped_events=$5,geo_release=$6,feed_version=$7,buffered_events=$8,queue_write_errors=$9,protection_version=$10,routing_version=$11 WHERE id=$1", a.ID, version, depth, bytes, dropped, geo, feed, q.Buffered, q.WriteErrors, q.ProtectionVersion, q.RoutingVersion); err != nil {
		return err
	}
	if previous == nil || *previous != revision {
		if err = audit(ctx, tx, a.ID, "config_applied", a.DomainID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) Revoke(ctx context.Context, agent, actor string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var domain string
	if err = tx.QueryRow(ctx, "SELECT domain_id FROM agents WHERE id=$1", agent).Scan(&domain); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "SELECT id FROM domains WHERE id=$1 FOR UPDATE", domain); err != nil {
		return err
	}
	var alreadyRevoked bool
	if err = tx.QueryRow(ctx, "SELECT revoked_at IS NOT NULL FROM agents WHERE id=$1", agent).Scan(&alreadyRevoked); err != nil {
		return err
	}
	if alreadyRevoked {
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, "UPDATE agents SET revoked_at=coalesce(revoked_at,now()) WHERE id=$1", agent); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM agent_rotations WHERE agent_id=$1", agent); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE install_tokens SET revoked=true WHERE domain_id=$1", domain); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE domains SET readiness='installation_pending',connection='unknown',applied_revision=NULL,probe_checked_at=NULL,probe_result=NULL WHERE id=$1", domain); err != nil {
		return err
	}
	if err = audit(ctx, tx, actor, "agent_revoked", domain); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
