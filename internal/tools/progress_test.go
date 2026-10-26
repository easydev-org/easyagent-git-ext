package tools

import (
	"context"
	"testing"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
)

func collect() (*Reporter, *[]*easybus.ToolProgress) {
	var seen []*easybus.ToolProgress
	r := NewReporter(func(p *easybus.ToolProgress) { seen = append(seen, p) })
	return r, &seen
}

// A phase is a TRANSITION. Saying "cloning" eleven times tells a reader less than saying it once.
func TestPhaseIsSentOnlyOnChange(t *testing.T) {
	r, seen := collect()
	r.Phase("cloning")
	r.Phase("cloning")
	r.Phase("cloning")
	r.Phase("checking out files")
	if len(*seen) != 2 {
		t.Fatalf("sent %d updates, want 2 (one per distinct phase)", len(*seen))
	}
	if (*seen)[0].GetPhase() != "cloning" || (*seen)[1].GetPhase() != "checking out files" {
		t.Errorf("phases = %q then %q", (*seen)[0].GetPhase(), (*seen)[1].GetPhase())
	}
}

// Percentages are rate-limited: git reports progress far faster than anybody can read it.
func TestPercentIsRateLimited(t *testing.T) {
	r, seen := collect()
	for i := 0; i < 50; i++ {
		r.Percent("cloning", float64(i)/50)
	}
	if n := len(*seen); n > 2 {
		t.Errorf("sent %d percentage updates within a burst, want at most 2", n)
	}
	// After the interval elapses, an update is allowed again.
	time.Sleep(minProgressInterval + 20*time.Millisecond)
	r.Percent("cloning", 1.0)
	if len(*seen) == 0 {
		t.Fatal("no update after the interval")
	}
	last := (*seen)[len(*seen)-1]
	if last.GetProgress() != 1 {
		t.Errorf("final progress = %v, want 1", last.GetProgress())
	}
}

// A fraction outside 0..1 is clamped rather than sent as-is: a client drawing a bar should not
// have to defend against 1.4.
func TestPercentIsClamped(t *testing.T) {
	r, seen := collect()
	r.Percent("cloning", 1.4)
	if got := (*seen)[0].GetProgress(); got != 1 {
		t.Errorf("1.4 clamped to %v, want 1", got)
	}
	r2, seen2 := collect()
	r2.Percent("cloning", -2)
	if got := (*seen2)[0].GetProgress(); got != 0 {
		t.Errorf("-2 clamped to %v, want 0", got)
	}
}

// A handler under test has no reporter, and must not have to know that.
func TestProgressWithoutReporterIsNoOp(t *testing.T) {
	r := Progress(context.Background())
	if r == nil {
		t.Fatal("Progress must never return nil")
	}
	// None of these may panic.
	r.Phase("x")
	r.Percent("x", 0.5)
	r.Note("hello")
}

// A reporter with no send function is also safe — that is the shape when the deployment starts
// without progress wired.
func TestReporterWithoutSendIsNoOp(t *testing.T) {
	r := &Reporter{}
	r.Phase("x")
	r.Percent("x", 0.5)
	r.Note("hello")
}
