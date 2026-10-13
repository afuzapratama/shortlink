package httpapi

import (
	"errors"
	"golang.org/x/text/language"
	"net/http"
	"shortlink.local/control/internal/store"
	"shortlink.local/policy"
	"strings"
)

func (s *Server) policyFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrRoutingCapability):
		s.fail(w, 422, "capability_missing", "Update the Go agent to support this configuration. Selected BEON-IPQuality flags require routingVersion=4.")
	case errors.Is(err, store.ErrPolicy), errors.Is(err, store.ErrBinding):
		s.fail(w, 422, "validation_failed", err.Error())
	case errors.Is(err, store.ErrCapability):
		s.fail(w, 422, "capability_missing", "Local risk rules/enforcement require a fresh B13 heartbeat; API rules run centrally. Challenge is unavailable.")
	case errors.Is(err, store.ErrPrerequisite):
		s.fail(w, 422, "domain_not_ready", "Verify the domain and enroll its bound agent before publishing.")
	default:
		s.resourceFailure(w, err, "rule_set_conflict")
	}
}
func (s *Server) policyAdmin(w http.ResponseWriter, r *http.Request, actor string) bool {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/admin/"), "/")
	if r.URL.Path == "/v1/admin/countries" {
		if !s.method(w, r, "GET") {
			return true
		}
		items := []string{}
		for _, region := range language.Supported.Regions() {
			code := region.String()
			if policy.ValidCountry(code) && len(code) == 2 {
				items = append(items, code)
			}
		}
		s.json(w, 200, map[string]any{"items": items})
		return true
	}
	if r.URL.Path == "/v1/admin/visitors/detect" {
		if !s.method(w, r, "POST") {
			return true
		}
		var input struct {
			UserAgent string `json:"userAgent"`
		}
		if !s.decode(w, r, &input) {
			return true
		}
		if len(input.UserAgent) > 4096 {
			s.fail(w, 400, "validation_failed", "User-Agent exceeds 4096 bytes.")
			return true
		}
		s.json(w, 200, policy.Detect(input.UserAgent))
		return true
	}
	if r.URL.Path == "/v1/admin/rules/simulate" {
		if !s.method(w, r, "POST") {
			return true
		}
		var input struct {
			IPQuality           *policy.IPQualityFlags `json:"ipQuality"`
			ProxyDetected       bool                   `json:"proxyDetected"`
			ProxyStatus         string                 `json:"proxyStatus"`
			ASN                 *uint32                `json:"asn"`
			LinkID              string                 `json:"linkId"`
			Country             string                 `json:"country"`
			Device              string                 `json:"device"`
			OS                  string                 `json:"os"`
			Browser             string                 `json:"browser"`
			KnownBot            bool                   `json:"knownBot"`
			SuspectedAutomation bool                   `json:"suspectedAutomation"`
			TorExit             bool                   `json:"torExit"`
			ReputationFlag      bool                   `json:"reputationFlag"`
			RateLimited         bool                   `json:"rateLimited"`
			BotCategory         string                 `json:"botCategory"`
			RuleSetID           string                 `json:"ruleSetId"`
			ExpectedRevision    *int64                 `json:"expectedRevision"`
		}
		if !s.decode(w, r, &input) {
			return true
		}
		if input.BotCategory == "" {
			input.BotCategory = "unknown"
		}
		if input.BotCategory != "unknown" && input.BotCategory != "search" && input.BotCategory != "preview" && input.BotCategory != "automation" {
			s.fail(w, 400, "validation_failed", "Invalid bot category.")
			return true
		}
		signals := policy.Signals{IPQuality: input.IPQuality, ProxyDetected: input.ProxyDetected, ProxyStatus: input.ProxyStatus, ASN: input.ASN, Country: input.Country, Device: input.Device, OS: input.OS, Browser: input.Browser, KnownBot: input.KnownBot, SuspectedAutomation: input.SuspectedAutomation, TorExit: input.TorExit, ReputationFlag: input.ReputationFlag, RateLimited: input.RateLimited, BotCategory: input.BotCategory, FeedStatus: "synthetic", TorStatus: "synthetic", ReputationStatus: "synthetic"}
		if (input.ProxyStatus != "" && input.ProxyStatus != "known" && input.ProxyStatus != "unavailable") || (input.ProxyDetected && input.ProxyStatus != "known") || !policy.ValidSignals(signals) || (input.RuleSetID == "") != (input.ExpectedRevision == nil) {
			s.fail(w, 400, "validation_failed", "Invalid visitor signals or draft revision.")
			return true
		}
		result, err := s.store.Simulate(r.Context(), input.LinkID, signals, input.RuleSetID, input.ExpectedRevision)
		if err != nil {
			s.policyFailure(w, err)
			return true
		}
		s.json(w, 200, result)
		return true
	}
	if len(parts) == 3 && (parts[0] == "links" || parts[0] == "rule-sets") && (parts[2] == "publish" || parts[2] == "rollback" || parts[2] == "history") {
		isLink := parts[0] == "links"
		if parts[2] == "history" {
			if !s.method(w, r, "GET") {
				return true
			}
			if isLink {
				_, err := s.store.Link(r.Context(), parts[1])
				if err != nil {
					s.policyFailure(w, err)
					return true
				}
			} else {
				_, err := s.store.RuleSet(r.Context(), parts[1])
				if err != nil {
					s.policyFailure(w, err)
					return true
				}
			}
			items, err := s.store.History(r.Context(), parts[1], isLink)
			if err != nil {
				s.policyFailure(w, err)
				return true
			}
			s.json(w, 200, map[string]any{"items": items})
			return true
		}
		if !s.method(w, r, "POST") {
			return true
		}
		var input struct {
			ExpectedRevision *int64 `json:"expectedRevision"`
			TargetRevision   *int64 `json:"targetRevision"`
		}
		if !s.decode(w, r, &input) {
			return true
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || parts[2] == "rollback" && (input.TargetRevision == nil || *input.TargetRevision < 1) || parts[2] == "publish" && input.TargetRevision != nil {
			s.fail(w, 400, "validation_failed", "Expected revision and rollback target are required.")
			return true
		}
		target := int64(0)
		if input.TargetRevision != nil {
			target = *input.TargetRevision
		}
		var result store.PublishResult
		var err error
		if isLink {
			result, err = s.store.PublishLink(r.Context(), parts[1], *input.ExpectedRevision, target, actor)
		} else {
			result, err = s.store.PublishRules(r.Context(), parts[1], *input.ExpectedRevision, target, actor)
		}
		if err != nil {
			s.policyFailure(w, err)
			return true
		}
		s.json(w, 200, result)
		return true
	}
	if parts[0] != "rule-sets" || len(parts) > 2 {
		return false
	}
	if len(parts) == 1 {
		switch r.Method {
		case "GET":
			after, limit, ok := s.pagination(w, r, "rule-sets", "")
			if !ok {
				return true
			}
			items, err := s.store.RuleSets(r.Context(), after, limit+1)
			if err != nil {
				s.policyFailure(w, err)
				return true
			}
			var next *string
			if len(items) > limit {
				items = items[:limit]
				v := encodeCursor("rule-sets", items[limit-1].ID, "")
				next = &v
			}
			s.json(w, 200, map[string]any{"items": items, "nextCursor": next})
		case "POST":
			var input struct {
				Scope   string  `json:"scope"`
				ScopeID *string `json:"scopeId"`
			}
			if !s.decode(w, r, &input) {
				return true
			}
			result, err := s.store.CreateRuleSet(r.Context(), input.Scope, input.ScopeID, actor)
			if err != nil {
				s.policyFailure(w, err)
				return true
			}
			s.json(w, 201, result)
		default:
			w.Header().Set("Allow", "GET, POST")
			s.fail(w, 405, "method_not_allowed", "Method not allowed.")
		}
		return true
	}
	switch r.Method {
	case "GET":
		result, err := s.store.RuleSet(r.Context(), parts[1])
		if err != nil {
			s.policyFailure(w, err)
			return true
		}
		s.json(w, 200, result)
	case "PATCH":
		var input struct {
			ExpectedRevision *int64        `json:"expectedRevision"`
			Rules            []policy.Rule `json:"rules"`
		}
		if !s.decodeLimit(w, r, &input, 128*1024) {
			return true
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || input.Rules == nil {
			s.fail(w, 400, "validation_failed", "Expected revision and rules array are required.")
			return true
		}
		result, err := s.store.UpdateRuleSet(r.Context(), parts[1], *input.ExpectedRevision, input.Rules, actor)
		if err != nil {
			s.policyFailure(w, err)
			return true
		}
		s.json(w, 200, result)
	default:
		w.Header().Set("Allow", "GET, PATCH")
		s.fail(w, 405, "method_not_allowed", "Method not allowed.")
	}
	return true
}
