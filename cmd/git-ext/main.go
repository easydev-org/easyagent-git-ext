// Command git-ext is the easyagent git extension server: git operations
// (clone/status/log/diff/branch/commit/pull/push/show) against any git host,
// over the easybus protocol. Pure Go (go-git) — no system `git`.
package main

import (
	"context"
	_ "embed"
	"log/slog"
	"os"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
	"github.com/easydev-org/easybus-sdk/manifest"
	"github.com/easydev-org/easybus-sdk/transport/nats"

	"github.com/easydev-org/easyagent-git-ext/internal/tools"
	"github.com/easydev-org/easyagent-git-ext/internal/workspace"
)

//go:embed manifest.yaml
var manifestYAML []byte

func main() {
	log := slog.Default().With("svc", "git-ext")

	natsURL := envOr("NATS_URL", "nats://127.0.0.1:4222")
	bus, err := nats.Connect(natsURL)
	if err != nil {
		log.Error("nats connect failed", "err", err)
		os.Exit(1)
	}

	m, err := manifest.ParseManifest(manifestYAML)
	if err != nil {
		log.Error("load manifest failed", "err", err)
		os.Exit(1)
	}

	home := envOr("GITEXT_HOME", "/data/repos")
	// Build the extension first so the tool closures can read live config from
	// it (config is per-tenant and changes at runtime).
	var ext *extension.Extension
	handlers, err := tools.Registry(tools.Deps{
		Home: home,
		GetConfig: func(name, session, tenant string) any {
			if ext == nil {
				return nil
			}
			return ext.GetConfig(tenant, name, session)
		},
		Locale: func(session, tenant string) string {
			if ext == nil {
				return ""
			}
			return ext.GetSessionVariable(context.Background(), tenant, "agent", session, "locale", "")
		},
	})
	if err != nil {
		log.Error("build tools failed", "err", err)
		os.Exit(1)
	}

	specs := make(map[string]extension.ToolSpec, len(handlers))
	for name, h := range handlers {
		h := h
		specs[name] = extension.ToolSpec{
			Execute: func(ctx context.Context, args map[string]any, callID, session, tenant string) (extension.ToolResultData, error) {
				return h(ctx, args, session, tenant)
			},
		}
	}

	cfg := m.BuildConfig(manifest.Bindings{
		Handlers: specs,
		OnLifecycle: func(ctx context.Context, ev *easybus.LifecycleEvent, tenant string) error {
			if ev.Kind != easybus.LifecycleEventKindDeleted {
				return nil
			}
			// A deleted session's clones are kept (they are tenant-scoped, not
			// session-scoped); nothing to reap here. Reserved for future GC.
			log.Info("lifecycle deleted", "tenant", tenant, "session", ev.SessionName)
			return nil
		},
	})
	ext = extension.New(bus, cfg)

	if err := extension.Serve(ext, extension.ServeOptions{
		Run: func(ctx context.Context, _ *extension.Extension) {
			log.Info("listening", "nats", natsURL, "home", home)
		},
	}); err != nil {
		log.Error("serve failed", "err", err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var _ = workspace.Resolve
