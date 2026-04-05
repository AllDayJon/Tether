package watcher

// NewLinesSince finds lines in curr that appeared after prev ended.
//
// It looks for the longest suffix of prev (up to 10 lines) that matches a
// prefix of curr, then returns everything in curr after that overlap.
// If no overlap is found (buffer was reset or replaced), it returns all of curr.
func NewLinesSince(prev, curr []string) []string {
	if len(prev) == 0 {
		return curr
	}

	maxOverlap := len(prev)
	if maxOverlap > 10 {
		maxOverlap = 10
	}
	if maxOverlap > len(curr) {
		maxOverlap = len(curr)
	}

	for k := maxOverlap; k > 0; k-- {
		prevSuffix := prev[len(prev)-k:]
		currPrefix := curr[:k]
		match := true
		for i := range prevSuffix {
			if prevSuffix[i] != currPrefix[i] {
				match = false
				break
			}
		}
		if match {
			return curr[k:]
		}
	}
	return curr // no overlap: treat everything as new
}
