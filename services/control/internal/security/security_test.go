package security

import (
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	password := "a test password with spaces"
	a, b := HashPassword(password), HashPassword(password)
	if a == b || !VerifyPassword(password, a) || VerifyPassword("a different password", a) {
		t.Fatal("salt or password verification failed")
	}
	if strings.Contains(a, password) {
		t.Fatal("password stored verbatim")
	}
	for _, bad := range []string{"plain", strings.Replace(a, "m=65536", "m=999999999", 1), "$argon2id$v=19$m=65536,t=3,p=2$bad$bad"} {
		if VerifyPassword(password, bad) {
			t.Fatal("malformed hash accepted")
		}
	}
}
func TestTokensAndCSRF(t *testing.T) {
	a, b := Token(), Token()
	if a == b || !ValidToken(a) || ValidToken("forged") {
		t.Fatal("invalid token generation")
	}
	if CSRF(a) == CSRF(b) || !Equal(CSRF(a), CSRF(a)) || Equal(CSRF(a), a) {
		t.Fatal("CSRF token not session bound")
	}
	if Digest(a) == a || len(Digest(a)) != 64 {
		t.Fatal("token not hashed")
	}
}
