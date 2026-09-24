package beads

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/gastownhall/gascity/internal/deps"
)

// bdListBriefMinVersion is the first bd whose list verb accepts --brief.
const bdListBriefMinVersion = "1.3.0"

// BdVersionMemo shares one `bd version` answer across every BdStore that
// carries it, so a caller that rebuilds a store per scan pays the subprocess
// once rather than once per store. The control dispatcher rebuilds its scope
// store on every re-prime, and each rebuild used to re-spawn `bd version`
// (gsc-dtay).
//
// An answer is keyed by the identity of the bd binary on PATH (resolved path,
// size, and modification time), so replacing bd invalidates it without a
// restart. When bd cannot be located on PATH nothing is memoized and every
// store probes for itself, exactly as a store without a memo does.
type BdVersionMemo struct {
	mu      sync.Mutex
	key     bdBinaryIdentity
	version string
}

type bdBinaryIdentity struct {
	path    string
	size    int64
	modTime time.Time
}

// NewBdVersionMemo returns an empty memo to share between BdStores.
func NewBdVersionMemo() *BdVersionMemo {
	return &BdVersionMemo{}
}

// WithBdStoreVersionMemo makes the store take its bd version from m, and
// record it there, instead of probing once per store.
func WithBdStoreVersionMemo(m *BdVersionMemo) BdStoreOption {
	return func(s *BdStore) {
		s.versionMemo = m
	}
}

// bdBinaryIdentityFn resolves the identity the memo is keyed on. It is a seam
// so tests can model a bd upgrade without replacing a binary on PATH.
var bdBinaryIdentityFn = currentBdBinaryIdentity

func currentBdBinaryIdentity() (bdBinaryIdentity, bool) {
	path, err := exec.LookPath("bd")
	if err != nil {
		return bdBinaryIdentity{}, false
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	info, err := os.Stat(path)
	if err != nil {
		return bdBinaryIdentity{}, false
	}
	return bdBinaryIdentity{path: path, size: info.Size(), modTime: info.ModTime()}, true
}

func (m *BdVersionMemo) lookup(key bdBinaryIdentity) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.version == "" || m.key != key {
		return "", false
	}
	return m.version, true
}

func (m *BdVersionMemo) store(key bdBinaryIdentity, version string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.key = key
	m.version = version
}

// bdCLIVersion returns the parsed version of the bd this store drives. A
// successful answer is memoized on the store, and on its shared memo when it
// has one; a failed probe is not, so the next caller probes again.
func (s *BdStore) bdCLIVersion() (string, error) {
	s.versionMu.Lock()
	defer s.versionMu.Unlock()
	if s.version != "" {
		return s.version, nil
	}
	key, keyed := bdBinaryIdentity{}, false
	if s.versionMemo != nil {
		key, keyed = bdBinaryIdentityFn()
		if keyed {
			if version, ok := s.versionMemo.lookup(key); ok {
				s.version = version
				return version, nil
			}
		}
	}
	out, err := s.runner(s.dir, "bd", "version")
	if err != nil {
		return "", err
	}
	version, err := parseBDVersion(string(out))
	if err != nil {
		return "", err
	}
	s.version = version
	if keyed {
		s.versionMemo.store(key, version)
	}
	return version, nil
}

// listBriefSupported reports whether bd list accepts --brief. A bd whose
// version cannot be read is treated as not supporting it, which costs the
// caller bytes, never correctness.
func (s *BdStore) listBriefSupported() bool {
	version, err := s.bdCLIVersion()
	if err != nil {
		return false
	}
	return deps.CompareVersions(version, bdListBriefMinVersion) >= 0
}
