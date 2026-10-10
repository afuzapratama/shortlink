package policy

import (
	"errors"
	"github.com/hashicorp/golang-lru"
	"github.com/x-way/crawlerdetect"
	"strings"
)

var crawlerCache, _ = lru.New(4096)

// Crawler identifies UA patterns, not verified crawler identity. A spoofed normal UA can bypass this signal.
func Crawler(ua string) (bool, string, bool) {
	if ua == "" || len(ua) > 4096 {
		return false, "unknown", true
	}
	if value, ok := crawlerCache.Get(ua); ok {
		return value.(crawlerResult).bot, value.(crawlerResult).category, false
	}
	bot := crawlerdetect.IsCrawler(ua)
	category := "unknown"
	if bot {
		category = "automation"
		lower := strings.ToLower(ua)
		for _, marker := range []string{"googlebot", "bingbot", "duckduckbot", "yandexbot", "baiduspider"} {
			if strings.Contains(lower, marker) {
				category = "search"
				break
			}
		}
		for _, marker := range []string{"facebookexternalhit", "twitterbot", "slackbot", "discordbot", "telegrambot", "whatsapp"} {
			if strings.Contains(lower, marker) {
				category = "preview"
				break
			}
		}
	}
	crawlerCache.Add(ua, crawlerResult{bot, category})
	return bot, category, false
}

type crawlerResult struct {
	bot      bool
	category string
}
type Protection struct {
	Mode          string `json:"mode"`
	RatePerSecond int    `json:"ratePerSecond"`
	Burst         int    `json:"burst"`
}

func DefaultProtection() Protection { return Protection{Mode: "observe", RatePerSecond: 20, Burst: 40} }
func NormalizeProtection(p Protection) Protection {
	if p.Mode == "" {
		p.Mode = "observe"
	}
	if p.RatePerSecond == 0 {
		p.RatePerSecond = 20
	}
	if p.Burst == 0 {
		p.Burst = 40
	}
	return p
}
func ValidateProtection(p Protection) error {
	if !member(p.Mode, "off|observe|enforce") || p.RatePerSecond < 1 || p.RatePerSecond > 1000 || p.Burst < 1 || p.Burst > 5000 {
		return errors.New("invalid protection mode or rate budget")
	}
	return nil
}

type RiskObservation struct {
	Mode                string   `json:"mode"`
	Signals             []string `json:"signals"`
	BotCategory         string   `json:"botCategory"`
	FeedStatus          string   `json:"feedStatus"`
	TorStatus           string   `json:"torStatus"`
	ReputationStatus    string   `json:"reputationStatus"`
	CandidateAction     string   `json:"candidateAction"`
	CandidateRuleID     *string  `json:"candidateRuleId"`
	CandidateReasonCode string   `json:"candidateReasonCode"`
}

func RiskMatch(kind string, s Signals) bool {
	switch kind {
	case "*":
		return true
	case "known_bot":
		return s.KnownBot
	case "suspected_automation":
		return s.SuspectedAutomation
	case "tor_exit":
		return s.TorExit
	case "reputation_flag":
		return s.ReputationFlag
	case "rate_limited":
		return s.RateLimited
	}
	return false
}
func RiskSignals(s Signals) []string {
	out := []string{}
	for _, kind := range []string{"known_bot", "suspected_automation", "tor_exit", "reputation_flag", "rate_limited"} {
		if RiskMatch(kind, s) {
			out = append(out, kind)
		}
	}
	return out
}

// EvaluateProtection preserves ordinary routing in observation mode while recording the full-rule candidate.
func (e *Engine) EvaluateProtection(link, destination string, s Signals, p Protection) Decision {
	if p.Mode == "off" {
		return e.evaluate(link, destination, s, false)
	}
	if s.BotCategory == "" {
		s.BotCategory = "unknown"
	}
	if s.FeedStatus == "" {
		s.FeedStatus = "unavailable"
	}
	if s.TorStatus == "" {
		s.TorStatus = "unavailable"
	}
	if s.ReputationStatus == "" {
		s.ReputationStatus = "unavailable"
	}
	candidate := e.evaluate(link, destination, s, true)
	result := candidate
	if p.Mode == "observe" {
		result = e.evaluate(link, destination, s, false)
	}
	result.Risk = &RiskObservation{Mode: p.Mode, Signals: RiskSignals(s), BotCategory: s.BotCategory, FeedStatus: s.FeedStatus, TorStatus: s.TorStatus, ReputationStatus: s.ReputationStatus, CandidateAction: candidate.Action, CandidateRuleID: candidate.RuleID, CandidateReasonCode: candidate.ReasonCode}
	return result
}
