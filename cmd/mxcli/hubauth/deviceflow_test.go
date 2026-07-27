// SPDX-License-Identifier: Apache-2.0

package hubauth

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeGitHub serves the device-flow endpoints. It returns authorization_pending
// for the first (pendingRounds) polls, then the token.
func fakeGitHub(t *testing.T, pendingRounds int) *httptest.Server {
	t.Helper()
	var polls int
	mux := http.NewServeMux()
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"DC","user_code":"WXYZ-1234","verification_uri":"https://github.com/login/device","expires_in":900,"interval":1}`))
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if polls < pendingRounds {
			polls++
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"gho_from_device"}`))
	})
	return httptest.NewServer(mux)
}

// fakeHub serves /api/auth-config and /api/keys.
func fakeHub(t *testing.T, clientID, login string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth-config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authEnabled":true,"requireAuth":true,"githubClientId":"` + clientID + `"}`))
	})
	mux.HandleFunc("/api/keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer gho_from_device" {
			t.Errorf("mint auth header = %q, want Bearer gho_from_device", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"hub_minted_key","login":"` + login + `"}`))
	})
	return httptest.NewServer(mux)
}

// testClient wires a Client to the stub servers with an instant sleep.
func testClient(hub, gh *httptest.Server) *Client {
	return &Client{
		HubURL:     hub.URL,
		GitHubBase: gh.URL,
		HTTP:       hub.Client(),
		sleep:      func(context.Context, time.Duration) {}, // no real waiting
	}
}

func TestFetchAuthConfig(t *testing.T) {
	hub := fakeHub(t, "cid123", "alice")
	defer hub.Close()
	c := &Client{HubURL: hub.URL, HTTP: hub.Client()}

	cfg, err := c.FetchAuthConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AuthEnabled || cfg.GitHubClientID != "cid123" {
		t.Errorf("cfg = %+v, want enabled/cid123", cfg)
	}
}

func TestPollForToken_PendingThenSuccess(t *testing.T) {
	gh := fakeGitHub(t, 2) // two pending rounds, then success
	defer gh.Close()
	c := &Client{GitHubBase: gh.URL, HTTP: gh.Client(), sleep: func(context.Context, time.Duration) {}}

	tok, err := c.PollForToken(context.Background(), "cid", DeviceCode{DeviceCode: "DC", Interval: 1, ExpiresIn: 900})
	if err != nil {
		t.Fatalf("PollForToken: %v", err)
	}
	if tok != "gho_from_device" {
		t.Errorf("token = %q, want gho_from_device", tok)
	}
}

func TestMintHubKey(t *testing.T) {
	hub := fakeHub(t, "cid", "alice")
	defer hub.Close()
	c := &Client{HubURL: hub.URL, HTTP: hub.Client()}

	key, login, err := c.MintHubKey(context.Background(), "gho_from_device")
	if err != nil {
		t.Fatal(err)
	}
	if key != "hub_minted_key" || login != "alice" {
		t.Errorf("mint = %q/%q, want hub_minted_key/alice", key, login)
	}
}

func TestLogin_FullFlowStoresKey(t *testing.T) {
	withTempStore(t)
	t.Setenv(EnvHubKey, "")

	gh := fakeGitHub(t, 1)
	defer gh.Close()
	hub := fakeHub(t, "cid", "alice")
	defer hub.Close()
	c := testClient(hub, gh)

	var out bytes.Buffer
	login, err := c.Login(context.Background(), &out)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if login != "alice" {
		t.Errorf("login = %q, want alice", login)
	}
	// The instructions were printed.
	if !strings.Contains(out.String(), "WXYZ-1234") {
		t.Errorf("device instructions missing the user code, got: %s", out.String())
	}
	// And the minted key was stored for the hub host.
	if k := ResolveKey(hub.URL); k != "hub_minted_key" {
		t.Errorf("stored key = %q, want hub_minted_key", k)
	}
}

func TestLogin_OpenModeHubIsAnError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth-config", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"authEnabled":false}`))
	})
	hub := httptest.NewServer(mux)
	defer hub.Close()
	c := &Client{HubURL: hub.URL, HTTP: hub.Client()}

	if _, err := c.Login(context.Background(), &bytes.Buffer{}); err == nil {
		t.Error("Login against an open-mode hub should error (no key needed)")
	}
}
