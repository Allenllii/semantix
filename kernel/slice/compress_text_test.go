package slice

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCompressTextRulesAreDeterministic(t *testing.T) {
	input := "title  \r\n\r\n\r\n---\r\nrepeat\r\nrepeat\r\n```\r\ncode  \r\n```\r\n"
	want := "title\n\nrepeat\n```\ncode\n```"

	first := compressText(input, maxPromptLen)
	second := compressText(input, maxPromptLen)
	if string(first) != want {
		t.Fatalf("compressed = %q, want %q", first, want)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("same input produced different bytes: %q vs %q", first, second)
	}
}

func TestCompressTextNoisyFixtureSavesAtLeastThirtyPercent(t *testing.T) {
	input := strings.Repeat("useful result   \nuseful result   \n\n\n***\n", 40)
	got := compressText(input, maxResultLen)
	if len(got)*10 > len(input)*7 {
		t.Fatalf("compressed %d bytes from %d, want at least 30%% reduction", len(got), len(input))
	}
	if !strings.Contains(string(got), "useful result") {
		t.Fatalf("useful content was removed: %q", got)
	}
}

func TestCompressTextOversizeRetainsUTF8HeadAndTail(t *testing.T) {
	input := "开头-" + strings.Repeat("中", 100) + "-结尾"
	got := compressText(input, 80)
	if len(got) > 80 {
		t.Fatalf("compressed length = %d, want <= 80", len(got))
	}
	if !utf8.Valid(got) {
		t.Fatalf("compressed output is invalid UTF-8: %q", got)
	}
	if !strings.HasPrefix(string(got), "开头-") || !strings.HasSuffix(string(got), "-结尾") {
		t.Fatalf("head or tail missing: %q", got)
	}
	if !strings.Contains(string(got), strings.TrimSpace(compactionMarker)) {
		t.Fatalf("compaction marker missing: %q", got)
	}
}

func TestCompressTextSmallLimitStaysValid(t *testing.T) {
	got := compressText(strings.Repeat("界", 10), 5)
	if len(got) > 5 || !utf8.Valid(got) {
		t.Fatalf("small-limit output = %q (%d bytes)", got, len(got))
	}
}
