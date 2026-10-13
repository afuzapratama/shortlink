package runtime

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var Version = "b20-dev"

var hostnamePattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

type Config struct {
	Listen             string   `json:"listen"`
	ControlURL         string   `json:"controlUrl"`
	DomainID           string   `json:"domainId"`
	Hostname           string   `json:"hostname"`
	Credential         string   `json:"credential"`
	VerificationKey    string   `json:"verificationKey"`
	DataDir            string   `json:"dataDir"`
	TrustedProxies     []string `json:"trustedProxies"`
	Development        bool     `json:"development"`
	GeoIPRequired      bool     `json:"geoipRequired"`
	QueueMaxBytes      int64    `json:"queueMaxBytes,omitempty"`
	QueueMaxAgeSeconds int64    `json:"queueMaxAgeSeconds,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	file, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return c, err
	}
	if info.Mode().Perm()&0077 != 0 {
		return c, errors.New("credential config must have mode 0600")
	}
	if info.Size() > 16<<10 {
		return c, errors.New("agent config exceeds size limit")
	}
	dec := json.NewDecoder(file)
	dec.DisallowUnknownFields()
	if err = dec.Decode(&c); err != nil {
		return c, errors.New("invalid agent config")
	}
	if dec.Decode(new(any)) != io.EOF {
		return c, errors.New("agent config must contain one JSON value")
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if c.QueueMaxBytes < 0 || c.QueueMaxBytes > 1<<30 || c.QueueMaxAgeSeconds < 0 || c.QueueMaxAgeSeconds > 172800 {
		return errors.New("queue limits must be positive; maximum age is 48 hours")
	}
	if c.Listen == "" || c.DomainID == "" || c.DataDir == "" || len(c.Hostname) > 253 || !hostnamePattern.MatchString(c.Hostname) {
		return errors.New("invalid agent binding or paths")
	}
	ip, port, err := net.SplitHostPort(c.Listen)
	if err != nil || port == "" {
		return errors.New("listen must be an IP:port")
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return errors.New("listen must be an IP:port")
	}
	u, err := url.Parse(c.ControlURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("controlUrl must be an origin")
	}
	if u.Scheme != "https" {
		controlIP, e := netip.ParseAddr(u.Hostname())
		if !c.Development || u.Scheme != "http" || e != nil || !controlIP.IsLoopback() {
			return errors.New("controlUrl requires HTTPS; development permits loopback HTTP")
		}
	}
	if c.Development && !addr.IsLoopback() {
		return errors.New("development listener must be loopback")
	}
	key, err := base64.RawURLEncoding.DecodeString(c.VerificationKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("invalid pinned verification key")
	}
	secret, err := base64.RawURLEncoding.DecodeString(c.Credential)
	if err != nil || len(secret) != 32 || base64.RawURLEncoding.EncodeToString(secret) != c.Credential {
		return errors.New("invalid credential")
	}
	for _, prefix := range c.TrustedProxies {
		if _, err := netip.ParsePrefix(prefix); err != nil {
			return errors.New("trustedProxies must contain CIDRs")
		}
	}
	return nil
}
func normalizeHost(host string) string { return strings.ToLower(strings.TrimSuffix(host, ".")) }
