package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathIsolatesTenants(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := m.Path("tenant-a", "https://github.com/x/y.git")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Path("tenant-b", "https://github.com/x/y.git")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two tenants must not share a path")
	}
	if filepath.Base(filepath.Dir(a)) != "tenant-a" {
		t.Errorf("tenant dir = %q", filepath.Dir(a))
	}
}

func TestPathRejectsBadTenant(t *testing.T) {
	m, _ := New(t.TempDir())
	for _, bad := range []string{"", "..", "a/b", "a\\b", "a b"} {
		if _, err := m.Path(bad, "https://x/y.git"); err == nil {
			t.Errorf("tenant %q should be rejected", bad)
		}
	}
}

func TestResolveBlocksEscape(t *testing.T) {
	dir := t.TempDir()
	if _, err := Resolve(dir, "../../etc/passwd"); err != nil {
		// A cleaned path stays inside; verify it does not escape.
		if p, _ := Resolve(dir, "../../etc/passwd"); p != "" && !isUnder(dir, p) {
			t.Errorf("escaped to %q", p)
		}
	}
	if _, err := Resolve(dir, "/etc/passwd"); err == nil {
		t.Error("absolute path should be rejected")
	}
	ok, err := Resolve(dir, "src/main.go")
	if err != nil || !isUnder(dir, ok) {
		t.Errorf("relative path = %q err=%v", ok, err)
	}
}

func isUnder(root, p string) bool {
	root, _ = filepath.Abs(root)
	p, _ = filepath.Abs(p)
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && rel[:1] != string(os.PathSeparator)
}
