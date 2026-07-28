// SPDX-License-Identifier: Apache-2.0

// Package hubauth is the client side of tunnel-hub authentication: it stores and
// resolves the hub API key per hub host in ~/.mxcli/auth.json, and (headless)
// mints one from a GitHub token.
//
// The primary way to obtain a key is the hub's browser page (`https://<hub>/cli`);
// `mxcli auth hub login --token` covers the headless/CI path. `mxcli run --hub`
// calls ResolveKey (MXCLI_HUB_KEY env → store) to attach an X-Hub-Key header.
package hubauth

import (
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mendixlabs/mxcli/internal/auth"
)

// EnvHubKey overrides the stored key — set once as a repo/environment secret so a
// reaped Claude Code web container re-registers without an interactive login.
const EnvHubKey = "MXCLI_HUB_KEY"

// DefaultHubURL is the hosted hub used when --hub is not given.
const DefaultHubURL = "https://hub.mxcli.org"

// HostOf returns the host[:port] of a hub URL (used as the storage key).
func HostOf(hubURL string) string {
	if !strings.Contains(hubURL, "://") {
		hubURL = "https://" + hubURL
	}
	u, err := url.Parse(hubURL)
	if err != nil || u.Host == "" {
		return strings.TrimRight(hubURL, "/")
	}
	return u.Host
}

// ProfileKey is the auth.json profile name a hub host's key is stored under.
func ProfileKey(host string) string { return "hub:" + host }

// ResolveKey returns the hub API key to present to hubURL: the MXCLI_HUB_KEY env
// var first (deterministic for web sessions), then the stored key for the host.
// Returns "" when none is configured (open-mode hubs need no key).
func ResolveKey(hubURL string) string {
	if k := strings.TrimSpace(os.Getenv(EnvHubKey)); k != "" {
		return k
	}
	store, err := loadStore()
	if err != nil {
		return ""
	}
	if cred, err := store.Get(ProfileKey(HostOf(hubURL))); err == nil {
		return cred.Token
	}
	return ""
}

// SaveKey stores a minted hub key for hubURL's host.
func SaveKey(hubURL, key string) error {
	store, err := loadStore()
	if err != nil {
		return err
	}
	return store.Put(ProfileKey(HostOf(hubURL)), &auth.Credential{
		Scheme:    auth.SchemeHubKey,
		Token:     key,
		CreatedAt: time.Now().UTC(),
	})
}

// DeleteKey removes the stored hub key for hubURL's host.
func DeleteKey(hubURL string) error {
	store, err := loadStore()
	if err != nil {
		return err
	}
	return store.Delete(ProfileKey(HostOf(hubURL)))
}

// StoredKey returns the stored key for a host (ignoring the env override) and
// whether one exists — used by `auth hub status`.
func StoredKey(hubURL string) (key string, ok bool) {
	store, err := loadStore()
	if err != nil {
		return "", false
	}
	cred, err := store.Get(ProfileKey(HostOf(hubURL)))
	if err != nil {
		return "", false
	}
	return cred.Token, true
}

// storeOverride lets tests point the store at a temp file.
var storeOverride auth.Store

func loadStore() (auth.Store, error) {
	if storeOverride != nil {
		return storeOverride, nil
	}
	return auth.DefaultFileStore()
}
