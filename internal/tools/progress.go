package tools

import (
	"context"
	"sync"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"

	"github.com/easydev-org/easyagent-github-ext/internal/ghclient"
	"github.com/easydev-org/easyagent-github-ext/internal/workspace"
)

// In-flight progress for a tool call.
//
// WHY THIS EXISTS: a tool that runs for a minute and says nothing is indistinguishable from one
// that has hung. The result only arrives at the end, so a caller watching the session sees silence
// either way. Reporting progress turns "no news" into "still working, on this phase".
//
// WHY IT IS ON THE CONTEXT: the alternative is a callID parameter on all 46 handler signatures.
// The callID is not something a handler decides anything with — it is plumbing from the transport
// — so it goes where plumbing belongs. A handler calls `Progress(ctx).Phase("cloning")` and does
// not have to have been given anything new.

type reporterKey struct{}

// Reporter sends progress for one tool call.
//
// All methods are safe when the reporter is absent (a handler under test, or an extension that was
// started without progress): they do nothing rather than making every caller check for nil.
type Reporter struct {
	// send publishes one update. Nil disables everything.
	send func(*easybus.ToolProgress)

	mu sync.Mutex
	// last is the previous phase, so a repeated Phase() at the same value is not resent — a
	// phase is a transition, and re-announcing it is noise.
	last string
	// lastAt rate-limits updates that carry a percentage: git emits its progress far faster than
	// anybody can read it, and one message per percent would flood the bus.
	lastAt time.Time
}

// minProgressInterval is how often a percentage update may be sent.
//
// 400ms: fast enough that a multi-minute clone visibly moves, slow enough that a fast clone sends
// a handful of messages instead of hundreds.
const minProgressInterval = 400 * time.Millisecond

// WithReporter attaches a reporter to the context.
func WithReporter(ctx context.Context, r *Reporter) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, reporterKey{}, r)
}

// Progress returns the reporter for this call, or a no-op one.
//
// Never returns nil, so a handler can call it without checking. The value receiver would be enough
// for that, but a pointer keeps the rate-limit state shared across the call.
func Progress(ctx context.Context) *Reporter {
	if r, ok := ctx.Value(reporterKey{}).(*Reporter); ok && r != nil {
		return r
	}
	return &Reporter{}
}

// progressCtx attaches the reporter to the context under every lower package's key.
//
// Each package declares its own interface rather than importing tools, so the adaptation happens
// here — the one place that knows both. A call site writes progressCtx(ctx) and nothing else.
func progressCtx(ctx context.Context) context.Context {
	r := Progress(ctx)
	ctx = ghclient.WithPhaseReporter(ctx, r)
	ctx = workspace.WithPhaseReporter(ctx, r)
	return ctx
}

// NewReporter builds a reporter that hands updates to send.
func NewReporter(send func(*easybus.ToolProgress)) *Reporter {
	return &Reporter{send: send}
}

// Phase announces what the tool is doing now.
//
// Repeated calls with the same phase are ignored: the point is to show a transition, and saying
// "cloning" eleven times tells a reader less than saying it once.
func (r *Reporter) Phase(phase string) {
	if r == nil || r.send == nil || phase == "" {
		return
	}
	r.mu.Lock()
	if r.last == phase {
		r.mu.Unlock()
		return
	}
	r.last = phase
	r.mu.Unlock()
	r.send(&easybus.ToolProgress{Phase: ptr(phase)})
}

// Percent reports how far a phase has got, from 0 to 1.
func (r *Reporter) Percent(phase string, fraction float64) {
	if r == nil || r.send == nil {
		return
	}
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	now := time.Now()
	r.mu.Lock()
	if now.Sub(r.lastAt) < minProgressInterval {
		r.mu.Unlock()
		return
	}
	r.lastAt = now
	r.last = phase
	r.mu.Unlock()
	f := float32(fraction)
	r.send(&easybus.ToolProgress{Phase: ptr(phase), Progress: &f})
}

// Note sends free text — something worth saying that is neither a phase change nor a percentage.
func (r *Reporter) Note(text string) {
	if r == nil || r.send == nil || text == "" {
		return
	}
	r.send(&easybus.ToolProgress{Text: ptr(text)})
}

// ptr is a tiny address-of helper for the optional proto fields.
func ptr[T any](v T) *T { return &v }
