// SPDX-License-Identifier: Apache-2.0

package tunnelhub

import (
	"encoding/json"
	"net/http"
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
	key := strings.TrimSpace(r.Header.Get("X-Hub-Key"))
	if a.opts.Keys != nil {
		if login, found := a.opts.Keys.Resolve(key); found {
			return login, true
		}
	}
	// No valid key. Enforce only when required; soft mode registers anonymously.
	if a.opts.Auth.RequireAuth {
		a.opts.Audit.Log(audit.Event{
			Event: audit.EventRegisterDeny, IP: clientIP(r), Outcome: "deny",
			Detail: "missing or invalid X-Hub-Key",
		})
		http.Error(w, "missing or invalid X-Hub-Key (run 'mxcli auth hub login')", http.StatusUnauthorized)
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
	case http.MethodPost:
		a.handleKeyMint(w, r)
	case http.MethodDelete:
		a.handleKeyRevoke(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleKeyMint validates the caller's GitHub token (Bearer) against GitHub, then
// issues an opaque hub key bound to that login. The GitHub token is used once and
// discarded — never stored.
func (a *API) handleKeyMint(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		http.Error(w, "missing GitHub token (Authorization: Bearer <token>)", http.StatusUnauthorized)
		return
	}
	login, err := a.opts.Auth.fetchLogin(token)
	if err != nil || login == "" {
		http.Error(w, "could not verify GitHub identity", http.StatusUnauthorized)
		return
	}
	key := a.opts.Keys.Mint(login)
	a.opts.Audit.Log(audit.Event{
		Event: audit.EventKeyMint, Login: login, IP: clientIP(r), Outcome: "ok",
	})
	writeJSON(w, http.StatusOK, KeyResponse{Key: key, Login: login})
}

// handleKeyRevoke removes the presented key (X-Hub-Key). Idempotent: revoking an
// unknown key still returns 204.
func (a *API) handleKeyRevoke(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("X-Hub-Key"))
	if login, ok := a.opts.Keys.Revoke(key); ok {
		a.opts.Audit.Log(audit.Event{
			Event: audit.EventKeyRevoke, Login: login, IP: clientIP(r), Outcome: "ok",
		})
	}
	w.WriteHeader(http.StatusNoContent)
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
	// In open mode Auth.sessionLogin returns "" and every backend is listed;
	// with auth on it returns the viewer's GitHub login so the list is filtered
	// to the caller's own previews.
	writeJSON(w, http.StatusOK, a.opts.Registry.List(r.URL.Query().Get("sort"), a.opts.Auth.sessionLogin(r)))
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
