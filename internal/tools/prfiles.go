package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/easydev-org/easybus-sdk/extension"
)

// What a pull request changed. Reviewing without this is reviewing the title, not the change.

// prFiles lists the files a pull request touches, with per-file counts.
func (t *toolset) prFiles(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("listing changed files")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	n := argInt(args, "issue_id")
	if n <= 0 {
		return extension.ToolResultData{}, errArg(`"number" is required`)
	}
	files, err := c.PRFiles(ctx, n)
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s#%d: %v", repo, n, err)
	}

	var b strings.Builder
	adds, dels := 0, 0
	for _, f := range files {
		adds += f.Additions
		dels += f.Deletions
		path := f.Path
		if f.PreviousPath != "" {
			path = f.PreviousPath + " -> " + f.Path
		}
		fmt.Fprintf(&b, "%-9s +%-5d -%-5d %s\n", f.Status, f.Additions, f.Deletions, path)
		if f.PatchTruncated {
			// GitHub omits the patch for binary files and oversized diffs. Saying so prevents a
			// caller reading "no patch" as "no change".
			b.WriteString("          (patch not provided by GitHub: binary or too large)\n")
		}
	}
	if len(files) == 0 {
		b.WriteString("(no files changed)\n")
	}
	fmt.Fprintf(&b, "\n%d files, +%d -%d", len(files), adds, dels)
	return extension.ToolResultData{
		Content: b.String(),
		Data: map[string]any{
			// The struct slice is converted to maps by the registry wrapper (see withNormalisedData),
			// so every tool gets the same treatment instead of each one remembering.
			"files": files, "count": len(files), "additions": adds, "deletions": dels,
			// The per-file list IS the answer, so the card lets the UI read it rather than restating
			// a count: a card that summarises a summary is worth nothing.
			"card": cardFor(cardTemplatePRFiles),
			// The page reads GitHub, so opening it shows the CURRENT pull request rather than this
			// snapshot — and a workspace that expired cannot break it.
			"view": pullView(repo, n, fmt.Sprintf("%s#%d", repo, n)),
		},
	}, nil
}

// prDiff shows the whole change as one unified diff.
func (t *toolset) prDiff(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
	Progress(ctx).Phase("fetching the diff")
	c, repo, err := t.clientFor(tenant, args)
	if err != nil {
		return extension.ToolResultData{}, err
	}
	n := argInt(args, "issue_id")
	if n <= 0 {
		return extension.ToolResultData{}, errArg(`"number" is required`)
	}
	d, err := c.PRDiff(ctx, n)
	if err != nil {
		return extension.ToolResultData{}, errFailed("%s#%d: %v", repo, n, err)
	}
	if strings.TrimSpace(d) == "" {
		return extension.ToolResultData{Content: "(the pull request has no diff)"}, nil
	}
	// Capped: a large refactor can run to tens of thousands of lines. The cap is stated when it
	// applies rather than silently cutting.
	const maxLines = 2000
	lines := strings.Split(d, "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	out := strings.Join(lines, "\n")
	if truncated {
		out += fmt.Sprintf("\n\n... [truncated at %d lines of %d; use gh-pr-files for the per-file view]", maxLines, len(strings.Split(d, "\n")))
	}
	return extension.ToolResultData{
		Content: out,
		Data: map[string]any{
			"number": n, "truncated": truncated,
			"card": cardFor(cardTemplatePRDiff),
			"view": pullView(repo, n, fmt.Sprintf("%s#%d", repo, n)),
		},
	}, nil
}
