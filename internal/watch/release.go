// Package watch publishes GitHub events onto the bus so a session can follow them.
//
// WHY POLLING: this extension is a bus client, not an HTTP server — it has no public endpoint,
// so a GitHub webhook has nowhere to arrive. Polling is the only shape available, which makes
// the poll interval the latency a subscriber sees.
//
// WHY ITS OWN LOOP: a release can be published by a CI workflow, by a human in the GitHub UI, or
// by another agent. None of those pass through this extension, so reacting to our own tool calls
// would miss almost everything.
package watch

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ChannelName is the channel release events are published on.
//
// Deliberately NOT "gitea": the two extensions report different forges, and a session that
// follows releases from GitHub should not also receive tags from a self-hosted Gitea.
const ChannelName = "github"

// Release is one published release, as the watcher sees it.
type Release struct {
	// Tag is the release's tag name, e.g. "v1.2.3".
	Tag string
	// Name is the release title, which may differ from the tag.
	Name string
	// Draft and Prerelease are forwarded so a subscriber can ignore them.
	Draft      bool
	Prerelease bool
	URL        string
	// PublishedAt distinguishes a release being edited from a new one: only a change of this
	// value means "something was published".
	PublishedAt string
}

// ReleaseReader lists a repository's releases. Satisfied by the GitHub client.
type ReleaseReader interface {
	Releases(ctx context.Context, limit int) ([]Release, error)
}

// Publisher publishes one channel event. Satisfied by the extension host.
type Publisher interface {
	PublishChannel(ctx context.Context, tenant, channel, topic string, payload any) error
}

// Repo is one repository to watch.
type Repo struct {
	// Slug is "owner/name", used as the topic segment.
	Slug string
	// Releases lists the current releases.
	Releases ReleaseReader
}

// Options tunes the watcher.
type Options struct {
	// Interval between polls. Zero means DefaultInterval.
	Interval time.Duration
	// IncludePrereleases controls whether prereleases are announced. Default false: a
	// prerelease is by definition not what a downstream build should react to, and announcing
	// it would make every -rc wake every consumer.
	IncludePrereleases bool
	// IncludeDrafts controls whether drafts are announced. Default false, and there is almost
	// no reason to change it: a draft is invisible on GitHub, so announcing one would tell
	// subscribers about a release nobody else can see.
	IncludeDrafts bool
}

// DefaultInterval is how often releases are polled.
//
// Longer than a tag poll would need: releases are published by a person or a CI job at human
// pace, and a minute is already far below the time anybody cares about.
const DefaultInterval = time.Minute

// ReleaseWatcher polls repositories and publishes releases it has not seen before.
type ReleaseWatcher struct {
	tenant string
	repos  []Repo
	pub    Publisher
	opts   Options
	log    *slog.Logger
	mu     sync.Mutex
	// known maps a repo slug to the release keys already announced. Keyed by tag+publishedAt,
	// because a re-run can republish a release and the tag alone would miss it.
	known map[string]map[string]bool
}

// New builds a watcher.
func New(tenant string, repos []Repo, pub Publisher, opts Options) *ReleaseWatcher {
	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}
	return &ReleaseWatcher{
		tenant: tenant, repos: repos, pub: pub, opts: opts,
		log:   slog.Default().With("svc", "github-releasewatch", "tenant", tenant),
		known: map[string]map[string]bool{},
	}
}

// Run polls until the context is cancelled.
func (w *ReleaseWatcher) Run(ctx context.Context) {
	t := time.NewTicker(w.opts.Interval)
	defer t.Stop()
	w.pollOnce(ctx, true)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.pollOnce(ctx, false)
		}
	}
}

// pollOnce reads every repo once. `seed` suppresses publishing.
//
// The first pass only SEEDS: it records what already exists and publishes nothing. A subscriber
// that arrives later should hear about the NEXT release, not the project's whole history —
// otherwise every new session wakes up to a year of old versions.
func (w *ReleaseWatcher) pollOnce(ctx context.Context, seed bool) {
	for _, repo := range w.repos {
		releases, err := repo.Releases.Releases(ctx, 30)
		if err != nil {
			// A repository that is briefly unreachable must not kill the watcher: the next
			// poll retries, and the known-set is untouched so nothing is silently skipped.
			w.log.Warn("list releases failed", "repo", repo.Slug, "err", err)
			continue
		}

		var fresh []Release
		for _, r := range releases {
			if r.Draft && !w.opts.IncludeDrafts {
				continue
			}
			if r.Prerelease && !w.opts.IncludePrereleases {
				continue
			}
			key := releaseKey(r)
			w.mu.Lock()
			seen := w.known[repo.Slug][key]
			w.mu.Unlock()
			if !seen {
				fresh = append(fresh, r)
			}
		}

		if seed {
			for _, r := range releases {
				w.markKnown(repo.Slug, releaseKey(r))
			}
			continue
		}
		for _, r := range fresh {
			if perr := w.publish(repo.Slug, r); perr != nil {
				// NOT recorded as known: the release is still un-announced, so the next poll
				// retries. Recording it here would lose the event for good.
				w.log.Warn("publish release failed", "repo", repo.Slug, "tag", r.Tag, "err", perr)
				continue
			}
			w.markKnown(repo.Slug, releaseKey(r))
			w.log.Info("published release event", "repo", repo.Slug, "tag", r.Tag)
		}
	}
}

// releaseKey identifies a release publication.
//
// tag alone is not enough: a release can be re-published (a draft going live, or a re-run
// updating it), and the second publication is a new event. publishedAt changes in both cases.
func releaseKey(r Release) string {
	if r.PublishedAt != "" {
		return r.Tag + "@" + r.PublishedAt
	}
	return r.Tag
}

func (w *ReleaseWatcher) markKnown(slug, key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	set := w.known[slug]
	if set == nil {
		set = map[string]bool{}
		w.known[slug] = set
	}
	set[key] = true
}

// publish emits one release event.
//
// The topic is `release.<owner>/<repo>.<tag>` for the same reason the tag watcher uses that
// shape: a subscriber can follow one repository (`release.owner/name.>`), one owner's repos, or
// everything, and the publisher never has to know which.
func (w *ReleaseWatcher) publish(slug string, r Release) error {
	if slug == "" || r.Tag == "" {
		return fmt.Errorf("empty slug or tag")
	}
	topic := "release." + slug + "." + r.Tag
	payload := map[string]any{
		"repo": slug,
		"tag":  r.Tag,
		"name": r.Name,
		"url":  r.URL,
		// ref is what a subscriber would check out to see the released code.
		"ref":          "refs/tags/" + r.Tag,
		"prerelease":   r.Prerelease,
		"published_at": r.PublishedAt,
	}
	return w.pub.PublishChannel(context.Background(), w.tenant, ChannelName, topic, payload)
}

// SubscribePatternForRepo is the pattern a session uses to follow one repository's releases.
//
// Exported so a client can SUGGEST it: a user should not have to derive
// "release.owner/name.>" from the documentation.
func SubscribePatternForRepo(slug string) string {
	return "release." + slug + ".>"
}
