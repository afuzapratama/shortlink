package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"shortlink.local/control/internal/security"
	"testing"
	"time"
)

func TestMFAEnrollmentRecoveryAndSessionBoundary(t *testing.T) {
	s, db := setup(t)
	old, _ := login(t, s, nil)
	current, csrf := login(t, s, nil)
	body := func(code string) string {
		b, _ := json.Marshal(map[string]string{"password": testPassword, "code": code})
		return string(b)
	}
	status(t, request(s, "POST", "/v1/admin/security/mfa/setup", body(""), testOrigin, "", current), 403)
	setupResponse := request(s, "POST", "/v1/admin/security/mfa/setup", body(""), testOrigin, csrf, current)
	status(t, setupResponse, 200)
	var provision struct{ Secret string }
	if err := json.Unmarshal(setupResponse.Body.Bytes(), &provision); err != nil {
		t.Fatal(err)
	}
	otp := security.TOTP(provision.Secret, time.Now().Unix()/30)
	confirmation := request(s, "POST", "/v1/admin/security/mfa/confirm", body(otp), testOrigin, csrf, current)
	status(t, confirmation, 200)
	var result struct{ RecoveryCodes []string }
	json.Unmarshal(confirmation.Body.Bytes(), &result)
	if len(result.RecoveryCodes) != 8 {
		t.Fatal("missing recovery")
	}
	status(t, request(s, "GET", "/v1/auth/session", "", "", "", old), 401)
	status(t, request(s, "GET", "/v1/auth/session", "", "", "", current), 200)
	challenge := func() string {
		r := request(s, "POST", "/v1/auth/login", loginBody("owner@example.com", testPassword), testOrigin, "", nil)
		status(t, r, 200)
		var v struct{ Status, ChallengeID string }
		json.Unmarshal(r.Body.Bytes(), &v)
		if v.Status != "mfa_required" {
			t.Fatal("password bypass")
		}
		return v.ChallengeID
	}
	verify := func(id, code string) *http.Cookie {
		b, _ := json.Marshal(map[string]string{"challengeId": id, "code": code})
		r := request(s, "POST", "/v1/auth/mfa/verify", string(b), testOrigin, "", nil)
		status(t, r, 200)
		return r.Result().Cookies()[0]
	}
	id := challenge()
	b, _ := json.Marshal(map[string]string{"challengeId": id, "code": otp})
	status(t, request(s, "POST", "/v1/auth/mfa/verify", string(b), testOrigin, "", nil), 401)
	authenticated := verify(id, result.RecoveryCodes[0])
	status(t, request(s, "GET", "/v1/auth/session", "", "", "", authenticated), 200)
	status(t, request(s, "POST", "/v1/auth/mfa/verify", string(b), testOrigin, "", nil), 401)
	id = challenge()
	b, _ = json.Marshal(map[string]string{"challengeId": id, "code": result.RecoveryCodes[0]})
	status(t, request(s, "POST", "/v1/auth/mfa/verify", string(b), testOrigin, "", nil), 401)
	// Five failed factors permanently close this challenge, even with an unused valid recovery.
	for i := 0; i < 4; i++ {
		status(t, request(s, "POST", "/v1/auth/mfa/verify", string(b), testOrigin, "", nil), 401)
	}
	b, _ = json.Marshal(map[string]string{"challengeId": id, "code": result.RecoveryCodes[1]})
	status(t, request(s, "POST", "/v1/auth/mfa/verify", string(b), testOrigin, "", nil), 401)
	id = challenge()
	if _, err := db.DB.Exec(context.Background(), "UPDATE mfa_challenges SET expires_at=now()-interval '1 second' WHERE challenge_hash=$1", security.Digest(id)); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(map[string]string{"challengeId": id, "code": result.RecoveryCodes[1]})
	status(t, request(s, "POST", "/v1/auth/mfa/verify", string(b), testOrigin, "", nil), 401)
	status(t, request(s, "POST", "/v1/admin/security/mfa/disable", body(result.RecoveryCodes[1]), testOrigin, security.CSRF(authenticated.Value), authenticated), 200)
	status(t, request(s, "GET", "/v1/auth/session", "", "", "", current), 401)
	login(t, s, nil)
}
