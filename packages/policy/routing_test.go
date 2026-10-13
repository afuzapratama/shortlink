package policy

import "testing"

func TestLinkAudienceFallbackAndScopeIntersection(t *testing.T) {
	domain, link := "domain", "link"
	allow := Rule{ID: "allow", Name: "Indonesia", Priority: 1, Country: "ID", Device: "*", OS: "*", Browser: "*", Risk: "*", Action: "redirect", DestinationSource: "link", Enabled: true}
	deny := allow
	deny.ID = "deny"
	deny.Name = "Desktop"
	deny.Device = "desktop"
	deny.Country = "*"
	deny.Action = "block"
	deny.DestinationSource = ""
	engine, err := Compile([]ScopedRule{{Rule: allow, Scope: "global"}, {Rule: deny, Scope: "domain", ScopeID: &domain}}, domain, "go.example", map[string]bool{link: true})
	if err != nil {
		t.Fatal(err)
	}
	routing := &LinkRouting{Fallback: "https://fallback.example", Country: "*", Device: "*", OS: "ios", Browser: "*"}
	cases := []struct {
		name    string
		signals Signals
		action  string
	}{
		{"allowed", Signals{Country: "ID", Device: "mobile", OS: "ios", Browser: "safari"}, "redirect"},
		{"country outside global audience", Signals{Country: "US", Device: "mobile", OS: "ios", Browser: "safari"}, "fallback"},
		{"unknown OS", Signals{Country: "ID", Device: "mobile", OS: "unknown", Browser: "safari"}, "fallback"},
		{"domain deny", Signals{Country: "ID", Device: "desktop", OS: "ios", Browser: "safari"}, "fallback"},
		{"bot with allowed device", Signals{Country: "ID", Device: "mobile", OS: "ios", Browser: "safari", KnownBot: true}, "fallback"},
		{"automation", Signals{Country: "ID", Device: "mobile", OS: "ios", Browser: "safari", SuspectedAutomation: true}, "fallback"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, mode := range []string{"off", "observe", "enforce"} {
				d := engine.EvaluateRouting(link, "https://main.example", routing, c.signals, Protection{Mode: mode})
				want := "https://main.example"
				if c.action == "fallback" {
					want = routing.Fallback
				}
				if d.Action != c.action || d.Destination == nil || *d.Destination != want {
					t.Fatalf("%s: %+v", mode, d)
				}
				if mode != "off" && d.Risk == nil {
					t.Fatal("risk observation lost")
				}
			}
		})
	}
	if err := ValidateRouting(&LinkRouting{Fallback: "https://GO.EXAMPLE./loop", Country: "*", Device: "*", OS: "*", Browser: "*"}, "go.example"); err == nil {
		t.Fatal("fallback loop accepted")
	}
	if got := engine.EvaluateRouting(link, "https://main.example", nil, Signals{Country: "US", Device: "mobile", OS: "ios", Browser: "safari"}, DefaultProtection()); got.Action != "redirect" {
		t.Fatal("legacy routing changed")
	}
}

func TestProxyFallbackAndExplicitUnknownChoice(t *testing.T) {
	e, err := Compile(nil, "d", "go.example", map[string]bool{"l": true})
	if err != nil {
		t.Fatal(err)
	}
	r := &LinkRouting{Fallback: "https://alternate.example", Country: "*", Device: "*", OS: "*", Browser: "*", ProxyCheck: true, ProxyUnknownFallback: true}
	signals := Signals{Country: "ID", Device: "mobile", OS: "ios", Browser: "safari", ProxyStatus: "known", ProxyDetected: true}
	if d := e.EvaluateRouting("l", "https://main.example", r, signals, DefaultProtection()); d.Action != "fallback" || d.Proxy == nil || !d.Proxy.Detected {
		t.Fatal(d)
	}
	signals.ProxyDetected = false
	if d := e.EvaluateRouting("l", "https://main.example", r, signals, DefaultProtection()); d.Action != "redirect" {
		t.Fatal(d)
	}
	signals.ProxyStatus = "unavailable"
	if d := e.EvaluateRouting("l", "https://main.example", r, signals, DefaultProtection()); d.Action != "fallback" {
		t.Fatal(d)
	}
	r.ProxyUnknownFallback = false
	if d := e.EvaluateRouting("l", "https://main.example", r, signals, DefaultProtection()); d.Action != "redirect" || d.Proxy.Status != "unavailable" {
		t.Fatal(d)
	}
}

func TestIPQualitySelectedFlagsAndUnknown(t *testing.T) {
	engine, _ := Compile(nil, "domain", "short.example", map[string]bool{"link": true})
	all := IPQualityFlags{Proxy: true, VPN: true, Tor: true, Datacenter: true, Botnet: true, Spam: true, Malware: true, Attacker: true}
	for flag := range all.Values() {
		selected := []string{flag}
		r := &LinkRouting{ProxyCheck: true, ProxyUnknownFallback: true, IPQualityBlock: &selected, Fallback: "https://fallback.example", Country: "*", Device: "*", OS: "*", Browser: "*"}
		if err := ValidateRouting(r, "short.example"); err != nil {
			t.Fatal(err)
		}
		signals := Signals{ProxyStatus: "known", IPQuality: &all}
		if d := engine.EvaluateRouting("link", "https://target.example", r, signals, Protection{Mode: "off"}); d.Action != "fallback" || !d.Proxy.Detected {
			t.Fatal(flag, d)
		}
	}
	selected := []string{"proxy"}
	r := &LinkRouting{ProxyCheck: true, ProxyUnknownFallback: true, IPQualityBlock: &selected, Fallback: "https://fallback.example", Country: "*", Device: "*", OS: "*", Browser: "*"}
	s := Signals{ProxyStatus: "known", IPQuality: &IPQualityFlags{Datacenter: true}}
	if d := engine.EvaluateRouting("link", "https://target.example", r, s, Protection{Mode: "off"}); d.Action != "redirect" || d.Proxy.Detected {
		t.Fatal("unselected datacenter denied", d)
	}
	empty := []string{}
	r.IPQualityBlock = &empty
	s.IPQuality = &all
	if d := engine.EvaluateRouting("link", "https://target.example", r, s, Protection{Mode: "off"}); d.Action != "redirect" {
		t.Fatal("empty selection denied", d)
	}
	s.IPQuality = nil
	if d := engine.EvaluateRouting("link", "https://target.example", r, s, Protection{Mode: "off"}); d.Action != "fallback" || d.Proxy.Status != "invalid_response" {
		t.Fatal("missing flags became clean", d)
	}
	invalid := []string{"score"}
	r.IPQualityBlock = &invalid
	if ValidateRouting(r, "short.example") == nil {
		t.Fatal("unknown flag accepted")
	}
}
