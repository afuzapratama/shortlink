package policy

import (
	"strings"
	"testing"
)

func TestCrawlerCategoriesAndSpoofBoundary(t *testing.T) {
	for _, tc := range []struct {
		ua       string
		bot      bool
		category string
		suspect  bool
	}{
		{"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", true, "search", false},
		{"facebookexternalhit/1.1", true, "preview", false},
		{"curl/8.0.1", true, "automation", false},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile/15E148 Safari/604.1", false, "unknown", false},
		{"", false, "unknown", true}, {strings.Repeat("x", 4097), false, "unknown", true},
	} {
		for i := 0; i < 2; i++ {
			s := Detect(tc.ua)
			if s.KnownBot != tc.bot || s.BotCategory != tc.category || s.SuspectedAutomation != tc.suspect {
				t.Fatalf("classification %+v", s)
			}
		}
	}
}
func TestObserveEnforceAndOrdinaryMandatory(t *testing.T) {
	r := rule("crawler", "global", 1)
	r.Risk = "known_bot"
	engine, e := Compile([]ScopedRule{r}, "d", "short.example", map[string]bool{"l": true})
	if e != nil {
		t.Fatal(e)
	}
	signals := Signals{KnownBot: true, BotCategory: "search", FeedStatus: "fresh", TorStatus: "fresh", ReputationStatus: "unconfigured"}
	p := DefaultProtection()
	d := engine.EvaluateProtection("l", "https://example.org", signals, p)
	if d.Action != "redirect" || d.Risk == nil || d.Risk.CandidateAction != "block" || d.Risk.CandidateRuleID == nil || *d.Risk.CandidateRuleID != "crawler" {
		t.Fatal(d)
	}
	p.Mode = "enforce"
	d = engine.EvaluateProtection("l", "https://example.org", signals, p)
	if d.Action != "block" {
		t.Fatal(d)
	}
	p.Mode = "off"
	d = engine.EvaluateProtection("l", "https://example.org", signals, p)
	if d.Action != "redirect" || d.Risk != nil {
		t.Fatal(d)
	}
	r.Mandatory = true
	if Validate("global", []Rule{r.Rule}) == nil {
		t.Fatal("mandatory risk mode bypass allowed")
	}
	r.Risk = "*"
	engine, e = Compile([]ScopedRule{r}, "d", "short.example", map[string]bool{"l": true})
	if e != nil {
		t.Fatal(e)
	}
	if d = engine.EvaluateProtection("l", "https://example.org", signals, p); d.Action != "block" {
		t.Fatal("ordinary mandatory bypassed")
	}
}

func TestPreviewCategoryAndStrictEnums(t *testing.T) {
	r := rule("preview", "global", 1)
	r.Risk = "known_bot"
	r.BotCategory = "preview"
	e, err := Compile([]ScopedRule{r}, "d", "short.example", map[string]bool{"l": true})
	if err != nil {
		t.Fatal(err)
	}
	p := DefaultProtection()
	p.Mode = "enforce"
	if d := e.EvaluateProtection("l", "https://example.org", Detect("Googlebot/2.1"), p); d.Action != "redirect" {
		t.Fatal("search crawler matched preview rule")
	}
	if d := e.EvaluateProtection("l", "https://example.org", Detect("facebookexternalhit/1.1"), p); d.Action != "block" {
		t.Fatal("preview not recognized")
	}
	r.Device = "mobile|desktop"
	if Validate("global", []Rule{r.Rule}) == nil {
		t.Fatal("compound enum accepted")
	}
}
