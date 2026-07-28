// SPDX-License-Identifier: Apache-2.0

// Package audit is the tunnel-hub's durable audit trail — an append-only record
// of who authenticated, who was denied, and who registered a preview. It answers
// "who is using the hub" (login_ok / register_ok events) and supports abuse
// investigation (access_deny / register_deny), and is queryable after the fact
// (JSONL → DuckDB read_json_auto, per the analyze-runtime warehouse pattern).
//
// Secrets are un-loggable by construction: Event has no field for a token, cookie,
// hub key, or session secret, so a caller cannot accidentally record one.
package audit

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// Event names. Emitted in slice 2 (auth/session) and slice 3 (keys/registration).
const (
	EventLoginOK      = "login_ok"
	EventLogout       = "logout"
	EventCallbackFail = "callback_fail"
	EventAccessDeny   = "access_deny"
	EventKeyMint      = "key_mint"
	EventKeyRevoke    = "key_revoke"
	EventRegisterOK   = "register_ok"
	EventRegisterDeny = "register_deny"
)

// Event is one audit record. Only non-sensitive identifiers are carried — there
// is deliberately no field for a token/cookie/secret.
type Event struct {
	Ts        time.Time `json:"ts"`
	Event     string    `json:"event"`
	Login     string    `json:"login,omitempty"`     // GitHub login ("" = anonymous / open mode)
	IP        string    `json:"ip,omitempty"`        // source (from the 443 front's X-Forwarded-For)
	Subdomain string    `json:"subdomain,omitempty"` // target preview, when relevant
	Owner     string    `json:"owner,omitempty"`     // owner of the target preview, when relevant
	Outcome   string    `json:"outcome,omitempty"`   // "ok" | "deny" | "fail"
	Detail    string    `json:"detail,omitempty"`    // short human reason, never a secret
}

// Sink receives audit events. Implementations must be safe for concurrent use.
type Sink interface {
	Log(Event)
}

// noopSink drops everything — the default when auditing is off (open self-hosted
// hub, or no --audit-log).
type noopSink struct{}

func (noopSink) Log(Event) {}

// NoOp returns a sink that records nothing.
func NoOp() Sink { return noopSink{} }

// writerSink serialises each event as one JSON line to an io.Writer.
type writerSink struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *writerSink) Log(e Event) {
	if e.Ts.IsZero() {
		e.Ts = time.Now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	b = append(b, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.w.Write(b)
}

// Stdout returns a sink that writes JSONL to stdout (useful in containers where
// the platform captures stdout).
func Stdout() Sink { return &writerSink{w: os.Stdout} }

// NewWriter returns a JSONL sink over an arbitrary writer (used by tests).
func NewWriter(w io.Writer) Sink { return &writerSink{w: w} }

// JSONL opens (creating, append, mode 0600) an append-only JSONL audit file.
func JSONL(path string) (Sink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &writerSink{w: f}, nil
}

// New resolves an --audit-log spec into a sink: "" or "-" disables (NoOp),
// "stdout" writes to stdout, anything else is a JSONL file path.
func New(spec string) (Sink, error) {
	switch spec {
	case "", "-":
		return NoOp(), nil
	case "stdout":
		return Stdout(), nil
	default:
		return JSONL(spec)
	}
}
