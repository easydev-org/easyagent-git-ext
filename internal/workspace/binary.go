package workspace

// isBinary reports whether content looks binary.
//
// A NUL byte in the first 8000 bytes is what git itself checks, so a file git calls binary is
// called binary here too. Used by Status to mark an entry rather than pretend a binary file has
// countable lines.
func isBinary(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	for i := 0; i < n; i++ {
		if b[i] == 0 {
			return true
		}
	}
	return false
}
