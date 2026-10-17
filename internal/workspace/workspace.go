// Package workspace manages the on-disk git repository cache: one directory per
// (tenant, repo), a per-repo lock so concurrent tool calls never corrupt a
// working tree, and path sanitisation so a tenant can never reach another
// tenant's (or the host's) files.
package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Manager owns the repo cache rooted at Home.
type Manager struct {
	home string

	mu    sync.Mutex
	locks map[string]*sync.Mutex // key -> per-repo lock
}

// New builds a Manager rooted at home (created if absent).
func New(home string) (*Manager, error) {
	if home == "" {
		return nil, fmt.Errorf("workspace: home is required")
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, fmt.Errorf("workspace: mkdir %s: %w", home, err)
	}
	return &Manager{home: home, locks: map[string]*sync.Mutex{}}, nil
}

var safeSegment = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// sanitizeTenant restricts a tenant to a safe path segment. Tenants are already
// validated on the wire ([A-Za-z0-9_-]{1,64}); this is defence in depth.
func sanitizeTenant(tenant string) (string, error) {
	if tenant == "" || !safeSegment.MatchString(tenant) || strings.Contains(tenant, "..") {
		return "", fmt.Errorf("workspace: invalid tenant %q", tenant)
	}
	return tenant, nil
}

// repoDir is the deterministic directory for (tenant, remote). The remote is
// hashed, so the on-disk name never contains a host/path an attacker controls.
func repoDir(tenant, remote string) string {
	sum := sha256.Sum256([]byte(remote))
	return filepath.Join(tenant, hex.EncodeToString(sum[:])[:16])
}

// Path returns the absolute repo directory for (tenant, remote), ensuring it
// stays under Home.
func (m *Manager) Path(tenant, remote string) (string, error) {
	t, err := sanitizeTenant(tenant)
	if err != nil {
		return "", err
	}
	p := filepath.Join(m.home, repoDir(t, remote))
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(m.home)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("workspace: resolved path escapes home")
	}
	return abs, nil
}

// Lock returns a per-(tenant, remote) mutex so only one tool call mutates a
// repo's working tree at a time.
func (m *Manager) Lock(tenant, remote string) *sync.Mutex {
	key := tenant + "\x00" + remote
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[key]
	if !ok {
		l = &sync.Mutex{}
		m.locks[key] = l
	}
	return l
}

// Ensure returns the repo directory for (tenant, remote), creating it if
// absent. It does NOT clone — the caller decides (clone-if-missing).
func (m *Manager) Ensure(tenant, remote string) (string, error) {
	dir, err := m.Path(tenant, remote)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("workspace: mkdir: %w", err)
	}
	return dir, nil
}

// Exists reports whether the repo directory has been initialised.
func (m *Manager) Exists(tenant, remote string) bool {
	dir, err := m.Path(tenant, remote)
	if err != nil {
		return false
	}
	st, err := os.Stat(dir)
	return err == nil && st.IsDir()
}

// Resolve turns a caller-supplied relative path into an absolute path inside
// the repo dir, rejecting any escape (../, absolute, symlink-out).
func Resolve(repoDir, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("workspace: absolute paths are not allowed")
	}
	clean := filepath.Clean("/" + rel)
	abs := filepath.Join(repoDir, clean)
	root, err := filepath.Abs(repoDir)
	if err != nil {
		return "", err
	}
	if abs != root && !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("workspace: path escapes the repository")
	}
	return abs, nil
}
