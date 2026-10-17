package internal

import (
	"os"
	"testing"
)

// TestManifestCopiesMatch guards against the embedded copy (cmd/git-ext, which
// go:embed requires to be a real file) drifting from the canonical root one.
func TestManifestCopiesMatch(t *testing.T) {
	root, err := os.ReadFile("../../manifest.yaml")
	if err != nil {
		t.Skip("manifest not found from test cwd")
	}
	cmd, err := os.ReadFile("../../cmd/git-ext/manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(root) != string(cmd) {
		t.Fatal("cmd/git-ext/manifest.yaml differs from manifest.yaml; copy it again")
	}
}
