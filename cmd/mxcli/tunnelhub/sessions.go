// SPDX-License-Identifier: Apache-2.0

package tunnelhub

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// A Claude Code session groups the preview endpoints it exposed. The client
// sends its session id (CLAUDE_CODE_REMOTE_SESSION_ID, e.g. "cse_01JX…") on
// registration; the hub groups backends by it and — because a live Backend is
// reaped ~10 min after the container goes idle — keeps a durable record here so
// past sessions stay visible in the overview.

// EndpointRecord is a persisted record of one preview endpoint a session
// exposed. It outlives the live Backend (which is dropped on reap) so the
// overview can show sessions that have gone offline.
type EndpointRecord struct {
	Session      string    `json:"session"`
	Owner        string    `json:"owner"`
	Prefix       string    `json:"prefix"`
	Project      string    `json:"project"`
	Solution     string    `json:"solution"`
	Branch       string    `json:"branch"`
	Worktree     string    `json:"worktree"`
	Subdomain    string    `json:"subdomain"`
	URL          string    `json:"url"`
	RegisteredAt time.Time `json:"registeredAt"`
	LastSeenAt   time.Time `json:"lastSeenAt"`
}

// key is the stable identity of an endpoint within the log: same session + owner
// + slot re-registers to the same record (so a reconnect updates rather than
// duplicates). It mirrors Backend.identity() with the session prepended.
func (e *EndpointRecord) key() string {
	return strings.Join([]string{e.Session, e.Owner, e.Prefix, e.Solution, e.Project, e.Branch, e.Worktree}, "\x00")
}

// SessionLog is the durable history of endpoints seen per session. Records are
// pruned once their last-seen is older than the retention window. All methods
// are safe for concurrent use.
type SessionLog struct {
	mu        sync.Mutex
	byKey     map[string]*EndpointRecord
	path      string        // "" = in-memory only (no persistence)
	retention time.Duration // records older than this (by LastSeenAt) are pruned
	now       func() time.Time
}

// sessionsFile is the on-disk layout for the durable session log.
type sessionsFile struct {
	Version   int               `json:"version"`
	Endpoints []*EndpointRecord `json:"endpoints"`
}

const sessionsFileVersion = 1

// DefaultSessionRetention is how long an offline endpoint stays in the overview.
const DefaultSessionRetention = 30 * 24 * time.Hour

// NewSessionLog returns an in-memory session log (no persistence). Suitable for
// tests and open hubs that don't need history across restarts.
func NewSessionLog(retention time.Duration) *SessionLog {
	if retention <= 0 {
		retention = DefaultSessionRetention
	}
	return &SessionLog{byKey: map[string]*EndpointRecord{}, retention: retention, now: time.Now}
}

// NewSessionLogFile returns a durable session log backed by path. An existing
// file is loaded and pruned; Record writes through (atomic, mode 0600).
func NewSessionLogFile(path string, retention time.Duration) (*SessionLog, error) {
	s := NewSessionLog(retention)
	s.path = path
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SessionLog) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// Record upserts an endpoint sighting: the earliest RegisteredAt and the latest
// LastSeenAt win, so a reconnect extends the same record. Mutable fields (owner,
// subdomain, url) are refreshed. Prunes and persists.
func (s *SessionLog) Record(e EndpointRecord) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := e.key()
	if cur, ok := s.byKey[k]; ok {
		if !e.RegisteredAt.IsZero() && (cur.RegisteredAt.IsZero() || e.RegisteredAt.Before(cur.RegisteredAt)) {
			cur.RegisteredAt = e.RegisteredAt
		}
		if e.LastSeenAt.After(cur.LastSeenAt) {
			cur.LastSeenAt = e.LastSeenAt
		}
		cur.Owner, cur.Subdomain, cur.URL = e.Owner, e.Subdomain, e.URL
	} else {
		cp := e
		s.byKey[k] = &cp
	}
	s.pruneLocked()
	_ = s.saveLocked()
}

// Snapshot returns a pruned copy of all records.
func (s *SessionLog) Snapshot() []EndpointRecord {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	out := make([]EndpointRecord, 0, len(s.byKey))
	for _, r := range s.byKey {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	return out
}

// pruneLocked drops records whose LastSeenAt is older than the retention window.
func (s *SessionLog) pruneLocked() {
	cutoff := s.clock().Add(-s.retention)
	for k, r := range s.byKey {
		if r.LastSeenAt.Before(cutoff) {
			delete(s.byKey, k)
		}
	}
}

func (s *SessionLog) load() error {
	if s.path == "" {
		return nil
	}
	info, err := os.Stat(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("session log: stat %s: %w", s.path, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("session log %s has too-open permissions %o (want 0600)", s.path, info.Mode().Perm())
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("session log: read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		return nil
	}
	var sf sessionsFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return fmt.Errorf("session log: parse %s: %w", s.path, err)
	}
	for _, r := range sf.Endpoints {
		if r != nil {
			s.byKey[r.key()] = r
		}
	}
	s.pruneLocked()
	return nil
}

// saveLocked atomically writes the log to disk (temp + rename, mode 0600). The
// caller must hold s.mu. No-op for an in-memory log.
func (s *SessionLog) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	eps := make([]*EndpointRecord, 0, len(s.byKey))
	for _, r := range s.byKey {
		eps = append(eps, r)
	}
	sort.Slice(eps, func(i, j int) bool { return eps[i].LastSeenAt.After(eps[j].LastSeenAt) })
	data, err := json.MarshalIndent(sessionsFile{Version: sessionsFileVersion, Endpoints: eps}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".hub-sessions.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// EndpointView is one endpoint in a SessionView, live or historical.
type EndpointView struct {
	Subdomain    string    `json:"subdomain"`
	URL          string    `json:"url"`
	Prefix       string    `json:"prefix"`
	Project      string    `json:"project"`
	Solution     string    `json:"solution"`
	Branch       string    `json:"branch"`
	Worktree     string    `json:"worktree"`
	State        string    `json:"state"` // "available" | "stale" | "offline"
	RegisteredAt time.Time `json:"registeredAt"`
	LastSeenAt   time.Time `json:"lastSeenAt"`
	LastUsedAt   time.Time `json:"lastUsedAt"`
	UptimeSec    int64     `json:"uptimeSec"`
}

// SessionView groups the endpoints a single Claude Code session exposed, live
// and historical.
type SessionView struct {
	Session    string         `json:"session"`
	SessionURL string         `json:"sessionUrl"` // claude.ai link when derivable, else ""
	Owner      string         `json:"owner"`
	Online     bool           `json:"online"` // any endpoint currently available/stale
	FirstSeen  time.Time      `json:"firstSeen"`
	LastSeen   time.Time      `json:"lastSeen"`
	Endpoints  []EndpointView `json:"endpoints"`
}

// sessionURL maps a Claude Code remote session id to its conversation URL.
// CLAUDE_CODE_REMOTE_SESSION_ID is "cse_<id>" and the web URL is
// "https://claude.ai/code/session_<id>". Other id shapes get no link.
func sessionURL(session string) string {
	if id, ok := strings.CutPrefix(session, "cse_"); ok && id != "" {
		return "https://claude.ai/code/session_" + id
	}
	return ""
}
