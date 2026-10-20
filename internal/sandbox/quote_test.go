package sandbox

import "testing"

// TestQuoteIfNeededDoesNotDoubleQuote is the regression test for a bug that made
// `git-add` fail on every path.
//
// Callers pass values that are ALREADY shell-quoted (paths, refs, messages), and
// Git() used to quote them again. `'E2E.md'` became `”E2E.md”`: an empty quoted
// string followed by the literal text, which git parsed as a pathspec named
// "'E2E.md'" and rejected with "did not match any files".
func TestQuoteIfNeededDoesNotDoubleQuote(t *testing.T) {
	cases := map[string]string{
		// Already quoted by the caller: unchanged.
		`'E2E.md'`: `'E2E.md'`,
		`'my dir'`: `'my dir'`,
		// Not quoted, nothing to escape: left bare so the command stays readable
		// in logs (quoteArg's documented behaviour).
		"E2E.md": `E2E.md`,
		// Not quoted but it NEEDS quoting: quoted now.
		"a b": `'a b'`,
		// Plain flags stay readable so logs show what actually ran.
		"--short": "--short",
		"HEAD":    "HEAD",
		"main":    "main",
		"v1.2.3":  "v1.2.3",
	}
	for in, want := range cases {
		if got := quoteIfNeeded(in); got != want {
			t.Errorf("quoteIfNeeded(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestQuoteIfNeededKeepsEscapedSingleQuote: shellQuote turns an inner ' into
// '\” and the result must survive untouched, or a path with an apostrophe
// would be mangled.
func TestQuoteIfNeededKeepsEscapedSingleQuote(t *testing.T) {
	in := shellQuote("it's a file.md")
	if got := quoteIfNeeded(in); got != in {
		t.Errorf("quoteIfNeeded(%q) = %q, want it unchanged", in, got)
	}
}
