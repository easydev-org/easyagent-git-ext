package workspace

import (
	"context"
	"io"
	"strings"
)

// gitProgress forwards git's own output, unchanged.
//
// WHY NOT PARSE IT: the first attempt lifted the percentage out of "Receiving objects:  42%
// (21/50)" and re-labelled it ("receiving objects", 0.42). That is the extension rewriting git —
// a reader who knows what a clone looks like then sees something they did not ask for, and every
// line git prints that ISN'T "stage: n%" was silently dropped. The output below is what
// `git clone` prints, which is the thing people already know how to read.
//
// One line per update, because git separates them with carriage returns and a caller reading a
// transcript should not receive half an overwritten line.
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

// flush emits one line exactly as git wrote it.
func (g *gitProgress) flush() {
	line := strings.TrimSpace(string(g.buf))
	g.buf = g.buf[:0]
	if line == "" {
		return
	}
	if r, ok := g.ctx.Value(progressKey{}).(TextReporter); ok && r != nil {
		r.Text(line)
	}
}
