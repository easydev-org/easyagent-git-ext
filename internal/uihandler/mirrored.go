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
	// Mirrored reports which of the given `owner/name` repositories exist in the mirror.
	Mirrored(ctx context.Context, repos []string) (map[string]bool, error)
}
