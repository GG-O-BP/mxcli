// SPDX-License-Identifier: Apache-2.0

package tunnelhub

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Availability is a backend's liveness, derived from heartbeat freshness.
type Availability string

const (
	// Available: the container heartbeat is fresh — the app should be reachable.
	Available Availability = "available"
	// Stale: no recent heartbeat (e.g. a Claude Code web container reaped on idle),
	// but not yet expired — shown so the user can see it went away.
	Stale Availability = "stale"
)

// Backend is one registered preview: a locally-running app reverse-tunnelled to
// the hub and served at its subdomain.
type Backend struct {
	ID          string `json:"id"`       // opaque token (auth for heartbeat/deregister + chisel)
	Prefix      string `json:"prefix"`   // optional hostname namespace (org/solution/team/env)
	Project     string `json:"project"`  // e.g. the .mpr name
	Solution    string `json:"solution"` // optional grouping for multi-app solutions
	Branch      string `json:"branch"`   // git branch
	Worktree    string `json:"worktree"` // optional, distinguishes worktrees of one branch
	Owner       string `json:"owner"`    // GitHub login that registered it ("" = anonymous / self-hosted / auth off)
	Session     string `json:"session"`  // Claude Code session id that registered it ("" = none / older client)
	Subdomain   string `json:"subdomain"`
	ReversePort int    `json:"reversePort"`
	AppPort     int    `json:"appPort"`

	RegisteredAt time.Time `json:"registeredAt"`
	LastSeenAt   time.Time `json:"lastSeenAt"` // last heartbeat
	LastUsedAt   time.Time `json:"lastUsedAt"` // last browser request to the subdomain
}

// identity is the stable key for a preview across reconnects: same owner + prefix
// + project + branch + worktree + solution re-registers to the same slot (stable
// URL). Owner is first so two different users' identically-named project/branch
// never collide on one slot (Owner is "" until auth stamps it — see slice 3).
func (b *Backend) identity() string {
	return strings.Join([]string{b.Owner, b.Prefix, b.Solution, b.Project, b.Branch, b.Worktree}, "\x00")
}

// BackendView is a Backend plus derived fields, for the API/admin page.
type BackendView struct {
	Backend
	URL          string       `json:"url"`
	Availability Availability `json:"availability"`
	UptimeSec    int64        `json:"uptimeSec"`
}

// RegisterRequest is the registration payload from `mxcli run --hub`.
type RegisterRequest struct {
	Prefix   string `json:"prefix"`
	Project  string `json:"project"`
	Solution string `json:"solution"`
	Branch   string `json:"branch"`
	Worktree string `json:"worktree"`
	Session  string `json:"session"` // Claude Code session id (client-supplied; groups a session's endpoints)
	AppPort  int    `json:"appPort"`
	// Owner is set server-side from the X-Hub-Key → login lookup, never trusted
	// from the client body (json:"-" keeps it off the wire).
	Owner string `json:"-"`
}

// Registry is the in-memory store of registered backends. All methods are safe
// for concurrent use.
type Registry struct {
	mu          sync.Mutex
	byID        map[string]*Backend
	bySubdomain map[string]*Backend
	byIdentity  map[string]*Backend
	usedPorts   map[int]bool

	domain    string // e.g. "example.com"
	portBase  int    // first reverse port to allocate
	portCount int    // number of reverse ports available
	staleFor  time.Duration
	expireFor time.Duration
	now       func() time.Time
	sessions  *SessionLog // durable per-session endpoint history (nil = disabled)
}

// RegistryOptions configures a Registry. Zero values get sensible defaults.
type RegistryOptions struct {
	Domain    string
	PortBase  int
	PortCount int
	StaleFor  time.Duration // no heartbeat within this -> Stale (default 45s)
	ExpireFor time.Duration // no heartbeat within this -> removed (default 10m)
	Now       func() time.Time
	Sessions  *SessionLog // durable per-session endpoint history (nil = disabled)
}

// NewRegistry creates an empty registry.
func NewRegistry(o RegistryOptions) *Registry {
	if o.PortBase == 0 {
		o.PortBase = 9001
	}
	if o.PortCount == 0 {
		o.PortCount = 200
	}
	if o.StaleFor == 0 {
		o.StaleFor = 45 * time.Second
	}
	if o.ExpireFor == 0 {
		o.ExpireFor = 10 * time.Minute
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Registry{
		byID:        map[string]*Backend{},
		bySubdomain: map[string]*Backend{},
		byIdentity:  map[string]*Backend{},
		usedPorts:   map[int]bool{},
		domain:      o.Domain,
		portBase:    o.PortBase,
		portCount:   o.PortCount,
		staleFor:    o.StaleFor,
		expireFor:   o.ExpireFor,
		now:         o.Now,
		sessions:    o.Sessions,
	}
}

// Register allocates (or refreshes) a backend for the request and returns it. A
// re-registration with the same identity (project/branch/worktree/solution)
// returns the existing slot with a fresh heartbeat, so URLs are stable across
// reconnects.
func (r *Registry) Register(req RegisterRequest) (*Backend, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reapLocked()

	now := r.now()
	b := &Backend{
		Prefix:   strings.TrimSpace(req.Prefix),
		Project:  strings.TrimSpace(req.Project),
		Solution: strings.TrimSpace(req.Solution),
		Branch:   strings.TrimSpace(req.Branch),
		Worktree: strings.TrimSpace(req.Worktree),
		Owner:    strings.TrimSpace(req.Owner),
		Session:  strings.TrimSpace(req.Session),
		AppPort:  req.AppPort,
	}
	if existing, ok := r.byIdentity[b.identity()]; ok {
		existing.LastSeenAt = now
		existing.AppPort = req.AppPort
		existing.Session = b.Session // a reconnect may carry a newer session id
		r.recordSessionLocked(existing)
		return existing, nil
	}

	port, err := r.allocPortLocked()
	if err != nil {
		return nil, err
	}
	b.ID = newToken()
	b.Subdomain = r.allocSubdomainLocked(b.Prefix, b.Project, b.Branch, b.Worktree)
	b.ReversePort = port
	b.RegisteredAt = now
	b.LastSeenAt = now
	b.LastUsedAt = time.Time{}

	r.byID[b.ID] = b
	r.bySubdomain[b.Subdomain] = b
	r.byIdentity[b.identity()] = b
	r.usedPorts[port] = true
	r.recordSessionLocked(b)
	return b, nil
}

// recordSessionLocked mirrors a backend's current state into the durable session
// log so it survives reaping. No-op when the session log is disabled.
func (r *Registry) recordSessionLocked(b *Backend) {
	if r.sessions == nil {
		return
	}
	r.sessions.Record(EndpointRecord{
		Session:      b.Session,
		Owner:        b.Owner,
		Prefix:       b.Prefix,
		Project:      b.Project,
		Solution:     b.Solution,
		Branch:       b.Branch,
		Worktree:     b.Worktree,
		Subdomain:    b.Subdomain,
		URL:          r.urlForLocked(b),
		RegisteredAt: b.RegisteredAt,
		LastSeenAt:   b.LastSeenAt,
	})
}

// urlForLocked is the public URL of a backend (subdomain under the hub domain).
func (r *Registry) urlForLocked(b *Backend) string {
	host := b.Subdomain
	if r.domain != "" {
		host = b.Subdomain + "." + r.domain
	}
	return "https://" + host
}

// Heartbeat refreshes a backend's liveness by token.
func (r *Registry) Heartbeat(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.byID[id]
	if !ok {
		return false
	}
	b.LastSeenAt = r.now()
	return true
}

// TouchUsed records a browser request to a subdomain (updates LastUsedAt).
func (r *Registry) TouchUsed(subdomain string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.bySubdomain[subdomain]; ok {
		b.LastUsedAt = r.now()
	}
}

// Deregister removes a backend by token.
func (r *Registry) Deregister(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.byID[id]
	if !ok {
		return false
	}
	r.removeLocked(b)
	return true
}

// LookupSubdomain returns the backend serving a subdomain.
func (r *Registry) LookupSubdomain(subdomain string) (*Backend, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bySubdomain[subdomain]
	if !ok {
		return nil, false
	}
	cp := *b
	return &cp, true
}

// List returns a snapshot of backends as views, sorted by the given key ("used",
// "registered", "project"; default "used"), most-recent/first. When viewerLogin is
// non-empty, only that owner's backends are returned; an empty viewerLogin (auth
// off / self-hosted) returns all — preserving today's behaviour.
func (r *Registry) List(sortKey, viewerLogin string) []BackendView {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reapLocked()

	out := make([]BackendView, 0, len(r.byID))
	for _, b := range r.byID {
		if viewerLogin != "" && b.Owner != viewerLogin {
			continue
		}
		out = append(out, r.viewLocked(b))
	}
	sortViews(out, sortKey)
	return out
}

// Sessions returns the registered endpoints grouped by Claude Code session,
// merging live backends (available/stale) with the durable history of offline
// ones. When viewerLogin is non-empty, only that owner's sessions are returned
// (auth off / self-hosted passes "" for all). Sessions are sorted most-recently
// -seen first; endpoints within a session likewise.
func (r *Registry) Sessions(viewerLogin string) []SessionView {
	r.mu.Lock()
	r.reapLocked()

	// Live endpoints first — keyed by identity so a history record for the same
	// slot is treated as the same (live) endpoint, not duplicated as offline.
	type epKey = string
	live := map[epKey]EndpointView{}
	meta := map[epKey]struct{ session, owner string }{}
	for _, b := range r.byID {
		if viewerLogin != "" && b.Owner != viewerLogin {
			continue
		}
		v := r.viewLocked(b)
		k := b.identity()
		live[k] = EndpointView{
			Subdomain: b.Subdomain, URL: v.URL, Prefix: b.Prefix, Project: b.Project,
			Solution: b.Solution, Branch: b.Branch, Worktree: b.Worktree,
			State: string(v.Availability), RegisteredAt: b.RegisteredAt,
			LastSeenAt: b.LastSeenAt, LastUsedAt: b.LastUsedAt, UptimeSec: v.UptimeSec,
		}
		meta[k] = struct{ session, owner string }{b.Session, b.Owner}
	}
	history := r.sessions.Snapshot() // nil-safe
	r.mu.Unlock()

	// Group by session. Live endpoints override any offline record for the same slot.
	type grp struct {
		owner string
		eps   map[epKey]EndpointView
	}
	groups := map[string]*grp{}
	ensure := func(session, owner string) *grp {
		g, ok := groups[session]
		if !ok {
			g = &grp{owner: owner, eps: map[epKey]EndpointView{}}
			groups[session] = g
		}
		if g.owner == "" {
			g.owner = owner
		}
		return g
	}
	for k, ev := range live {
		m := meta[k]
		ensure(m.session, m.owner).eps[k] = ev
	}
	for _, rec := range history {
		if viewerLogin != "" && rec.Owner != viewerLogin {
			continue
		}
		g := ensure(rec.Session, rec.Owner)
		k := strings.Join([]string{rec.Owner, rec.Prefix, rec.Solution, rec.Project, rec.Branch, rec.Worktree}, "\x00")
		if _, isLive := g.eps[k]; isLive {
			continue // live entry wins
		}
		g.eps[k] = EndpointView{
			Subdomain: rec.Subdomain, URL: rec.URL, Prefix: rec.Prefix, Project: rec.Project,
			Solution: rec.Solution, Branch: rec.Branch, Worktree: rec.Worktree,
			State: "offline", RegisteredAt: rec.RegisteredAt, LastSeenAt: rec.LastSeenAt,
		}
	}

	out := make([]SessionView, 0, len(groups))
	for session, g := range groups {
		sv := SessionView{Session: session, SessionURL: sessionURL(session), Owner: g.owner}
		for _, ev := range g.eps {
			sv.Endpoints = append(sv.Endpoints, ev)
			if ev.State != "offline" {
				sv.Online = true
			}
			if sv.FirstSeen.IsZero() || (!ev.RegisteredAt.IsZero() && ev.RegisteredAt.Before(sv.FirstSeen)) {
				sv.FirstSeen = ev.RegisteredAt
			}
			if ev.LastSeenAt.After(sv.LastSeen) {
				sv.LastSeen = ev.LastSeenAt
			}
		}
		sort.Slice(sv.Endpoints, func(i, j int) bool {
			return sv.Endpoints[i].LastSeenAt.After(sv.Endpoints[j].LastSeenAt)
		})
		out = append(out, sv)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online // online sessions first
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out
}

// viewLocked builds the derived view for a backend.
func (r *Registry) viewLocked(b *Backend) BackendView {
	host := b.Subdomain
	if r.domain != "" {
		host = b.Subdomain + "." + r.domain
	}
	av := Available
	if r.now().Sub(b.LastSeenAt) > r.staleFor {
		av = Stale
	}
	return BackendView{
		Backend:      *b,
		URL:          "https://" + host,
		Availability: av,
		UptimeSec:    int64(r.now().Sub(b.RegisteredAt).Seconds()),
	}
}

// reapLocked removes backends whose heartbeat is older than expireFor.
func (r *Registry) reapLocked() {
	cutoff := r.now().Add(-r.expireFor)
	for _, b := range r.byID {
		if b.LastSeenAt.Before(cutoff) {
			r.removeLocked(b)
		}
	}
}

func (r *Registry) removeLocked(b *Backend) {
	// Stamp the final liveness into the session log before dropping the live
	// entry, so the offline history shows an accurate last-seen.
	r.recordSessionLocked(b)
	delete(r.byID, b.ID)
	delete(r.bySubdomain, b.Subdomain)
	delete(r.byIdentity, b.identity())
	delete(r.usedPorts, b.ReversePort)
}

// allocPortLocked returns a free reverse port from the configured range.
func (r *Registry) allocPortLocked() (int, error) {
	for p := r.portBase; p < r.portBase+r.portCount; p++ {
		if !r.usedPorts[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free reverse port (all %d in use)", r.portCount)
}

// allocSubdomainLocked returns a unique subdomain slug, disambiguating a
// collision with the worktree name then a numeric suffix.
func (r *Registry) allocSubdomainLocked(prefix, project, branch, worktree string) string {
	base := baseSlug(prefix, project, branch)
	if _, taken := r.bySubdomain[base]; !taken {
		return base
	}
	if wt := slugify(worktree); wt != "" {
		cand := truncateLabel(base + "-" + wt)
		if _, taken := r.bySubdomain[cand]; !taken {
			return cand
		}
	}
	for i := 2; ; i++ {
		cand := truncateLabel(fmt.Sprintf("%s-%d", base, i))
		if _, taken := r.bySubdomain[cand]; !taken {
			return cand
		}
	}
}

func newToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// sortViews orders views in place by key, newest first for time keys.
func sortViews(v []BackendView, key string) {
	less := map[string]func(a, b BackendView) bool{
		"registered": func(a, b BackendView) bool { return a.RegisteredAt.After(b.RegisteredAt) },
		"project": func(a, b BackendView) bool {
			if a.Solution != b.Solution {
				return a.Solution < b.Solution
			}
			if a.Project != b.Project {
				return a.Project < b.Project
			}
			return a.Branch < b.Branch
		},
		"used": func(a, b BackendView) bool { return a.LastUsedAt.After(b.LastUsedAt) },
	}[key]
	if less == nil {
		less = func(a, b BackendView) bool { return a.LastUsedAt.After(b.LastUsedAt) }
	}
	sort.SliceStable(v, func(i, j int) bool { return less(v[i], v[j]) })
}
