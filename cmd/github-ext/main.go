// github-ext serves the gh-* tools: work on GitHub repositories and their CI with no local
// sandbox.
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
	easyvcsstore "github.com/easylab-platform/easyvcs/store"

	"github.com/easydev-org/easyagent-github-ext/internal/stage"
	"github.com/easydev-org/easyagent-github-ext/internal/tools"
)

//go:embed manifest.yaml
var manifestYAML []byte

func main() {
	log := slog.Default().With("svc", "github-ext")

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

	// The staging area needs an easyvcs database. It is opened eagerly so a bad DSN fails HERE,
	// with a clear message, rather than inside the first user's tool call — and the extension
	// refuses to start without it, because every write tool depends on it and advertising tools
	// that cannot work is worse than not starting.
	dsn := envOr("EASYVCS_DSN", "")
	if dsn == "" {
		log.Error("EASYVCS_DSN is required: the staging area holds the edits a pull request is built from")
		os.Exit(1)
	}
	kind := envOr("EASYVCS_DRIVER", "postgres")
	cs, err := easyvcsstore.OpenDriver(easyvcsstore.DriverConfig{Kind: kind, DSN: dsn})
	if err != nil {
		log.Error("easyvcs open failed", "driver", kind, "err", err)
		os.Exit(1)
	}
	if err := cs.Init(); err != nil {
		log.Error("easyvcs init failed", "err", err)
		os.Exit(1)
	}
	stageArea := stage.New(cs, func(tenant string) easyvcsstore.RepoRef {
		// One namespace per tenant: the content a tenant stages against is its own mirror.
		return easyvcsstore.RepoRef{Name: "tenant-" + tenant}
	})
	log.Info("staging enabled", "driver", kind)

	var ext *extension.Extension
	handlers := tools.Registry(tools.Deps{
		Stage: stageArea,
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

	specs := make(map[string]extension.ToolSpec, len(handlers))
	requires := tools.RequireMap()
	for name, h := range handlers {
		h := h
		specs[name] = extension.ToolSpec{
			// Attach the permission keys HERE rather than in each handler: the SDK needs them
			// before it calls the handler, and a handler that forgot to declare its own
			// requirement would simply never be gated.
			Requires: requires[name],
			Execute: func(ctx context.Context, args map[string]any, callID, session, tenant string) (extension.ToolResultData, error) {
				return h(ctx, args, session, tenant)
			},
		}
	}

	cfg := m.BuildConfig(manifest.Bindings{
		Handlers: specs,
		OnLifecycle: func(ctx context.Context, ev *easybus.LifecycleEvent, tenant string) error {
			// Logged rather than ignored: a deleted session is when its staging area should stop
			// being resumable, and that is worth seeing in the log.
			log.Info("lifecycle", "kind", easybus.LifecycleKindString(ev.Kind),
				"tenant", tenant, "session", ev.SessionName)
			return nil
		},
	})
	ext = extension.New(bus, cfg)

	if err := extension.Serve(ext, extension.ServeOptions{
		Run: func(ctx context.Context, _ *extension.Extension) {
			log.Info("listening", "nats", natsURL)
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
