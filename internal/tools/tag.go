package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"

	"github.com/easydev-org/easyagent-github-ext/internal/ghclient"
)

// Publishing: tags and releases.
//
// These are the tools that make an upstream event HAPPEN. Everything else in this extension
// changes a repository; this is what tells the world a version exists — and it is what a
// downstream channel subscription reacts to.

// tagCreate creates an annotated tag.
func (t *toolset) tagCreate(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
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
	if err := t.requireRepo(ctx, PermTagCreate, repo); err != nil {
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
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	name, err := required(args, "tag")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	if err := t.requireRepo(ctx, PermTagDelete, repo); err != nil {
		return extension.ToolResultData{}, err
	}
	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	if err := c.DeleteTag(ctx, name); err != nil {
		return extension.ToolResultData{}, errFailed("%s: %v", repo, err)
	}
	return extension.ToolResultData{Content: fmt.Sprintf("deleted tag %s", name)}, nil
}

// releaseCreate publishes a release — the event a downstream subscription waits for.
func (t *toolset) releaseCreate(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	tag, err := required(args, "tag")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	body, err := required(args, "body")
	if err != nil {
		// A release with no notes announces a version and says nothing about it. Forcing the
		// body keeps a publish from being a content-free event that nobody downstream can act
		// on.
		return extension.ToolResultData{}, errArg(
			`"body" is required: a release announces a version, and downstream consumers need to know what changed`)
	}
	if err := t.requireRepo(ctx, PermReleaseCreate, repo); err != nil {
		return extension.ToolResultData{}, err
	}
	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	rel, err := c.CreateRelease(ctx, ghclient.CreateReleaseOptions{
		TagName: tag,
		Target:  argStr(args, "ref"),
		Name:    argStr(args, "name"),
		Body:    body,
		Draft:   argBool(args, "draft"),
		// Prerelease is a separate flag, not inferred from the tag name: guessing from a
		// "-rc" suffix would mark a project's own naming convention as authoritative.
		Prerelease: argBool(args, "prerelease"),
	})
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s: %v", repo, err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "published release %s\n", rel.TagName)
	fmt.Fprintf(&b, "%s\n", rel.HTMLURL)
	if rel.Draft {
		b.WriteString("(draft — not visible until published)\n")
	}
	if rel.Prerelease {
		b.WriteString("(prerelease)\n")
	}
	return extension.ToolResultData{
		Content: b.String(),
		Data:    map[string]any{"release": rel},
	}, nil
}

// releaseDelete removes a published release, and optionally the tag with it.
//
// The two are SEPARATE objects on GitHub. `gh-tag-delete` removes only the ref, which leaves the
// release behind pointing at a tag that no longer exists — a version that is still announced but
// can no longer be checked out. `with_tag` is the "remove this version entirely" operation.
func (t *toolset) releaseDelete(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	repo, err := required(args, "repo")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	tag, err := required(args, "tag")
	if err != nil {
		return extension.ToolResultData{}, err
	}
	if err := t.requireRepo(ctx, PermReleaseDelete, repo); err != nil {
		return extension.ToolResultData{}, err
	}
	c, _, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}

	if argBool(args, "with_tag") {
		// Removing the tag needs its own permission: deleting a ref is a different act from
		// unpublishing an announcement, and a caller trusted with one is not automatically
		// trusted with the other.
		if err := t.requireRepo(ctx, PermTagDelete, repo); err != nil {
			return extension.ToolResultData{}, err
		}
		deleted, derr := c.DeleteTagAndRelease(ctx, tag)
		if derr != nil {
			return extension.ToolResultData{}, errFailed("%s@%s: %v", repo, tag, derr)
		}
		msg := fmt.Sprintf("deleted release %s", tag)
		if deleted {
			msg += " and its tag"
		}
		return extension.ToolResultData{Content: msg}, nil
	}

	if err := c.DeleteRelease(ctx, tag); err != nil {
		return extension.ToolResultData{}, errFailed("%s@%s: %v", repo, tag, err)
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("deleted release %s (the tag remains)", tag),
	}, nil
}
