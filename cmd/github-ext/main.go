// github-ext serves the gh-* tools: work on GitHub repositories and their CI with no local
// sandbox.
package main

import (
	"context"
	_ "embed"
	"log/slog"
	"os"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
	"github.com/easydev-org/easybus-sdk/manifest"
	"github.com/easydev-org/easybus-sdk/transport/nats"

	"github.com/easydev-org/easyagent-github-ext/internal/tools"
	"github.com/easydev-org/easyagent-github-ext/internal/workspace"
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

	// Workspaces are plain directories on this pod's disk. No database: a workspace is a git
	// checkout, and git already knows how to be one.
	//
	// The root is deliberately on LOCAL disk (not a shared volume): a checkout is a session's
	// private scratchpad, and two pods sharing one directory would fight over the index.
	root := envOr("WORKSPACE_ROOT", "/tmp/workspaces")
	idle := 2 * time.Hour
	if v := envOr("WORKSPACE_IDLE_TTL", ""); v != "" {
		if d, derr := time.ParseDuration(v); derr == nil {
			idle = d
		}
	}
	wsMgr, err := workspace.NewManager(root, idle)
	if err != nil {
		log.Error("workspace root", "root", root, "err", err)
		os.Exit(1)
	}
	log.Info("workspaces enabled", "root", root, "idle_ttl", idle.String())

	var ext *extension.Extension
	handlers := tools.Registry(tools.Deps{
		Workspaces: wsMgr,
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
			// Reap abandoned workspaces. Without this a long-lived pod fills its disk with
			// checkouts nobody will come back to.
			go func() {
				t := time.NewTicker(15 * time.Minute)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case now := <-t.C:
						if n := wsMgr.SweepIdle(now); n > 0 {
							log.Info("swept idle workspaces", "removed", n)
						}
					}
				}
			}()
			// Publish releases onto the `github` channel, so a session can follow a
			// repository and be woken when a version ships.
			startReleaseWatchers(ctx, ext, log)
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
