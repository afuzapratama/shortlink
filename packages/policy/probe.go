package policy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// ProbeMAC separates readiness authentication from API bearer credentials.
func ProbeMAC(digest, purpose string, payload []byte) string {
	m := hmac.New(sha256.New, []byte(digest))
	m.Write([]byte("shortlink-probe-v1:" + purpose + ":"))
	m.Write(payload)
	return hex.EncodeToString(m.Sum(nil))
}
func ProbeDigest(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(sum[:])
}
func ValidProbeNonce(n string) bool {
	b, e := base64.RawURLEncoding.DecodeString(n)
	return e == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == n
}
func CheckProbeMAC(expected, received string) bool {
	return hmac.Equal([]byte(expected), []byte(received))
}

type ProbeState struct {
	Nonce      string  `json:"nonce"`
	DomainID   string  `json:"domainId"`
	Revision   int64   `json:"revision"`
	GeoRelease *string `json:"geoRelease"`
	Ready      bool    `json:"ready"`
	Version    string  `json:"version"`
}
