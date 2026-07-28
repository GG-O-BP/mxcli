// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestHeartbeatReRegistersAfterHubForgets simulates a hub that restarts and loses
// its registry: /api/status then 404s. The heartbeat must re-register in place
// (findings #16 — previously it pinged once at startup and never recovered).
func TestHeartbeatReRegistersAfterHubForgets(t *testing.T) {
	var registerCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/register":
			n := atomic.AddInt32(&registerCalls, 1)
			// First registration: port 40001. Re-registration: a different port
			// (40002) so the onReRegister callback fires.
			port := 40001
			token := "tok-1"
			if n > 1 {
				port = 40002
				token = "tok-2"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"subdomain": "app", "url": srvURL(r), "reversePort": port,
				"controlUrl": srvURL(r), "token": token, "tunnelAuth": "auth",
				"heartbeatIntervalSec": 1,
			})
		case "/api/status":
			// The hub has forgotten us.
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	reg, err := RegisterWithHub(srv.URL, "", "", HubMeta{Project: "p"}, 8080)
	if err != nil {
		t.Fatalf("initial register: %v", err)
	}
	if reg.ReversePort != 40001 || reg.Token != "tok-1" {
		t.Fatalf("unexpected initial reg: port=%d token=%s", reg.ReversePort, reg.Token)
	}

	changed := make(chan *HubRegistration, 1)
	hb := StartHeartbeat(reg, func(r *HubRegistration) { changed <- r })
	defer hb.Stop()

	select {
	case r := <-changed:
		if r.ReversePort != 40002 || r.Token != "tok-2" {
			t.Errorf("re-register did not update reg: port=%d token=%s", r.ReversePort, r.Token)
		}
		if atomic.LoadInt32(&registerCalls) < 2 {
			t.Errorf("expected a second /api/register call, got %d", registerCalls)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeat did not re-register within 5s after the hub 404'd")
	}
}

func srvURL(r *http.Request) string { return "http://" + r.Host }
