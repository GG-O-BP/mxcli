// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWriterSink_WritesOneJSONLinePerEvent(t *testing.T) {
	var buf bytes.Buffer
	s := NewWriter(&buf)

	s.Log(Event{Event: EventLoginOK, Login: "alice", IP: "203.0.113.1", Outcome: "ok"})
	s.Log(Event{Event: EventAccessDeny, Login: "bob", Owner: "alice", Subdomain: "app-x", Outcome: "deny", Detail: "not owner"})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 JSONL lines, got %d: %q", len(lines), buf.String())
	}
	var e1 Event
	if err := json.Unmarshal([]byte(lines[0]), &e1); err != nil {
		t.Fatalf("line 1 not valid JSON: %v", err)
	}
	if e1.Event != EventLoginOK || e1.Login != "alice" || e1.Outcome != "ok" {
		t.Errorf("line 1 = %+v, want login_ok/alice/ok", e1)
	}
	if e1.Ts.IsZero() {
		t.Error("sink must stamp Ts when zero")
	}
	var e2 Event
	if err := json.Unmarshal([]byte(lines[1]), &e2); err != nil {
		t.Fatalf("line 2 not valid JSON: %v", err)
	}
	if e2.Event != EventAccessDeny || e2.Owner != "alice" || e2.Subdomain != "app-x" {
		t.Errorf("line 2 = %+v, want access_deny/owner=alice/sub=app-x", e2)
	}
}

// TestEvent_HasNoSecretField is the structural guarantee: a caller cannot record
// a token/cookie/hub-key/secret because Event carries no such field.
func TestEvent_HasNoSecretField(t *testing.T) {
	banned := []string{"token", "secret", "cookie", "key", "password", "auth"}
	tp := reflect.TypeOf(Event{})
	for i := 0; i < tp.NumField(); i++ {
		name := strings.ToLower(tp.Field(i).Name)
		for _, b := range banned {
			if strings.Contains(name, b) {
				t.Errorf("Event field %q looks sensitive (contains %q) — audit records must not carry secrets", tp.Field(i).Name, b)
			}
		}
	}
}

func TestNew_ResolvesSpecs(t *testing.T) {
	// "" and "-" → NoOp (a no-op writes nothing and never errors).
	for _, spec := range []string{"", "-"} {
		s, err := New(spec)
		if err != nil {
			t.Fatalf("New(%q): %v", spec, err)
		}
		if _, ok := s.(noopSink); !ok {
			t.Errorf("New(%q) = %T, want noopSink", spec, s)
		}
	}
	// "stdout" → a writer sink.
	if s, err := New("stdout"); err != nil || s == nil {
		t.Fatalf("New(stdout) = %v, %v", s, err)
	}
	// A path → a JSONL file, mode 0600, appended.
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	s, err := New(path)
	if err != nil {
		t.Fatalf("New(path): %v", err)
	}
	s.Log(Event{Ts: time.Unix(1_700_000_000, 0).UTC(), Event: EventRegisterOK, Login: "carol"})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("audit file mode = %o, want 600", perm)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"event":"register_ok"`) || !strings.Contains(string(data), `"login":"carol"`) {
		t.Errorf("audit file missing the event: %s", data)
	}
}
