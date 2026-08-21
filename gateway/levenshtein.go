package gateway

// normalizedEditRatio returns the similarity of a and b in [0,1]:
// 1 - levenshtein(a,b)/max(len(a),len(b)) on runes — 1 for identical
// strings, ~0 for disjoint ones, 1 for empty-empty. It backs the
// suspected-false-hit retry detection (Issue #262 §3.3): a retry of a
// recently L3-served query is textually close to the original.
//
// The work is bounded: levenshtein is O(|a|·|b|), so inputs longer than
// maxEditLen runes yield 0 (never a false-hit trigger) instead of an
// unbounded allocation — a hostile request cannot burn gateway CPU.
func normalizedEditRatio(a, b string) float64 {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 && len(br) == 0 {
		return 1
	}
	maxLen := len(ar)
	if len(br) > maxLen {
		maxLen = len(br)
	}
	if maxLen == 0 {
		return 1
	}
	if maxLen > maxEditLen {
		return 0 // bound the DP; long texts never count as retries
	}
	return 1 - float64(levenshteinRunes(ar, br))/float64(maxLen)
}

// maxEditLen bounds the levenshtein DP matrix (runes per side).
const maxEditLen = 2048

// levenshteinRunes computes the edit distance between two rune slices with
// a rolling two-row DP (classic Wagner–Fischer).
func levenshteinRunes(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		ai := a[i-1]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if ai == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := cur[j-1] + 1
			sub := prev[j-1] + cost
			m := del
			if ins < m {
				m = ins
			}
			if sub < m {
				m = sub
			}
			cur[j] = m
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
