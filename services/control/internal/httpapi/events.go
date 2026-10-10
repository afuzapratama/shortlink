package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"shortlink.local/control/internal/security"
	"shortlink.local/control/internal/store"
	"shortlink.local/geoip"
	"shortlink.local/policy"
	"shortlink.local/risk"
	"slices"
	"strings"
	"time"
)

func validTrafficEvent(e policy.TrafficEvent) bool {
	if !security.ValidToken(e.EventID) || e.DomainID == "" || len(e.DomainID) > 128 || e.LinkID == "" || len(e.LinkID) > 128 || e.ConfigRevision < 1 || e.OccurredAt.IsZero() || e.OccurredAt.Before(time.Now().Add(-48*time.Hour)) || e.OccurredAt.After(time.Now().Add(5*time.Minute)) || e.Source != "go" {
		return false
	}
	if e.Action != "redirect" && e.Action != "block" && e.Action != "fallback" {
		return false
	}
	if e.ReasonCode != "link_destination" && e.ReasonCode != "rule_redirect" && e.ReasonCode != "rule_block" && e.ReasonCode != "rule_fallback" {
		return false
	}
	if e.Country != "unknown" && !policy.ValidCountry(e.Country) {
		return false
	}
	if !slices.Contains([]string{"mobile", "desktop", "tablet", "unknown"}, e.Device) || !slices.Contains([]string{"ios", "android", "windows", "macos", "linux", "chromeos", "other", "unknown"}, e.OS) || !slices.Contains([]string{"chrome", "safari", "firefox", "edge", "samsung_internet", "opera", "other", "unknown"}, e.Browser) {
		return false
	}

	for _, p := range []*string{e.RuleID, e.OSVersion, e.BrowserVersion, e.DeviceBrand, e.DeviceModel} {
		if p != nil && (len(*p) > 128 || strings.ContainsAny(*p, "\r\n\x00")) {
			return false
		}
	}
	if e.GeoRelease != nil && !geoip.ValidRelease(*e.GeoRelease) {
		return false
	}
	if e.FeedVersion != nil && !risk.ValidVersion(*e.FeedVersion) {
		return false
	}
	if e.Risk != nil && !validRiskObservation(*e.Risk) {
		return false
	}
	if e.Action == "block" {
		return e.Destination == nil
	}
	if e.Destination == nil {
		return false
	}
	u, err := url.Parse(*e.Destination)
	return err == nil && len(*e.Destination) <= 4096 && !strings.ContainsAny(*e.Destination, "\r\n\x00") && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil
}
func (s *Server) ingestEvents(w http.ResponseWriter, r *http.Request, a store.Agent) {
	if !s.method(w, r, http.MethodPost) {
		return
	}
	var input policy.EventBatch
	if !s.decodeLimit(w, r, &input, 1<<20) {
		return
	}
	if len(input.Events) < 1 || len(input.Events) > 500 {
		s.fail(w, 400, "validation_failed", "Send 1–500 events.")
		return
	}
	for _, e := range input.Events {
		if !validTrafficEvent(e) {
			s.fail(w, 400, "validation_failed", "Invalid traffic event or event outside the 48-hour ingest window.")
			return
		}
	}
	accepted, duplicates, err := s.store.IngestEvents(r.Context(), a, input.Events)
	if errors.Is(err, store.ErrEventConflict) {
		s.fail(w, 409, "event_conflict", "An event ID was reused with different content.")
		return
	}
	if errors.Is(err, store.ErrBinding) {
		s.fail(w, 422, "binding_mismatch", "Events must belong to this agent's domain and links.")
		return
	}
	if errors.Is(err, store.ErrUsed) {
		s.fail(w, 401, "unauthorized", "Agent credential is revoked.")
		return
	}
	if err != nil {
		s.unavailable(w)
		return
	}
	s.json(w, 200, policy.BatchAccepted{Accepted: accepted, Duplicates: duplicates, RequestID: w.Header().Get("X-Request-ID")})
}

func validRiskObservation(o policy.RiskObservation) bool {
	if !slices.Contains([]string{"observe", "enforce"}, o.Mode) || len(o.Signals) > 5 || !slices.Contains([]string{"unknown", "search", "preview", "automation"}, o.BotCategory) || !slices.Contains([]string{"redirect", "block", "fallback"}, o.CandidateAction) {
		return false
	}
	for _, status := range []string{o.FeedStatus, o.TorStatus, o.ReputationStatus} {
		if !slices.Contains([]string{"fresh", "unavailable", "expired", "stale", "unconfigured"}, status) {
			return false
		}
	}
	seen := map[string]bool{}
	for _, signal := range o.Signals {
		if seen[signal] {
			return false
		}
		seen[signal] = true
		if !slices.Contains([]string{"known_bot", "suspected_automation", "tor_exit", "reputation_flag", "rate_limited"}, signal) {
			return false
		}
	}
	if o.CandidateRuleID != nil && (len(*o.CandidateRuleID) > 128 || strings.ContainsAny(*o.CandidateRuleID, "\r\n\x00")) {
		return false
	}
	return slices.Contains([]string{"link_destination", "rule_redirect", "rule_block", "rule_fallback"}, o.CandidateReasonCode)
}
