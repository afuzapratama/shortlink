package httpapi

import (
	"shortlink.local/control/internal/security"
	"shortlink.local/policy"
	"testing"
	"time"
)

func TestEventNetworkFieldsValidation(t *testing.T) {
	destination := "https://example.com/"
	asn := uint32(13335)
	event := policy.TrafficEvent{Decision: policy.Decision{EventID: security.Token(), Action: "redirect", ReasonCode: "link_destination", ConfigRevision: 1, Destination: &destination}, OccurredAt: time.Now(), DomainID: "domain", LinkID: "link", Country: "ID", Device: "mobile", OS: "ios", Browser: "safari", Source: "go"}
	if !validTrafficEvent(event) {
		t.Fatal("legacy event rejected")
	}
	event.ClientIP = "1.1.1.1"
	event.ASN = &asn
	event.ASNOrganization = "CLOUDFLARENET"
	if !validTrafficEvent(event) {
		t.Fatal("network fields rejected")
	}
	event.ClientIP = "2001:4860:4860::8888"
	if !validTrafficEvent(event) {
		t.Fatal("IPv6 rejected")
	}
	for _, ip := range []string{"not-an-ip", "1.1.1.1:443", "::ffff:1.1.1.1", "fe80::1%eth0"} {
		e := event
		e.ClientIP = ip
		if validTrafficEvent(e) {
			t.Fatal("invalid IP accepted", ip)
		}
	}
	e := event
	e.ASNOrganization = "bad\nnetwork"
	if validTrafficEvent(e) {
		t.Fatal("invalid organization accepted")
	}
	e = event
	e.ASN = nil
	if validTrafficEvent(e) {
		t.Fatal("organization without ASN accepted")
	}
	zero := uint32(0)
	e = event
	e.ASN = &zero
	if validTrafficEvent(e) {
		t.Fatal("zero ASN accepted")
	}
}
