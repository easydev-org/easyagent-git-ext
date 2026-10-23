// Command gitea-ext is the easyagent git extension server: git operations
// (clone/status/log/diff/branch/checkout/add/commit/pull/push/show) run IN a
// remote easyworker sandbox, over the easybus protocol. git runs in the sandbox
// (via the worker's Execute RPC), so this binary needs no system git.
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
	easyvcsstore "github.com/easylab-platform/easyvcs/store"

	"github.com/easydev-org/easyagent-gitea-ext/internal/config"
	"github.com/easydev-org/easyagent-gitea-ext/internal/forge"
	"github.com/easydev-org/easyagent-gitea-ext/internal/sandbox"
	"github.com/easydev-org/easyagent-gitea-ext/internal/stage"
	"github.com/easydev-org/easyagent-gitea-ext/internal/tools"
	"github.com/easydev-org/easyagent-gitea-ext/internal/watch"
)

//go:embed manifest.yaml
var manifestYAML []byte

func main() {
	log := slog.Default().With("svc", "gitea-ext")

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

	var ext *extension.Extension
	cache := sandbox.NewClientCache(16)

	// The staging area is OPTIONAL: it needs an easyvcs database, and a deployment
	// without one should still serve every other tool. It is opened once at startup
	// rather than lazily so a bad DSN fails here, with a clear message, instead of
	// inside the first user's tool call.
	var stageArea *stage.Stage
	if dsn := envOr("EASYVCS_DSN", ""); dsn != "" {
		kind := envOr("EASYVCS_DRIVER", "postgres")
		cs, err := easyvcsstore.OpenDriver(easyvcsstore.DriverConfig{Kind: kind, DSN: dsn})
		if err != nil {
			log.Error("easyvcs open failed; staging tools disabled", "driver", kind, "err", err)
		} else if err := cs.Init(); err != nil {
			log.Error("easyvcs init failed; staging tools disabled", "err", err)
		} else {
			stageArea = stage.New(cs, func(tenant string) easyvcsstore.RepoRef {
				// One namespace per tenant: the mirror content a tenant stages against is
				// the same content its git-clone tools see.
				return easyvcsstore.RepoRef{Name: "tenant-" + tenant}
			})
			log.Info("staging enabled", "driver", kind)
		}
	}

	handlers := tools.Registry(tools.Deps{
		Cache: cache,
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
			// Attach the permission keys HERE rather than in each handler: the SDK needs
			// them before it calls the handler, and a handler that forgot to declare its
			// own requirement would simply never be gated.
			Requires: requires[name],
			Execute: func(ctx context.Context, args map[string]any, callID, session, tenant string) (extension.ToolResultData, error) {
				// Install a progress sink for THIS call: the tenant and call id are
				// only known here. Without it a `git clone` of a large repo shows
				// nothing until it completes.
				cctx := sandbox.WithProgress(ctx, func(pctx context.Context, p *easybus.ToolProgress) {
					if ext != nil {
						// A dropped UI update must never fail the command: it already
						// ran.
						_ = ext.ReportProgress(pctx, tenant, callID, p)
					}
				})
				return h(cctx, args, session, tenant)
			},
		}
	}

	cfg := m.BuildConfig(manifest.Bindings{
		Handlers: specs,
		OnLifecycle: func(ctx context.Context, ev *easybus.LifecycleEvent, tenant string) error {
			log.Info("lifecycle", "kind", easybus.LifecycleKindString(ev.Kind), "tenant", tenant, "session", ev.SessionName)
			return nil
		},
	})
	ext = extension.New(bus, cfg)

	if err := extension.Serve(ext, extension.ServeOptions{
		Run: func(ctx context.Context, _ *extension.Extension) {
			log.Info("listening", "nats", natsURL)
			// Publish repository tag events on the `gitea` channel.
			//
			// Started here rather than on a timer inside the extension so it shares the
			// process's context: when the pod stops, the poller stops with it instead of
			// logging into a closed connection.
			startTagWatchers(ctx, ext, log)
		},
	}); err != nil {
		log.Error("serve failed", "err", err)
		os.Exit(1)
	}
}

// startTagWatchers polls the configured repositories for new tags and publishes them.
//
// A tag can be pushed by CI, by a human in the forge UI, or by another agent — none of which
// pass through this extension — so polling is the only way to notice. The interval is the
// user's latency for hearing about a tag.
//
// One watcher per tenant, started lazily when a tenant's repositories are first known. A
// tenant with no repository configured simply has nothing to watch, which is the normal
// starting state and not an error.
func startTagWatchers(ctx context.Context, ext *extension.Extension, log *slog.Logger) {
	if ext == nil {
		return
	}
	// Repositories are declared per tenant in the `servers` config, and the tenant list is
	// not known at boot. Polling for tenants on a slow tick keeps this simple: a tenant that
	// adds a repository is picked up within one interval without a restart, and a tenant
	// that never configures one costs a map lookup.
	go func() {
		started := map[string]bool{}
		t := time.NewTicker(watch.DefaultInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			for _, tenant := range knownTenants(ext) {
				if started[tenant] {
					continue
				}
				repos, ok := tagWatchRepos(ctx, ext, tenant)
				if !ok || len(repos) == 0 {
					continue
				}
				started[tenant] = true
				w := watch.New(tenant, repos, ext, watch.Options{})
				go w.Run(ctx)
				log.Info("tag watcher started", "tenant", tenant, "repos", len(repos))
			}
		}
	}()
}

// tagWatchRepos builds the watcher inputs for one tenant, or ok=false when the tenant has
// no usable forge configured yet.
func tagWatchRepos(ctx context.Context, ext *extension.Extension, tenant string) ([]watch.Repo, bool) {
	_ = ctx
	raw, _ := ext.GetConfig(tenant, config.KnobServers, "").(string)
	if raw == "" {
		return nil, false
	}
	servers, err := config.ParseServers(raw)
	if err != nil || len(servers) == 0 {
		return nil, false
	}
	var out []watch.Repo
	for _, srv := range servers {
		// The forge client is per repository, and the API base is derived from the server's
		// kind/host the same way the read tools derive it.
		cl, err := forge.New(srv.APIBase(), srv.Token, srv.Host)
		if err != nil {
			continue
		}
		slug := cl.Owner + "/" + cl.Repo
		if cl.Repo == "" {
			continue
		}
		out = append(out, watch.Repo{Slug: slug, Tags: cl})
	}
	return out, len(out) > 0
}

// knownTenants returns the tenants this extension has seen config for.
func knownTenants(ext *extension.Extension) []string {
	if ext == nil {
		return nil
	}
	return ext.Tenants()
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
