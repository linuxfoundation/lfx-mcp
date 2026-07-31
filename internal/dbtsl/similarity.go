// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package dbtsl provides a client for the dbt Semantic Layer API.
package dbtsl

// similarityRatio returns the Ratcliff/Obershelp similarity of a and b, in
// the range 0 to 1.
//
// This is the algorithm behind Python's difflib.SequenceMatcher.ratio(), which
// the lfx-lens implementation used for its metric suggestion fallback. It is
// reproduced rather than approximated with an edit distance so that the
// suggestions a caller sees do not change as part of the port.
//
// difflib's "autojunk" heuristic is not reproduced: it only engages on
// sequences of 200 elements or more, and metric names are far shorter.
func similarityRatio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	total := len(ra) + len(rb)
	if total == 0 {
		return 1
	}
	return 2 * float64(matchingRunes(ra, rb)) / float64(total)
}

// matchingRunes counts the runes a and b have in common, by finding the
// longest common contiguous run and recursing into the segments on either
// side of it.
func matchingRunes(a, b []rune) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	aStart, bStart, length := longestCommonRun(a, b)
	if length == 0 {
		return 0
	}

	return length +
		matchingRunes(a[:aStart], b[:bStart]) +
		matchingRunes(a[aStart+length:], b[bStart+length:])
}

// longestCommonRun returns the start offsets in a and b of their longest
// common contiguous run, and its length.
//
// Ties resolve to the earliest run in a, then the earliest in b, matching
// difflib's behaviour.
// The j scan runs forwards, which is what makes the tie-break match. difflib
// walks the positions of each character in b in ascending order and keeps a
// run only when it is strictly longer, so among equal-length runs the
// earliest j wins. Scanning j backwards over a single rolling array is the
// more compact way to write this, but it inverts that rule and keeps the
// latest j instead, which strands the rest of a against a shorter tail and
// makes the recursion undercount. Two rows cost one extra allocation and are
// worth it.
func longestCommonRun(a, b []rune) (aStart, bStart, length int) {
	// prev and cur hold, for the previous and current row, the length of the
	// common run ending at a[i], b[j]. Every j is assigned on every row, so
	// no stale values survive the swap.
	prev := make([]int, len(b))
	cur := make([]int, len(b))

	for i := range a {
		for j := range b {
			if a[i] != b[j] {
				cur[j] = 0
				continue
			}
			if j == 0 {
				cur[j] = 1
			} else {
				cur[j] = prev[j-1] + 1
			}
			if cur[j] > length {
				length = cur[j]
				aStart = i - length + 1
				bStart = j - length + 1
			}
		}
		prev, cur = cur, prev
	}
	return aStart, bStart, length
}
