// SPDX-License-Identifier: Apache-2.0

package tunnelhub

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// KeyStore maps opaque hub API keys to the GitHub login they were minted for.
// Registration presents a key (X-Hub-Key) instead of a GitHub token, so the
// GitHub token never has to reach the hub on the hot path — only once, at mint.
//
// Keys are stored hashed (SHA-256): a dump of the store never yields a usable
// credential, mirroring how the audit trail refuses to carry secrets. The plain
// key is returned to the caller exactly once, at Mint.
//
// In-memory for the first cut (keys are lost on hub restart; a user re-runs
// `mxcli auth hub login`). All methods are safe for concurrent use.
type KeyStore struct {
	mu       sync.RWMutex
	byHash   map[string]string // sha256(key) -> login
	newToken func() string     // overridable for tests; default newToken()
}

// NewKeyStore returns an empty key store.
func NewKeyStore() *KeyStore {
	return &KeyStore{byHash: map[string]string{}}
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (s *KeyStore) mintToken() string {
	if s.newToken != nil {
		return s.newToken()
	}
	return newToken()
}

// Mint issues a fresh opaque key bound to login and returns the plain key (shown
// to the caller once — only its hash is retained).
func (s *KeyStore) Mint(login string) string {
	key := s.mintToken()
	s.mu.Lock()
	s.byHash[hashKey(key)] = login
	s.mu.Unlock()
	return key
}

// Resolve returns the login a key was minted for, and whether it is known.
func (s *KeyStore) Resolve(key string) (login string, ok bool) {
	if key == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	login, ok = s.byHash[hashKey(key)]
	return login, ok
}

// Revoke removes a key. It returns the login it was bound to (for auditing) and
// whether it existed.
func (s *KeyStore) Revoke(key string) (login string, ok bool) {
	if key == "" {
		return "", false
	}
	h := hashKey(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	login, ok = s.byHash[h]
	if ok {
		delete(s.byHash, h)
	}
	return login, ok
}
