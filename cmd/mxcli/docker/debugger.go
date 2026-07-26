// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// debugger.go is slice 1 of the Mendix microflow debugger (see
// docs/11-proposals/PROPOSAL_microflow_debugger.md). It covers the two-plane
// wiring only: the M2EE admin plane toggles the debugger on/off and reports
// status, and the app's /debugger/ endpoint starts a debug session. Breakpoints,
// paused-microflow inspection, and stepping are later slices.
//
// Two APIs, two auth schemes:
//
//	admin plane   POST http://<host>:<adminPort>/   X-M2EE-Authentication: base64(adminPass)
//	              actions: enable_debugger {password}, disable_debugger, get_debugger_status
//	debugger plane POST <appURL>/debugger/          X-Debugger-Authentication: base64(debugPass)
//	              body {action, session_token, params}; params is mandatory even when {}

// DebuggerOptions configures a DebuggerClient.
type DebuggerOptions struct {
	// Admin is the M2EE admin connection (Host/Port/Token/Direct) used for
	// enable/disable/status. For a `run --local` runtime: 127.0.0.1:8090, token
	// "mxcli-local-dev", Direct: true.
	Admin M2EEOptions
	// AppURL is the app base URL the /debugger/ endpoint lives under
	// (e.g. http://127.0.0.1:8080).
	AppURL string
	// DebugPass is the debugger password. It is passed to enable_debugger and
	// used as the X-Debugger-Authentication credential; the two must match.
	DebugPass string
	// TokenPath, when set, caches the session token across invocations (the CLI
	// is one-shot per command). Empty keeps the token in memory only.
	TokenPath string
	// Timeout bounds a single debugger HTTP request (default 30s). Note: a
	// breakpoint-driven call in a later slice can block far longer while paused —
	// those will use their own timeout.
	Timeout time.Duration
}

// DebuggerClient drives both planes of the runtime debugger.
type DebuggerClient struct {
	opts  DebuggerOptions
	http  *http.Client
	token string
}

// DebuggerStatus mirrors the get_debugger_status feedback.
type DebuggerStatus struct {
	Enabled                  bool `json:"enabled"`
	ClientConnected          bool `json:"client_connected"`
	NumberOfPausedMicroflows int  `json:"number_of_paused_microflows"`
}

// NewDebuggerClient returns a client with defaults applied.
func NewDebuggerClient(opts DebuggerOptions) *DebuggerClient {
	if opts.AppURL == "" {
		opts.AppURL = "http://127.0.0.1:8080"
	}
	if opts.DebugPass == "" {
		opts.DebugPass = "mxdebug"
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	return &DebuggerClient{opts: opts, http: &http.Client{Timeout: opts.Timeout}}
}

// Token returns the current in-memory session token (may be empty).
func (c *DebuggerClient) Token() string { return c.token }

// Status returns the debugger state via the admin plane.
func (c *DebuggerClient) Status() (*DebuggerStatus, error) {
	resp, err := CallM2EE(c.opts.Admin, "get_debugger_status", nil)
	if err != nil {
		return nil, err
	}
	if msg := resp.M2EEError(); msg != "" {
		return nil, fmt.Errorf("get_debugger_status: %s", msg)
	}
	var st DebuggerStatus
	if len(resp.RawFeedback) > 0 {
		if err := json.Unmarshal(resp.RawFeedback, &st); err != nil {
			return nil, fmt.Errorf("decoding debugger status: %w", err)
		}
	}
	return &st, nil
}

// Enable turns the debugger on (admin plane). The runtime requires the password
// here; the same value must be used as the debugger-endpoint credential.
func (c *DebuggerClient) Enable() error {
	resp, err := CallM2EE(c.opts.Admin, "enable_debugger", map[string]any{"password": c.opts.DebugPass})
	if err != nil {
		return err
	}
	if msg := resp.M2EEError(); msg != "" {
		return fmt.Errorf("enable_debugger: %s", msg)
	}
	return nil
}

// Disable turns the debugger off (admin plane) and clears the cached session
// token, so a stale token can't be reused against a fresh session.
func (c *DebuggerClient) Disable() error {
	resp, err := CallM2EE(c.opts.Admin, "disable_debugger", nil)
	if err != nil {
		return err
	}
	if msg := resp.M2EEError(); msg != "" {
		return fmt.Errorf("disable_debugger: %s", msg)
	}
	c.clearToken()
	return nil
}

// StartSession opens a debug session on the /debugger/ endpoint and caches the
// returned token. It is the only debugger-plane call that carries no token
// (it mints one).
func (c *DebuggerClient) StartSession() (string, error) {
	result, err := c.post("start_session", false, map[string]any{"breakpoints": []any{}})
	if err != nil {
		return "", err
	}
	var r struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(result, &r); err != nil || r.SessionToken == "" {
		return "", fmt.Errorf("start_session returned no session_token (response: %s)", string(result))
	}
	c.token = r.SessionToken
	if err := c.saveToken(r.SessionToken); err != nil {
		return "", fmt.Errorf("caching session token: %w", err)
	}
	return r.SessionToken, nil
}

// LoadToken reads a previously cached session token into memory (best-effort:
// a missing file is not an error, since the session may not exist yet).
func (c *DebuggerClient) LoadToken() error {
	if c.opts.TokenPath == "" {
		return nil
	}
	b, err := os.ReadFile(c.opts.TokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	c.token = strings.TrimSpace(string(b))
	return nil
}

func (c *DebuggerClient) saveToken(token string) error {
	if c.opts.TokenPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.opts.TokenPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.opts.TokenPath, []byte(token), 0o600)
}

func (c *DebuggerClient) clearToken() {
	c.token = ""
	if c.opts.TokenPath != "" {
		_ = os.Remove(c.opts.TokenPath)
	}
}

// post drives the /debugger/ plane. The envelope is {action, session_token?,
// params}; params is always serialized (the runtime rejects a missing params
// with "Missing property"). Returns the raw "result" object from the response.
func (c *DebuggerClient) post(action string, withToken bool, params map[string]any) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	body := map[string]any{"action": action, "params": params}
	if withToken {
		if c.token == "" {
			return nil, fmt.Errorf("no debug session — run 'mxcli debug enable' first")
		}
		body["session_token"] = c.token
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling debugger request: %w", err)
	}

	url := strings.TrimRight(c.opts.AppURL, "/") + "/debugger/"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("creating debugger request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// The debugger endpoint accepts ONLY this header form — raw password, Basic,
	// and Bearer all 401 (verified against a running runtime).
	req.Header.Set("X-Debugger-Authentication", m2eeAuthHeader(c.opts.DebugPass))

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach debugger endpoint at %s -- is the app running (mxcli run --local)?", url)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("debugger auth failed (HTTP %d) -- is the debugger enabled ('mxcli debug enable') and --debug-pass correct?", resp.StatusCode)
	default:
		return nil, fmt.Errorf("debugger endpoint returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(respBody, &env); err != nil {
		return nil, fmt.Errorf("decoding debugger response: %w (body: %s)", err, strings.TrimSpace(string(respBody)))
	}
	return env.Result, nil
}
