package policy

import "errors"

// LinkRouting is opt-in. Nil preserves the routing of existing links.
type LinkRouting struct {
	IPQualityBlock       *[]string `json:"ipQualityBlock,omitempty"`
	ProxyCheck           bool      `json:"proxyCheck,omitempty"`
	ProxyUnknownFallback bool      `json:"proxyUnknownFallback,omitempty"`
	Fallback             string    `json:"fallback"`
	Country              string    `json:"country"`
	Device               string    `json:"device"`
	OS                   string    `json:"os"`
	Browser              string    `json:"browser"`
}

func ValidateRouting(r *LinkRouting, host string) error {
	if r == nil {
		return nil
	}
	if !URL(r.Fallback, host) || (r.Country != "*" && !ValidCountry(r.Country)) || !member(r.Device, "*|mobile|tablet|desktop|unknown") || !member(r.OS, "*|ios|android|windows|macos|linux|chromeos|other|unknown") || !member(r.Browser, "*|chrome|safari|firefox|edge|samsung_internet|opera|other|unknown") {
		return errors.New("invalid link fallback or audience")
	}
	if r.IPQualityBlock != nil {
		if len(*r.IPQualityBlock) > 8 {
			return errors.New("too many IP quality flags")
		}
		seen := map[string]bool{}
		for _, flag := range *r.IPQualityBlock {
			if !member(flag, "proxy|vpn|tor|datacenter|botnet|spam|malware|attacker") || seen[flag] {
				return errors.New("invalid IP quality flag")
			}
			seen[flag] = true
		}
	}
	return nil
}

// EvaluateRouting uses allow lists per scope: OR within a scope, AND between
// scopes and the link audience. Deny rules and detected bots use this link's
// fallback. No rule can route a configured link to another custom destination.
func (e *Engine) EvaluateRouting(link, destination string, r *LinkRouting, s Signals, p Protection) Decision {
	if r == nil {
		return e.EvaluateProtection(link, destination, s, p)
	}
	result := e.evaluateRouting(link, destination, r, s, p)
	if r.ProxyCheck {
		detected, status := ipQualityOutcome(r, s)
		result.Proxy = &ProxyObservation{Status: status, Detected: detected}
	}
	if p.Mode != "off" {
		observation := e.EvaluateProtection(link, destination, s, p).Risk
		candidateProtection := p
		candidateProtection.Mode = "enforce"
		candidate := e.evaluateRouting(link, destination, r, s, candidateProtection)
		observation.CandidateAction = candidate.Action
		observation.CandidateRuleID = candidate.RuleID
		observation.CandidateReasonCode = candidate.ReasonCode
		result.Risk = observation
	}
	return result
}
func (e *Engine) evaluateRouting(link, destination string, r *LinkRouting, s Signals, p Protection) Decision {
	if r == nil {
		return e.EvaluateProtection(link, destination, s, p)
	}
	result := Decision{Action: "redirect", ReasonCode: "link_destination", Destination: &destination}
	fallback := func(id *string) Decision {
		result.Action = "fallback"
		result.ReasonCode = "rule_fallback"
		result.RuleID = id
		result.Destination = &r.Fallback
		return result
	}
	detected, status := ipQualityOutcome(r, s)
	if (r.ProxyCheck && (detected || (r.ProxyUnknownFallback && status != "known"))) || s.KnownBot || s.SuspectedAutomation || !match(r.Country, s.Country) || !match(r.Device, s.Device) || !match(r.OS, s.OS) || !match(r.Browser, s.Browser) {
		return fallback(nil)
	}
	required, allowed := map[string]bool{}, map[string]bool{}
	for _, list := range [][]ScopedRule{e.rules, e.links[link]} {
		for _, rule := range list {
			if !rule.Enabled {
				continue
			}
			if rule.Risk != "*" && p.Mode != "enforce" {
				continue
			}
			matches := (rule.ASN == nil || (s.ASN != nil && *rule.ASN == *s.ASN)) && match(rule.Country, s.Country) && match(rule.Device, s.Device) && match(rule.OS, s.OS) && match(rule.Browser, s.Browser) && RiskMatch(rule.Risk, s) && (rule.BotCategory == "" || match(rule.BotCategory, s.BotCategory))
			if rule.Action == "redirect" {
				required[rule.Scope] = true
				allowed[rule.Scope] = allowed[rule.Scope] || matches
			} else if matches {
				id := rule.ID
				return fallback(&id)
			}
		}
	}
	for scope := range required {
		if !allowed[scope] {
			return fallback(nil)
		}
	}
	return result
}
