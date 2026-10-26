// Package e2e drives this extension over a REAL broker against a REAL GitHub repository.
//
// The AGENT IS NOT PART OF THIS. A tool call is a request on
// `easybus.<tenant>.tool.call.<extId>.<tool>`, so this test plays the agent's part with the SDK's
// own client: `CallTool` uses `Request`, which sets ReplyTo, and the extension SILENTLY IGNORES a
// call that arrives without one.
//
// The repository is real because the tool under test is git: a clone of a stub would prove nothing
// about credentials, transport or branch handling, which is where this extension actually fails.
//
// Gated on E2E_NATS_URL and E2E_GITHUB_TOKEN: the unit suite has no services and no credential, and
// a test that fails for want of either teaches people to ignore red.
package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/agent"
	"github.com/easydev-org/easybus-sdk/extension"
	"github.com/easydev-org/easybus-sdk/manifest"
	"github.com/easydev-org/easybus-sdk/transport/nats"

	"github.com/easydev-org/easyagent-github-ext/internal/tools"
	"github.com/easydev-org/easyagent-github-ext/internal/workspace"
)

const (
	extID   = "github"
	tenant  = "e2e"
	session = "s-e2e"
)

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// harness wires the extension to a real broker and returns an agent-side client plus a way to
// configure it.
func harness(t *testing.T) (*agent.Agent, *nats.Bus) {
	t.Helper()
	natsURL := os.Getenv("E2E_NATS_URL")
	if natsURL == "" {
		t.Skip("E2E_NATS_URL not set")
	}

	raw, err := os.ReadFile("../../manifest.yaml")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	m, err := manifest.ParseManifest(raw)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	extBus, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("connect ext: %v", err)
	}
	t.Cleanup(func() { _ = extBus.Close() })

	// A workspace root under the test's own temp dir: the extension keeps checkouts on LOCAL disk by
	// design, and sharing one between runs would leave stale clones that make a later test pass by
	// accident.
	wsMgr, err := workspace.NewManager(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatalf("workspace manager: %v", err)
	}

	var ext *extension.Extension
	handlers := tools.Registry(tools.Deps{
		Workspaces: wsMgr,
		PutObject: func(ctx context.Context, tn, name string, data []byte) error {
			if ext == nil {
				return fmt.Errorf("extension not ready")
			}
			return ext.PutObject(ctx, tn, name, data)
		},
		GetConfig: func(name, sess, tn string) any {
			if ext == nil {
				return nil
			}
			return ext.GetConfig(tn, name, sess)
		},
		Locale:         func(sess, tn string) string { return "" },
		ReportProgress: func(ctx context.Context, tn, callID string, p any) error { return nil },
	})
	specs := make(map[string]extension.ToolSpec, len(handlers))
	requires := tools.RequireMap()
	for name, h := range handlers {
		h := h
		specs[name] = extension.ToolSpec{
			Requires: requires[name],
			Execute: func(ctx context.Context, args map[string]any, callID, sess, tn string) (extension.ToolResultData, error) {
				return h(ctx, args, callID, sess, tn)
			},
		}
	}
	cfg := m.BuildConfig(manifest.Bindings{Handlers: specs})
	ext = extension.New(extBus, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = ext.Serve(ctx) }()
	t.Cleanup(func() { _ = ext.Close() })

	agBus, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("connect agent: %v", err)
	}
	t.Cleanup(func() { _ = agBus.Close() })
	return agent.New(agBus), agBus
}

// configure writes the token the way the console does, so the extension sees a write from somebody
// else — the same relationship a real console has to it.
func configure(t *testing.T, bus *nats.Bus, token string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// A SECRET knob: a JSON string scalar, which is one of the shapes the decoder accepts.
	if err := bus.KVPut(ctx, "cfg", fmt.Sprintf("t.%s.%s.token", tenant, extID),
		fmt.Sprintf("%q", token), 0); err != nil {
		t.Fatalf("put token: %v", err)
	}
}

func ctxWithTimeout(t *testing.T, seconds int) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// callTool calls a tool and returns content, failing on a tool error. Grants are passed because
// several of these tools declare `requires`, and a call without them is refused before the work
// begins — the extension enforcing its manifest, not a bug in it.
func callTool(t *testing.T, a *agent.Agent, tool string, args map[string]any, grants []*easybus.Grant) string {
	t.Helper()
	ctx, cancel := ctxWithTimeout(t, 180)
	defer cancel()
	tr, err := a.CallToolWithGrants(ctx, tenant, session, extID, tool,
		fmt.Sprintf("c-%s-%d", tool, time.Now().UnixNano()), args, grants)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if tr.Error != nil {
		t.Fatalf("%s: %s", tool, tr.Error.Message)
	}
	return tr.Content
}
