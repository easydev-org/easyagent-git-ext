package workspace

import (
	"context"
	"io"
	"strconv"
	"strings"
)

// gitProgress turns git's own progress text into phase updates.
//
// go-git writes what `git clone` prints — "Receiving objects:  42% (21/50)" — and separates the
// updates with carriage returns. Forwarding the percentage is what makes a bar move; without it a
// clone is one message and then silence, which is the case worth fixing.
//
// The reporter's own rate limit applies (one update per 400ms), so git may emit a hundred lines
// here and the bus sees a handful.
type gitProgress struct {
	ctx context.Context
	buf []byte
}

// newGitProgress returns a writer for go-git's CloneOptions.Progress.
func newGitProgress(ctx context.Context) io.Writer { return &gitProgress{ctx: ctx} }

func (g *gitProgress) Write(p []byte) (int, error) {
	for _, b := range p {
		switch b {
		case '\n', '\r':
			g.flush()
		default:
			g.buf = append(g.buf, b)
		}
	}
	return len(p), nil
}

// flush parses one line and reports the percentage, if there is one.
//
// Lines without a percentage ("done.", "remote: Enumerating objects") are ignored rather than
// guessed at: a phase with no number is what Phase() is for, and inventing a number would be
// worse than saying nothing.
func (g *gitProgress) flush() {
	line := strings.TrimSpace(string(g.buf))
	g.buf = g.buf[:0]
	if line == "" {
		return
	}
	colon := strings.LastIndex(line, ":")
	pct := strings.LastIndex(line, "%")
	if colon < 0 || pct < colon {
		return
	}
	stage := strings.ToLower(strings.TrimSpace(line[:colon]))
	frac, err := strconv.ParseFloat(strings.TrimSpace(line[colon+1:pct]), 64)
	if err != nil {
		return
	}
	switch {
	case strings.Contains(stage, "counting"):
		stage = "counting objects"
	case strings.Contains(stage, "compressing"):
		stage = "compressing objects"
	case strings.Contains(stage, "receiving"):
		stage = "receiving objects"
	case strings.Contains(stage, "resolving"):
		stage = "resolving deltas"
	}
	if r, ok := g.ctx.Value(progressKey{}).(interface {
		Phase(string)
		Percent(string, float64)
	}); ok && r != nil {
		r.Percent(stage, frac/100)
	}
}
