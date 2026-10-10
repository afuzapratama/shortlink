package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"shortlink.local/control/internal/store"
	"shortlink.local/policy"
	"slices"
	"strconv"
	"strings"
	"time"
)

type trafficCursor struct {
	Filter   string    `json:"filter"`
	Snapshot int64     `json:"snapshot"`
	Before   time.Time `json:"before"`
	ID       int64     `json:"id"`
}

func (s *Server) trafficAdmin(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path != "/v1/admin/events" && path != "/v1/admin/events/export" && path != "/v1/admin/analytics" {
		return false
	}
	method := http.MethodGet
	if strings.HasSuffix(path, "/export") {
		method = http.MethodPost
	}
	if !s.method(w, r, method) {
		return true
	}
	if path == "/v1/admin/events/export" {
		select {
		case s.trafficSlots <- struct{}{}:
			defer func() { <-s.trafficSlots }()
		default:
			s.fail(w, 503, "export_busy", "Export is busy; retry shortly.")
			return true
		}
	}
	q := r.URL.Query()
	for key, values := range q {
		if !slices.Contains([]string{"from", "to", "domainId", "linkId", "action", "country", "device", "os", "browser", "cursor", "limit"}, key) || len(values) != 1 {
			s.fail(w, 400, "invalid_filter", "Unknown or repeated filter.")
			return true
		}
	}
	f := store.TrafficFilter{Domain: q.Get("domainId"), Link: q.Get("linkId"), Action: q.Get("action"), Country: q.Get("country"), Device: q.Get("device"), OS: q.Get("os"), Browser: q.Get("browser")}
	var err error
	f.From, err = time.Parse(time.RFC3339, q.Get("from"))
	if err != nil {
		s.fail(w, 400, "invalid_filter", "Use explicit RFC3339 from/to.")
		return true
	}
	f.To, err = time.Parse(time.RFC3339, q.Get("to"))
	days := s.cfg.EventRawDays
	if days == 0 {
		days = 7
	}
	if path == "/v1/admin/analytics" {
		days = s.cfg.EventAggregateDays
		if days == 0 {
			days = 90
		}
	}
	cutoff := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -days)
	if path == "/v1/admin/analytics" {
		cutoff = time.Now().UTC().AddDate(0, 0, -days).Truncate(time.Minute).Add(time.Minute)
	}
	invalid := err != nil || !f.To.After(f.From) || f.To.After(time.Now().Add(time.Minute)) || f.From.Before(cutoff) || !f.From.Equal(f.From.Truncate(time.Minute)) || !f.To.Equal(f.To.Truncate(time.Minute))
	for _, v := range []string{f.Domain, f.Link, f.Action, f.Country, f.Device, f.OS, f.Browser} {
		if len(v) > 128 || strings.ContainsAny(v, "\x00\r\n") {
			invalid = true
		}
	}
	for _, key := range []string{"action", "country", "device", "os", "browser"} {
		v := q.Get(key)
		if v == "" {
			continue
		}
		valid := false
		switch key {
		case "action":
			valid = v == "redirect" || v == "block" || v == "fallback"
		case "country":
			valid = v == "unknown" || policy.ValidCountry(v)
		case "device":
			valid = slices.Contains([]string{"mobile", "desktop", "tablet", "unknown"}, v)
		case "os":
			valid = slices.Contains([]string{"ios", "android", "windows", "macos", "linux", "chromeos", "other", "unknown"}, v)
		case "browser":
			valid = slices.Contains([]string{"chrome", "safari", "firefox", "edge", "samsung_internet", "opera", "other", "unknown"}, v)
		}
		if !valid {
			invalid = true
		}
	}
	if invalid {
		s.fail(w, 400, "invalid_filter", "Invalid filter or range; minute boundaries and retained data required.")
		return true
	}
	h, err := s.store.EventHealth(r.Context())
	if err != nil {
		s.unavailable(w)
		return true
	}
	if path == "/v1/admin/analytics" {
		b, er := s.store.TrafficSummary(r.Context(), f)
		if er != nil {
			s.unavailable(w)
		} else {
			s.json(w, 200, map[string]any{"buckets": b, "health": h, "filter": f, "rawRetentionDays": func() int {
				if s.cfg.EventRawDays == 0 {
					return 7
				}
				return s.cfg.EventRawDays
			}(), "aggregateRetentionDays": days, "uncollected": []string{"challenge", "runtime_error"}})
		}
		return true
	}
	limit := 25
	if q.Get("limit") != "" {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > 100 {
			s.fail(w, 400, "invalid_filter", "Limit must be 1–100.")
			return true
		}
	}
	raw, _ := json.Marshal(f)
	hash := sha256.Sum256(raw)
	binding := hex.EncodeToString(hash[:])
	c := trafficCursor{Filter: binding}
	if token := q.Get("cursor"); token != "" {
		if len(token) > 1024 {
			s.fail(w, 400, "invalid_cursor", "Invalid cursor.")
			return true
		}
		b, e := base64.RawURLEncoding.DecodeString(token)
		if e != nil || json.Unmarshal(b, &c) != nil || c.Filter != binding || c.Snapshot < 1 || c.ID < 1 || c.ID > c.Snapshot || c.Before.IsZero() {
			s.fail(w, 400, "invalid_cursor", "Cursor does not match filters.")
			return true
		}
	} else {
		c.Snapshot, err = s.store.EventSnapshot(r.Context())
		if err != nil {
			s.unavailable(w)
			return true
		}
	}
	export := path == "/v1/admin/events/export"
	if export {
		limit = 1000
		if q.Get("cursor") != "" {
			s.fail(w, 400, "invalid_cursor", "Export begins at first page.")
			return true
		}
	}
	rows, err := s.store.ReadEvents(r.Context(), f, c.Snapshot, c.Before, c.ID, limit+1)
	if err != nil {
		s.unavailable(w)
		return true
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		c.Before = last.ReceivedAt
		c.ID = last.ID
		b, _ := json.Marshal(c)
		token := base64.RawURLEncoding.EncodeToString(b)
		next = &token
	}
	if !export {
		s.json(w, 200, map[string]any{"items": rows, "nextCursor": next, "watermark": h.Watermark})
		return true
	}
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	writer.Write([]string{"event_id", "agent_id", "occurred_at", "received_at", "domain_id", "link_id", "action", "reason", "country", "device", "os", "browser", "destination", "revision", "risk_observation"})
	for _, row := range rows {
		e := row.Event
		destination := ""
		if e.Destination != nil {
			destination = *e.Destination
		}
		risk, _ := json.Marshal(e.Risk)
		cells := []string{e.EventID, row.AgentID, e.OccurredAt.Format(time.RFC3339Nano), row.ReceivedAt.Format(time.RFC3339Nano), e.DomainID, e.LinkID, e.Action, e.ReasonCode, e.Country, e.Device, e.OS, e.Browser, destination, strconv.FormatInt(e.ConfigRevision, 10), string(risk)}
		for i, v := range cells {
			trim := strings.TrimLeft(v, " \t\r\n")
			if trim != "" && strings.ContainsAny(trim[:1], "=+-@") {
				cells[i] = "'" + v
			}
		}
		writer.Write(cells)
	}
	writer.Flush()
	if writer.Error() != nil || buffer.Len() > 8*1024*1024 {
		s.unavailable(w)
		return true
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=traffic.csv")
	w.Header().Set("X-Export-Truncated", strconv.FormatBool(next != nil))
	w.WriteHeader(200)
	w.Write(buffer.Bytes())
	return true
}
