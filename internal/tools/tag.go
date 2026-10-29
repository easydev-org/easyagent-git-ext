package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-git-ext/internal/ghclient"
)

// Publishing: tags and releases.
//
// These are the tools that make an upstream event HAPPEN. Everything else in this extension
// changes a repository; this is what tells the world a version exists — and it is what a
// downstream channel subscription reacts to.

// tagCreate creates an annotated tag.
func (t *toolset) tagCreate(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("resolving the target commit")
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	name, err := required(args, "tag")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	msg, err := required(args, "message")
	if err != nil {
		// Spelled out rather than a bare `"message" is required`: the caller needs to know
		// that a tag without a message is a different kind of object, not a smaller one.
		return extension.ToolResultData{}, errArg(
			`"message" is required: tags here are annotated, because an annotated tag is a published ` +
				`event with a description and a release is built on one`)
	}
	if err := t.requireRepo(ctx, PermMaintainer, repo); err != nil {
		return extension.ToolResultData{}, err
	}
	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	tag, err := c.CreateTag(ctx, ghclient.CreateTagOptions{
		Name: name, Ref: argStr(args, "ref"), Message: msg,
	})
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s: %v", repo, err)
	}
	ref := argStr(args, "ref")
	if ref == "" {
		ref = "(default branch)"
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("created annotated tag %s at %s\n%s", tag.Name, shortSHA(tag.SHA), ref),
		Data:    map[string]any{"tag": tag.Name, "sha": tag.SHA, "annotated": true},
	}, nil
}

func (t *toolset) tagList(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("listing tags")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	tags, err := c.ListTags(ctx)
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s: %v", repo, err)
	}
	var b strings.Builder
	for _, tg := range tags {
		fmt.Fprintf(&b, "%-24s %s\n", tg.Name, shortSHA(tg.SHA))
	}
	if len(tags) == 0 {
		b.WriteString("(no tags)\n")
	}
	fmt.Fprintf(&b, "\n%d tags", len(tags))
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"tags": tags, "count": len(tags)},
	}, nil
}

func (t *toolset) tagDelete(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("deleting the release")
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	name, err := required(args, "tag")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	if err := t.requireRepo(ctx, PermMaintainer, repo); err != nil {
		return extension.ToolResultData{}, err
	}
	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}

	// Deleting a tag deletes its release too, ALWAYS — never as an option.
	//
	// They are separate objects on GitHub, so removing only the ref left a release pointing at
	// a tag that no longer existed: still announced, impossible to check out, and invisible
	// until somebody tried to download it. A tool that can produce that state is a tool that
	// eventually does, so the two go together.
	//
	// The release is removed first: deleting the tag first would orphan it and make it harder
	// to find.
	releaseDeleted, derr := c.DeleteTagAndRelease(ctx, name)
	if derr != nil {
		return extension.ToolResultData{}, errFailed("%s: %v", repo, derr)
	}
	if releaseDeleted {
		return extension.ToolResultData{
			Content: fmt.Sprintf("deleted tag %s and its release", name),
			Data:    map[string]any{"tag": name, "release_deleted": true},
		}, nil
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("deleted tag %s (no release was published at it)", name),
		Data:    map[string]any{"tag": name, "release_deleted": false},
	}, nil
}

// tagShow reads one tag's annotation.
//
// Separate from gh-tag-list because GitHub's tag LIST does not include the message: reading it
// costs one extra request PER TAG. Folding that into the list would turn a 1-request listing
// into 1+N, and anonymous access allows 60 requests an hour — so listing a repository with more
// than 60 tags would fail outright, which is worse than not showing messages.
func (t *toolset) tagShow(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("reading the tag annotation")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	name, err := required(args, "tag")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	d, err := c.ShowTag(ctx, name)
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s@%s: %v", repo, name, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", d.Name)
	fmt.Fprintf(&b, "commit: %s\n", d.SHA)
	if !d.Annotated {
		// Said explicitly: "no annotation" and "an empty annotation" are different facts, and a
		// caller that asked for a message needs to know this tag does not have one.
		b.WriteString("annotated: no (lightweight tag; it carries no message)\n")
		return extension.ToolResultData{
			Content: b.String(),
			Data:    map[string]any{"tag": d},
		}, nil
	}
	b.WriteString("annotated: yes\n")
	if d.TaggerName != "" || d.TaggerDate != "" {
		fmt.Fprintf(&b, "tagger: %s <%s> %s\n", d.TaggerName, d.TaggerEmail, d.TaggerDate)
	}
	b.WriteString("\n")
	b.WriteString(d.Message)
	if !strings.HasSuffix(d.Message, "\n") {
		b.WriteString("\n")
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"tag": d},
	}, nil
}
