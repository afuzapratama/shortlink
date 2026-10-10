package policy

import (
	"fmt"
	"sync"
	"testing"
)

func rule(id, scope string, priority int) ScopedRule {
	return ScopedRule{Rule: Rule{ID: id, Name: id, Priority: priority, Country: "*", Device: "*", OS: "*", Browser: "*", Risk: "*", Action: "block", Enabled: true}, Scope: scope}
}
func TestOrderAndConditions(t *testing.T) {
	d, l := "domain", "link"
	g := rule("global", "global", 50)
	dr := rule("domain", "domain", 50)
	dr.ScopeID = &d
	lr := rule("link", "link", 50)
	lr.ScopeID = &l
	lr.Country = "ID"
	lr.Device = "mobile"
	lr.OS = "ios"
	lr.Browser = "safari"
	engine, err := Compile([]ScopedRule{g, dr, lr}, d, "short.example", map[string]bool{l: true})
	if err != nil {
		t.Fatal(err)
	}
	s := Signals{Country: "ID", Device: "mobile", OS: "ios", Browser: "safari"}
	result := engine.Evaluate(l, "https://example.org", s)
	if *result.RuleID != "link" {
		t.Fatal(result)
	}
	s.OS = "android"
	if got := engine.Evaluate(l, "https://example.org", s); *got.RuleID != "domain" {
		t.Fatal(got)
	}
	g.Mandatory = true
	engine, err = Compile([]ScopedRule{g, lr}, d, "short.example", map[string]bool{l: true})
	if err != nil {
		t.Fatal(err)
	}
	s.OS = "ios"
	if *engine.Evaluate(l, "https://example.org", s).RuleID != "global" {
		t.Fatal("mandatory bypassed")
	}
}
func TestInvalidAndFallback(t *testing.T) {
	r := rule("one", "global", 10)
	r.Action = "fallback"
	r.DestinationSource = "custom"
	r.Destination = "https://example.org/other"
	e, err := Compile([]ScopedRule{r}, "d", "short.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Evaluate("l", "https://base.example", Signals{}); got.Action != "fallback" || *got.Destination != r.Destination {
		t.Fatal(got)
	}
	r.Destination = "https://short.example/loop"
	if _, err = Compile([]ScopedRule{r}, "d", "short.example", nil); err == nil {
		t.Fatal("loop accepted")
	}
	for _, url := range []string{"javascript:alert(1)", "https://a.example:0/", "https://a.example:99999/", "https://user:pass@a.example/", "https://a.example/\n"} {
		if URL(url, "") {
			t.Fatal(url)
		}
	}
	if ValidCountry("ZZ") || ValidCountry("id") || !ValidCountry("ID") {
		t.Fatal("country validation")
	}
	r = rule("one", "global", 10)
	if err = Validate("global", []Rule{r.Rule, r.Rule}); err == nil {
		t.Fatal("duplicates accepted")
	}
	r.Action = "challenge"
	if err = Validate("global", []Rule{r.Rule}); err == nil {
		t.Fatal("challenge accepted")
	}
}
func TestMultipleLinks(t *testing.T) {
	rules := []ScopedRule{}
	ids := map[string]bool{}
	for n := 0; n < 4; n++ {
		id := fmt.Sprint(n)
		ids[id] = true
		for p := 1; p <= 100; p++ {
			r := rule(fmt.Sprintf("%s-%d", id, p), "link", p)
			r.ScopeID = &id
			rules = append(rules, r)
		}
	}
	if _, err := Compile(rules, "d", "short.example", ids); err != nil {
		t.Fatal(err)
	}
}
func TestVisitor(t *testing.T) {
	for _, c := range []struct{ ua, device, os, browser string }{
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", "mobile", "ios", "safari"},
		{"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", "tablet", "ios", "safari"},
		{"Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36", "mobile", "android", "chrome"},
		{"Mozilla/5.0 (Linux; Android 13; SM-X700) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", "tablet", "android", "chrome"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", "desktop", "windows", "chrome"},
		{"", "unknown", "unknown", "unknown"}, {"unrecognized-agent", "unknown", "unknown", "unknown"},
	} {
		got := Detect(c.ua)
		if got.Device != c.device || got.OS != c.os || got.Browser != c.browser {
			t.Errorf("%s: %+v", c.ua, got)
		}
	}
}
func TestASN(t *testing.T) {
	asn := uint32(15169)
	r := rule("asn", "global", 10)
	r.ASN = &asn
	e, err := Compile([]ScopedRule{r}, "d", "short.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []Signals{{Country: "unknown", Device: "unknown", OS: "unknown", Browser: "unknown"}, {Country: "US", Device: "desktop", OS: "linux", Browser: "chrome", ASN: &asn}} {
		got := e.Evaluate("l", "https://example.org", s)
		if (got.Action == "block") != (s.ASN != nil) {
			t.Fatal(got)
		}
	}
}
func TestConcurrentDetector(t *testing.T) {
	var wg sync.WaitGroup
	for n := 0; n < 32; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				s := Detect("Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36")
				if s.Device != "mobile" || s.OS != "android" {
					t.Error(s)
				}
			}
		}()
	}
	wg.Wait()
}
