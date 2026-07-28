// SPDX-License-Identifier: Apache-2.0

// Package auth provides a shared authentication layer for Mendix platform APIs.
//
// The package exposes a Credential model, pluggable storage, an env-var-first
// resolver, and an *http.Client factory that injects the correct headers for
// each Mendix API host (PAT today; API key in a follow-up).
//
// Callers never handle tokens directly — they ask for a client and make
// requests. The package keeps tokens out of logs, error messages, and
// formatted output.
package auth

import (
	"fmt"
	"time"
)

// Scheme identifies which header-injection strategy a credential uses.
type Scheme string

const (
	// SchemePAT uses "Authorization: MxToken <token>" (Content API, marketplace).
	SchemePAT Scheme = "pat"
	// SchemeHubKey is an opaque tunnel-hub API key sent as "X-Hub-Key: <token>"
	// on /api/register. Minted by `mxcli auth hub login`, stored per hub host
	// under the profile "hub:<host>".
	SchemeHubKey Scheme = "hubkey"
)

// ProfileDefault is the profile name used when none is specified.
const ProfileDefault = "default"

// Credential is a single stored authentication principal for one profile.
//
// Tokens must never be logged, printed, or included in error messages.
// The String method redacts the token; callers should prefer %s over %v.
type Credential struct {
	Profile   string    `json:"-"` // populated by the store; not serialized
	Scheme    Scheme    `json:"scheme"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

// String redacts the token. Never returns the raw secret.
func (c *Credential) String() string {
	if c == nil {
		return "<nil credential>"
	}
	return fmt.Sprintf("<%s token=REDACTED>", c.Scheme)
}

// GoString also redacts — protects against %#v accidentally leaking the token.
func (c *Credential) GoString() string { return c.String() }
