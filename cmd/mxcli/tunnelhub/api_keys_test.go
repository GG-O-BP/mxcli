// SPDX-License-Identifier: Apache-2.0

package tunnelhub

import (
	"bytes"
	"encoding/json"
	"net/http"
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
