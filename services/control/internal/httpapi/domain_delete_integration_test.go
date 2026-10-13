package httpapi

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDeleteDomainConfirmationAndCredentialInvalidation(t *testing.T) {
	s, db, d, a, l, credential := apiFixture(t)
	ctx := context.Background()
	cookie, csrf := login(t, s, nil)
	body := func(host string, revision int64) string {
		b, _ := json.Marshal(map[string]any{"hostname": host, "expectedRevision": revision})
		return string(b)
	}
	rs, err := db.CreateRuleSet(ctx, "domain", &d.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.PublishRules(ctx, rs.ID, rs.Revision, 0, "test"); err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateDomain(ctx, "unrelated-domain", "unrelated.example.test", "api", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec(ctx, `INSERT INTO traffic_aggregates(grain,bucket,domain_id,link_id,country,device,os,browser,action,reason,events) VALUES('hour',now(),$1,$2,'ID','mobile','ios','safari','redirect','link_destination',1)`, d.ID, l.ID); err != nil {
		t.Fatal(err)
	}
	d, err = db.Domain(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	status(t, request(s, "DELETE", "/v1/admin/domains/"+d.ID, `{"hostname":"`+d.Hostname+`"}`, testOrigin, csrf, cookie), 422)
	// Check all confirmation and authorization failures leave the domain intact.
	status(t, request(s, "DELETE", "/v1/admin/domains/"+d.ID, body(d.Hostname, d.DesiredRevision), testOrigin, "", nil), 401)
	status(t, request(s, "DELETE", "/v1/admin/domains/"+d.ID, body(d.Hostname, d.DesiredRevision), testOrigin, "", cookie), 403)
	status(t, request(s, "DELETE", "/v1/admin/domains/"+d.ID, body("wrong.example", d.DesiredRevision), testOrigin, csrf, cookie), 409)
	status(t, request(s, "DELETE", "/v1/admin/domains/"+d.ID, body(d.Hostname, d.DesiredRevision+1), testOrigin, csrf, cookie), 409)
	if _, err := db.Domain(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	// Seed credential-dependent records to exercise foreign-key cleanup.
	for _, sql := range []string{
		"INSERT INTO agent_manage_keys(key_hash,agent_id,domain_id,expires_at) VALUES('delete-test-key',$1,$2,now()+interval '5 minutes')",
		"INSERT INTO agent_manage_sessions(session_hash,agent_id,domain_id,expires_at) VALUES('delete-test-session',$1,$2,now()+interval '5 minutes')",
	} {
		if _, err := db.DB.Exec(ctx, sql, a.ID, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.DB.Exec(ctx, "INSERT INTO event_keys(agent_id,event_id,payload_hash) VALUES($1,'delete-event','hash')", a.ID); err != nil {
		t.Fatal(err)
	}
	// Existing API decisions must also be cleaned up when deleting the credential.
	status(t, agentCall(s, "/v1/decisions", credential, decisionBody(d.ID, apiIphone)), 200)
	status(t, request(s, "DELETE", "/v1/admin/domains/"+d.ID, body(d.Hostname, d.DesiredRevision), testOrigin, csrf, cookie), 200)
	status(t, request(s, "GET", "/v1/admin/domains/"+d.ID, "", "", "", cookie), 404)
	status(t, request(s, "GET", "/v1/admin/links/"+l.ID, "", "", "", cookie), 404)
	status(t, agentCall(s, "/v1/decisions", credential, decisionBody(d.ID, apiIphone)), 401)
	var auditCount int
	if err := db.DB.QueryRow(ctx, "SELECT count(*) FROM audit_logs WHERE domain_id=$1 AND action='domain_deleted'", d.ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatal("delete audit", auditCount, err)
	}
	status(t, request(s, "DELETE", "/v1/admin/domains/"+d.ID, body(d.Hostname, d.DesiredRevision), testOrigin, csrf, cookie), 404)
	if _, err = db.Domain(ctx, other.ID); err != nil {
		t.Fatal("unrelated domain changed", err)
	}
	var retained int
	if err = db.DB.QueryRow(ctx, "SELECT count(*) FROM traffic_aggregates WHERE domain_id=$1", d.ID).Scan(&retained); err != nil || retained != 1 {
		t.Fatal("historical traffic lost", retained, err)
	}
	// Hostname can be added again, with a new identity and no old credentials.
	if _, err := db.CreateDomain(ctx, "replacement-domain", d.Hostname, d.Mode, "test"); err != nil {
		t.Fatal(err)
	}
	domains, err := db.Domains(ctx, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range domains {
		if domain.ID == d.ID {
			t.Fatal("deleted domain listed")
		}
	}
	links, err := db.Links(ctx, "", d.ID, 100)
	if err != nil || len(links) != 0 {
		t.Fatal(links, err)
	}
}
