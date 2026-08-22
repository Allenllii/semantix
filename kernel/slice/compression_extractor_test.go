package slice

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExtractCompressionMetadataAndIDAreDeterministic(t *testing.T) {
	content := "answer   \n\n\n---\nanswer   \nanswer   \nuseful tail"
	transcript := []byte(`{"role":"user","content":"question"}` + "\n" +
		`{"role":"assistant","content":` + quoteJSON(content) + `}`)
	meta := SliceMeta{SourceSession: "session-1"}

	first, err := NewExtractor().Extract(transcript, meta)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewExtractor().Extract(transcript, meta)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("slice counts differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID || !reflect.DeepEqual(first[i].Content, second[i].Content) ||
			!reflect.DeepEqual(first[i].Meta, second[i].Meta) {
			t.Fatalf("slice %d is not deterministic:\n%+v\n%+v", i, first[i], second[i])
		}
	}

	result := sliceOfType(first, Result)
	if result == nil {
		t.Fatal("result slice missing")
	}
	if result.Meta.CompressionVersion != compressionVersion {
		t.Fatalf("compression version = %q", result.Meta.CompressionVersion)
	}
	if result.Meta.OriginalBytes != len([]byte(content)) || result.Meta.StoredBytes != len(result.Content) {
		t.Fatalf("compression bytes = original %d stored %d; content=%d", result.Meta.OriginalBytes, result.Meta.StoredBytes, len(result.Content))
	}
	if string(result.Content) != "answer\n\nanswer\nuseful tail" {
		t.Fatalf("result content = %q", result.Content)
	}
}

func TestExtractCompressionKeepsOrdinaryTextAndLimits(t *testing.T) {
	ordinary := []byte(`{"role":"user","content":"keep this ordinary sentence"}`)
	items, err := NewExtractor().Extract(ordinary, SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	prompt := sliceOfType(items, Prompt)
	if prompt == nil || string(prompt.Content) != "keep this ordinary sentence" {
		t.Fatalf("ordinary prompt changed: %+v", prompt)
	}

	long := "开头-" + strings.Repeat("文", maxResultLen) + "-结尾"
	transcript := []byte(`{"role":"assistant","content":` + quoteJSON(long) + `}`)
	items, err = NewExtractor().Extract(transcript, SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	result := sliceOfType(items, Result)
	if result == nil || len(result.Content) > maxResultLen || !utf8.Valid(result.Content) {
		t.Fatalf("bounded result invalid: %+v", result)
	}
	if !strings.HasPrefix(string(result.Content), "开头-") || !strings.HasSuffix(string(result.Content), "-结尾") {
		t.Fatalf("bounded result lost head or tail: %q", result.Content)
	}
}

func sliceOfType(items []*Slice, typ SliceType) *Slice {
	for _, item := range items {
		if item.Type == typ {
			return item
		}
	}
	return nil
}

func quoteJSON(value string) string {
	quoted, _ := json.Marshal(value)
	return string(quoted)
}
