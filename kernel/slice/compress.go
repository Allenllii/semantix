package slice

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

const (
	compressionVersion = "rules-v1"
	compactionMarker   = "\n...[content compacted]...\n"
)

// compressText applies the versioned, deterministic text rules documented in
// docs/specs/issue-276-extract-compression.md and then enforces max bytes.
func compressText(content string, max int) []byte {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	var fence byte
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if delimiter := markdownFenceDelimiter(line); delimiter != 0 {
			kept = append(kept, line)
			if fence == 0 {
				fence = delimiter
			} else if fence == delimiter {
				fence = 0
			}
			continue
		}
		if fence != 0 {
			kept = append(kept, line)
			continue
		}
		blank := strings.TrimSpace(line) == ""
		if isMarkdownDecoration(line) {
			continue
		}
		if len(kept) > 0 {
			previous := kept[len(kept)-1]
			if blank && strings.TrimSpace(previous) == "" {
				continue
			}
			if !blank && line == previous {
				continue
			}
		}
		kept = append(kept, line)
	}

	compacted := []byte(strings.TrimSpace(strings.Join(kept, "\n")))
	if len(compacted) <= max {
		return compacted
	}
	return retainHeadAndTail(compacted, max)
}

func markdownFenceDelimiter(line string) byte {
	line = strings.TrimSpace(line)
	if len(line) < 3 {
		return 0
	}
	if line[0] == '`' && line[1] == '`' && line[2] == '`' {
		return '`'
	}
	if line[0] == '~' && line[1] == '~' && line[2] == '~' {
		return '~'
	}
	return 0
}

func isMarkdownDecoration(line string) bool {
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) < 3 {
		return false
	}
	for _, r := range line {
		if !strings.ContainsRune("-*_=~#", r) {
			return false
		}
	}
	return true
}

func retainHeadAndTail(content []byte, max int) []byte {
	if len(content) <= max {
		return content
	}
	marker := []byte(compactionMarker)
	if max <= len(marker) {
		return utf8Prefix(content, max)
	}

	available := max - len(marker)
	headBudget := available / 2
	tailBudget := available - headBudget
	head := utf8Prefix(content, headBudget)
	tail := utf8Suffix(content, tailBudget)

	out := make([]byte, 0, max)
	out = append(out, head...)
	out = append(out, marker...)
	out = append(out, tail...)
	return out
}

func utf8Prefix(content []byte, max int) []byte {
	if len(content) <= max {
		return content
	}
	prefix := content[:max]
	for len(prefix) > 0 && !utf8.Valid(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix
}

func utf8Suffix(content []byte, max int) []byte {
	if len(content) <= max {
		return content
	}
	suffix := content[len(content)-max:]
	for len(suffix) > 0 && !utf8.Valid(suffix) {
		suffix = suffix[1:]
	}
	return bytes.Clone(suffix)
}
