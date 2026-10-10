package security

import (
	"bytes"
	"testing"
	"time"
)

func TestTOTPReferenceAndReplay(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	// RFC 6238 SHA1 vectors, reduced to the configured six digits.
	for _, v := range []struct {
		seconds int64
		code    string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		step, ok := MatchTOTP(secret, v.code, -1, time.Unix(v.seconds, 0))
		if !ok {
			t.Fatal("reference rejected")
		}
		if _, ok = MatchTOTP(secret, v.code, step, time.Unix(v.seconds, 0)); ok {
			t.Fatal("replay accepted")
		}
	}
	now := time.Unix(3000, 0)
	if _, ok := MatchTOTP(secret, TOTP(secret, 98), -1, now); ok {
		t.Fatal("expired factor accepted")
	}
	if _, ok := MatchTOTP(secret, TOTP(secret, 101), -1, now); !ok {
		t.Fatal("clock tolerance rejected")
	}
}
func TestSecretEncryptionBoundToOwner(t *testing.T) {
	seed := bytes.Repeat([]byte{7}, 32)
	encrypted, err := Seal(seed, "mfa:owner", []byte("private secret"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := OpenSecret(seed, "mfa:owner", encrypted)
	if err != nil || string(raw) != "private secret" {
		t.Fatal("roundtrip failed")
	}
	if _, err = OpenSecret(seed, "mfa:other", encrypted); err == nil {
		t.Fatal("wrong owner accepted")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err = OpenSecret(seed, "mfa:owner", encrypted); err == nil {
		t.Fatal("tampering accepted")
	}
}
