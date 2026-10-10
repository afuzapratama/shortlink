// Package risk publishes signed local threat signals. Absence is unknown, never proof of a clean proxy.
package risk

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const MaxBytes = 8 << 20
const TorURL = "https://check.torproject.org/torbulkexitlist"

var ErrCredential = errors.New("feed credential rejected")

type Feed struct {
	Schema              int        `json:"schema"`
	PublishedAt         time.Time  `json:"publishedAt"`
	ExpiresAt           time.Time  `json:"expiresAt"`
	TorFetchedAt        *time.Time `json:"torFetchedAt"`
	ReputationFetchedAt *time.Time `json:"reputationFetchedAt"`
	Tor                 []string   `json:"tor"`
	Reputation          []string   `json:"reputation"`
}
type Envelope struct {
	Version       string `json:"version"`
	SignedPayload string `json:"signedPayload"`
	Signature     string `json:"signature"`
}
type Index struct {
	exact    map[netip.Addr]bool
	prefixes map[int]map[netip.Prefix]bool
	bits     []int
}

func NewIndex(values []string) (*Index, error) {
	i := &Index{exact: map[netip.Addr]bool{}, prefixes: map[int]map[netip.Prefix]bool{}}
	if len(values) > 100000 {
		return nil, errors.New("feed exceeds 100000 entries")
	}
	for _, raw := range values {
		if ip, e := netip.ParseAddr(raw); e == nil {
			ip = ip.Unmap()
			if !publicIP(ip) {
				return nil, errors.New("feed contains non-public IP")
			}
			i.exact[ip] = true
			continue
		}
		p, e := netip.ParsePrefix(raw)
		if e != nil || p.Addr().Is4In6() || p != p.Masked() || !publicIP(p.Addr()) || (p.Addr().Is4() && p.Bits() < 16) || (p.Addr().Is6() && p.Bits() < 32) {
			return nil, errors.New("invalid or overly broad feed prefix")
		}
		if i.prefixes[p.Bits()] == nil {
			i.prefixes[p.Bits()] = map[netip.Prefix]bool{}
			i.bits = append(i.bits, p.Bits())
		}
		i.prefixes[p.Bits()][p] = true
	}
	slices.Sort(i.bits)
	return i, nil
}
func publicIP(ip netip.Addr) bool {
	return ip.IsValid() && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}
func (i *Index) Contains(ip netip.Addr) bool {
	ip = ip.Unmap()
	if i.exact[ip] {
		return true
	}
	for _, bits := range i.bits {
		if bits > ip.BitLen() {
			continue
		}
		if i.prefixes[bits][netip.PrefixFrom(ip, bits).Masked()] {
			return true
		}
	}
	return false
}
func ValidVersion(v string) bool {
	if len(v) != 64 {
		return false
	}
	_, e := hex.DecodeString(v)
	return e == nil && v == strings.ToLower(v)
}
func Sign(f Feed, key ed25519.PrivateKey) (Envelope, error) {
	if _, e := NewIndex(f.Tor); e != nil {
		return Envelope{}, e
	}
	if _, e := NewIndex(f.Reputation); e != nil {
		return Envelope{}, e
	}
	raw, e := json.Marshal(f)
	if e != nil || len(raw) > MaxBytes*3/4 {
		return Envelope{}, errors.New("feed payload exceeds limit")
	}
	hash := sha256.Sum256(raw)
	return Envelope{Version: hex.EncodeToString(hash[:]), SignedPayload: base64.RawURLEncoding.EncodeToString(raw), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, raw))}, nil
}
func Verify(raw []byte, key ed25519.PublicKey, now time.Time, fresh bool) (Envelope, Feed, error) {
	var e Envelope
	var f Feed
	if len(raw) > MaxBytes || json.Unmarshal(raw, &e) != nil || !ValidVersion(e.Version) {
		return e, f, errors.New("invalid feed envelope")
	}
	payload, err := base64.RawURLEncoding.DecodeString(e.SignedPayload)
	if err != nil {
		return e, f, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(e.Signature)
	if err != nil || !ed25519.Verify(key, payload, sig) {
		return e, f, errors.New("invalid feed signature")
	}
	hash := sha256.Sum256(payload)
	if e.Version != hex.EncodeToString(hash[:]) || json.Unmarshal(payload, &f) != nil || f.Schema != 1 || f.PublishedAt.IsZero() || f.PublishedAt.After(now.Add(5*time.Minute)) || !f.ExpiresAt.After(f.PublishedAt) || f.ExpiresAt.Sub(f.PublishedAt) > 48*time.Hour || (fresh && !now.Before(f.ExpiresAt)) {
		return e, f, errors.New("invalid or expired feed")
	}
	if _, err = NewIndex(f.Tor); err != nil {
		return e, f, err
	}
	if _, err = NewIndex(f.Reputation); err != nil {
		return e, f, err
	}
	for _, t := range []*time.Time{f.TorFetchedAt, f.ReputationFetchedAt} {
		if t != nil && (t.After(now.Add(5*time.Minute)) || t.IsZero()) {
			return e, f, errors.New("invalid source timestamp")
		}
	}
	if len(f.Tor) > 0 && f.TorFetchedAt == nil || len(f.Reputation) > 0 && f.ReputationFetchedAt == nil {
		return e, f, errors.New("missing source timestamp")
	}
	return e, f, nil
}
func AtomicFile(path string, raw []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".feed-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(0600); e != nil {
		return e
	}
	if _, e = f.Write(raw); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func Read(root string, key ed25519.PublicKey, now time.Time, fresh bool) (Envelope, Feed, []byte, error) {
	file, e := os.Open(filepath.Join(root, "current.json"))
	if e != nil {
		return Envelope{}, Feed{}, nil, e
	}
	defer file.Close()
	raw, e := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if e != nil {
		return Envelope{}, Feed{}, nil, e
	}
	env, feed, e := Verify(raw, key, now, fresh)
	return env, feed, raw, e
}
func ParseList(raw []byte) ([]string, error) {
	if len(raw) > 4<<20 {
		return nil, errors.New("source exceeds size limit")
	}
	values := []string{}
	seen := map[string]bool{}
	scan := bufio.NewScanner(bytes.NewReader(raw))
	scan.Buffer(make([]byte, 4096), 1024)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, e := NewIndex([]string{line}); e != nil {
			return nil, e
		}
		if !seen[line] {
			seen[line] = true
			values = append(values, line)
		}
		if len(values) > 100000 {
			return nil, errors.New("source exceeds entry limit")
		}
	}
	if e := scan.Err(); e != nil {
		return nil, e
	}
	slices.Sort(values)
	return values, nil
}

// RefreshTor downloads only the fixed official HTTPS URL, never user-controlled URLs or per-click lookups.
func RefreshTor(ctx context.Context, root string, now time.Time) error {
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, "GET", TorURL, nil)
	if e != nil {
		return e
	}
	response, e := client.Do(req)
	if e != nil {
		return errors.New("Tor source unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("Tor source HTTP %d", response.StatusCode)
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if e != nil {
		return e
	}
	values, e := ParseList(raw)
	if e != nil || len(values) < 10 {
		return errors.New("Tor source invalid/empty")
	}
	// A drastic shrink is quarantined; retain the last healthy source for operator review.
	old, err := os.ReadFile(filepath.Join(root, "staging", "tor.txt"))
	if err == nil {
		previous, er := ParseList(old)
		if er == nil && len(previous) > 100 && (len(values)*4 < len(previous) || len(values) > len(previous)*4) {
			return errors.New("Tor source unexpectedly shrank")
		}
	}
	return AtomicFile(filepath.Join(root, "staging", "tor.txt"), []byte(strings.Join(values, "\n")+"\n"))
}
func Publish(root string, seed []byte, now time.Time) (Envelope, Feed, error) {
	key := ed25519.NewKeyFromSeed(seed)
	f := Feed{Schema: 1, PublishedAt: now.UTC(), ExpiresAt: now.UTC().Add(48 * time.Hour), Tor: []string{}, Reputation: []string{}}
	for name, target := range map[string]*[]string{"tor": &f.Tor, "reputation": &f.Reputation} {
		path := filepath.Join(root, "staging", name+".txt")
		file, e := os.Open(path)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return Envelope{}, f, e
		}
		info, e := file.Stat()
		if e != nil {
			file.Close()
			return Envelope{}, f, e
		}
		raw, e := io.ReadAll(io.LimitReader(file, (4<<20)+1))
		file.Close()
		if e != nil {
			return Envelope{}, f, e
		}
		*target, e = ParseList(raw)
		if e != nil {
			return Envelope{}, f, e
		}
		t := info.ModTime().UTC()
		if t.After(now.Add(5 * time.Minute)) {
			return Envelope{}, f, errors.New("source timestamp is in future")
		}
		if name == "tor" {
			f.TorFetchedAt = &t
		} else {
			f.ReputationFetchedAt = &t
		}
	}
	oldEnv, old, _, err := Read(root, key.Public().(ed25519.PublicKey), now, false)
	if err == nil && old.ExpiresAt.After(now.Add(time.Hour)) && slices.Equal(old.Tor, f.Tor) && slices.Equal(old.Reputation, f.Reputation) && sameTime(old.TorFetchedAt, f.TorFetchedAt) && sameTime(old.ReputationFetchedAt, f.ReputationFetchedAt) {
		return oldEnv, old, nil
	}
	env, e := Sign(f, key)
	if e != nil {
		return env, f, e
	}
	raw, _ := json.Marshal(env)
	if e = AtomicFile(filepath.Join(root, "current.json"), raw); e != nil {
		return env, f, e
	}
	return env, f, nil
}
func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
