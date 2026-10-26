package workspace

import "context"

// Progress reporting for the package's slow steps.
//
// WHY AN INTERFACE, not the tools package's Reporter: workspace must not import tools (a cycle),
// and it does not care where progress goes. It needs to say "this stage began".

type progressKey struct{}

// PhaseReporter is what a slow operation may narrate itself through.
type PhaseReporter interface {
	Phase(name string)
}

// WithPhaseReporter attaches a reporter to the context.
func WithPhaseReporter(ctx context.Context, r PhaseReporter) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, progressKey{}, r)
}

// workspacePhase reports to the attached reporter, if any.
func workspacePhase(ctx context.Context, name string) {
	if r, ok := ctx.Value(progressKey{}).(PhaseReporter); ok && r != nil {
		r.Phase(name)
	}
}
