// SPDX-License-Identifier: Apache-2.0

package tunnelhub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// KeyStore maps opaque hub API keys to the GitHub login they were minted for.
// Registration presents a key (X-Hub-Key) instead of a GitHub token, so the
// GitHub token never has to reach the hub on the hot path — only once, at mint.
//
// Keys are stored hashed (SHA-256): a dump of the store never yields a usable
// credential, mirroring how the audit trail refuses to carry secrets. The plain
// key is returned to the caller exactly once, at Mint.
//
// A key does not expire and is not tied to any browser session — it stays valid
// until explicitly revoked, so a user configures MXCLI_HUB_KEY once. When a file
// path is set the store is durable: keys survive hub restarts (otherwise every
// restart would force everyone to re-mint). All methods are safe for concurrent
// use.
type KeyStore struct {
	mu       sync.RWMutex
	byHash   map[string]keyRecord // sha256(key) -> record
	path     string               // "" = in-memory only (no persistence)
	newToken func() string        // overridable for tests; default newToken()
	now      func() time.Time     // overridable for tests; default time.Now
}

// keyRecord is the persisted per-key metadata (never the plaintext key).
type keyRecord struct {
	Login     string    `json:"login"`
	CreatedAt time.Time `json:"createdAt"`
}

// keysFile is the on-disk layout for the durable key store.
type keysFile struct {
	Version int                  `json:"version"`
	Keys    map[string]keyRecord `json:"keys"`
}

const keysFileVersion = 1

// NewKeyStore returns an empty in-memory key store (no persistence). Suitable for
// tests and open-mode hubs; keys are lost on restart.
func NewKeyStore() *KeyStore {
	return &KeyStore{byHash: map[string]keyRecord{}}
}

// NewKeyStoreFile returns a durable key store backed by path. An existing file is
// loaded; mint/revoke write through (atomic, mode 0600). Keys persist across hub
// restarts so users need not reconfigure MXCLI_HUB_KEY.
func NewKeyStoreFile(path string) (*KeyStore, error) {
	s := &KeyStore{byHash: map[string]keyRecord{}, path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
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

func (s *KeyStore) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now().UTC()
}

// Mint issues a fresh opaque key bound to login and returns the plain key (shown
// to the caller once — only its hash is retained). The store is persisted when
// backed by a file.
func (s *KeyStore) Mint(login string) string {
	key := s.mintToken()
	s.mu.Lock()
	s.byHash[hashKey(key)] = keyRecord{Login: login, CreatedAt: s.clock()}
	_ = s.saveLocked()
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
	rec, ok := s.byHash[hashKey(key)]
	return rec.Login, ok
}

// Revoke removes a key. It returns the login it was bound to (for auditing) and
// whether it existed. Persisted when backed by a file.
func (s *KeyStore) Revoke(key string) (login string, ok bool) {
	if key == "" {
		return "", false
	}
	h := hashKey(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byHash[h]
	if ok {
		delete(s.byHash, h)
		_ = s.saveLocked()
	}
	return rec.Login, ok
}

// load reads the key file into memory. A missing file is not an error (fresh
// store). Refuses a world/group-readable file on Unix (keys metadata is sensitive).
func (s *KeyStore) load() error {
	if s.path == "" {
		return nil
	}
	info, err := os.Stat(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("key store: stat %s: %w", s.path, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("key store %s has too-open permissions %o (want 0600)", s.path, info.Mode().Perm())
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("key store: read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		return nil
	}
	var kf keysFile
	if err := json.Unmarshal(data, &kf); err != nil {
		return fmt.Errorf("key store: parse %s: %w", s.path, err)
	}
	if kf.Keys != nil {
		s.byHash = kf.Keys
	}
	return nil
}

// saveLocked atomically writes the store to disk (temp + rename, mode 0600). The
// caller must hold s.mu. No-op for an in-memory store.
func (s *KeyStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(keysFile{Version: keysFileVersion, Keys: s.byHash}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".hub-keys.*")
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
