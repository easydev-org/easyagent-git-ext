package tools

import (
	"context"
	"strings"
	"testing"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
)

// TestPushIsRefused: `git-push` must not reach the remote at all. It explains the
// workflow instead, so a caller that remembers the name is not left guessing why
// nothing happened.
func TestPushIsRefused(t *testing.T) {
	tool := &toolset{}
	_, err := tool.refusePush(context.Background(), map[string]any{}, "s", "t")
	if err == nil {
		t.Fatal("git-push must be refused")
	}
	te, ok := err.(*extension.TypedError)
	if !ok {
		t.Fatalf("want a TypedError, got %T", err)
	}
	if te.Code != easybus.ToolResultErrorCodePermissionDenied {
		t.Errorf("code = %v, want permission_denied", te.Code)
	}
	// The message has to name the replacement, or the caller cannot recover.
	if !strings.Contains(te.Message, "git-submit-mr") {
		t.Errorf("the refusal must point at git-submit-mr, got: %s", te.Message)
	}
}

// TestMRHeadBranchIsUniqueAndPrefixed: every submission must land on a NEW
// agent/* branch. If two submissions reused a name, the second would rewrite the
// head of an MR a reviewer is already looking at.
func TestMRHeadBranchIsUniqueAndPrefixed(t *testing.T) {
	a := mrHeadBranch("session-one")
	b := mrHeadBranch("session-one")
	if a == b {
		t.Fatal("two submissions from one session must NOT share a head branch")
	}
	for _, name := range []string{a, b} {
		if !strings.HasPrefix(name, mrBranchPrefix) {
			t.Errorf("%q must start with %q", name, mrBranchPrefix)
		}
		if strings.ContainsAny(name, " ~^:?*[\\") || strings.Contains(name, "..") {
			t.Errorf("%q contains characters git refuses in a ref", name)
		}
	}
	// Different sessions must not collide either.
	if mrHeadBranch("session-two") == a {
		t.Error("different sessions produced the same head")
	}
}

// TestSanitizeBranchPart: a session name is arbitrary text; a branch name is not.
func TestSanitizeBranchPart(t *testing.T) {
	cases := map[string]string{
		"Simple":             "simple",
		"with spaces here":   "with-spaces-here",
		"a/b.c":              "a-b-c",
		"--leading-dashes--": "leading-dashes",
	}
	for in, want := range cases {
		if got := sanitizeBranchPart(in); got != want {
			t.Errorf("sanitizeBranchPart(%q) = %q, want %q", in, got, want)
		}
	}
	// An empty result must not produce a bare "agent/-<ts>" ref.
	if mrHeadBranch("///") == "" || strings.HasPrefix(mrHeadBranch("///"), mrBranchPrefix+"-") {
		t.Error("an unusable session must fall back to a safe segment, not an empty one")
	}
	// A very long session must be truncated so the ref stays usable.
	long := strings.Repeat("x", 200)
	if got := sanitizeBranchPart(long); len(got) > 40 {
		t.Errorf("long session not truncated: %d chars", len(got))
	}
}
