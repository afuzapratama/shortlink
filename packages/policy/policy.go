// Package policy is shared by control simulation and the local redirect agent.
package policy

import (
	"errors"
	"golang.org/x/text/language"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type Rule struct {
	ASN               *uint32 `json:"asn,omitempty"`
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Priority          int     `json:"priority"`
	Country           string  `json:"country"`
	Device            string  `json:"device"`
	OS                string  `json:"os"`
	Browser           string  `json:"browser"`
	Risk              string  `json:"risk"`
	BotCategory       string  `json:"botCategory,omitempty"`
	Action            string  `json:"action"`
	DestinationSource string  `json:"destinationSource,omitempty"`
	Destination       string  `json:"destination,omitempty"`
	Enabled           bool    `json:"enabled"`
	Mandatory         bool    `json:"mandatory,omitempty"`
}
type ScopedRule struct {
	Rule
	Scope   string  `json:"scope"`
	ScopeID *string `json:"scopeId"`
}
type Signals struct {
	ASN                 *uint32 `json:"asn"`
	Country             string  `json:"country"`
	Device              string  `json:"device"`
	OS                  string  `json:"os"`
	Browser             string  `json:"browser"`
	KnownBot            bool    `json:"knownBot"`
	SuspectedAutomation bool    `json:"suspectedAutomation"`
	TorExit             bool    `json:"torExit"`
	ReputationFlag      bool    `json:"reputationFlag"`
	RateLimited         bool    `json:"rateLimited"`
	BotCategory         string  `json:"botCategory"`
	FeedStatus          string  `json:"feedStatus"`
	TorStatus           string  `json:"torStatus"`
	ReputationStatus    string  `json:"reputationStatus"`
	OSVersion           *string `json:"osVersion"`
	BrowserVersion      *string `json:"browserVersion"`
	DeviceBrand         *string `json:"deviceBrand"`
	DeviceModel         *string `json:"deviceModel"`
}
type Decision struct {
	EventID        string           `json:"eventId"`
	Risk           *RiskObservation `json:"risk,omitempty"`
	Action         string           `json:"action"`
	ReasonCode     string           `json:"reasonCode"`
	RuleID         *string          `json:"ruleId"`
	ConfigRevision int64            `json:"configRevision"`
	GeoRelease     *string          `json:"geoRelease"`
	FeedVersion    *string          `json:"feedVersion"`
	Destination    *string          `json:"destination"`
}

func member(value, list string) bool {
	if value == "" {
		return false
	}
	for {
		item, rest, more := strings.Cut(list, "|")
		if item == value {
			return true
		}
		if !more {
			return false
		}
		list = rest
	}
}
func ValidCountry(value string) bool {
	if value == "unknown" {
		return true
	}
	if len(value) != 2 || value != strings.ToUpper(value) {
		return false
	}
	region, err := language.ParseRegion(value)
	return err == nil && region.IsCountry() && region.String() == value
}
func ValidSignals(s Signals) bool {
	return (s.ASN == nil || *s.ASN > 0) && ValidCountry(s.Country) && member(s.Device, "mobile|tablet|desktop|unknown") && member(s.OS, "ios|android|windows|macos|linux|chromeos|other|unknown") && member(s.Browser, "chrome|safari|firefox|edge|samsung_internet|opera|other|unknown")
}
func URL(value, host string) bool {
	u, err := url.Parse(value)
	if err != nil || len(value) > 4096 || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" || strings.ContainsAny(value, "\r\n\\") || strings.ContainsAny(u.Host, " \t") || strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), strings.TrimSuffix(host, ".")) {
		return false
	}
	if u.Port() != "" {
		n, e := strconv.Atoi(u.Port())
		if e != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return true
}
func Normalize(rules []Rule) []Rule {
	out := append([]Rule{}, rules...)
	for i := range out {
		if out[i].BotCategory == "" {
			out[i].BotCategory = "*"
		}
		if out[i].OS == "" {
			out[i].OS = "*"
		}
		if out[i].Browser == "" {
			out[i].Browser = "*"
		}
		if out[i].DestinationSource == "" && (out[i].Action == "redirect" || out[i].Action == "fallback") {
			out[i].DestinationSource = "link"
		}
	}
	return out
}
func Validate(scope string, rules []Rule) error {
	if !member(scope, "global|domain|link") || len(rules) > 100 {
		return errors.New("scope invalid or more than 100 rules")
	}
	ids := map[string]bool{}
	priorities := map[int]bool{}
	for _, r := range rules {
		if r.ID == "" || len(r.ID) > 128 || ids[r.ID] || len(strings.TrimSpace(r.Name)) == 0 || len(r.Name) > 120 || r.Priority < 1 || r.Priority > 1000 || priorities[r.Priority] {
			return errors.New("rule IDs/priorities must be unique; name/priority invalid")
		}
		ids[r.ID] = true
		priorities[r.Priority] = true
		if (r.Country != "*" && !ValidCountry(r.Country)) || !member(r.Device, "*|mobile|tablet|desktop|unknown") || !member(r.OS, "*|ios|android|windows|macos|linux|chromeos|other|unknown") || !member(r.Browser, "*|chrome|safari|firefox|edge|samsung_internet|opera|other|unknown") || !member(r.Risk, "*|known_bot|suspected_automation|tor_exit|reputation_flag|rate_limited") {
			return errors.New("invalid condition")
		}
		if r.BotCategory != "" && (!member(r.BotCategory, "*|search|preview|automation|unknown") || (r.BotCategory != "*" && r.Risk != "known_bot")) {
			return errors.New("botCategory requires a known_bot condition")
		}
		if !member(r.Action, "redirect|fallback|block") {
			return errors.New("unsupported action; challenge unavailable")
		}
		if r.ASN != nil && *r.ASN == 0 {
			return errors.New("ASN must be positive")
		}
		if r.Mandatory && (scope != "global" || r.Action != "block" || r.Risk != "*") {
			return errors.New("mandatory only supports ordinary global blocks; risk rules follow the domain protection mode")
		}
		if r.Action == "redirect" || r.Action == "fallback" {
			if !member(r.DestinationSource, "link|custom") || (r.DestinationSource == "custom" && !URL(r.Destination, "")) || (r.DestinationSource == "link" && r.Destination != "") {
				return errors.New("invalid destination source or URL")
			}
		} else if r.Destination != "" || r.DestinationSource != "" {
			return errors.New("block has no destination")
		}
	}
	return nil
}

type Engine struct {
	rules []ScopedRule
	links map[string][]ScopedRule
}

func Compile(rules []ScopedRule, domain, host string, linkIDs map[string]bool) (*Engine, error) {
	if len(rules) > 10000 {
		return nil, errors.New("too many effective rules")
	}
	scopeRules := map[string][]Rule{}
	ids := map[string]bool{}
	for _, r := range rules {
		if (r.Scope == "global" && r.ScopeID != nil) || (r.Scope == "domain" && (r.ScopeID == nil || *r.ScopeID != domain)) || (r.Scope == "link" && (r.ScopeID == nil || !linkIDs[*r.ScopeID])) {
			return nil, errors.New("rule binding mismatch")
		}
		key := r.Scope
		if r.ScopeID != nil {
			key += ":" + *r.ScopeID
		}
		scopeRules[key] = append(scopeRules[key], r.Rule)
		if ids[r.ID] {
			return nil, errors.New("duplicate effective rule ID")
		}
		ids[r.ID] = true
		if r.DestinationSource == "custom" && !URL(r.Destination, host) {
			return nil, errors.New("rule destination loop")
		}
	}
	for key, list := range scopeRules {
		scope := strings.SplitN(key, ":", 2)[0]
		if err := Validate(scope, list); err != nil {
			return nil, err
		}
	}
	sorted := append([]ScopedRule{}, rules...)
	sort.Slice(sorted, func(i, j int) bool { return precedes(sorted[i], sorted[j]) })
	engine := &Engine{rules: []ScopedRule{}, links: map[string][]ScopedRule{}}
	for _, r := range sorted {
		if r.Scope == "link" {
			engine.links[*r.ScopeID] = append(engine.links[*r.ScopeID], r)
		} else {
			engine.rules = append(engine.rules, r)
		}
	}
	return engine, nil
}
func precedes(a, b ScopedRule) bool {
	if a.Mandatory != b.Mandatory {
		return a.Mandatory
	}
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	rank := map[string]int{"link": 0, "domain": 1, "global": 2}
	if rank[a.Scope] != rank[b.Scope] {
		return rank[a.Scope] < rank[b.Scope]
	}
	return a.ID < b.ID
}
func match(want, actual string) bool { return want == "*" || want == actual }
func (e *Engine) Evaluate(linkID, destination string, s Signals) Decision {
	return e.evaluate(linkID, destination, s, true)
}
func (e *Engine) evaluate(linkID, destination string, s Signals, risks bool) Decision {
	result := Decision{Action: "redirect", ReasonCode: "link_destination", Destination: &destination}
	local := e.links[linkID]
	i, j := 0, 0
	for i < len(e.rules) || j < len(local) {
		var r ScopedRule
		if j >= len(local) || (i < len(e.rules) && precedes(e.rules[i], local[j])) {
			r = e.rules[i]
			i++
		} else {
			r = local[j]
			j++
		}
		if !r.Enabled || (r.ASN != nil && (s.ASN == nil || *s.ASN != *r.ASN)) || (r.Scope == "link" && *r.ScopeID != linkID) || !match(r.Country, s.Country) || !match(r.Device, s.Device) || !match(r.OS, s.OS) || !match(r.Browser, s.Browser) || (!risks && r.Risk != "*") || !RiskMatch(r.Risk, s) || (r.BotCategory != "" && !match(r.BotCategory, s.BotCategory)) {
			continue
		}
		id := r.ID
		result.RuleID = &id
		result.Action = r.Action
		result.ReasonCode = "rule_" + r.Action
		if r.Action == "block" {
			result.Destination = nil
		} else if r.DestinationSource == "custom" {
			target := r.Destination
			result.Destination = &target
		}
		return result
	}
	return result
}
