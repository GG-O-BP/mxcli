// SPDX-License-Identifier: Apache-2.0

package tunnelhub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeyStore_MintResolveRevoke(t *testing.T) {
	ks := NewKeyStore()

	key := ks.Mint("alice")
	if key == "" {
		t.Fatal("Mint returned an empty key")
	}
	if login, ok := ks.Resolve(key); !ok || login != "alice" {
		t.Errorf("Resolve = %q, %v; want alice, true", login, ok)
	}

	// Revoke returns the bound login and removes it.
	if login, ok := ks.Revoke(key); !ok || login != "alice" {
		t.Errorf("Revoke = %q, %v; want alice, true", login, ok)
	}
	if _, ok := ks.Resolve(key); ok {
		t.Error("key still resolves after revoke")
	}
	// Revoking again is a no-op (idempotent).
	if _, ok := ks.Revoke(key); ok {
		t.Error("second revoke should report not-found")
	}
}

func TestKeyStore_UnknownAndEmpty(t *testing.T) {
	ks := NewKeyStore()
	if _, ok := ks.Resolve(""); ok {
		t.Error("empty key must not resolve")
	}
	if _, ok := ks.Resolve("bogus"); ok {
		t.Error("unknown key must not resolve")
	}
}

func TestKeyStore_DistinctKeysPerMint(t *testing.T) {
	ks := NewKeyStore()
	k1 := ks.Mint("alice")
	k2 := ks.Mint("alice")
	if k1 == k2 {
		t.Error("each Mint must return a distinct key")
	}
	// Both are valid until individually revoked.
	if _, ok := ks.Resolve(k1); !ok {
		t.Error("k1 should resolve")
	}
	ks.Revoke(k1)
	if _, ok := ks.Resolve(k2); !ok {
		t.Error("revoking k1 must not affect k2")
	}
}

// TestKeyStore_StoresHashedNotPlaintext guards the "a store dump yields no usable
// credential" property: the plain key must not appear as a map key.
func TestKeyStore_StoresHashedNotPlaintext(t *testing.T) {
	ks := NewKeyStore()
	key := ks.Mint("alice")
	if _, present := ks.byHash[key]; present {
		t.Error("plain key must not be a map key — keys are stored hashed")
	}
	if _, present := ks.byHash[hashKey(key)]; !present {
		t.Error("hashed key should be the map key")
	}
}

// TestKeyStore_PersistsAcrossRestart is the core of "keys outlive the session":
// a key minted by one store instance resolves in a fresh instance loaded from the
// same file (simulating a hub restart), and a revoke is likewise durable.
func TestKeyStore_PersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub-keys.json")

	s1, err := NewKeyStoreFile(path)
	if err != nil {
		t.Fatalf("NewKeyStoreFile: %v", err)
	}
	key := s1.Mint("alice")

	// A brand-new store (hub restart) loads the same file and still knows the key.
	s2, err := NewKeyStoreFile(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if login, ok := s2.Resolve(key); !ok || login != "alice" {
		t.Errorf("after restart Resolve = %q, %v; want alice, true", login, ok)
	}

	// Revoke through s2, then reopen again — the revoke persisted.
	s2.Revoke(key)
	s3, _ := NewKeyStoreFile(path)
	if _, ok := s3.Resolve(key); ok {
		t.Error("revoked key must not resolve after restart")
	}
}

func TestKeyStore_FileMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub-keys.json")
	s, _ := NewKeyStoreFile(path)
	s.Mint("alice")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file mode = %o, want 600", perm)
	}
}

func TestKeyStore_RejectsTooOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub-keys.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"keys":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewKeyStoreFile(path); err == nil {
		t.Error("a world-readable key file should be refused")
	}
}

func TestKeyStore_CountAndRevokeLogin(t *testing.T) {
	ks := NewKeyStore()
	ks.Mint("alice")
	ks.Mint("alice")
	ks.Mint("bob")

	if n := ks.CountLogin("alice"); n != 2 {
		t.Errorf("alice count = %d, want 2", n)
	}
	if n := ks.CountLogin("carol"); n != 0 {
		t.Errorf("carol count = %d, want 0", n)
	}
	// Revoke all of alice's keys; bob's are untouched.
	if n := ks.RevokeLogin("alice"); n != 2 {
		t.Errorf("RevokeLogin(alice) = %d, want 2", n)
	}
	if n := ks.CountLogin("alice"); n != 0 {
		t.Errorf("alice count after revoke = %d, want 0", n)
	}
	if n := ks.CountLogin("bob"); n != 1 {
		t.Errorf("bob count = %d, want 1 (unaffected)", n)
	}
}
