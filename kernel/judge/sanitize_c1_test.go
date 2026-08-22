package judge

import "testing"

// TestSanitizeKeepsUTF8EncodedC1 is the Issue #358 regression: a legally
// UTF-8-encoded C1 rune (0xC2 0x9B -> U+009B) is ordinary content, not a raw C1
// escape. The old valid-rune switch re-entered consumeC1Seq with the UTF-8 lead
// byte, mis-dispatching and silently deleting unrelated text between the encoded
// C1 and a later BEL/ST. Content on both sides must survive.
func TestSanitizeKeepsUTF8EncodedC1(t *testing.T) {
	// string(rune(0x9b)) is the 2-byte UTF-8 encoding of U+009B (a C1 value).
	// It is ordinary content; the later BEL (0x07) is unrelated. Nothing may be
	// deleted from between them.
	in := "AAAA" + string(rune(0x9b)) + "BBBB" + string(rune(0x07)) + "CCCC"
	if out := Sanitize(in); out != in {
		t.Fatalf("UTF-8-encoded C1 must not delete surrounding text: got %q, want %q", out, in)
	}

	// A raw single-byte C1 CSI (invalid UTF-8) is still recognized and stripped.
	rawC1 := "x" + string([]byte{0x9b}) + "31mY"
	if got := Sanitize(rawC1); got != "xY" {
		t.Fatalf("raw C1 CSI should still be stripped: got %q, want %q", got, "xY")
	}

	// A real ESC-CSI sequence is still stripped.
	escCSI := "x" + string([]byte{0x1b}) + "[31mY"
	if got := Sanitize(escCSI); got != "xY" {
		t.Fatalf("ESC-CSI should still be stripped: got %q, want %q", got, "xY")
	}
}
