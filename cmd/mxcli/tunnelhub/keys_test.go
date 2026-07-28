// SPDX-License-Identifier: Apache-2.0

package tunnelhub

import "testing"

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
