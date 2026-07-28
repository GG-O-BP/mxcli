// SPDX-License-Identifier: Apache-2.0

package tunnelhub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/mendixlabs/mxcli/cmd/mxcli/tunnelhub/audit"
)

// APIOptions configures the registration API.
type APIOptions struct {
	Registry *Registry
	// ControlURL is where the client points its chisel control connection
	// (e.g. https://hub.example.com). Returned in the registration response.
	ControlURL string
	// TunnelAuth is the shared chisel auth ("user:pass") the client must use, if
	// the hub's tunnel server requires one. Returned to the client.
	TunnelAuth string
	// RegisterSecret, if set, gates /api/register: the client must send a matching
	// X-Hub-Secret header (from --hub-secret). Empty means open registration.
	RegisterSecret string
	// HeartbeatIntervalSec is how often the client should heartbeat (default 20).
	HeartbeatIntervalSec int
	// Auth, when enabled, resolves the viewer's GitHub login from the session
	// cookie so /api/backends is filtered to the caller's own previews. Nil or
	// open-mode → "" (list everything, today's behaviour).
	Auth *AuthConfig
	// Keys is the hub API-key store backing /api/keys and X-Hub-Key registration.
	// Nil disables the key endpoints (open mode uses the legacy X-Hub-Secret).
	Keys *KeyStore
	// Audit receives key/registration events. Nil → audit.NoOp().
	Audit audit.Sink
}

// API serves the hub's registration + query endpoints over the registry.
type API struct {
	opts APIOptions
}

// KeyResponse is returned by POST /api/keys after a successful mint. The key is
// shown exactly once; the client caches it in ~/.mxcli/auth.json.
type KeyResponse struct {
	Key   string `json:"key"`
	Login string `json:"login"`
}

// KeyListResponse is returned by GET /api/keys: how many active keys the caller
// has (for the /cli page's "you have N keys").
type KeyListResponse struct {
	Login string `json:"login"`
	Count int    `json:"count"`
}

// KeyRevokeResponse is returned by a session-authed DELETE /api/keys (revoke-all).
type KeyRevokeResponse struct {
	Revoked int `json:"revoked"`
}

// RegisterResponse is returned to `mxcli run --hub` after registration.
type RegisterResponse struct {
	Subdomain            string `json:"subdomain"`
	URL                  string `json:"url"`
	ReversePort          int    `json:"reversePort"`
	ControlURL           string `json:"controlUrl"`
	Token                string `json:"token"`
	TunnelAuth           string `json:"tunnelAuth,omitempty"`
	HeartbeatIntervalSec int    `json:"heartbeatIntervalSec"`
}

// NewAPI builds the API handler set.
func NewAPI(o APIOptions) *API {
	if o.HeartbeatIntervalSec == 0 {
		o.HeartbeatIntervalSec = 20
	}
	if o.Audit == nil {
		o.Audit = audit.NoOp()
	}
	return &API{opts: o}
}

// Mount registers the API routes on mux under /api/.
func (a *API) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/api/register", a.handleRegister)
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/deregister", a.handleDeregister)
	mux.HandleFunc("/api/backends", a.handleBackends)
	mux.HandleFunc("/api/keys", a.handleKeys)
	mux.HandleFunc("/api/auth-config", a.handleAuthConfig)
	mux.HandleFunc("/api/whoami", a.handleWhoami)
}

// WhoamiResponse tells the admin page who the current session belongs to, so a
// signed-in viewer can confirm their identity (and see a Sign-out control).
type WhoamiResponse struct {
	AuthEnabled bool   `json:"authEnabled"`
	Login       string `json:"login,omitempty"`
}

// handleWhoami (GET /api/whoami) returns the authenticated GitHub login for the
// current session, or an empty login in open mode / when unauthenticated.
func (a *API) handleWhoami(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := WhoamiResponse{AuthEnabled: a.opts.Auth.enabled()}
	if resp.AuthEnabled {
		resp.Login = a.opts.Auth.sessionLogin(r)
	}
	writeJSON(w, http.StatusOK, resp)
}

// AuthConfigResponse tells a client whether the hub requires GitHub auth and, if
// so, which OAuth App client id to use for the device flow. The client id is a
// public value (it appears in every browser redirect); no secret is exposed.
type AuthConfigResponse struct {
	AuthEnabled    bool   `json:"authEnabled"`
	RequireAuth    bool   `json:"requireAuth"`
	GitHubClientID string `json:"githubClientId,omitempty"`
}

// handleAuthConfig (GET /api/auth-config) lets `mxcli auth hub login` discover the
// hub's OAuth App client id so the user needn't configure one. Open-mode hubs
// report authEnabled:false and the client falls back to the shared secret.
func (a *API) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := AuthConfigResponse{}
	if a.opts.Auth.enabled() {
		resp.AuthEnabled = true
		resp.RequireAuth = a.opts.Auth.RequireAuth
		resp.GitHubClientID = a.opts.Auth.GitHubClientID
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Authenticate the registrant and (when auth is on) learn the owner login.
	owner, ok := a.authorizeRegister(w, r)
	if !ok {
		return // authorizeRegister has written the 401 + a register_deny audit line
	}
	var req RegisterRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Project) == "" {
		http.Error(w, "project is required", http.StatusBadRequest)
		return
	}
	req.Owner = owner // server-derived; never trusted from the body
	b, err := a.opts.Registry.Register(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	a.opts.Audit.Log(audit.Event{
		Event: audit.EventRegisterOK, Login: owner, IP: clientIP(r),
		Subdomain: b.Subdomain, Owner: owner, Outcome: "ok",
	})
	host := b.Subdomain
	if d := a.opts.Registry.domain; d != "" {
		host = b.Subdomain + "." + d
	}
	writeJSON(w, http.StatusOK, RegisterResponse{
		Subdomain:            b.Subdomain,
		URL:                  "https://" + host,
		ReversePort:          b.ReversePort,
		ControlURL:           a.opts.ControlURL,
		Token:                b.ID,
		TunnelAuth:           a.opts.TunnelAuth,
		HeartbeatIntervalSec: a.opts.HeartbeatIntervalSec,
	})
}

// authorizeRegister decides whether a /api/register call may proceed and, when
// auth is on, resolves the X-Hub-Key to the owner login. It writes the 401 +
// register_deny audit line itself on rejection and returns ok=false.
//
//   - Open mode (auth off): the legacy X-Hub-Secret gate; owner is "".
//   - Auth on: an X-Hub-Key is resolved to a login and stamped as owner. With
//     --require-auth (default) a missing/invalid key is rejected; in soft mode a
//     keyless registration is allowed but stays owner-less.
func (a *API) authorizeRegister(w http.ResponseWriter, r *http.Request) (owner string, ok bool) {
	if !a.opts.Auth.enabled() {
		if a.opts.RegisterSecret != "" && r.Header.Get("X-Hub-Secret") != a.opts.RegisterSecret {
			http.Error(w, "invalid or missing hub secret", http.StatusUnauthorized)
			return "", false
		}
		return "", true
	}
	// A valid per-user key stamps an owner.
	key := strings.TrimSpace(r.Header.Get("X-Hub-Key"))
	if a.opts.Keys != nil {
		if login, found := a.opts.Keys.Resolve(key); found {
			return login, true
		}
	}
	// Fall back to the shared secret: a valid X-Hub-Secret registers owner-less,
	// even with auth on. This keeps the legacy secret a meaningful registration
	// credential during a transition (key → owner, secret → owner-less), rather
	// than being silently ignored once OAuth is enabled. (findings #31B)
	if a.opts.RegisterSecret != "" && r.Header.Get("X-Hub-Secret") == a.opts.RegisterSecret {
		return "", true
	}
	// No valid credential. Refuse when auth is required or a shared secret is
	// configured (so a set secret actually gates); otherwise (soft mode, no
	// secret) register anonymously as before.
	if a.opts.Auth.RequireAuth || a.opts.RegisterSecret != "" {
		a.opts.Audit.Log(audit.Event{
			Event: audit.EventRegisterDeny, IP: clientIP(r), Outcome: "deny",
			Detail: "missing or invalid X-Hub-Key / X-Hub-Secret",
		})
		http.Error(w, "missing or invalid X-Hub-Key (run 'mxcli auth hub login') or X-Hub-Secret", http.StatusUnauthorized)
		return "", false
	}
	return "", true
}

// handleKeys mints (POST) or revokes (DELETE) a hub API key. Only available when
// auth is configured; the key store is the registration credential that replaces
// the shared secret for a hosted hub.
func (a *API) handleKeys(w http.ResponseWriter, r *http.Request) {
	if !a.opts.Auth.enabled() || a.opts.Keys == nil {
		http.Error(w, "hub keys are not enabled on this hub", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.handleKeyList(w, r)
	case http.MethodPost:
		a.handleKeyMint(w, r)
	case http.MethodDelete:
		a.handleKeyRevoke(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// keyCaller resolves the login for a key-management request. Two auth methods:
// an Authorization: Bearer <github-token> (verified against GitHub, no CSRF risk
// as it isn't ambient), or a hub session cookie. State-changing cookie calls
// (mint, revoke-all) pass writeCSRF=true so they additionally require the
// X-Hub-Mint header + a same-origin request; reads (list) pass false. On failure
// it writes the response and returns ok=false.
func (a *API) keyCaller(w http.ResponseWriter, r *http.Request, writeCSRF bool) (login string, ok bool) {
	if token := bearerToken(r); token != "" {
		l, err := a.opts.Auth.fetchLogin(token)
		if err != nil || l == "" {
			http.Error(w, "could not verify GitHub identity", http.StatusUnauthorized)
			return "", false
		}
		return l, true
	}
	if l := a.opts.Auth.sessionLogin(r); l != "" {
		if writeCSRF && (r.Header.Get(mintHeader) == "" || !sameOrigin(r, a.opts.Auth.HubHost)) {
			http.Error(w, "this request requires the "+mintHeader+" header and a same-origin request", http.StatusForbidden)
			return "", false
		}
		return l, true
	}
	http.Error(w, "sign in at the hub, or send Authorization: Bearer <github-token>", http.StatusUnauthorized)
	return "", false
}

// handleKeyMint issues an opaque hub key bound to a GitHub login. Two ways to
// authenticate the mint:
//
//  1. A signed-in **browser session** (the /cli page) — the natural path for a
//     Claude Code web / mobile user, whose container can't reach GitHub's device
//     endpoints. Cookie auth is CSRF-sensitive, so it additionally requires a
//     same-origin custom header (a cross-site form can't set one) and, when the
//     browser sends an Origin, a matching one.
//  2. An `Authorization: Bearer <github-token>` (CLI `auth hub login --token`,
//     CI). The token is verified against GitHub once and discarded — never stored.
func (a *API) handleKeyMint(w http.ResponseWriter, r *http.Request) {
	login, ok := a.keyCaller(w, r, true)
	if !ok {
		return
	}
	// By default a new key replaces the caller's existing keys (rotate) so the
	// store doesn't silently accumulate live credentials; ?replace=false mints an
	// additional key and keeps the others.
	if r.URL.Query().Get("replace") != "false" {
		if n := a.opts.Keys.RevokeLogin(login); n > 0 {
			a.opts.Audit.Log(audit.Event{
				Event: audit.EventKeyRevoke, Login: login, IP: clientIP(r), Outcome: "ok",
				Detail: fmt.Sprintf("rotate: revoked %d", n),
			})
		}
	}
	key := a.opts.Keys.Mint(login)
	a.opts.Audit.Log(audit.Event{
		Event: audit.EventKeyMint, Login: login, IP: clientIP(r), Outcome: "ok",
	})
	writeJSON(w, http.StatusOK, KeyResponse{Key: key, Login: login})
}

// handleKeyList (GET /api/keys) reports how many active keys the caller has, so
// the /cli page can show "you have N keys".
func (a *API) handleKeyList(w http.ResponseWriter, r *http.Request) {
	login, ok := a.keyCaller(w, r, false)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, KeyListResponse{Login: login, Count: a.opts.Keys.CountLogin(login)})
}

// mintHeader is the custom header the /cli page sends on a cookie-authenticated
// mint; its presence (unsettable by a cross-site form) is the CSRF guard.
const mintHeader = "X-Hub-Mint"

// sameOrigin reports whether the request's Origin (when present) is the hub host.
// Absent Origin falls through to the custom-header requirement.
func sameOrigin(r *http.Request, hubHost string) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	return stripPort(u.Host) == hubHost
}

// handleKeyRevoke removes the presented key (X-Hub-Key). Idempotent: revoking an
// unknown key still returns 204.
// handleKeyRevoke (DELETE /api/keys) revokes either a single key presented as
// X-Hub-Key (the CLI logout path) or, for a signed-in browser with no X-Hub-Key,
// all of the caller's keys (leak recovery / cleanup).
func (a *API) handleKeyRevoke(w http.ResponseWriter, r *http.Request) {
	if key := strings.TrimSpace(r.Header.Get("X-Hub-Key")); key != "" {
		if login, ok := a.opts.Keys.Revoke(key); ok {
			a.opts.Audit.Log(audit.Event{
				Event: audit.EventKeyRevoke, Login: login, IP: clientIP(r), Outcome: "ok",
			})
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	login, ok := a.keyCaller(w, r, true)
	if !ok {
		return
	}
	n := a.opts.Keys.RevokeLogin(login)
	if n > 0 {
		a.opts.Audit.Log(audit.Event{
			Event: audit.EventKeyRevoke, Login: login, IP: clientIP(r), Outcome: "ok",
			Detail: fmt.Sprintf("revoke-all: %d", n),
		})
	}
	writeJSON(w, http.StatusOK, KeyRevokeResponse{Revoked: n})
}

func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	a.byToken(w, r, func(token string) {
		if !a.opts.Registry.Heartbeat(token) {
			http.Error(w, "unknown token", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (a *API) handleDeregister(w http.ResponseWriter, r *http.Request) {
	a.byToken(w, r, func(token string) {
		a.opts.Registry.Deregister(token)
		w.WriteHeader(http.StatusNoContent)
	})
}

// byToken extracts the bearer token (Authorization: Bearer <t> or ?token=) and
// invokes fn. POST only.
func (a *API) byToken(w http.ResponseWriter, r *http.Request, fn func(token string)) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := bearerToken(r)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	fn(token)
}

func (a *API) handleBackends(w http.ResponseWriter, r *http.Request) {
	sort := r.URL.Query().Get("sort")
	if a.opts.Auth.enabled() {
		// Auth on: the listing is the caller's own-previews view. An unauthenticated
		// caller must NOT receive the full list (that would leak every user's
		// subdomains/owners/ports); require a valid session and filter to its login.
		login := a.opts.Auth.sessionLogin(r)
		if login == "" {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, a.opts.Registry.List(sort, login))
		return
	}
	// Open mode (no auth): list everything — today's behaviour.
	writeJSON(w, http.StatusOK, a.opts.Registry.List(sort, ""))
}

func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
