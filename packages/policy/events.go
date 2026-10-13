package policy

import "time"

// TrafficEvent contains visitor IP and classified signals, never raw UA or request headers.
type TrafficEvent struct {
	ClientIP        string  `json:"clientIp,omitempty"`
	ASN             *uint32 `json:"asn,omitempty"`
	ASNOrganization string  `json:"asnOrganization,omitempty"`
	Decision
	OccurredAt     time.Time `json:"occurredAt"`
	DomainID       string    `json:"domainId"`
	LinkID         string    `json:"linkId"`
	Country        string    `json:"country"`
	Device         string    `json:"device"`
	OS             string    `json:"os"`
	Browser        string    `json:"browser"`
	Source         string    `json:"source"`
	OSVersion      *string   `json:"osVersion,omitempty"`
	BrowserVersion *string   `json:"browserVersion,omitempty"`
	DeviceBrand    *string   `json:"deviceBrand,omitempty"`
	DeviceModel    *string   `json:"deviceModel,omitempty"`
}
type EventBatch struct {
	Events []TrafficEvent `json:"events"`
}
type BatchAccepted struct {
	Accepted   int    `json:"accepted"`
	Duplicates int    `json:"duplicates"`
	RequestID  string `json:"requestId"`
}
