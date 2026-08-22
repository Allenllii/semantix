package slice

import (
	"strings"
	"testing"
)

func TestExtractContextSliceDeterministicAndSanitized(t *testing.T) {
	transcript := []byte(`{"role":"user","content":"inspect the extractor"}
{"role":"assistant","tool_calls":[{"name":"read_file","arguments":{"path":"kernel/slice/extractor.go","token":"secret-value"}},{"name":"bash","arguments":{"command":"go test ./kernel/slice"}}]}
{"role":"assistant","tool_calls":[{"name":"edit_file","arguments":{"path":"kernel/slice/extractor.go"}},{"name":"bash","arguments":{"command":"go test ./kernel/slice"}}]}
{"role":"assistant","tool_calls":[{"name":"read_file","arguments":{"path":"/etc/passwd"}},{"name":"read_file","arguments":{"path":"C:\\Users\\secret\\key.txt"}},{"name":"read_file","arguments":{"path":"../escape.txt"}}]}
{"role":"assistant","content":"done"}`)

	first, err := NewExtractor().Extract(transcript, SliceMeta{ProjectSlug: "semantix"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewExtractor().Extract(transcript, SliceMeta{ProjectSlug: "semantix"})
	if err != nil {
		t.Fatal(err)
	}
	a := contextFrom(first)
	b := contextFrom(second)
	if a == nil || b == nil {
		t.Fatal("expected a Context slice")
	}
	if a.ID != b.ID || string(a.Content) != string(b.Content) {
		t.Fatalf("context slice is not deterministic:\nfirst=%s %q\nsecond=%s %q", a.ID, a.Content, b.ID, b.Content)
	}
	if a.Scope != Project {
		t.Fatalf("context scope = %v, want Project", a.Scope)
	}
	content := string(a.Content)
	for _, want := range []string{
		"- kernel/slice/extractor.go (2)",
		"- kernel/slice (2)",
		"- go test (2)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("context missing %q:\n%s", want, content)
		}
	}
	for _, forbidden := range []string{"secret-value", "/etc/passwd", "Users/secret", "escape.txt"} {
		if strings.Contains(content, forbidden) {
			t.Errorf("context leaked %q:\n%s", forbidden, content)
		}
	}
}

func TestExtractContextSliceRequiresRepeatedObservation(t *testing.T) {
	transcript := []byte(`{"role":"assistant","tool_calls":[{"name":"read_file","arguments":{"path":"kernel/slice/extractor.go"}},{"name":"bash","arguments":{"command":"go test ./..."}}]}`)
	got, err := NewExtractor().Extract(transcript, SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if contextFrom(got) != nil {
		t.Fatal("one-off observations must not create a Context slice")
	}
}

func TestExtractContextSliceTopNUsesLexicalTieBreak(t *testing.T) {
	var lines strings.Builder
	for _, name := range []string{"f.go", "e.go", "d.go", "c.go", "b.go", "a.go"} {
		for range 2 {
			lines.WriteString(`{"role":"assistant","tool_calls":[{"name":"read_file","arguments":{"path":"`)
			lines.WriteString(name)
			lines.WriteString(`"}}]}` + "\n")
		}
	}
	got, err := NewExtractor().Extract([]byte(lines.String()), SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	context := contextFrom(got)
	if context == nil {
		t.Fatal("expected a Context slice")
	}
	content := string(context.Content)
	for _, want := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		if !strings.Contains(content, "- "+want+" (2)") {
			t.Errorf("top-five output missing %s:\n%s", want, content)
		}
	}
	if strings.Contains(content, "- f.go (2)") {
		t.Fatalf("top-five output retained sixth lexical tie:\n%s", content)
	}
}

func TestExtractContextSliceAcceptsEncodedArguments(t *testing.T) {
	transcript := []byte(`{"role":"assistant","tool_calls":[{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}]}
{"role":"assistant","tool_calls":[{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}]}`)
	got, err := NewExtractor().Extract(transcript, SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	context := contextFrom(got)
	if context == nil || !strings.Contains(string(context.Content), "- README.md (2)") {
		t.Fatalf("encoded tool arguments were not extracted: %#v", context)
	}
}

func contextFrom(items []*Slice) *Slice {
	for _, item := range items {
		if item.Type == Context {
			return item
		}
	}
	return nil
}
