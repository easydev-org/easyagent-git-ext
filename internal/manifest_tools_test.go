package internal

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestManifestDeclaresEveryRegisteredTool guards the seam between the Go registry
// (what the process can execute) and manifest.yaml (what the client can SEE).
//
// They are two separate lists, and a tool missing from the manifest is invisible
// even though the code works: that is exactly how four MR tools were written,
// compiled, tested — and never reached the running extension.
func TestManifestDeclaresEveryRegisteredTool(t *testing.T) {
	root := repoRoot(t)

	registered := registryToolNames(t, filepath.Join(root, "internal", "tools", "tools.go"))
	declared := manifestToolNames(t, filepath.Join(root, "manifest.yaml"))

	if len(registered) == 0 {
		t.Fatal("found no tools in the registry; the parser is staring at the wrong file")
	}
	for name := range registered {
		if !declared[name] {
			t.Errorf("tool %q is registered in Go but NOT declared in manifest.yaml — "+
				"the client will never see it", name)
		}
	}
	for name := range declared {
		if !registered[name] {
			t.Errorf("tool %q is declared in manifest.yaml but NOT registered in Go — "+
				"calling it would fail at runtime", name)
		}
	}
}

// registryToolNames parses the `"git-...": t.handler` pairs of the Registry func.
func registryToolNames(t *testing.T, path string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	// Match entries like:  "git-clone":    t.clone,
	re := regexp.MustCompile(`"(git-[a-z-]+)"\s*:\s*t\.`)
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		out[m[1]] = true
	}
	return out
}

// manifestToolNames returns the tool names declared under `tools:`.
func manifestToolNames(t *testing.T, path string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	out := map[string]bool{}
	inTools := false
	nameRE := regexp.MustCompile(`^  - name: (git-[a-z-]+)\s*$`)
	for _, line := range strings.Split(string(b), "\n") {
		// The tools block ends at the next top-level key.
		if strings.HasPrefix(line, "tools:") {
			inTools = true
			continue
		}
		if inTools && line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#") {
			inTools = false
		}
		if !inTools {
			continue
		}
		if m := nameRE.FindStringSubmatch(line); m != nil {
			out[m[1]] = true
		}
	}
	return out
}
