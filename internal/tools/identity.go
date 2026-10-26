package tools

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Who this token is, and who owns a given object.
//
// The identity is cached per tenant for a short time because ownership checks sit in front of every
// close/cancel/rerun call, and a /user request per call would add a round trip to each of them for
// a value that changes when the token is replaced — not between two tool calls.

type cachedLogin struct {
	login   string
	expires time.Time
}

var (
	loginMu    sync.Mutex
	loginCache = map[string]cachedLogin{}
)

// loginTTL is how long a resolved identity is trusted.
//
// Ten minutes: long enough that a burst of tool calls pays for it once, short enough that rotating
// a token takes effect without waiting for a restart. A stale identity would make ownership
// decisions against the wrong name, so this is deliberately not "forever".
const loginTTL = 10 * time.Minute

// tokenLogin returns the login the token authenticates as.
func (t *toolset) tokenLogin(ctx context.Context, tenant, repo string) (string, error) {
	loginMu.Lock()
	if c, ok := loginCache[repoKey(repo)]; ok && time.Now().Before(c.expires) {
		loginMu.Unlock()
		return c.login, nil
	}
	loginMu.Unlock()

	c, err := t.accountClient(tenant)
	if err != nil {
		return "", err
	}
	id, _, ierr := c.IdentityAndScopes(ctx)
	if ierr != nil {
		return "", ierr
	}
	loginMu.Lock()
	loginCache[repoKey(repo)] = cachedLogin{login: id.Login, expires: time.Now().Add(loginTTL)}
	loginMu.Unlock()
	return id.Login, nil
}

func repoKey(repo string) string { return repo }

// issueAuthor is the login that opened an issue or pull request.
func (t *toolset) issueAuthor(ctx context.Context, tenant, repo string, number int) (string, error) {
	c, _, err := t.clientFor(tenant, map[string]any{"repo": repo})
	if err != nil {
		return "", err
	}
	is, ierr := c.Issue(ctx, number)
	if ierr != nil {
		return "", ierr
	}
	if is.Author == "" {
		return "", fmt.Errorf("GitHub reported no author for #%d", number)
	}
	return is.Author, nil
}

// runActor is the login that triggered a workflow run.
func (t *toolset) runActor(ctx context.Context, tenant, repo string, runID int64) (string, error) {
	c, _, err := t.clientFor(tenant, map[string]any{"repo": repo})
	if err != nil {
		return "", err
	}
	return c.RunActor(ctx, runID)
}
