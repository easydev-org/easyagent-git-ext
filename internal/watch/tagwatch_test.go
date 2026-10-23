package watch

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// fakeTags returns whatever the test set, and can fail.
type fakeTags struct {
	mu   sync.Mutex
	tags []string
	err  error
}

func (f *fakeTags) Tags(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tags...), f.err
}

func (f *fakeTags) set(tags ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tags = tags
}

type published struct {
	channel, topic string
	payload        any
}

type fakePub struct {
	mu   sync.Mutex
	sent []published
	err  error
}

func (f *fakePub) PublishChannel(_ context.Context, _, channel, topic string, payload any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, published{channel: channel, topic: topic, payload: payload})
	return nil
}

func (f *fakePub) all() []published {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]published(nil), f.sent...)
}

// The first poll must SEED and publish nothing.
//
// Otherwise every restart would announce a repository's whole tag history as if it had just
// happened, and every subscriber would wake for tags that are months old.
func TestFirstPollDoesNotPublish(t *testing.T) {
	tags := &fakeTags{tags: []string{"v1.0.0", "v1.1.0"}}
	pub := &fakePub{}
	w := New("alice", []Repo{{Slug: "agentdev/mrflow", Tags: tags}}, pub, Options{})

	w.pollOnce(context.Background(), true)

	if got := pub.all(); len(got) != 0 {
		t.Fatalf("the seeding poll published %d events: %+v", len(got), got)
	}
}

// A tag that appears after seeding is published exactly once.
func TestNewTagIsPublishedOnce(t *testing.T) {
	tags := &fakeTags{tags: []string{"v1.0.0"}}
	pub := &fakePub{}
	w := New("alice", []Repo{{Slug: "agentdev/mrflow", Tags: tags}}, pub, Options{})

	w.pollOnce(context.Background(), true) // seed
	tags.set("v1.0.0", "v2.0.0")
	w.pollOnce(context.Background(), false)

	got := pub.all()
	if len(got) != 1 {
		t.Fatalf("expected one event, got %d: %+v", len(got), got)
	}
	if got[0].channel != "gitea" {
		t.Errorf("channel = %q, want gitea", got[0].channel)
	}
	// The topic must let a session follow ONE repository, so it starts with tag.<slug>.
	if got[0].topic != "tag.agentdev/mrflow.v2.0.0" {
		t.Errorf("topic = %q", got[0].topic)
	}

	// Polling again with no change must not re-publish.
	w.pollOnce(context.Background(), false)
	if n := len(pub.all()); n != 1 {
		t.Fatalf("a second poll repeated the event: %d events", n)
	}
}

// The published topic must be matched by the pattern the client suggests, or a user who
// follows the suggestion would never receive anything.
func TestTopicMatchesSuggestedPattern(t *testing.T) {
	tags := &fakeTags{tags: []string{}}
	pub := &fakePub{}
	w := New("alice", []Repo{{Slug: "agentdev/mrflow", Tags: tags}}, pub, Options{})
	w.pollOnce(context.Background(), true)
	tags.set("v3.1.4")
	w.pollOnce(context.Background(), false)

	got := pub.all()
	if len(got) != 1 {
		t.Fatalf("expected one event, got %+v", got)
	}
	pattern := SubscribePatternForRepo("agentdev/mrflow")
	if !matchesTopic(pattern, got[0].topic) {
		t.Errorf("suggested pattern %q does not match published topic %q", pattern, got[0].topic)
	}
	// And it must NOT match another repository.
	if matchesTopic(pattern, "tag.other/repo.v1") {
		t.Error("the pattern for one repository must not match another repository's tags")
	}
}

// A forge that fails must not lose track: the next successful poll still sees everything
// that appeared while it was down.
func TestTransientFailureDoesNotLoseTags(t *testing.T) {
	tags := &fakeTags{tags: []string{"v1"}}
	pub := &fakePub{}
	w := New("alice", []Repo{{Slug: "agentdev/mrflow", Tags: tags}}, pub, Options{})
	w.pollOnce(context.Background(), true)

	tags.err = errors.New("forge unreachable")
	w.pollOnce(context.Background(), false)
	if len(pub.all()) != 0 {
		t.Fatal("a failed poll must not publish")
	}

	// While it was down a tag appeared; the recovery poll must report it.
	tags.mu.Lock()
	tags.err = nil
	tags.tags = []string{"v1", "v2"}
	tags.mu.Unlock()
	w.pollOnce(context.Background(), false)

	got := pub.all()
	if len(got) != 1 || got[0].topic != "tag.agentdev/mrflow.v2" {
		t.Fatalf("the tag added during the outage was lost: %+v", got)
	}
}

// A publish failure must not corrupt the known-set: the tag would be reported as new only
// once, so a failed publish would lose it forever.
func TestPublishFailureIsRetriedOnNextPoll(t *testing.T) {
	tags := &fakeTags{tags: []string{}}
	pub := &fakePub{err: errors.New("bus unavailable")}
	w := New("alice", []Repo{{Slug: "agentdev/mrflow", Tags: tags}}, pub, Options{})
	w.pollOnce(context.Background(), true)

	tags.set("v1")
	w.pollOnce(context.Background(), false)
	if len(pub.all()) != 0 {
		t.Fatal("nothing should have been delivered while publishing failed")
	}

	pub.mu.Lock()
	pub.err = nil
	pub.mu.Unlock()
	w.pollOnce(context.Background(), false)

	got := pub.all()
	if len(got) != 1 || got[0].topic != "tag.agentdev/mrflow.v1" {
		t.Fatalf("a failed publish must be retried, not dropped: %+v", got)
	}
}

// Several repositories are watched independently.
func TestMultipleRepos(t *testing.T) {
	a := &fakeTags{tags: []string{}}
	b := &fakeTags{tags: []string{}}
	pub := &fakePub{}
	w := New("alice", []Repo{
		{Slug: "agentdev/one", Tags: a},
		{Slug: "agentdev/two", Tags: b},
	}, pub, Options{})

	w.pollOnce(context.Background(), true)
	a.set("v1")
	b.set("v9")
	w.pollOnce(context.Background(), false)

	got := pub.all()
	if len(got) != 2 {
		t.Fatalf("expected two events, got %+v", got)
	}
	topics := map[string]bool{}
	for _, p := range got {
		topics[p.topic] = true
	}
	if !topics["tag.agentdev/one.v1"] || !topics["tag.agentdev/two.v9"] {
		t.Errorf("topics = %+v", topics)
	}
}

// The payload must let a subscriber act without a second request.
func TestPayloadCarriesRepoAndTag(t *testing.T) {
	tags := &fakeTags{tags: []string{}}
	pub := &fakePub{}
	w := New("alice", []Repo{{Slug: "agentdev/mrflow", Tags: tags}}, pub, Options{})
	w.pollOnce(context.Background(), true)
	tags.set("v1.2.3")
	w.pollOnce(context.Background(), false)

	got := pub.all()
	if len(got) != 1 {
		t.Fatalf("expected one event, got %+v", got)
	}
	m, ok := got[0].payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", got[0].payload)
	}
	if m["repo"] != "agentdev/mrflow" || m["tag"] != "v1.2.3" || m["ref"] != "refs/tags/v1.2.3" {
		t.Errorf("payload = %+v", m)
	}
}

// matchesTopic mirrors the SDK's subject matcher, so this package's test does not depend on
// the SDK's internals while still asserting the two agree.
func matchesTopic(pattern, topic string) bool {
	p := splitDots(pattern)
	t := splitDots(topic)
	for i, seg := range p {
		if seg == ">" {
			return len(t) > i
		}
		if i >= len(t) || (seg != "*" && seg != t[i]) {
			return false
		}
	}
	return len(p) == len(t)
}

func splitDots(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '.' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}
