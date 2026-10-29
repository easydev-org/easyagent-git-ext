package tools

import (
	"fmt"
	"strings"
)

/**
 * indent prefixes every line of s, for a live test that prints a multi-line result.
 *
 * Lives here rather than in one of the live test files: several of them use it, and Go compiles all
 * of a package's test files together — so putting it in any single one would make the others depend
 * on that file existing.
 */
func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = fmt.Sprintf("    %s", l)
	}
	return strings.Join(lines, "\n")
}
