package internal

import (
	"os"
	"path/filepath"
	"testing"
)

// TestManifestCopiesMatch guards against the embedded copy (cmd/gitea-ext, which
// go:embed requires to be a real file) drifting from the canonical root one.
//
// WHY the root is located by walking UP instead of a fixed "../../": the old
// version SKIPPED when the path did not resolve, which is the one thing a guard
// must never do — it reported success while checking nothing, and the drift it
// was written to prevent went unnoticed twice.
func TestManifestCopiesMatch(t *testing.T) {
	root := repoRoot(t)
	canonical, err := os.ReadFile(filepath.Join(root, "manifest.yaml"))
	if err != nil {
		t.Fatalf("read canonical manifest: %v", err)
	}
	embedded, err := os.ReadFile(filepath.Join(root, "cmd", "gitea-ext", "manifest.yaml"))
	if err != nil {
		t.Fatalf("read embedded copy: %v", err)
	}
	if string(canonical) != string(embedded) {
		t.Fatal("cmd/gitea-ext/manifest.yaml differs from manifest.yaml; copy it again " +
			"(go:embed reads the COPY, so editing only the root one ships the old manifest)")
	}
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate the module root (no go.mod found above the test directory)")
		}
		dir = parent
	}
}
