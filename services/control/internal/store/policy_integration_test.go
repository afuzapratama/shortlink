package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/control/internal/testdb"
	"shortlink.local/policy"
	"sync"
	"testing"
)

func TestGlobalPublishConcurrentAndAtomicSnapshots(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	ids := []string{}
	for n := 0; n < 2; n++ {
		d, err := db.CreateDomain(ctx, security.Token(), fmt.Sprintf("global%d.example.test", n), "local")
		if err != nil {
			t.Fatal(err)
		}
		c, e := db.Challenge(ctx, d.ID, "test")
		if e != nil {
			t.Fatal(e)
		}
		if e = db.Verify(ctx, d.ID, c.Value, "test"); e != nil {
			t.Fatal(e)
		}
		token, e := db.IssueToken(ctx, d.ID, "cli", "test")
		if e != nil {
			t.Fatal(e)
		}
		if _, _, e = db.Enroll(ctx, token.Token, token.InstallationID, security.Token(), d.Hostname, "cli", "test"); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, d.ID)
	}
	set, e := db.CreateRuleSet(ctx, "global", nil, "test")
	if e != nil {
		t.Fatal(e)
	}
	rule := policy.Rule{ID: "shared-global", Name: "Global", Priority: 10, Country: "*", Device: "*", OS: "*", Browser: "*", Risk: "*", Action: "block", Mandatory: true, Enabled: true}
	set, e = db.UpdateRuleSet(ctx, set.ID, 0, []policy.Rule{rule}, "test")
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := db.PublishRules(ctx, set.ID, set.Revision, 0, "test")
			if e == nil && (len(r.PendingAgentIDs) != 2 || len(r.DomainRevisions) != 2) {
				results <- fmt.Errorf("fanout missing: %+v", r)
				return
			}
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for e := range results {
		if e == nil {
			wins++
		} else if errors.Is(e, store.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal(wins, conflicts)
	}
	for _, id := range ids {
		d, e := db.Domain(ctx, id)
		if e != nil || d.DesiredRevision != 2 {
			t.Fatal(d, e)
		}
		var payload []byte
		if e = db.DB.QueryRow(ctx, "SELECT payload FROM snapshots WHERE domain_id=$1", id).Scan(&payload); e != nil {
			t.Fatal(e)
		}
		var snapshot store.Snapshot
		if e = json.Unmarshal(payload, &snapshot); e != nil {
			t.Fatal(e)
		}
		if snapshot.Revision != 2 || len(snapshot.Rules) != 1 || snapshot.Rules[0].ID != rule.ID {
			t.Fatal("snapshot is not atomic", snapshot)
		}
	}
	// Duplicate effective IDs must reject the entire publish and leave every snapshot unchanged.
	domainSet, e := db.CreateRuleSet(ctx, "domain", &ids[0], "test")
	if e != nil {
		t.Fatal(e)
	}
	rule.Mandatory = false
	domainSet, e = db.UpdateRuleSet(ctx, domainSet.ID, 0, []policy.Rule{rule}, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.PublishRules(ctx, domainSet.ID, domainSet.Revision, 0, "test"); !errors.Is(e, store.ErrPolicy) {
		t.Fatal("duplicate effective ID published", e)
	}
	d, _ := db.Domain(ctx, ids[0])
	if d.DesiredRevision != 2 {
		t.Fatal("failed transaction advanced desired revision")
	}
	after, _ := db.RuleSet(ctx, domainSet.ID)
	if after.PublishedRevision != nil {
		t.Fatal("failed transaction published rules")
	}
}
