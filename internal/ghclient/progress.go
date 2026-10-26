package ghclient

import "context"

// Progress reporting for the client's own slow steps.
//
// WHY AN INTERFACE HERE, not the tools package's Reporter: the client must not import tools (that
// direction would be a cycle), and it has no business knowing how progress reaches a session. It
// needs one thing — "tell somebody this phase started" — and this is the smallest declaration of
// that.

type progressKey struct{}

// PhaseReporter is what a slow client operation may narrate itself through.
type PhaseReporter interface {
	// Phase announces that a named stage has begun.
	Phase(name string)
}

// WithPhaseReporter attaches a reporter to the context.
func WithPhaseReporter(ctx context.Context, r PhaseReporter) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, progressKey{}, r)
}

// phase reports to the attached reporter, if any. Safe when there is none.
func phase(ctx context.Context, name string) {
	if r, ok := ctx.Value(progressKey{}).(PhaseReporter); ok && r != nil {
		r.Phase(name)
	}
}
