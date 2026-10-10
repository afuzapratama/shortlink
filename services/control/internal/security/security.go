package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const PasswordMin = 12
const PasswordMax = 256

func ValidPassword(p string) bool { return len(p) >= PasswordMin && len(p) <= PasswordMax }
func Token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func ValidToken(t string) bool {
	b, e := base64.RawURLEncoding.DecodeString(t)
	return e == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == t
}
func Digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func CSRF(token string) string {
	m := hmac.New(sha256.New, []byte(token))
	m.Write([]byte("shortlink-csrf-v1"))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func Equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// Fixed parameters keep malformed database hashes from causing unbounded allocation.
// Future parameter upgrades need an explicitly versioned decoder and rehash policy.
func HashPassword(p string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	key := argon2.IDKey([]byte(p), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}
func VerifyPassword(p, encoded string) bool {
	v := strings.Split(encoded, "$")
	if len(v) != 6 || v[1] != "argon2id" || v[2] != "v=19" || v[3] != "m=65536,t=3,p=2" || len(p) > PasswordMax {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(v[4])
	if e != nil || len(salt) != 16 {
		return false
	}
	expected, e := base64.RawStdEncoding.DecodeString(v[5])
	if e != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(p), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
