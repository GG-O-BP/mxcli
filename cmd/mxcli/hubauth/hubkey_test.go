// SPDX-License-Identifier: Apache-2.0

package hubauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeHub serves the hub key endpoint used by the headless mint path.
func fakeHub(t *testing.T, login string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer gho_token" {
			t.Errorf("mint auth header = %q, want Bearer gho_token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"hub_minted_key","login":"` + login + `"}`))
	})
	return httptest.NewServer(mux)
}

func TestMintHubKey(t *testing.T) {
	hub := fakeHub(t, "alice")
	defer hub.Close()
	c := &Client{HubURL: hub.URL, HTTP: hub.Client()}

	key, login, err := c.MintHubKey(context.Background(), "gho_token")
	if err != nil {
		t.Fatal(err)
	}
	if key != "hub_minted_key" || login != "alice" {
		t.Errorf("mint = %q/%q, want hub_minted_key/alice", key, login)
	}
}

func TestMintHubKey_ErrorStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/keys", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	})
	hub := httptest.NewServer(mux)
	defer hub.Close()
	c := &Client{HubURL: hub.URL, HTTP: hub.Client()}

	if _, _, err := c.MintHubKey(context.Background(), "bad"); err == nil {
		t.Error("a non-200 mint response should be an error")
	}
}

func TestLoginWithToken_MintsAndStores(t *testing.T) {
	withTempStore(t)
	t.Setenv(EnvHubKey, "")

	hub := fakeHub(t, "alice")
	defer hub.Close()
	c := &Client{HubURL: hub.URL, HTTP: hub.Client()}

	login, err := c.LoginWithToken(context.Background(), "gho_token")
	if err != nil {
		t.Fatalf("LoginWithToken: %v", err)
	}
	if login != "alice" {
		t.Errorf("login = %q, want alice", login)
	}
	if k := ResolveKey(hub.URL); k != "hub_minted_key" {
		t.Errorf("stored key = %q, want hub_minted_key", k)
	}
}
