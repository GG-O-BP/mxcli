// SPDX-License-Identifier: Apache-2.0

package hubauth

import (
	"path/filepath"
	"testing"

	"github.com/mendixlabs/mxcli/internal/auth"
)

func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"https://hub.mxcli.org":        "hub.mxcli.org",
		"https://hub.mxcli.org/":       "hub.mxcli.org",
		"http://localhost:8080/x":      "localhost:8080",
		"hub.mxcli.org":                "hub.mxcli.org",
		"https://hub.example.com/api/": "hub.example.com",
	}
	for in, want := range cases {
		if got := HostOf(in); got != want {
			t.Errorf("HostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// withTempStore points the package store at a fresh temp auth.json and restores
// the previous override afterward.
func withTempStore(t *testing.T) {
	t.Helper()
	prev := storeOverride
	storeOverride = auth.NewFileStore(filepath.Join(t.TempDir(), "auth.json"))
	t.Cleanup(func() { storeOverride = prev })
}

func TestKeyStoreRoundTrip(t *testing.T) {
	withTempStore(t)
	t.Setenv(EnvHubKey, "") // ensure env override is off

	const hub = "https://hub.mxcli.org"
	if k := ResolveKey(hub); k != "" {
		t.Fatalf("expected no key initially, got %q", k)
	}
	if _, ok := StoredKey(hub); ok {
		t.Fatal("StoredKey should report none initially")
	}

	if err := SaveKey(hub, "hk_secret"); err != nil {
		t.Fatalf("SaveKey: %v", err)
	}
	if k := ResolveKey(hub); k != "hk_secret" {
		t.Errorf("ResolveKey = %q, want hk_secret", k)
	}
	if k, ok := StoredKey(hub); !ok || k != "hk_secret" {
		t.Errorf("StoredKey = %q, %v; want hk_secret, true", k, ok)
	}
	// Host-keyed: a different hub has no key.
	if k := ResolveKey("https://other.example.com"); k != "" {
		t.Errorf("different host should have no key, got %q", k)
	}

	if err := DeleteKey(hub); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if k := ResolveKey(hub); k != "" {
		t.Errorf("ResolveKey after delete = %q, want empty", k)
	}
}

func TestResolveKey_EnvOverridesStore(t *testing.T) {
	withTempStore(t)
	const hub = "https://hub.mxcli.org"
	if err := SaveKey(hub, "stored-key"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvHubKey, "env-key")
	if k := ResolveKey(hub); k != "env-key" {
		t.Errorf("ResolveKey = %q, want env-key (env must win over store)", k)
	}
}

func TestProfileKey(t *testing.T) {
	if got := ProfileKey("hub.mxcli.org"); got != "hub:hub.mxcli.org" {
		t.Errorf("ProfileKey = %q", got)
	}
}
