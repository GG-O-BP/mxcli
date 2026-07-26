// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// debuggerTestServer stubs BOTH planes on one httptest server, routed by path:
// "/" is the M2EE admin plane, "/debugger/" is the debugger endpoint. It records
// what it received so tests can assert the envelope and auth.
type debuggerTestServer struct {
	*httptest.Server
	adminActions []string
	enablePass   string // password seen on enable_debugger
	dbgActions   []string
	dbgAuth      string         // X-Debugger-Authentication seen on the last /debugger/ call
	dbgHadParams bool           // whether the last /debugger/ body had a params key
	dbgToken     string         // session_token seen on the last /debugger/ call
	dbgParams    map[string]any // params of the last /debugger/ call
}

func newDebuggerTestServer(t *testing.T) (*debuggerTestServer, DebuggerOptions) {
	t.Helper()
	ts := &debuggerTestServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/debugger/" {
			ts.dbgAuth = r.Header.Get("X-Debugger-Authentication")
			var body map[string]json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&body)
			var action string
			_ = json.Unmarshal(body["action"], &action)
			ts.dbgActions = append(ts.dbgActions, action)
			_, ts.dbgHadParams = body["params"]
			ts.dbgParams = nil
			if p, ok := body["params"]; ok {
				_ = json.Unmarshal(p, &ts.dbgParams)
			}
			if tok, ok := body["session_token"]; ok {
				_ = json.Unmarshal(tok, &ts.dbgToken)
			}
			switch action {
			case "start_session":
				_, _ = w.Write([]byte(`{"result":{"session_token":"tok-123","runtime_version":"11.6"}}`))
			default:
				_, _ = w.Write([]byte(`{"result":{}}`))
			}
			return
		}
		// admin plane
		var req struct {
			Action string `json:"action"`
			Params struct {
				Password string `json:"password"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		ts.adminActions = append(ts.adminActions, req.Action)
		switch req.Action {
		case "enable_debugger":
			ts.enablePass = req.Params.Password
			_ = json.NewEncoder(w).Encode(M2EEResponse{})
		case "get_debugger_status":
			_, _ = w.Write([]byte(`{"result":0,"feedback":{"enabled":true,"client_connected":true,"number_of_paused_microflows":2}}`))
		default:
			_ = json.NewEncoder(w).Encode(M2EEResponse{})
		}
	}))
	t.Cleanup(ts.Close)

	host, port := parseTestServerAddr(t, ts.URL)
	opts := DebuggerOptions{
		Admin:     M2EEOptions{Host: host, Port: port, Token: "adminpass", Direct: true},
		AppURL:    ts.URL,
		DebugPass: "mxdebug",
		TokenPath: filepath.Join(t.TempDir(), "debug-session.token"),
	}
	return ts, opts
}

func TestDebugger_Status(t *testing.T) {
	ts, opts := newDebuggerTestServer(t)
	st, err := NewDebuggerClient(opts).Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Enabled || !st.ClientConnected || st.NumberOfPausedMicroflows != 2 {
		t.Errorf("status = %+v, want enabled+connected+2 paused", st)
	}
	if len(ts.adminActions) != 1 || ts.adminActions[0] != "get_debugger_status" {
		t.Errorf("adminActions = %v", ts.adminActions)
	}
}

func TestDebugger_EnableSendsPassword(t *testing.T) {
	ts, opts := newDebuggerTestServer(t)
	if err := NewDebuggerClient(opts).Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if len(ts.adminActions) != 1 || ts.adminActions[0] != "enable_debugger" {
		t.Fatalf("adminActions = %v, want [enable_debugger]", ts.adminActions)
	}
	if ts.enablePass != "mxdebug" {
		t.Errorf("enable password = %q, want mxdebug", ts.enablePass)
	}
}

func TestDebugger_StartSessionCachesToken(t *testing.T) {
	ts, opts := newDebuggerTestServer(t)
	c := NewDebuggerClient(opts)
	tok, err := c.StartSession()
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if tok != "tok-123" || c.Token() != "tok-123" {
		t.Errorf("token = %q / %q, want tok-123", tok, c.Token())
	}
	// start_session carries no session_token but MUST carry params.
	if len(ts.dbgActions) != 1 || ts.dbgActions[0] != "start_session" {
		t.Errorf("dbgActions = %v", ts.dbgActions)
	}
	if !ts.dbgHadParams {
		t.Error("start_session body must include a params key")
	}
	if ts.dbgToken != "" {
		t.Errorf("start_session should not send a session_token, got %q", ts.dbgToken)
	}
	// The debugger auth header is base64(debugPass).
	if ts.dbgAuth != m2eeAuthHeader("mxdebug") {
		t.Errorf("X-Debugger-Authentication = %q, want base64(mxdebug)", ts.dbgAuth)
	}
	// Token cached to disk and reloadable.
	if b, err := os.ReadFile(opts.TokenPath); err != nil || string(b) != "tok-123" {
		t.Errorf("cached token file = %q err=%v, want tok-123", string(b), err)
	}
	fresh := NewDebuggerClient(opts)
	if err := fresh.LoadToken(); err != nil || fresh.Token() != "tok-123" {
		t.Errorf("LoadToken -> %q err=%v, want tok-123", fresh.Token(), err)
	}
}

func TestDebugger_DisableClearsToken(t *testing.T) {
	_, opts := newDebuggerTestServer(t)
	c := NewDebuggerClient(opts)
	if _, err := c.StartSession(); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if _, err := os.Stat(opts.TokenPath); err != nil {
		t.Fatalf("token file should exist before disable: %v", err)
	}
	if err := c.Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if c.Token() != "" {
		t.Errorf("in-memory token not cleared: %q", c.Token())
	}
	if _, err := os.Stat(opts.TokenPath); !os.IsNotExist(err) {
		t.Errorf("token file should be removed after disable, stat err=%v", err)
	}
}

func TestDebugger_AddBreakpoint(t *testing.T) {
	ts, opts := newDebuggerTestServer(t)
	c := NewDebuggerClient(opts)
	if _, err := c.StartSession(); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := c.AddBreakpoint("Sudoku.ACT_Hint", "guid-1", "$Game/Solved = false"); err != nil {
		t.Fatalf("AddBreakpoint: %v", err)
	}
	if got := ts.dbgActions[len(ts.dbgActions)-1]; got != "add_breakpoint" {
		t.Fatalf("last action = %q, want add_breakpoint", got)
	}
	// Breakpoint calls MUST carry the session token from start_session.
	if ts.dbgToken != "tok-123" {
		t.Errorf("add_breakpoint session_token = %q, want tok-123", ts.dbgToken)
	}
	if ts.dbgParams["microflow_name"] != "Sudoku.ACT_Hint" || ts.dbgParams["object_id"] != "guid-1" {
		t.Errorf("params = %v, want microflow_name+object_id", ts.dbgParams)
	}
	if ts.dbgParams["condition"] != "$Game/Solved = false" {
		t.Errorf("condition = %v, want the expression", ts.dbgParams["condition"])
	}
}

func TestDebugger_AddBreakpointOmitsEmptyCondition(t *testing.T) {
	ts, opts := newDebuggerTestServer(t)
	c := NewDebuggerClient(opts)
	if _, err := c.StartSession(); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := c.AddBreakpoint("M.F", "guid-2", ""); err != nil {
		t.Fatalf("AddBreakpoint: %v", err)
	}
	if _, ok := ts.dbgParams["condition"]; ok {
		t.Errorf("empty condition must be omitted, got %v", ts.dbgParams["condition"])
	}
}

func TestDebugger_RemoveBreakpoint(t *testing.T) {
	ts, opts := newDebuggerTestServer(t)
	c := NewDebuggerClient(opts)
	if _, err := c.StartSession(); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := c.RemoveBreakpoint("guid-1"); err != nil {
		t.Fatalf("RemoveBreakpoint: %v", err)
	}
	if got := ts.dbgActions[len(ts.dbgActions)-1]; got != "remove_breakpoint" {
		t.Fatalf("last action = %q, want remove_breakpoint", got)
	}
	if ts.dbgParams["object_id"] != "guid-1" {
		t.Errorf("params = %v, want object_id guid-1", ts.dbgParams)
	}
}

func TestDebugger_BreakpointNeedsSession(t *testing.T) {
	// Without a session token, a breakpoint call must fail with a clear message.
	_, opts := newDebuggerTestServer(t)
	c := NewDebuggerClient(opts) // no StartSession / LoadToken
	err := c.AddBreakpoint("M.F", "guid-1", "")
	if err == nil || !strings.Contains(err.Error(), "debug enable") {
		t.Errorf("want a 'run debug enable first' error, got %v", err)
	}
}

func TestDebugger_StepActions(t *testing.T) {
	ts, opts := newDebuggerTestServer(t)
	c := NewDebuggerClient(opts)
	if _, err := c.StartSession(); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	cases := []struct{ kind, want string }{
		{"over", "step_over"},
		{"into", "step_into"},
		{"out", "step_out"},
	}
	for _, tc := range cases {
		if err := c.Step(tc.kind, "dbg-9"); err != nil {
			t.Fatalf("Step(%s): %v", tc.kind, err)
		}
		if got := ts.dbgActions[len(ts.dbgActions)-1]; got != tc.want {
			t.Errorf("Step(%s) action = %q, want %q", tc.kind, got, tc.want)
		}
		if ts.dbgParams["debug_id"] != "dbg-9" {
			t.Errorf("Step(%s) debug_id = %v, want dbg-9", tc.kind, ts.dbgParams["debug_id"])
		}
	}
	if err := c.Step("sideways", "dbg-9"); err == nil {
		t.Error("Step with an unknown kind should error")
	}
}

func TestDebugger_Continue(t *testing.T) {
	ts, opts := newDebuggerTestServer(t)
	c := NewDebuggerClient(opts)
	if _, err := c.StartSession(); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := c.Continue(false); err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if got := ts.dbgActions[len(ts.dbgActions)-1]; got != "continue" {
		t.Errorf("Continue(false) = %q, want continue", got)
	}
	if err := c.Continue(true); err != nil {
		t.Fatalf("Continue(all): %v", err)
	}
	if got := ts.dbgActions[len(ts.dbgActions)-1]; got != "continue_all" {
		t.Errorf("Continue(true) = %q, want continue_all", got)
	}
}

func TestDebugger_GetObject(t *testing.T) {
	ts, opts := newDebuggerTestServer(t)
	c := NewDebuggerClient(opts)
	if _, err := c.StartSession(); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if _, err := c.GetObject("dbg-9", "Game"); err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if got := ts.dbgActions[len(ts.dbgActions)-1]; got != "get_object" {
		t.Errorf("action = %q, want get_object", got)
	}
	if ts.dbgParams["debug_id"] != "dbg-9" || ts.dbgParams["variable_name"] != "Game" {
		t.Errorf("params = %v, want debug_id+variable_name", ts.dbgParams)
	}
}

func TestDebugger_AuthFailure(t *testing.T) {
	// A 401 from the debugger endpoint must produce an actionable error.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer ts.Close()
	c := NewDebuggerClient(DebuggerOptions{AppURL: ts.URL, DebugPass: "wrong"})
	_, err := c.StartSession()
	if err == nil {
		t.Fatal("expected an auth error on HTTP 401")
	}
	if got := err.Error(); !strings.Contains(got, "auth failed") || !strings.Contains(got, "debug enable") {
		t.Errorf("error = %q, want it to mention auth + how to enable", got)
	}
}
