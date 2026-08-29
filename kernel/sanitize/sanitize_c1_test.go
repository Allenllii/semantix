package sanitize

import "testing"

// TestSanitizeC1UTF8EncodedDispatch is the Issue #358 regression. A multi-byte
// UTF-8-encoded C1 escape must still be stripped (a terminal may act on it —
// the judge and terminal paths share this scanner), but dispatched on its
// DECODED value so it consumes only its own sequence. The old code fed the
// UTF-8 lead byte (0xC2) to the byte-level consumer, always took the OSC/DCS
// (to-ST) branch, and over-consumed unrelated text up to a distant BEL/ST.
func TestSanitizeC1UTF8EncodedDispatch(t *testing.T) {
	csi := string(rune(0x9b)) // U+009B, 2-byte UTF-8
	st := string(rune(0x9c))  // U+009C

	// CSI consumes up to its own final byte 'm'; "KEEP" between it and the later
	// ST must survive. The buggy code scanned to the ST and deleted "KEEP".
	if out := Sanitize(csi + "mKEEP" + st + "TAIL"); out != "KEEPTAIL" {
		t.Fatalf("UTF-8 C1 CSI over-consumed: got %q, want %q", out, "KEEPTAIL")
	}

	// A UTF-8-encoded DCS is consumed up to its ST.
	if out := Sanitize("A" + string(rune(0x90)) + "payload" + st + "B"); out != "AB" {
		t.Fatalf("UTF-8 C1 DCS should strip to ST: got %q, want %q", out, "AB")
	}

	// A CJK rune whose UTF-8 encoding contains a C1-valued *continuation* byte
	// (亐 = U+4E90 -> E4 BA 90; 0x90 is a C1 value) must survive untouched.
	if out := Sanitize("亐修复"); out != "亐修复" {
		t.Fatalf("CJK with C1-valued continuation byte was corrupted: got %q", out)
	}

	// Raw single-byte C1 CSI (invalid UTF-8) is still recognized and stripped.
	if out := Sanitize("x" + string([]byte{0x9b}) + "31mY"); out != "xY" {
		t.Fatalf("raw C1 CSI should still be stripped: got %q, want %q", out, "xY")
	}
}
