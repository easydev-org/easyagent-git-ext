package tools

import (
	"context"
	"fmt"
	"strings"

	easybus "github.com/easydev-org/easybus-sdk"
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

// releaseCreate REFUSES, on purpose.
//
// Publishing a release is not a thing this extension may do by hand. A release is the event
// every downstream consumer reacts to, and a hand-made one bypasses whatever the repository's
// CI does first: the tests, the build, the artefact push. The version would be announced as
// real while the thing it announces was never produced.
//
// So releases are created by CI. This handler exists rather than the tool being absent because
// a caller that expects the name should be told WHY it is refused, instead of finding an
// unknown tool and guessing.
func (t *toolset) releaseCreate(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	return extension.ToolResultData{}, &extension.TypedError{
		Code: easybus.ToolResultErrorCodePermissionDenied,
		Message: "publishing a release by hand is disabled: releases must be created by CI, so that " +
			"the version is only announced after the tests, build and artefact push have succeeded. " +
			"Push a tag (gh-tag-create) and let the repository's release workflow publish it.",
	}
}

// releaseDelete unpublishes a release, leaving the tag.
//
// This is the narrow operation: withdraw the announcement while keeping the version. For
// "remove this version entirely" use gh-tag-delete, which always removes both — so there is no
// way to leave a release orphaned behind a deleted tag.
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
	if err := c.DeleteRelease(ctx, tag); err != nil {
		return extension.ToolResultData{}, errFailed("%s@%s: %v", repo, tag, err)
	}
	return extension.ToolResultData{
		Content: fmt.Sprintf("deleted release %s (the tag remains)", tag),
	}, nil
}
