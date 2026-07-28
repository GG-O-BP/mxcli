// SPDX-License-Identifier: Apache-2.0

package tunnelhub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mendixlabs/mxcli/cmd/mxcli/tunnelhub/audit"
)

// newAuthedAPI builds an API with auth enabled, a key store, an audit buffer, and
// GitHub stubbed (for the key-mint /user lookup).
func newAuthedAPI(t *testing.T, requireAuth bool) (*API, *bytes.Buffer, func()) {
	t.Helper()
	gh := stubGitHub(t, "alice")
	var buf bytes.Buffer
	sink := audit.NewWriter(&buf)

	auth := testAuth(time.Unix(1_700_000_000, 0))
	auth.RequireAuth = requireAuth
	auth.githubAPIBase = gh.URL
	auth.httpClient = gh.Client()
	auth.Audit = sink

	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	api := NewAPI(APIOptions{
		Registry:   newTestRegistry(clk),
		ControlURL: "https://hub.mxcli.org",
		Auth:       auth,
		Keys:       NewKeyStore(),
		Audit:      sink,
	})
	return api, &buf, gh.Close
}

func mintKey(t *testing.T, api *API, githubToken string) string {
	t.Helper()
	rec := doJSON(t, api, http.MethodPost, "/api/keys", "",
		map[string]string{"Authorization": "Bearer " + githubToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("mint status = %d, body %s", rec.Code, rec.Body)
	}
	var kr KeyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &kr); err != nil {
		t.Fatal(err)
	}
	if kr.Key == "" || kr.Login != "alice" {
		t.Fatalf("mint response = %+v", kr)
	}
	return kr.Key
}

func TestAPI_MintKeyThenRegisterStampsOwner(t *testing.T) {
	api, buf, done := newAuthedAPI(t, true)
	defer done()

	key := mintKey(t, api, "gho_test")

	rec := doJSON(t, api, http.MethodPost, "/api/register",
		`{"project":"MyApp","branch":"main"}`, map[string]string{"X-Hub-Key": key})
	if rec.Code != http.StatusOK {
		t.Fatalf("register status = %d, body %s", rec.Code, rec.Body)
	}

	// The backend is stamped with the owner and the list filters to alice.
	views := api.opts.Registry.List("project", "alice")
	if len(views) != 1 || views[0].Owner != "alice" {
		t.Fatalf("registry after register = %+v", views)
	}
	if got := api.opts.Registry.List("project", "bob"); len(got) != 0 {
		t.Errorf("bob should see none of alice's previews, got %d", len(got))
	}

	out := buf.String()
	if !strings.Contains(out, `"event":"key_mint"`) || !strings.Contains(out, `"event":"register_ok"`) {
		t.Errorf("expected key_mint + register_ok audit lines, got: %s", out)
	}
	if !strings.Contains(out, `"owner":"alice"`) {
		t.Errorf("register_ok should record owner=alice, got: %s", out)
	}
}

func TestAPI_RegisterRequiresKeyWhenRequireAuth(t *testing.T) {
	api, buf, done := newAuthedAPI(t, true)
	defer done()

	// No X-Hub-Key under --require-auth → 401 + register_deny.
	rec := doJSON(t, api, http.MethodPost, "/api/register", `{"project":"A","branch":"main"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("keyless register status = %d, want 401", rec.Code)
	}
	// An unknown key is likewise rejected.
	bad := doJSON(t, api, http.MethodPost, "/api/register", `{"project":"A","branch":"main"}`,
		map[string]string{"X-Hub-Key": "not-a-real-key"})
	if bad.Code != http.StatusUnauthorized {
		t.Errorf("bogus-key register status = %d, want 401", bad.Code)
	}
	if !strings.Contains(buf.String(), `"event":"register_deny"`) {
		t.Errorf("expected register_deny audit line, got: %s", buf.String())
	}
}

func TestAPI_SoftModeAllowsAnonymousRegister(t *testing.T) {
	api, _, done := newAuthedAPI(t, false) // auth on, require-auth off
	defer done()

	rec := doJSON(t, api, http.MethodPost, "/api/register", `{"project":"A","branch":"main"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("soft-mode keyless register status = %d, want 200", rec.Code)
	}
	// It registered without an owner (still visible to everyone).
	if got := api.opts.Registry.List("project", ""); len(got) != 1 || got[0].Owner != "" {
		t.Errorf("soft-mode register should be owner-less, got %+v", got)
	}
}

func TestAPI_RevokedKeyCannotRegister(t *testing.T) {
	api, buf, done := newAuthedAPI(t, true)
	defer done()

	key := mintKey(t, api, "gho_test")
	// Revoke it.
	rev := doJSON(t, api, http.MethodDelete, "/api/keys", "", map[string]string{"X-Hub-Key": key})
	if rev.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", rev.Code)
	}
	// The revoked key no longer authorizes registration.
	rec := doJSON(t, api, http.MethodPost, "/api/register", `{"project":"A","branch":"main"}`,
		map[string]string{"X-Hub-Key": key})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("register with revoked key status = %d, want 401", rec.Code)
	}
	if !strings.Contains(buf.String(), `"event":"key_revoke"`) {
		t.Errorf("expected key_revoke audit line, got: %s", buf.String())
	}
}

func TestAPI_MintKeyRejectsBadGitHubToken(t *testing.T) {
	// A stub that fails the /user lookup (no token match) → fetchLogin errors.
	api, _, done := newAuthedAPI(t, true)
	defer done()

	// Missing bearer → 401.
	if rec := doJSON(t, api, http.MethodPost, "/api/keys", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("mint without token status = %d, want 401", rec.Code)
	}
}

func TestAPI_KeysDisabledInOpenMode(t *testing.T) {
	// Open mode: no Auth → /api/keys is 404 (keys only exist with GitHub OAuth).
	api, _ := newTestAPI(t, "")
	if rec := doJSON(t, api, http.MethodPost, "/api/keys", "",
		map[string]string{"Authorization": "Bearer x"}); rec.Code != http.StatusNotFound {
		t.Errorf("open-mode mint status = %d, want 404", rec.Code)
	}
}

func TestAPI_OpenModeRegisterUnchanged(t *testing.T) {
	// With no Auth, registration still works with (or without) the legacy secret
	// and stamps no owner — byte-for-byte the pre-auth behaviour.
	api, _ := newTestAPI(t, "")
	rec := doJSON(t, api, http.MethodPost, "/api/register", `{"project":"A","branch":"main"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("open-mode register status = %d, want 200", rec.Code)
	}
	if got := api.opts.Registry.List("project", ""); len(got) != 1 || got[0].Owner != "" {
		t.Errorf("open-mode register should be owner-less, got %+v", got)
	}
}

func TestAPI_AuthConfig(t *testing.T) {
	// Open mode: authEnabled false, no client id.
	open, _ := newTestAPI(t, "")
	rec := doJSON(t, open, http.MethodGet, "/api/auth-config", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("open auth-config status = %d", rec.Code)
	}
	var oc AuthConfigResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &oc)
	if oc.AuthEnabled || oc.GitHubClientID != "" {
		t.Errorf("open mode auth-config = %+v, want disabled/empty", oc)
	}

	// Auth on: exposes the client id.
	api, _, done := newAuthedAPI(t, true)
	defer done()
	r2 := doJSON(t, api, http.MethodGet, "/api/auth-config", "", nil)
	var ac AuthConfigResponse
	_ = json.Unmarshal(r2.Body.Bytes(), &ac)
	if !ac.AuthEnabled || !ac.RequireAuth || ac.GitHubClientID != "cid" {
		t.Errorf("authed auth-config = %+v, want enabled/require/cid", ac)
	}
}

// TestAPI_BackendsRequiresAuthWhenEnabled is the fix for the anonymous-listing
// leak: with auth on, an unauthenticated GET /api/backends must 401 (not return
// every user's previews). A valid session gets its own filtered list.
func TestAPI_BackendsRequiresAuthWhenEnabled(t *testing.T) {
	api, _, done := newAuthedAPI(t, true)
	defer done()

	// Register a preview owned by alice (via a minted key).
	key := mintKey(t, api, "gho_test")
	if rec := doJSON(t, api, http.MethodPost, "/api/register",
		`{"project":"Secret","branch":"main"}`, map[string]string{"X-Hub-Key": key}); rec.Code != http.StatusOK {
		t.Fatalf("register status = %d", rec.Code)
	}

	// Anonymous (no cookie) must NOT get the listing.
	anon := doJSON(t, api, http.MethodGet, "/api/backends", "", nil)
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous /api/backends = %d, want 401 (leak!)", anon.Code)
	}
	if strings.Contains(anon.Body.String(), "Secret") {
		t.Errorf("anonymous response leaked a preview: %s", anon.Body)
	}

	// A valid alice session sees her own preview.
	cookie := signSession(api.opts.Auth.SessionSecret, "alice", time.Unix(1_700_000_000, 0).Add(time.Hour))
	ok := doJSON(t, api, http.MethodGet, "/api/backends", "", map[string]string{"Cookie": sessionCookieName + "=" + cookie})
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), "Secret") {
		t.Errorf("alice /api/backends = %d body %s, want 200 with her preview", ok.Code, ok.Body)
	}
}

func TestAPI_Whoami(t *testing.T) {
	// Open mode: authEnabled false, no login.
	open, _ := newTestAPI(t, "")
	rec := doJSON(t, open, http.MethodGet, "/api/whoami", "", nil)
	var wo WhoamiResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &wo)
	if wo.AuthEnabled || wo.Login != "" {
		t.Errorf("open whoami = %+v, want disabled/empty", wo)
	}

	// Auth on, no session → authEnabled true, empty login.
	api, _, done := newAuthedAPI(t, true)
	defer done()
	r1 := doJSON(t, api, http.MethodGet, "/api/whoami", "", nil)
	var w1 WhoamiResponse
	_ = json.Unmarshal(r1.Body.Bytes(), &w1)
	if !w1.AuthEnabled || w1.Login != "" {
		t.Errorf("no-session whoami = %+v, want enabled + empty login", w1)
	}

	// Auth on, valid session cookie → the login.
	rec2 := httptest.NewRecorder()
	api.opts.Auth.setSessionCookie(rec2, "alice")
	mux := http.NewServeMux()
	api.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	for _, c := range rec2.Result().Cookies() {
		req.AddCookie(c)
	}
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)
	var w2 WhoamiResponse
	_ = json.Unmarshal(rw.Body.Bytes(), &w2)
	if !w2.AuthEnabled || w2.Login != "alice" {
		t.Errorf("session whoami = %+v, want alice", w2)
	}
}

// TestAPI_RegisterSecretFallbackWhenAuthOn covers finding #31B: with auth on, a
// valid X-Hub-Secret registers owner-less (the legacy secret stays a meaningful
// credential), while a wrong/absent one is refused.
func TestAPI_RegisterSecretFallbackWhenAuthOn(t *testing.T) {
	api, buf, done := newAuthedAPI(t, true) // require-auth on
	defer done()
	api.opts.RegisterSecret = "s3cret"

	// Valid shared secret, no key → 200, owner-less.
	ok := doJSON(t, api, http.MethodPost, "/api/register", `{"project":"A","branch":"main"}`,
		map[string]string{"X-Hub-Secret": "s3cret"})
	if ok.Code != http.StatusOK {
		t.Fatalf("secret register status = %d, want 200 (body %s)", ok.Code, ok.Body)
	}
	if got := api.opts.Registry.List("project", ""); len(got) != 1 || got[0].Owner != "" {
		t.Errorf("secret register should be owner-less, got %+v", got)
	}
	// Wrong secret, no key → 401 + register_deny.
	bad := doJSON(t, api, http.MethodPost, "/api/register", `{"project":"B","branch":"main"}`,
		map[string]string{"X-Hub-Secret": "nope"})
	if bad.Code != http.StatusUnauthorized {
		t.Errorf("wrong-secret status = %d, want 401", bad.Code)
	}
	if !strings.Contains(buf.String(), `"event":"register_deny"`) {
		t.Errorf("expected register_deny audit line, got: %s", buf.String())
	}
}

// TestAPI_SoftModeWithSecretRequiresIt covers the flip side of #31B: in soft mode
// (require-auth off) a configured secret still gates — a keyless, secretless
// registration is refused rather than silently allowed.
func TestAPI_SoftModeWithSecretRequiresIt(t *testing.T) {
	api, _, done := newAuthedAPI(t, false) // soft
	defer done()
	api.opts.RegisterSecret = "s3cret"

	if rec := doJSON(t, api, http.MethodPost, "/api/register", `{"project":"A"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("soft-mode + secret, no creds: status = %d, want 401", rec.Code)
	}
	if rec := doJSON(t, api, http.MethodPost, "/api/register", `{"project":"A"}`,
		map[string]string{"X-Hub-Secret": "s3cret"}); rec.Code != http.StatusOK {
		t.Errorf("soft-mode + valid secret: status = %d, want 200", rec.Code)
	}
}

// TestAPI_BrowserMint covers the session-cookie mint path (the /cli page): a
// signed-in browser mints a key with the X-Hub-Mint header; without the header
// (CSRF) it is refused; a cross-origin Origin is refused.
func TestAPI_BrowserMint(t *testing.T) {
	api, buf, done := newAuthedAPI(t, true)
	defer done()

	// Build a request carrying a valid session cookie for alice.
	setW := httptest.NewRecorder()
	api.opts.Auth.setSessionCookie(setW, "alice")
	cookies := setW.Result().Cookies()
	withCookie := func(req *http.Request) {
		for _, c := range cookies {
			req.AddCookie(c)
		}
	}
	mux := http.NewServeMux()
	api.Mount(mux)

	// (1) Cookie + X-Hub-Mint header + same-origin → 200, minted for alice.
	req := httptest.NewRequest(http.MethodPost, "https://hub.mxcli.org/api/keys", nil)
	req.Header.Set("X-Hub-Mint", "1")
	req.Header.Set("Origin", "https://hub.mxcli.org")
	withCookie(req)
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("browser mint status = %d, want 200 (body %s)", rw.Code, rw.Body)
	}
	var kr KeyResponse
	_ = json.Unmarshal(rw.Body.Bytes(), &kr)
	if kr.Login != "alice" || kr.Key == "" {
		t.Errorf("mint response = %+v", kr)
	}
	if login, ok := api.opts.Keys.Resolve(kr.Key); !ok || login != "alice" {
		t.Errorf("minted key resolves to %q,%v; want alice", login, ok)
	}
	if !strings.Contains(buf.String(), `"event":"key_mint"`) {
		t.Errorf("expected key_mint audit line")
	}

	// (2) Cookie but NO X-Hub-Mint header (CSRF) → 403.
	noHdr := httptest.NewRequest(http.MethodPost, "https://hub.mxcli.org/api/keys", nil)
	withCookie(noHdr)
	rw2 := httptest.NewRecorder()
	mux.ServeHTTP(rw2, noHdr)
	if rw2.Code != http.StatusForbidden {
		t.Errorf("no-header cookie mint status = %d, want 403", rw2.Code)
	}

	// (3) Cookie + header but cross-origin → 403.
	xorig := httptest.NewRequest(http.MethodPost, "https://hub.mxcli.org/api/keys", nil)
	xorig.Header.Set("X-Hub-Mint", "1")
	xorig.Header.Set("Origin", "https://evil.example.com")
	withCookie(xorig)
	rw3 := httptest.NewRecorder()
	mux.ServeHTTP(rw3, xorig)
	if rw3.Code != http.StatusForbidden {
		t.Errorf("cross-origin cookie mint status = %d, want 403", rw3.Code)
	}

	// (4) No cookie, no bearer → 401.
	anon := httptest.NewRequest(http.MethodPost, "https://hub.mxcli.org/api/keys", nil)
	anon.Header.Set("X-Hub-Mint", "1")
	rw4 := httptest.NewRecorder()
	mux.ServeHTTP(rw4, anon)
	if rw4.Code != http.StatusUnauthorized {
		t.Errorf("anon mint status = %d, want 401", rw4.Code)
	}
}
