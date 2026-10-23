package ghclient

import (
	"context"
	"fmt"
	"strings"

	gogithub "github.com/google/go-github/v93/github"
)

// Tags and releases: what a project PUBLISHES, and the event a downstream job waits for.

// Tag is one tag in the repository.
type Tag struct {
	Name string
	// SHA is the COMMIT the tag points at, not the tag object's own sha — a caller comparing
	// tags to branches means the commit.
	SHA string
	// Annotated is true when the tag is a tag OBJECT with a message, rather than a bare ref.
	Annotated bool
}

// CreateTagOptions describes a tag to create.
type CreateTagOptions struct {
	Name string
	// Ref is the branch, tag or commit to tag. Empty = the repository's default branch.
	Ref string
	// Message is REQUIRED. An annotated tag is a published EVENT with a description; a bare
	// ref is a pointer whose meaning lives only in whoever created it. Releases are built on
	// annotated tags, so allowing a message-less tag would let a caller create something that
	// looks like a version and cannot carry notes.
	Message string
}

// CreateTag creates an ANNOTATED tag pointing at a commit.
//
// Always annotated, never lightweight: see Message. The distinction is not cosmetic —
// `git describe` ignores lightweight tags, and a release needs a tag object to point at.
func (c *Client) CreateTag(ctx context.Context, o CreateTagOptions) (Tag, error) {
	name := strings.TrimSpace(o.Name)
	if name == "" {
		return Tag{}, fmt.Errorf("tag name is required")
	}
	if strings.TrimSpace(o.Message) == "" {
		return Tag{}, fmt.Errorf("a tag message is required: an annotated tag is a published event, and a release is built on one")
	}

	// Resolve the ref to a COMMIT sha first. The tag API takes an object sha and a type, and
	// passing a branch name directly would tag a branch name that does not exist as an object.
	ref := strings.TrimSpace(o.Ref)
	if ref == "" {
		info, err := c.Info(ctx)
		if err != nil {
			return Tag{}, err
		}
		ref = info.DefaultBranch
	}
	commitSHA, err := c.resolveCommit(ctx, ref)
	if err != nil {
		return Tag{}, err
	}

	tagObj, resp, err := c.gh.Git.CreateTag(ctx, c.Owner, c.Repo, gogithub.CreateTag{
		Tag:     name,
		Message: o.Message,
		Object:  commitSHA,
		Type:    "commit",
	})
	c.note(resp)
	if err != nil {
		return Tag{}, fmt.Errorf("create tag object: %w", wrap(err))
	}

	// Then the ref that makes the tag discoverable. Both steps: the object alone is unreachable.
	if _, resp, err := c.gh.Git.CreateRef(ctx, c.Owner, c.Repo, gogithub.CreateRef{
		Ref: "refs/tags/" + name,
		SHA: tagObj.GetSHA(),
	}); err != nil {
		return Tag{}, fmt.Errorf("create tag ref: %w", wrap(err))
	} else {
		c.note(resp)
	}
	return Tag{Name: name, SHA: commitSHA, Annotated: true}, nil
}

// resolveCommit turns a branch, tag or sha into a commit sha.
func (c *Client) resolveCommit(ctx context.Context, ref string) (string, error) {
	// A full sha passes through; a short one is resolved by the ref lookup below anyway, which
	// is simpler than guessing.
	if len(ref) == 40 && isHex(ref) {
		return ref, nil
	}
	for _, prefix := range []string{"refs/heads/", "refs/tags/"} {
		r, resp, err := c.gh.Git.GetRef(ctx, c.Owner, c.Repo, prefix+ref)
		c.note(resp)
		if err != nil {
			continue
		}
		sha := r.GetObject().GetSHA()
		// A tag ref may point at a tag OBJECT; peel it to the commit.
		if strings.HasPrefix(prefix, "refs/tags") {
			if t, _, terr := c.gh.Git.GetTag(ctx, c.Owner, c.Repo, sha); terr == nil {
				if inner := t.GetObject().GetSHA(); inner != "" {
					return inner, nil
				}
			}
		}
		return sha, nil
	}
	// Last resort: treat it as a commit sha and let GitHub reject it if it is not one.
	return ref, nil
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// TagDetail is one tag's full record.
type TagDetail struct {
	Tag
	// Message is the annotation. Empty for a lightweight tag — which cannot be created here,
	// but can exist in a repository that was tagged by other means.
	Message string
	// Tagger is who made the tag, and when.
	TaggerName  string
	TaggerEmail string
	TaggerDate  string
}

// ShowTag reads one tag's annotation.
//
// WHY A SEPARATE CALL. `GET /repos/{o}/{r}/tags` returns only name and commit — GitHub does not
// include the message in the list. Reading it means a second call per tag, so it is exposed as
// a per-tag operation rather than an option on the list: making the list fetch one object per
// tag would turn a 1-request listing into 1+N, and anonymous access allows 60 requests an hour.
// A caller that wants a specific tag's message asks for that tag.
//
// A LIGHTWEIGHT tag has no message at all; that is reported as such rather than as an empty
// string, because "no annotation" and "an empty annotation" are different facts.
func (c *Client) ShowTag(ctx context.Context, name string) (TagDetail, error) {
	if strings.TrimSpace(name) == "" {
		return TagDetail{}, fmt.Errorf("tag name is required")
	}

	// The ref gives either the tag object's sha (annotated) or the commit's (lightweight).
	ref, resp, err := c.gh.Git.GetRef(ctx, c.Owner, c.Repo, "refs/tags/"+name)
	c.note(resp)
	if err != nil {
		return TagDetail{}, wrap(err)
	}
	sha := ref.GetObject().GetSHA()

	d := TagDetail{Tag: Tag{Name: name, SHA: sha}}
	// Annotated: the ref points at a tag object, which carries the message.
	if t, tresp, terr := c.gh.Git.GetTag(ctx, c.Owner, c.Repo, sha); terr == nil {
		c.note(tresp)
		d.Annotated = true
		d.Message = t.GetMessage()
		if tg := t.GetTagger(); tg != nil {
			d.TaggerName = tg.GetName()
			d.TaggerEmail = tg.GetEmail()
			d.TaggerDate = tg.GetDate().Format("2006-01-02 15:04:05")
		}
		// The object a tag points at is the commit callers care about.
		if inner := t.GetObject().GetSHA(); inner != "" {
			d.SHA = inner
		}
	}
	return d, nil
}

// ListTags returns tags with the commit each points at. Paged.
func (c *Client) ListTags(ctx context.Context) ([]Tag, error) {
	var out []Tag
	err := paginate(func(page int) (*gogithub.Response, error) {
		tags, resp, err := c.gh.Repositories.ListTags(ctx, c.Owner, c.Repo,
			&gogithub.ListOptions{Page: page, PerPage: 100})
		c.note(resp)
		if err != nil {
			return resp, err
		}
		for _, t := range tags {
			out = append(out, Tag{
				Name: t.GetName(),
				SHA:  t.GetCommit().GetSHA(),
				// The list endpoint returns the commit (peeling the tag), so annotation has to
				// be checked per tag when a caller cares. Left false here, which is the safe
				// direction: it never claims a message exists that does not.
				Annotated: false,
			})
		}
		return resp, nil
	})
	return out, err
}

// DeleteTag removes a tag ref. Irreversible — a released version disappears with it.
func (c *Client) DeleteTag(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("tag name is required")
	}
	resp, err := c.gh.Git.DeleteRef(ctx, c.Owner, c.Repo, "refs/tags/"+name)
	c.note(resp)
	return wrap(err)
}

// CreateReleaseOptions describes a release.
type CreateReleaseOptions struct {
	// TagName is the tag the release is published at. May be an existing tag.
	TagName string
	// Target is the commit the tag is created at when TagName does not exist yet.
	Target string
	Name   string
	Body   string
	Draft  bool
	// Prerelease marks the release as not-yet-stable; GitHub shows it differently and tooling
	// treats it as not "the latest".
	Prerelease bool
}

// CreateRelease publishes a release.
//
// If the tag does not exist yet, GitHub creates it at Target — which is how a normal publish
// works: one call makes the version visible and announces it.
func (c *Client) CreateRelease(ctx context.Context, o CreateReleaseOptions) (Release, error) {
	if strings.TrimSpace(o.TagName) == "" {
		return Release{}, fmt.Errorf("tag name is required")
	}
	in := gogithub.CreateReleaseRequest{
		TagName:    o.TagName,
		Name:       gogithub.String(o.Name),
		Body:       gogithub.String(o.Body),
		Draft:      gogithub.Bool(o.Draft),
		Prerelease: gogithub.Bool(o.Prerelease),
	}
	if o.Target != "" {
		in.TargetCommitish = gogithub.String(o.Target)
	}
	rel, resp, err := c.gh.Repositories.CreateRelease(ctx, c.Owner, c.Repo, in)
	c.note(resp)
	if err != nil {
		return Release{}, wrap(err)
	}
	return releaseFromG(rel), nil
}

// releaseID finds a release by tag, so it can be deleted.
//
// A release and its tag are SEPARATE objects: deleting the tag ref leaves the release behind,
// pointing at a tag that no longer exists. A caller removing a version has to remove both, so
// the id is looked up here rather than requiring the caller to have remembered it.
func (c *Client) releaseID(ctx context.Context, tag string) (int64, bool) {
	rel, resp, err := c.gh.Repositories.GetReleaseByTag(ctx, c.Owner, c.Repo, tag)
	c.note(resp)
	if err != nil || rel == nil {
		return 0, false
	}
	return rel.GetID(), true
}

func (c *Client) deleteRelease(ctx context.Context, id int64) error {
	resp, err := c.gh.Repositories.DeleteRelease(ctx, c.Owner, c.Repo, id)
	c.note(resp)
	return wrap(err)
}

// DeleteRelease removes the release published at a tag.
//
// Separate from DeleteTag on purpose: they are different objects, and a caller may want to
// unpublish (remove the announcement) while keeping the tag, or retag while keeping the notes.
// DeleteTagAndRelease is the "remove this version entirely" operation.
func (c *Client) DeleteRelease(ctx context.Context, tag string) error {
	id, ok := c.releaseID(ctx, tag)
	if !ok {
		return fmt.Errorf("no release at tag %q", tag)
	}
	return c.deleteRelease(ctx, id)
}

// DeleteTagAndRelease removes both objects for a version.
//
// The order matters: the release goes first, because deleting the tag first would leave the
// release orphaned and harder to find.
func (c *Client) DeleteTagAndRelease(ctx context.Context, tag string) (releaseDeleted bool, err error) {
	id, ok := c.releaseID(ctx, tag)
	if ok {
		if derr := c.deleteRelease(ctx, id); derr != nil {
			return false, fmt.Errorf("delete release: %w", derr)
		}
		releaseDeleted = true
	}
	if terr := c.DeleteTag(ctx, tag); terr != nil {
		return releaseDeleted, fmt.Errorf("delete tag: %w", terr)
	}
	return releaseDeleted, nil
}
