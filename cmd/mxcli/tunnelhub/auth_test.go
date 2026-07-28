// SPDX-License-Identifier: Apache-2.0

package tunnelhub

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mendixlabs/mxcli/cmd/mxcli/tunnelhub/audit"
)

// testAuth builds an AuthConfig with a fixed clock and no real GitHub endpoints.
// RequireAuth is on so the preview owner-check tests exercise enforcement.
func testAuth(now time.Time) *AuthConfig {
	return &AuthConfig{
		GitHubClientID:     "cid",
		GitHubClientSecret: "csecret",
		SessionSecret:      []byte("test-session-secret-0123456789ab"),
		CookieDomain:       ".mxcli.org",
		HubHost:            "hub.mxcli.org",
		RequireAuth:        true,
		now:                func() time.Time { return now },
	}
}

func TestSignVerifySession_RoundTrip(t *testing.T) {
	secret := []byte("k")
	now := time.Unix(1_700_000_000, 0)
	val := signSession(secret, "alice", now.Add(time.Hour))

	login, ok := verifySession(secret, val, now)
	if !ok || login != "alice" {
		t.Fatalf("round-trip = %q, %v; want alice, true", login, ok)
	}
}

func TestVerifySession_RejectsTamperedSignature(t *testing.T) {
	secret := []byte("k")
	now := time.Unix(1_700_000_000, 0)
	val := signSession(secret, "alice", now.Add(time.Hour))

	// Flip the last character of the signature.
	tampered := val[:len(val)-1] + flip(val[len(val)-1])
	if _, ok := verifySession(secret, tampered, now); ok {
		t.Error("tampered signature must not verify")
	}
	// A different secret must not verify either.
	if _, ok := verifySession([]byte("other"), val, now); ok {
		t.Error("wrong secret must not verify")
	}
	// Garbage without a dot separator.
	if _, ok := verifySession(secret, "nonsense", now); ok {
		t.Error("malformed cookie must not verify")
	}
}

func TestVerifySession_RejectsExpired(t *testing.T) {
	secret := []byte("k")
	issued := time.Unix(1_700_000_000, 0)
	val := signSession(secret, "alice", issued.Add(time.Hour))

	// One second past expiry.
	if _, ok := verifySession(secret, val, issued.Add(time.Hour+time.Second)); ok {
		t.Error("expired session must not verify")
	}
}

func flip(b byte) string {
	if b == 'A' {
		return "B"
	}
	return "A"
}

func TestSignVerifyState_RoundTrip(t *testing.T) {
	a := testAuth(time.Unix(1_700_000_000, 0))
	ret := "https://app.mxcli.org/some/path?q=1"
	state := a.signState(ret)

	got, ok := a.verifyState(state)
	if !ok || got != ret {
		t.Fatalf("state round-trip = %q, %v; want %q, true", got, ok, ret)
	}
}

func TestVerifyState_RejectsExpired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := testAuth(now)
	state := a.signState("https://app.mxcli.org/")

	// Advance the clock past the 10-minute state TTL.
	a.now = func() time.Time { return now.Add(11 * time.Minute) }
	if _, ok := a.verifyState(state); ok {
		t.Error("expired state must not verify")
	}
}

func TestSessionLogin_OpenModeAndInvalid(t *testing.T) {
	// Open mode (no client id): always "".
	open := &AuthConfig{}
	r := httptest.NewRequest(http.MethodGet, "https://app.mxcli.org/", nil)
	if got := open.sessionLogin(r); got != "" {
		t.Errorf("open-mode sessionLogin = %q, want empty", got)
	}

	// Enabled but no cookie: "".
	a := testAuth(time.Unix(1_700_000_000, 0))
	if got := a.sessionLogin(r); got != "" {
		t.Errorf("no-cookie sessionLogin = %q, want empty", got)
	}

	// Enabled, valid cookie: the login.
	w := httptest.NewRecorder()
	a.setSessionCookie(w, "alice")
	r2 := httptest.NewRequest(http.MethodGet, "https://app.mxcli.org/", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}
	if got := a.sessionLogin(r2); got != "alice" {
		t.Errorf("valid-cookie sessionLogin = %q, want alice", got)
	}
}

// stubGitHub stands in for github.com (OAuth) and api.github.com (user), returning
// a fixed access token and login.
func stubGitHub(t *testing.T, login string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"gho_test","token_type":"bearer"}`))
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer gho_test" {
			t.Errorf("user request auth = %q, want Bearer gho_test", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"` + login + `"}`))
	})
	return httptest.NewServer(mux)
}

func TestCallback_SetsSessionAndAudits(t *testing.T) {
	gh := stubGitHub(t, "alice")
	defer gh.Close()

	var buf bytes.Buffer
	now := time.Unix(1_700_000_000, 0)
	a := testAuth(now)
	a.githubOAuthBase = gh.URL
	a.githubAPIBase = gh.URL
	a.httpClient = gh.Client()
	a.Audit = audit.NewWriter(&buf)

	ret := "https://app.mxcli.org/dashboard"
	state := a.signState(ret)
	r := httptest.NewRequest(http.MethodGet, "https://hub.mxcli.org/auth/github/callback?code=abc&state="+url.QueryEscape(state), nil)
	w := httptest.NewRecorder()
	a.handleCallback(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != ret {
		t.Errorf("redirect = %q, want %q", loc, ret)
	}
	// A valid session cookie was set for alice.
	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			found = true
			if login, ok := verifySession(a.SessionSecret, c.Value, now); !ok || login != "alice" {
				t.Errorf("session cookie login = %q, ok=%v; want alice", login, ok)
			}
		}
	}
	if !found {
		t.Error("no session cookie set on successful callback")
	}
	if !strings.Contains(buf.String(), `"event":"login_ok"`) || !strings.Contains(buf.String(), `"login":"alice"`) {
		t.Errorf("expected login_ok audit line, got: %s", buf.String())
	}
}

func TestCallback_InvalidStateIsRejected(t *testing.T) {
	var buf bytes.Buffer
	a := testAuth(time.Unix(1_700_000_000, 0))
	a.Audit = audit.NewWriter(&buf)

	r := httptest.NewRequest(http.MethodGet, "https://hub.mxcli.org/auth/github/callback?code=abc&state=forged", nil)
	w := httptest.NewRecorder()
	a.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("forged state status = %d, want 400", w.Result().StatusCode)
	}
	if !strings.Contains(buf.String(), `"event":"callback_fail"`) {
		t.Errorf("expected callback_fail audit line, got: %s", buf.String())
	}
}

func TestAuthorizePreview_OpenModeAllows(t *testing.T) {
	open := &AuthConfig{} // disabled
	b := &Backend{Subdomain: "app", Owner: "alice"}
	r := httptest.NewRequest(http.MethodGet, "https://app.mxcli.org/", nil)
	w := httptest.NewRecorder()
	if !open.authorizePreview(w, r, b, "app.mxcli.org") {
		t.Error("open mode must always allow")
	}
}

func TestAuthorizePreview_UnownedAllows(t *testing.T) {
	a := testAuth(time.Unix(1_700_000_000, 0))
	b := &Backend{Subdomain: "app"} // no Owner
	r := httptest.NewRequest(http.MethodGet, "https://app.mxcli.org/", nil)
	w := httptest.NewRecorder()
	if !a.authorizePreview(w, r, b, "app.mxcli.org") {
		t.Error("an unowned backend must be public even when auth is on")
	}
}

func TestAuthorizePreview_SoftModeLeavesPreviewsOpen(t *testing.T) {
	// Auth enabled for listing, but RequireAuth off: an owned preview is still
	// reachable by anyone (the listing is filtered, the preview itself is open).
	a := testAuth(time.Unix(1_700_000_000, 0))
	a.RequireAuth = false
	b := &Backend{Subdomain: "app", Owner: "alice"}
	r := httptest.NewRequest(http.MethodGet, "https://app.mxcli.org/", nil)
	w := httptest.NewRecorder()
	if !a.authorizePreview(w, r, b, "app.mxcli.org") {
		t.Error("soft mode (RequireAuth off) must leave previews open")
	}
}

func TestAuthorizePreview_NoSessionRedirectsToLogin(t *testing.T) {
	a := testAuth(time.Unix(1_700_000_000, 0))
	b := &Backend{Subdomain: "app", Owner: "alice"}
	r := httptest.NewRequest(http.MethodGet, "https://app.mxcli.org/dashboard?x=1", nil)
	w := httptest.NewRecorder()

	if a.authorizePreview(w, r, b, "app.mxcli.org") {
		t.Fatal("unauthenticated request must not be allowed")
	}
	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "https://hub.mxcli.org/auth/github/login?return=") {
		t.Errorf("redirect = %q, want a hub login URL", loc)
	}
	if !strings.Contains(loc, url.QueryEscape("https://app.mxcli.org/dashboard?x=1")) {
		t.Errorf("login redirect must carry the original URL as return, got %q", loc)
	}
}

func TestAuthorizePreview_OwnerAllowsOtherDenies(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	b := &Backend{Subdomain: "app", Owner: "alice"}

	// Owner alice: allowed.
	a := testAuth(now)
	w := httptest.NewRecorder()
	a.setSessionCookie(w, "alice")
	r := httptest.NewRequest(http.MethodGet, "https://app.mxcli.org/", nil)
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	if !a.authorizePreview(httptest.NewRecorder(), r, b, "app.mxcli.org") {
		t.Error("the owner must be allowed")
	}

	// Non-owner bob: 403 + access_deny audit line.
	var buf bytes.Buffer
	a2 := testAuth(now)
	a2.Audit = audit.NewWriter(&buf)
	w2 := httptest.NewRecorder()
	a2.setSessionCookie(w2, "bob")
	r2 := httptest.NewRequest(http.MethodGet, "https://app.mxcli.org/", nil)
	for _, c := range w2.Result().Cookies() {
		r2.AddCookie(c)
	}
	deny := httptest.NewRecorder()
	if a2.authorizePreview(deny, r2, b, "app.mxcli.org") {
		t.Fatal("a non-owner must be denied")
	}
	if deny.Result().StatusCode != http.StatusForbidden {
		t.Errorf("non-owner status = %d, want 403", deny.Result().StatusCode)
	}
	if !strings.Contains(buf.String(), `"event":"access_deny"`) ||
		!strings.Contains(buf.String(), `"login":"bob"`) ||
		!strings.Contains(buf.String(), `"owner":"alice"`) {
		t.Errorf("expected access_deny audit line for bob→alice, got: %s", buf.String())
	}
}

// The hub is the TLS edge, so clientIP must use RemoteAddr and must NOT trust a
// client-supplied X-Forwarded-For (which would let a caller spoof audit IPs).
func TestClientIP_UsesRemoteAddrNotForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "https://app.mxcli.org/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1") // attacker-supplied
	if got := clientIP(r); got != "10.0.0.1" {
		t.Errorf("clientIP = %q, want 10.0.0.1 (RemoteAddr, XFF ignored)", got)
	}
}

func TestLogout_ClearsCookieAndAudits(t *testing.T) {
	var buf bytes.Buffer
	a := testAuth(time.Unix(1_700_000_000, 0))
	a.Audit = audit.NewWriter(&buf)

	// GET is rejected.
	rGet := httptest.NewRequest(http.MethodGet, "https://hub.mxcli.org/auth/logout", nil)
	wGet := httptest.NewRecorder()
	a.handleLogout(wGet, rGet)
	if wGet.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET logout status = %d, want 405", wGet.Result().StatusCode)
	}

	// POST clears the cookie and records logout.
	setW := httptest.NewRecorder()
	a.setSessionCookie(setW, "alice")
	r := httptest.NewRequest(http.MethodPost, "https://hub.mxcli.org/auth/logout", nil)
	for _, c := range setW.Result().Cookies() {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.handleLogout(w, r)
	if w.Result().StatusCode != http.StatusNoContent {
		t.Errorf("POST logout status = %d, want 204", w.Result().StatusCode)
	}
	var cleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("logout must expire the session cookie")
	}
	if !strings.Contains(buf.String(), `"event":"logout"`) {
		t.Errorf("expected logout audit line, got: %s", buf.String())
	}
}

// TestSessionStateDomainSeparation asserts a session cookie can't be replayed as
// an OAuth state, and vice versa — the two share a secret but not a signing tag.
func TestSessionStateDomainSeparation(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := testAuth(now)

	// A valid session cookie must NOT verify as a state token.
	cookie := signSession(a.SessionSecret, "alice", now.Add(time.Hour))
	if _, ok := a.verifyState(cookie); ok {
		t.Error("a session cookie must not be accepted as an OAuth state")
	}
	// A valid state token must NOT verify as a session cookie.
	state := a.signState("https://app.mxcli.org/")
	if _, ok := verifySession(a.SessionSecret, state, now); ok {
		t.Error("an OAuth state must not be accepted as a session cookie")
	}
}
