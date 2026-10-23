package watch

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// TagWatcher publishes a channel event when a repository gains a tag.
//
// WHY polling: a forge can push a webhook, but this extension has no public endpoint to
// receive one — it is a bus client, not an HTTP server. Polling refs is the only shape
// available, so the interval is the latency the user gets.
//
// The watcher is deliberately its own loop rather than a hook on some other event: a tag can
// be pushed by CI, by a human in the forge UI, or by another agent, and none of those pass
// through this extension.

// ChannelName is the channel tag events are published on.
const ChannelName = "gitea"

// TagReader lists a repository's tags. Satisfied by the forge client.
type TagReader interface {
	// Tags returns the tag names currently present, newest last.
	Tags(ctx context.Context) ([]string, error)
}

// Publisher publishes one channel event. Satisfied by the extension host.
type Publisher interface {
	PublishChannel(ctx context.Context, tenant, channel, topic string, payload any) error
}

// Repo is one repository to watch.
type Repo struct {
	// Slug is "owner/name", used both as the topic segment and as the human label.
	Slug string
	// Tags lists the current tags.
	Tags TagReader
}

// Options tunes the watcher.
type Options struct {
	// Interval between polls. Zero means the default.
	Interval time.Duration
}

// DefaultInterval is how often tags are polled.
//
// A minute is the compromise: often enough that a tag announced by CI reaches a subscriber
// while it is still interesting, rare enough that a forge with many repositories is not
// hammered by a feature nobody is using.
const DefaultInterval = time.Minute

// TagWatcher polls repositories and publishes newly-seen tags.
type TagWatcher struct {
	tenant string
	repos  []Repo
	pub    Publisher
	opts   Options
	log    *slog.Logger
	mu     sync.Mutex
	// known maps a repo slug to the tags already announced, so a poll only publishes
	// differences. Seeded on the FIRST poll without publishing: existing tags are history,
	// not news, and announcing them would flood a new subscriber with old events.
	known map[string]map[string]bool
}

// New builds a watcher.
func New(tenant string, repos []Repo, pub Publisher, opts Options) *TagWatcher {
	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}
	return &TagWatcher{
		tenant: tenant, repos: repos, pub: pub, opts: opts,
		log:   slog.Default().With("svc", "gitea-tagwatch", "tenant", tenant),
		known: map[string]map[string]bool{},
	}
}

// Run polls until the context is cancelled.
//
// The first pass only SEEDS: it records what already exists and publishes nothing. A
// subscriber that arrives later should hear about the next tag, not the repository's whole
// history.
func (w *TagWatcher) Run(ctx context.Context) {
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
func (w *TagWatcher) pollOnce(ctx context.Context, seed bool) {
	for _, repo := range w.repos {
		tags, err := repo.Tags.Tags(ctx)
		if err != nil {
			// A forge that is briefly unreachable must not kill the watcher: the next poll
			// retries, and the known-set is untouched so nothing is silently skipped.
			w.log.Warn("list tags failed", "repo", repo.Slug, "err", err)
			continue
		}

		w.mu.Lock()
		prev := w.known[repo.Slug]
		if prev == nil {
			prev = map[string]bool{}
		}
		var fresh []string
		for _, tag := range tags {
			if !prev[tag] {
				fresh = append(fresh, tag)
			}
		}
		w.mu.Unlock()

		if seed {
			// Seeding records everything WITHOUT publishing: existing tags are history, and
			// announcing them would wake every subscriber for tags that are months old.
			w.markKnown(repo.Slug, tags)
			continue
		}
		if len(fresh) == 0 {
			continue
		}
		for _, tag := range fresh {
			if err := w.publish(repo.Slug, tag); err != nil {
				// NOT recorded as known: the tag is still un-announced, so the next poll
				// tries again. Recording it here would lose the event permanently — the
				// known-set is what makes it "old", and a failed publish is not delivery.
				w.log.Warn("publish tag failed", "repo", repo.Slug, "tag", tag, "err", err)
				continue
			}
			w.markKnown(repo.Slug, []string{tag})
		}
	}
}

// markKnown records tags as already announced.
//
// Separate from the diff computation so the two cannot drift: everything that becomes
// "known" got there either by being present at seeding or by being published successfully.
func (w *TagWatcher) markKnown(slug string, tags []string) {
	if len(tags) == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	set := w.known[slug]
	if set == nil {
		set = map[string]bool{}
		w.known[slug] = set
	}
	for _, t := range tags {
		set[t] = true
	}
}

// publish emits one tag event.
//
// The topic is `tag.<owner>/<repo>.<tag>` rather than a flat name, so a session can follow
// one repository (`tag.agentdev/mrflow.>`), all of one owner's repos, or every tag, without
// the publisher knowing anything about who is listening.
func (w *TagWatcher) publish(slug, tag string) error {
	if slug == "" || tag == "" {
		return fmt.Errorf("empty slug or tag")
	}
	topic := "tag." + slug + "." + tag
	payload := map[string]any{
		"repo": slug,
		"tag":  tag,
		// The name a subscriber would use to fetch the object.
		"ref": "refs/tags/" + tag,
	}
	return w.pub.PublishChannel(context.Background(), w.tenant, ChannelName, topic, payload)
}

// SubscribePatternForRepo is the pattern a session uses to follow one repository's tags.
//
// Exported so a client can offer it as a suggestion: a user should not have to derive
// "tag.owner/name.>" from the documentation.
func SubscribePatternForRepo(slug string) string {
	return "tag." + slug + ".>"
}
