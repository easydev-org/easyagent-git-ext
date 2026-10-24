package watch

import (
	"context"
	"errors"
	"testing"
)

// fakeReader returns whatever it is told, and can fail the first N times.
type fakeReader struct {
	releases []Release
	failNext int
}

func (f *fakeReader) Releases(ctx context.Context, limit int) ([]Release, error) {
	if f.failNext > 0 {
		f.failNext--
		return nil, errors.New("temporary failure")
	}
	return f.releases, nil
}

type fakePub struct {
	topics   []string
	events   []map[string]any
	failNext int
}

func (f *fakePub) PublishChannel(ctx context.Context, tenant, channel, topic string, payload any) error {
	if f.failNext > 0 {
		f.failNext--
		return errors.New("bus down")
	}
	f.topics = append(f.topics, topic)
	f.events = append(f.events, map[string]any{"channel": channel, "payload": payload})
	return nil
}

// The first poll must SEED, not publish. Otherwise a session that starts following a repository
// is woken for every release in its history.
func TestSeedDoesNotPublish(t *testing.T) {
	r := &fakeReader{releases: []Release{
		{Tag: "v1.0.0", PublishedAt: "2026-01-01"},
		{Tag: "v1.1.0", PublishedAt: "2026-02-01"},
	}}
	p := &fakePub{}
	w := New("alice", []Repo{{Slug: "o/r", Releases: r}}, p, Options{IncludePrereleases: true})

	w.pollOnce(context.Background(), true)
	if len(p.topics) != 0 {
		t.Fatalf("seeding published %d events, want 0: %v", len(p.topics), p.topics)
	}

	// The seeded releases must be remembered: the next poll sees no change.
	w.pollOnce(context.Background(), false)
	if len(p.topics) != 0 {
		t.Fatalf("a poll after seeding republished: %v", p.topics)
	}
}

// A release that appears after seeding must be published exactly once.
func TestNewReleaseIsPublishedOnce(t *testing.T) {
	r := &fakeReader{releases: []Release{{Tag: "v1.0.0", PublishedAt: "2026-01-01"}}}
	p := &fakePub{}
	w := New("alice", []Repo{{Slug: "o/r", Releases: r}}, p, Options{})

	w.pollOnce(context.Background(), true)

	r.releases = append(r.releases, Release{Tag: "v1.1.0", PublishedAt: "2026-02-01"})
	w.pollOnce(context.Background(), false)
	if len(p.topics) != 1 {
		t.Fatalf("published %d events, want 1: %v", len(p.topics), p.topics)
	}
	if want := "release.o/r.v1.1.0"; p.topics[0] != want {
		t.Errorf("topic = %q, want %q", p.topics[0], want)
	}
	if p.events[0]["channel"] != ChannelName {
		t.Errorf("channel = %v, want %q", p.events[0]["channel"], ChannelName)
	}

	// A second poll with no change must not republish.
	w.pollOnce(context.Background(), false)
	if len(p.topics) != 1 {
		t.Errorf("republished on an unchanged poll: %v", p.topics)
	}
}

// A failed publish must NOT be recorded as known: recording it would lose the event forever,
// because the known-set is what makes a release "old".
func TestFailedPublishIsRetried(t *testing.T) {
	r := &fakeReader{releases: []Release{{Tag: "v1.0.0", PublishedAt: "2026-01-01"}}}
	p := &fakePub{}
	w := New("alice", []Repo{{Slug: "o/r", Releases: r}}, p, Options{})
	w.pollOnce(context.Background(), true)

	r.releases = append(r.releases, Release{Tag: "v2.0.0", PublishedAt: "2026-03-01"})
	p.failNext = 1
	w.pollOnce(context.Background(), false)
	if len(p.topics) != 0 {
		t.Fatalf("the publish should have failed")
	}

	// The next poll retries and succeeds.
	w.pollOnce(context.Background(), false)
	if len(p.topics) != 1 {
		t.Fatalf("the failed publish was not retried: %v", p.topics)
	}
}

// Prereleases are skipped by default: an -rc is not what a downstream build should react to.
func TestPrereleaseSkippedByDefault(t *testing.T) {
	r := &fakeReader{releases: nil}
	p := &fakePub{}
	w := New("alice", []Repo{{Slug: "o/r", Releases: r}}, p, Options{})
	w.pollOnce(context.Background(), true)

	r.releases = []Release{{Tag: "v3.0.0-rc1", PublishedAt: "2026-04-01", Prerelease: true}}
	w.pollOnce(context.Background(), false)
	if len(p.topics) != 0 {
		t.Errorf("a prerelease was published: %v", p.topics)
	}

	// With the option on, it is.
	w2 := New("alice", []Repo{{Slug: "o/r", Releases: r}}, p, Options{IncludePrereleases: true})
	w2.pollOnce(context.Background(), true)
	r.releases = append(r.releases, Release{Tag: "v3.0.0-rc2", PublishedAt: "2026-04-02", Prerelease: true})
	w2.pollOnce(context.Background(), false)
	if len(p.topics) != 1 {
		t.Errorf("IncludePrereleases did not publish: %v", p.topics)
	}
}

// Drafts are never announced by default: a draft is invisible on GitHub, so announcing it would
// tell subscribers about a release nobody else can see.
func TestDraftSkipped(t *testing.T) {
	r := &fakeReader{releases: nil}
	p := &fakePub{}
	w := New("alice", []Repo{{Slug: "o/r", Releases: r}}, p, Options{IncludePrereleases: true})
	w.pollOnce(context.Background(), true)

	r.releases = []Release{{Tag: "v4.0.0", PublishedAt: "2026-05-01", Draft: true}}
	w.pollOnce(context.Background(), false)
	if len(p.topics) != 0 {
		t.Errorf("a draft was published: %v", p.topics)
	}
}

// A re-published release (a draft going live) is a NEW event even though the tag is unchanged.
func TestRepublishedReleaseIsAnEvent(t *testing.T) {
	r := &fakeReader{releases: []Release{{Tag: "v5.0.0", PublishedAt: "2026-06-01"}}}
	p := &fakePub{}
	w := New("alice", []Repo{{Slug: "o/r", Releases: r}}, p, Options{})
	w.pollOnce(context.Background(), true)

	// Same tag, new publication time.
	r.releases = []Release{{Tag: "v5.0.0", PublishedAt: "2026-06-02"}}
	w.pollOnce(context.Background(), false)
	if len(p.topics) != 1 {
		t.Errorf("a re-publication was not announced: %v", p.topics)
	}
}

// A repository that cannot be read must not stop the others: the next poll retries it.
func TestUnreadableRepoDoesNotStopOthers(t *testing.T) {
	broken := &fakeReader{failNext: 1}
	good := &fakeReader{releases: []Release{{Tag: "v1.0.0", PublishedAt: "2026-01-01"}}}
	p := &fakePub{}
	w := New("alice", []Repo{
		{Slug: "o/broken", Releases: broken},
		{Slug: "o/good", Releases: good},
	}, p, Options{})

	w.pollOnce(context.Background(), true) // broken fails, good seeds
	good.releases = append(good.releases, Release{Tag: "v1.1.0", PublishedAt: "2026-02-01"})
	w.pollOnce(context.Background(), false)
	if len(p.topics) != 1 || p.topics[0] != "release.o/good.v1.1.0" {
		t.Errorf("topics = %v, want the good repo's release", p.topics)
	}
}

func TestSubscribePatternForRepo(t *testing.T) {
	if got, want := SubscribePatternForRepo("easydev-org/test"), "release.easydev-org/test.>"; got != want {
		t.Errorf("pattern = %q, want %q", got, want)
	}
}
