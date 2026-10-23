package stage

import (
	"fmt"
)

// chainFrom returns the revisions to export so that `target` is a DESCENDANT of `base`.
//
// gitbridge exports whatever list it is given, one git commit per revision, linking each to
// the previous. Given only the newest revision it exports a single ROOT commit — which is what
// happened before, and GitHub then refuses the pull request ("the branch has no history in
// common with main").
//
// The list therefore has to start at the base the edits were made against and end at the new
// commit. Everything between them is included, because a commit is only a descendant of its
// actual parent chain.
//
// A base that cannot be found in the log is reported rather than silently skipped: exporting a
// chain that does not start at the base would produce exactly the orphan branch this exists to
// prevent.
func (w *Workspace) chainFrom(base, target string) ([]string, error) {
	if target == "" {
		return nil, fmt.Errorf("no target revision")
	}
	if base == "" || base == target {
		return []string{target}, nil
	}

	w.mu.Lock()
	ws := w.ws
	w.mu.Unlock()

	revs, err := ws.Log()
	if err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}
	// Log is newest-first. Walk back from target to base, collecting.
	var chain []string
	seenTarget := false
	for _, r := range revs {
		id := r.ID
		if !seenTarget {
			if id == target {
				seenTarget = true
				chain = append(chain, id)
			}
			continue
		}
		chain = append(chain, id)
		if id == base {
			// Reverse to oldest-first, which is the order gitbridge exports in.
			for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
				chain[i], chain[j] = chain[j], chain[i]
			}
			return chain, nil
		}
	}
	if !seenTarget {
		return nil, fmt.Errorf("target revision %s is not in this stage's log", target)
	}
	return nil, fmt.Errorf("base revision %s (the commit these edits were made against) is not in the log; "+
		"exporting from it is what makes the branch share the target branch's history, so this is refused "+
		"rather than pushed as an orphan", base)
}
