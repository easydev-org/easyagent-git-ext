package tools

import (
	"strings"
	"testing"

	"github.com/easydev-org/easyagent-gitdev-ext/internal/ghclient"
)

// The tri-state must read as three different things.
//
// Printing the pointer with %v produced "mergeable: 0x3bbecddc62d0" — an address, which a reader
// takes for a value. The three states are not decorative: nil means GitHub has not finished
// computing (ask again), false means it cannot be merged (do something about it).
func TestMergeableRendersThreeStates(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		val  *bool
		want string
	}{
		{nil, "unknown"},
		{&yes, "yes"},
		{&no, "NO"},
	}
	for _, c := range cases {
		got := renderMergeable(c.val)
		if !strings.Contains(got, c.want) {
			t.Errorf("mergeable %v rendered as %q, want it to contain %q", c.val, got, c.want)
		}
		// It must never render an address.
		if strings.Contains(got, "0x") {
			t.Errorf("mergeable %v rendered a pointer address: %q", c.val, got)
		}
	}
}

var _ = ghclient.PullRequest{}
