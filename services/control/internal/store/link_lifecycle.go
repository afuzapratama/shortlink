package store

import (
	"context"
	"time"
)

// ChangeLinkLifecycle retains historical binding so queued clicks still ingest.
func (s *Store) ChangeLinkLifecycle(ctx context.Context, id string, expected int64, slug string, disabled *bool, actor string) (PublishResult, error) {
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
	binding := &id
	ids, err := affected(ctx, tx, "link", binding)
	if err != nil {
		return result, err
	}
	l, err := scanLink(tx.QueryRow(ctx, "SELECT "+linkColumns+" FROM links WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", id))
	if err != nil {
		return result, err
	}
	if l.Revision != expected {
		return result, ErrConflict
	}
	action := "link_deleted"
	if disabled == nil {
		if l.Slug != slug {
			return result, ErrConflict
		}
		_, err = tx.Exec(ctx, "UPDATE links SET deleted_at=now(),disabled=true,revision=revision+1 WHERE id=$1", id)
	} else {
		if l.State != "published" {
			return result, ErrPrerequisite
		}
		if l.Disabled == *disabled {
			result.Revision = l.Revision
			return result, tx.Commit(ctx)
		}
		if !*disabled {
			var allowed bool
			if err = tx.QueryRow(ctx, "SELECT verified_at IS NOT NULL AND EXISTS(SELECT 1 FROM agents WHERE domain_id=$1 AND revoked_at IS NULL) FROM domains WHERE id=$1", l.DomainID).Scan(&allowed); err != nil {
				return result, err
			}
			if !allowed {
				return result, ErrPrerequisite
			}
		}
		action = "link_enabled"
		if *disabled {
			action = "link_disabled"
		}
		_, err = tx.Exec(ctx, "UPDATE links SET disabled=$2,revision=revision+1 WHERE id=$1", id, *disabled)
	}
	if err != nil {
		return result, err
	}
	result.Revision = l.Revision + 1
	if l.State == "published" {
		if err = refresh(ctx, tx, ids, seed, &result); err != nil {
			return result, err
		}
	}
	if err = audit(ctx, tx, actor, action, l.DomainID); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
