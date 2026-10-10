package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"

	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
)

func readJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func TestResourceWorkflowAndPublishedDraft(t *testing.T) {
	s, db := setup(t)
	c, csrf := login(t, s, nil)
	createDomain := func(host string) store.Domain {
		w := request(s, "POST", "/v1/admin/domains", fmt.Sprintf(`{"hostname":%q,"mode":"local"}`, host), testOrigin, csrf, c)
		status(t, w, 201)
		return readJSON[store.Domain](t, w)
	}
	for _, path := range []string{"/v1/admin/domains", "/v1/admin/links"} {
		w := request(s, "GET", path, "", "", "", c)
		status(t, w, 200)
		var empty struct {
			Items      []any
			NextCursor *string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &empty); err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != nil {
			t.Fatal("empty list is not an empty page", err)
		}
	}
	d := createDomain(" GO.DomainAnda.COM ")
	if d.Hostname != "go.domainanda.com" || d.Readiness != "draft" || d.Connection != "unknown" || d.AppliedRevision != nil || d.Capabilities.LocalGeo || d.Capabilities.PersistentQueue {
		t.Fatal("domain readiness or capabilities falsely confirmed", d)
	}
	status(t, request(s, "POST", "/v1/admin/domains", `{"hostname":"go.domainanda.com","mode":"api"}`, testOrigin, csrf, c), 409)
	for _, host := range []string{"https://go.domainanda.com", "127.0.0.1", "foo.local", "go.example", "go.domainanda.com/path", "go.domainanda.com:80"} {
		w := request(s, "POST", "/v1/admin/domains", fmt.Sprintf(`{"hostname":%q,"mode":"api"}`, host), testOrigin, csrf, c)
		status(t, w, 400)
	}
	status(t, request(s, "GET", "/v1/admin/domains/"+d.ID, "", "", "", c), 200)
	status(t, request(s, "GET", "/v1/admin/domains/missing", "", "", "", c), 404)
	create := fmt.Sprintf(`{"domainId":%q,"slug":"campaign","destination":"https://destination.com/old"}`, d.ID)
	w := request(s, "POST", "/v1/admin/links", create, testOrigin, csrf, c)
	status(t, w, 201)
	l := readJSON[store.Link](t, w)
	if l.State != "draft" || l.Revision != 0 || l.Clicks != 0 {
		t.Fatal("new link is not a draft")
	}
	status(t, request(s, "POST", "/v1/admin/links", create, testOrigin, csrf, c), 409)
	d2 := createDomain("second.domainanda.com")
	second := fmt.Sprintf(`{"domainId":%q,"slug":"campaign","destination":"https://destination.com/"}`, d2.ID)
	status(t, request(s, "POST", "/v1/admin/links", second, testOrigin, csrf, c), 201)
	for _, dest := range []string{"javascript:alert(1)", "https://user:password@destination.com/", "https://GO.DOMAINANDA.COM./campaign", "https://destination.com:99999"} {
		w := request(s, "POST", "/v1/admin/links", fmt.Sprintf(`{"domainId":%q,"destination":%q}`, d.ID, dest), testOrigin, csrf, c)
		status(t, w, 400)
	}
	status(t, request(s, "POST", "/v1/admin/links", `{"domainId":"missing","destination":"https://destination.com/"}`, testOrigin, csrf, c), 404)
	patch := fmt.Sprintf(`{"domainId":%q,"destination":"https://destination.com/new","expectedRevision":0}`, d.ID)
	w = request(s, "PATCH", "/v1/admin/links/"+l.ID, patch, testOrigin, csrf, c)
	status(t, w, 200)
	changed := readJSON[store.Link](t, w)
	if changed.Revision != 1 || changed.Destination != "https://destination.com/new" || changed.Slug != l.Slug {
		t.Fatal("draft update invalid", changed)
	}
	status(t, request(s, "PATCH", "/v1/admin/links/"+l.ID, patch, testOrigin, csrf, c), 409)
	status(t, request(s, "PATCH", "/v1/admin/links/"+l.ID, fmt.Sprintf(`{"domainId":%q,"destination":"https://destination.com/new"}`, d.ID), testOrigin, csrf, c), 400)
	if _, err := db.DB.Exec(context.Background(), "UPDATE links SET state='published',clicks=123 WHERE id=$1", l.ID); err != nil {
		t.Fatal(err)
	}
	patch = fmt.Sprintf(`{"domainId":%q,"destination":"https://destination.com/pending","expectedRevision":1}`, d.ID)
	w = request(s, "PATCH", "/v1/admin/links/"+l.ID, patch, testOrigin, csrf, c)
	status(t, w, 200)
	pending := readJSON[store.Link](t, w)
	if pending.Destination != changed.Destination || pending.DraftDestination == nil || *pending.DraftDestination != "https://destination.com/pending" || pending.Clicks != 123 || pending.ID != l.ID {
		t.Fatal("published destination/identity/analytics changed", pending)
	}
	status(t, request(s, "PATCH", "/v1/admin/links/"+l.ID, fmt.Sprintf(`{"domainId":%q,"slug":"new-slug","destination":"https://destination.com/pending","expectedRevision":2}`, d.ID), testOrigin, csrf, c), 422)
	status(t, request(s, "PATCH", "/v1/admin/links/"+l.ID, fmt.Sprintf(`{"domainId":%q,"destination":"https://destination.com/pending","expectedRevision":2}`, d2.ID), testOrigin, csrf, c), 422)
	patch = fmt.Sprintf(`{"domainId":%q,"destination":%q,"expectedRevision":2}`, d.ID, changed.Destination)
	w = request(s, "PATCH", "/v1/admin/links/"+l.ID, patch, testOrigin, csrf, c)
	status(t, w, 200)
	if readJSON[store.Link](t, w).DraftDestination != nil {
		t.Fatal("pending draft not cleared")
	}
	w = request(s, "GET", "/v1/admin/links?domainId="+d.ID, "", "", "", c)
	status(t, w, 200)
	page := readJSON[struct {
		Items      []store.Link
		NextCursor *string
	}](t, w)
	if len(page.Items) != 1 || page.NextCursor != nil {
		t.Fatal("domain filter wrong")
	}
	status(t, request(s, "POST", "/v1/admin/links/"+l.ID+"/publish", fmt.Sprintf(`{"expectedRevision":3}`), testOrigin, csrf, c), 422)
}
func TestPaginationAndConcurrentEdit(t *testing.T) {
	s, db := setup(t)
	c, csrf := login(t, s, nil)
	for i := 0; i < 4; i++ {
		if _, err := db.CreateDomain(context.Background(), security.Token(), fmt.Sprintf("host%d.domainanda.com", i), "api"); err != nil {
			t.Fatal(err)
		}
	}
	w := request(s, "GET", "/v1/admin/domains?limit=2", "", "", "", c)
	status(t, w, 200)
	page := readJSON[struct {
		Items      []store.Domain
		NextCursor *string
	}](t, w)
	if len(page.Items) != 2 || page.NextCursor == nil {
		t.Fatal("first page invalid")
	}
	w = request(s, "GET", "/v1/admin/domains?limit=2&cursor="+*page.NextCursor, "", "", "", c)
	status(t, w, 200)
	next := readJSON[struct {
		Items      []store.Domain
		NextCursor *string
	}](t, w)
	if len(next.Items) != 2 || next.NextCursor != nil || next.Items[0].ID == page.Items[0].ID {
		t.Fatal("pagination repeats or skips")
	}
	for _, path := range []string{"/v1/admin/domains?limit=0", "/v1/admin/domains?limit=101", "/v1/admin/domains?cursor=garbage", "/v1/admin/links?cursor=" + *page.NextCursor} {
		status(t, request(s, "GET", path, "", "", "", c), 400)
	}
	domain := page.Items[0]
	l, err := db.CreateLink(context.Background(), security.Token(), domain.ID, "concurrent", "https://destination.com/")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := request(s, "PATCH", "/v1/admin/links/"+l.ID, fmt.Sprintf(`{"domainId":%q,"destination":"https://destination.com/%d","expectedRevision":0}`, domain.ID, i), testOrigin, csrf, c)
			codes <- w.Code
		}(i)
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal("concurrent updates overwrote each other", counts)
	}
}
