package uihandler

import "context"

/**
 * MirroredSource answers ONE question about the mirror: which of these repositories exist in it.
 *
 * WHY IT IS A SEPARATE INTERFACE FROM BrowseSource: those read GITHUB — what you can see — and this
 * reads the MIRROR — what you can change. The panel's home page lists the first and its Files page
 * reads the second, and before this the two were never compared. A reader clicked a repository that
 * GitHub knew about, and got "The target couldn't be found" from a forge that had never heard of it.
 *
 * TAKES A LIST, not one name: the answer is a set, and asking per repository would be one round trip
 * per row on a page that shows eighty.
 */
type MirroredSource interface {
	// Mirrored reports each repository's state in the mirror.
	Mirrored(ctx context.Context, repos []string) (map[string]MirrorState, error)
}

// MirrorState is what the mirror has for one repository.
//
// THREE STATES, NOT A BOOLEAN. "Exists" and "can be opened" are different: a repository created with
// no initial commit exists, answers 200, and has no branches and no tree — so a check that only asks
// whether it is there marks it openable and the reader gets an empty page. That is what happened to
// the two repositories this was found on.
type MirrorState string

const (
	// MirrorMissing: the mirror has never heard of it.
	MirrorMissing MirrorState = "missing"
	// MirrorEmpty: the repository exists but has no commits, so there is nothing to browse.
	MirrorEmpty MirrorState = "empty"
	// MirrorReady: it exists and has at least one branch.
	MirrorReady MirrorState = "ready"
)
