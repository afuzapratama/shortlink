package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"shortlink.local/policy"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrRoutingCapability = errors.New("local agent requires link routing version 1")
var ErrConflict = errors.New("revision conflict")
var ErrDuplicate = errors.New("duplicate resource")
var ErrImmutable = errors.New("published URL is immutable")
var ErrDomainMissing = errors.New("domain missing")

type Capabilities struct {
	LocalGeo        bool `json:"localGeo"`
	PersistentQueue bool `json:"persistentQueue"`
	Challenge       bool `json:"challenge"`
}
type Domain struct {
	OpeningURL      string       `json:"openingUrl"`
	ID              string       `json:"id"`
	Hostname        string       `json:"hostname"`
	Mode            string       `json:"mode"`
	Readiness       string       `json:"readiness"`
	Connection      string       `json:"connection"`
	DesiredRevision int64        `json:"desiredRevision"`
	AppliedRevision *int64       `json:"appliedRevision"`
	LastSeenAt      *time.Time   `json:"lastSeenAt"`
	Capabilities    Capabilities `json:"capabilities"`
}
type Link struct {
	CreatedAt         time.Time           `json:"createdAt"`
	Disabled          bool                `json:"disabled"`
	PublishedRevision *int64              `json:"publishedRevision"`
	Routing           *policy.LinkRouting `json:"routing,omitempty"`
	DraftRouting      *policy.LinkRouting `json:"draftRouting,omitempty"`
	ID                string              `json:"id"`
	DomainID          string              `json:"domainId"`
	Slug              string              `json:"slug"`
	Destination       string              `json:"destination"`
	DraftDestination  *string             `json:"draftDestination,omitempty"`
	State             string              `json:"state"`
	Revision          int64               `json:"revision"`
	Clicks            int64               `json:"clicks"`
}

const domainColumns = "id,hostname,mode,CASE WHEN readiness='ready' AND ((mode='api' AND (probe_checked_at IS NULL OR probe_checked_at<now()-interval '5 minutes' OR applied_revision IS DISTINCT FROM desired_revision)) OR (mode='local' AND (last_seen_at IS NULL OR last_seen_at<now()-interval '90 seconds' OR applied_revision IS DISTINCT FROM desired_revision OR probe_checked_at IS NULL OR probe_checked_at<now()-interval '5 minutes'))) THEN 'attention' ELSE readiness END,CASE WHEN mode='local' AND connection='online' AND last_seen_at<now()-interval '90 seconds' THEN 'offline' WHEN mode='api' AND connection='online' AND last_seen_at<now()-interval '90 seconds' THEN 'idle' ELSE connection END,desired_revision,applied_revision,last_seen_at,opening_url"
const linkColumns = "id,domain_id,slug,destination,draft_destination,state,revision,clicks,routing,draft_routing,created_at,disabled,published_revision"

func scanDomain(row pgx.Row) (Domain, error) {
	var d Domain
	err := row.Scan(&d.ID, &d.Hostname, &d.Mode, &d.Readiness, &d.Connection, &d.DesiredRevision, &d.AppliedRevision, &d.LastSeenAt, &d.OpeningURL)
	return d, err
}
func scanLink(row pgx.Row) (Link, error) {
	var l Link
	var routing, draft []byte
	err := row.Scan(&l.ID, &l.DomainID, &l.Slug, &l.Destination, &l.DraftDestination, &l.State, &l.Revision, &l.Clicks, &routing, &draft, &l.CreatedAt, &l.Disabled, &l.PublishedRevision)
	if err == nil && routing != nil {
		err = json.Unmarshal(routing, &l.Routing)
	}
	if err == nil && draft != nil {
		err = json.Unmarshal(draft, &l.DraftRouting)
	}
	return l, err
}
func resourceError(err error) error {
	var e *pgconn.PgError
	if errors.As(err, &e) {
		if e.Code == "23505" {
			return ErrDuplicate
		}
		if e.Code == "23503" {
			return ErrDomainMissing
		}
	}
	return err
}
func actorID(actor []string) string {
	if len(actor) > 0 {
		return actor[0]
	}
	return "system"
}
func (s *Store) CreateDomain(ctx context.Context, id, host, mode string, actor ...string) (Domain, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Domain{}, err
	}
	defer tx.Rollback(ctx)
	if err = publishLock(ctx, tx); err != nil {
		return Domain{}, err
	}
	d, err := scanDomain(tx.QueryRow(ctx, "INSERT INTO domains(id,hostname,mode) VALUES($1,$2,$3) RETURNING "+domainColumns, id, host, mode))
	if err != nil {
		return d, resourceError(err)
	}
	if err = audit(ctx, tx, actorID(actor), "domain_created", id); err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}
func (s *Store) Domain(ctx context.Context, id string) (Domain, error) {
	return scanDomain(s.DB.QueryRow(ctx, "SELECT "+domainColumns+" FROM domains WHERE id=$1", id))
}
func (s *Store) Domains(ctx context.Context, after string, limit int) ([]Domain, error) {
	rows, err := s.DB.Query(ctx, "SELECT "+domainColumns+" FROM domains WHERE id>$1 ORDER BY id LIMIT $2", after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
func (s *Store) CreateLink(ctx context.Context, id, domain, slug, destination string, actor ...string) (Link, error) {
	return s.CreateLinkRouting(ctx, id, domain, slug, destination, nil, actor...)
}
func (s *Store) CreateLinkRouting(ctx context.Context, id, domain, slug, destination string, routing *policy.LinkRouting, actor ...string) (Link, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Link{}, err
	}
	defer tx.Rollback(ctx)
	l, err := scanLink(tx.QueryRow(ctx, "INSERT INTO links(id,domain_id,slug,destination,routing) VALUES($1,$2,$3,$4,$5) RETURNING "+linkColumns, id, domain, slug, destination, routing))
	if err != nil {
		return l, resourceError(err)
	}
	if err = audit(ctx, tx, actorID(actor), "link_created", domain); err != nil {
		return l, err
	}
	return l, tx.Commit(ctx)
}
func (s *Store) Link(ctx context.Context, id string) (Link, error) {
	return scanLink(s.DB.QueryRow(ctx, "SELECT "+linkColumns+" FROM links WHERE id=$1 AND deleted_at IS NULL", id))
}
func (s *Store) Links(ctx context.Context, after, domain string, limit int) ([]Link, error) {
	rows, err := s.DB.Query(ctx, "SELECT "+linkColumns+" FROM links WHERE deleted_at IS NULL AND id>$1 AND ($2='' OR domain_id=$2) ORDER BY id LIMIT $3", after, domain, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Link{}
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	return result, rows.Err()
}
func (s *Store) UpdateLink(ctx context.Context, id, domain, slug, destination string, expected int64, actor ...string) (Link, error) {
	return s.UpdateLinkRouting(ctx, id, domain, slug, destination, expected, nil, actor...)
}
func (s *Store) UpdateLinkRouting(ctx context.Context, id, domain, slug, destination string, expected int64, routing *policy.LinkRouting, actor ...string) (Link, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Link{}, err
	}
	defer tx.Rollback(ctx)
	if err = publishLock(ctx, tx); err != nil {
		return Link{}, err
	}
	old, err := scanLink(tx.QueryRow(ctx, "SELECT "+linkColumns+" FROM links WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", id))
	if err != nil {
		return old, err
	}
	var bound bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM rule_sets WHERE link_id=$1)", id).Scan(&bound); err != nil {
		return old, err
	}
	if bound && old.DomainID != domain {
		return old, ErrImmutable
	}
	if old.Revision != expected {
		return old, ErrConflict
	}
	if slug == "" {
		slug = old.Slug
	}
	if old.State == "published" && (old.DomainID != domain || old.Slug != slug) {
		return old, ErrImmutable
	}
	var draft *string
	published := destination
	if old.State == "published" {
		published = old.Destination
		if destination != old.Destination {
			draft = &destination
		}
	}
	activeRouting, draftRouting := old.Routing, old.DraftRouting
	if routing != nil {
		if old.State == "published" {
			draftRouting = routing
			if reflect.DeepEqual(routing, old.Routing) {
				draftRouting = nil
			}
		} else {
			activeRouting = routing
			draftRouting = nil
		}
	}
	updated, err := scanLink(tx.QueryRow(ctx, "UPDATE links SET domain_id=$2,slug=$3,destination=$4,draft_destination=$5,routing=$6,draft_routing=$7,revision=revision+1 WHERE id=$1 RETURNING "+linkColumns, id, domain, slug, published, draft, activeRouting, draftRouting))
	if err != nil {
		return old, resourceError(err)
	}
	if err = audit(ctx, tx, actorID(actor), "link_draft_updated", domain); err != nil {
		return old, err
	}
	if err = tx.Commit(ctx); err != nil {
		return old, err
	}
	return updated, nil
}
