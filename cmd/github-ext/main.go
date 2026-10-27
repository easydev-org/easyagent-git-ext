// github-ext serves the gh-* tools: work on GitHub repositories and their CI with no local
// sandbox.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	easybus "github.com/easydev-org/easybus-sdk"
	"github.com/easydev-org/easybus-sdk/extension"
	"github.com/easydev-org/easybus-sdk/manifest"
	"github.com/easydev-org/easybus-sdk/transport/nats"

	"github.com/easydev-org/easyagent-github-ext/internal/tools"
	"github.com/easydev-org/easyagent-github-ext/internal/uihandler"
	"github.com/easydev-org/easyagent-github-ext/internal/workspace"
)

//go:embed manifest.yaml
var manifestYAML []byte

// uiFiles is the browser UI, embedded so the binary is self-contained: no asset directory to ship
// alongside the image, and no chance of the UI and the API it calls drifting apart in a deployment.
//
//go:embed all:ui
var uiFiles embed.FS

// uiMux builds the extension's HTTP surface: the health/version probes plus the UI's data API.
//
// The probe routes are re-declared here because supplying a Handler REPLACES the SDK's default one;
// without them a deployment that added a UI would lose its liveness endpoints and start crash
// looping under a probe that had worked before.
func uiMux(ws *workspace.Manager, ver string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"name":"github"}`))
	})
	mux.HandleFunc("/api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"github","version":"` + ver + `"}`))
	})
	(&uihandler.Handler{Workspaces: ws}).Register(mux)
	return mux
}

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
		// Object storage for gh-artifact-download. An artifact archive is arbitrary in size, so
		// it is stored under a tenant-scoped key and referred to by name — never returned inline.
		PutObject: func(ctx context.Context, tenant, name string, data []byte) error {
			if ext == nil {
				return fmt.Errorf("extension not ready")
			}
			return ext.PutObject(ctx, tenant, name, data)
		},
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
		// In-flight progress. The extension needs the callID to address an update, and only the
		// Execute adapter has it — so the adapter builds a reporter per call and puts it on the
		// context, and handlers reach it with tools.Progress(ctx).
		ReportProgress: func(ctx context.Context, tenant, callID string, p *easybus.ToolProgress) error {
			if ext == nil {
				return nil
			}
			return ext.ReportProgress(ctx, tenant, callID, p)
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
				// A reporter per call: progress belongs to THIS call, and sharing one across
				// concurrent calls would interleave two tools' phases.
				// ext is nil while these specs are being built (it is assigned after
				// extension.New), so the reporter asks for it AT CALL TIME.
				rep := tools.NewReporter(func(p *easybus.ToolProgress) {
					// A failed progress publish must not fail the tool: the work is the point,
					// the narration is a courtesy.
					if e := ext; e != nil {
						if err := e.ReportProgress(ctx, tenant, callID, p); err != nil {
							log.Debug("progress publish failed", "tool", name, "err", err)
						}
					}
				})
				return h(tools.WithReporter(ctx, rep), args, session, tenant)
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
		// The browser UI, served under /ext/github and reverse-proxied there by the webui, so an
		// iframe embedding it is SAME-ORIGIN with the host. See extension.UISpec for why the prefix
		// matters.
		UI: &extension.UISpec{
			Assets:      uiFiles,
			StripPrefix: "ui",
			Base:        "/ext/github",
			Title:       "GitHub",
		},
		// API routes the UI reads from. They answer from the LOCAL workspace, never api.github.com:
		// see internal/uihandler.
		Handler: uiMux(wsMgr, m.Version),
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

// depReporter returns the progress publisher, or nil when the extension is not ready.
//
// Read through a function so the adapter picks up the extension AFTER it is assigned: ext is nil
// while the specs are being built.
func depReporter(ext *extension.Extension) func(ctx context.Context, tenant, callID string, p *easybus.ToolProgress) error {
	if ext == nil {
		return nil
	}
	return ext.ReportProgress
}
