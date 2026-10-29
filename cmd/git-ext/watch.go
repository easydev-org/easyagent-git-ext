package main

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-git-ext/internal/config"
	"github.com/easydev-org/easyagent-git-ext/internal/ghclient"
	"github.com/easydev-org/easyagent-git-ext/internal/watch"
)

// startReleaseWatchers publishes a channel event when a watched repository gains a release.
//
// Polling per TENANT, discovered on a slow tick: the tenant list is not known at boot, and a
// tenant that later configures `watch-repos` is picked up within one interval without a
// restart. A tenant that configures nothing costs a map lookup.
func startReleaseWatchers(ctx context.Context, ext *extension.Extension, log *slog.Logger) {
	if ext == nil {
		return
	}
	go func() {
		started := map[string]bool{}
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			for _, tenant := range ext.Tenants() {
				if started[tenant] {
					continue
				}
				w, ok := watcherFor(ctx, ext, tenant, log)
				if !ok {
					continue
				}
				started[tenant] = true
				go w.Run(ctx)
				log.Info("release watcher started", "tenant", tenant)
			}
		}
	}()
}

// watcherFor builds a watcher for one tenant, or reports that it has nothing to watch.
func watcherFor(ctx context.Context, ext *extension.Extension, tenant string, log *slog.Logger) (*watch.ReleaseWatcher, bool) {
	_ = ctx
	raw, _ := ext.GetConfig(tenant, config.KnobWatchRepos, "").(string)
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	token, _ := ext.GetConfig(tenant, config.KnobToken, "").(string)
	apiURL, _ := ext.GetConfig(tenant, config.KnobAPIURL, "").(string)

	var repos []watch.Repo
	for _, slug := range strings.Split(raw, ",") {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		// A malformed slug must be REPORTED, not skipped: a typo would otherwise look exactly
		// like "this repository has no releases".
		parts := strings.Split(slug, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			log.Warn("ignoring watch-repos entry: not owner/name", "tenant", tenant, "entry", slug)
			continue
		}
		c, err := ghclient.New(ghclient.Options{
			Remote:  "https://github.com/" + slug,
			Token:   token,
			BaseURL: apiURL,
		})
		if err != nil {
			log.Warn("watch client failed", "tenant", tenant, "repo", slug, "err", err)
			continue
		}
		repos = append(repos, watch.Repo{Slug: slug, Releases: watch.GHReader{Client: c}})
	}
	if len(repos) == 0 {
		return nil, false
	}

	opts := watch.Options{}
	if v, _ := ext.GetConfig(tenant, config.KnobWatchInterval, "").(string); strings.TrimSpace(v) != "" {
		if d, derr := time.ParseDuration(v); derr == nil && d > 0 {
			opts.Interval = d
		}
	}
	if v, ok := ext.GetConfig(tenant, config.KnobWatchPrereleases, "").(string); ok {
		if b, berr := strconv.ParseBool(v); berr == nil {
			opts.IncludePrereleases = b
		}
	}
	return watch.New(tenant, repos, ext, opts), true
}
