package sandbox

import (
	"context"
	"strings"
	"sync"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
)

// ProgressFunc reports an in-flight update for the current tool call.
type ProgressFunc func(ctx context.Context, p *easybus.ToolProgress)

type progressKey struct{}

// WithProgress attaches a progress sink to ctx.
//
// On the context rather than in Deps because the tenant and call id only exist
// inside the per-call closure in main, and every git command already receives ctx.
func WithProgress(ctx context.Context, fn ProgressFunc) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

// EmitProgress reports one update, or does nothing when no sink is installed.
func EmitProgress(ctx context.Context, p *easybus.ToolProgress) {
	if fn, ok := ctx.Value(progressKey{}).(ProgressFunc); ok && fn != nil {
		fn(ctx, p)
	}
}

// Progress batches command output into throttled frames.
//
// `git clone --progress` writes a status line per object received; relaying each
// one straight through would flood the bus and the browser. Batching keeps the
// stream useful and bounded.
type Progress struct {
	mu    sync.Mutex
	buf   []string
	last  time.Time
	every time.Duration
	max   int
}

// NewProgress returns a reporter at ~5 updates/second, 50 lines per frame.
func NewProgress() *Progress {
	return &Progress{every: 200 * time.Millisecond, max: 50}
}

// Add queues output (which may contain several lines).
func (p *Progress) Add(ctx context.Context, chunk string) {
	p.mu.Lock()
	for _, line := range strings.Split(strings.ReplaceAll(chunk, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		p.buf = append(p.buf, line)
	}
	due := time.Since(p.last) >= p.every
	var batch []string
	if due && len(p.buf) > 0 {
		batch, p.buf = p.buf, nil
		p.last = time.Now()
	}
	p.mu.Unlock()
	if len(batch) > 0 {
		p.emit(ctx, batch)
	}
}

// Phase reports a status line immediately: "cloning", "pushing" — sparse, and
// each one is the answer to "what is it doing".
func (p *Progress) Phase(ctx context.Context, text string) {
	EmitProgress(ctx, &easybus.ToolProgress{
		Phase: strPtr("running"),
		Text:  strPtr(text),
	})
}

// Flush sends whatever is pending.
func (p *Progress) Flush(ctx context.Context) {
	p.mu.Lock()
	batch := p.buf
	p.buf = nil
	p.last = time.Now()
	p.mu.Unlock()
	if len(batch) > 0 {
		p.emit(ctx, batch)
	}
}

func (p *Progress) emit(ctx context.Context, lines []string) {
	dropped := 0
	if len(lines) > p.max {
		dropped = len(lines) - p.max
		lines = lines[:p.max]
	}
	text := strings.Join(lines, "\n")
	if dropped > 0 {
		text += "\n… " + itoa(dropped) + " more line(s)"
	}
	EmitProgress(ctx, &easybus.ToolProgress{
		Phase: strPtr("running"),
		Text:  strPtr(text),
	})
}

func strPtr(s string) *string { return &s }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
