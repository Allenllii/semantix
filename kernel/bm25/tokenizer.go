package bm25

import (
	"strings"
	"unicode"
)

// Tokenize lowercases word tokens and emits each CJK rune as its own token.
func Tokenize(text string) []string {
	var tokens []string
	var word strings.Builder
	flush := func() {
		if word.Len() == 0 {
			return
		}
		tokens = append(tokens, word.String())
		word.Reset()
	}

	for _, r := range text {
		switch {
		case isCJK(r):
			flush()
			tokens = append(tokens, string(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			word.WriteRune(unicode.ToLower(r))
		default:
			flush()
		}
	}
	flush()
	return tokens
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

func countTerms(terms []string) map[string]int {
	counts := make(map[string]int, len(terms))
	for _, term := range terms {
		counts[term]++
	}
	return counts
}

func uniqueTerms(terms []string) []string {
	seen := make(map[string]struct{}, len(terms))
	unique := make([]string, 0, len(terms))
	for _, term := range terms {
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		unique = append(unique, term)
	}
	return unique
}

// QueryCoverage returns the fraction of query terms that appear in content,
// in [0,1]. A query with no terms (pure punctuation/symbols) returns 0
// — fail-closed for the lexical support gate (Issue #260).
func QueryCoverage(query, content string) float64 {
	qt := uniqueTerms(Tokenize(query))
	if len(qt) == 0 {
		return 0
	}
	ct := countTerms(Tokenize(content))
	covered := 0
	for _, q := range qt {
		if _, ok := ct[q]; ok {
			covered++
		}
	}
	return float64(covered) / float64(len(qt))
}
