package judge

import (
	"strings"
	"unicode/utf8"
)

// Sanitize strips ANSI/OSC/DCS escape sequences from untrusted slice content
// before it enters the judge prompt (Issue #8 acceptance ④): a cached answer
// may contain prompt-injection payloads, and the judge must read a
// deterministically cleaned copy — never raw history.
//
// The implementation is the same rune-level escape scanner used for terminal
// output (cmd/semantix/sanitize.go): C1 control bytes are only recognized as
// their own runes (never inside multi-byte UTF-8), every escape family
// (CSI/OSC/DCS/PM/APC/SOS/ST/two-char) is consumed to its terminator, and
// unterminated sequences strip only the start byte — legal text always
// survives intact.
func Sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			switch c := s[0]; {
			case c == 0x1b:
				if len(s) < 2 {
					return b.String()
				}
				s = consumeESCSeq(s)
			case c == 0x90 || c == 0x98 || c == 0x9b || c == 0x9c || c == 0x9d || c == 0x9e || c == 0x9f:
				s = consumeC1Seq(s)
			default:
				b.WriteByte(c)
				s = s[1:]
			}
			continue
		}
		switch r {
		case 0x1b:
			if len(s) < 2 {
				return b.String()
			}
			s = consumeESCSeq(s)
		case 0x90, 0x98, 0x9b, 0x9c, 0x9d, 0x9e, 0x9f:
			// A multi-byte-encoded C1 escape (e.g. 0xC2 0x9B → U+009B). It must
			// still be stripped — a terminal may act on it — but dispatch on the
			// DECODED value and scan the bytes AFTER the encoding (s[size:]). The
			// old code passed the un-advanced s to consumeC1Seq, whose switch saw
			// the UTF-8 lead byte 0xC2, always fell to the OSC/DCS (to-ST) branch,
			// and over-consumed unrelated text up to a distant BEL/ST (Issue #358).
			rest := s[size:]
			switch r {
			case 0x9b: // CSI: consume up to its own final byte
				if out, ok := consumeToFinal(rest); ok {
					s = out
				} else {
					s = rest
				}
			case 0x9c: // lone ST: nothing follows to consume
				s = rest
			default: // DCS/OSC/PM/APC/SOS: consume up to ST
				if out, ok := consumeToST(rest); ok {
					s = out
				} else {
					s = rest
				}
			}
		default:
			b.WriteString(s[:size])
			s = s[size:]
		}
	}
	return b.String()
}

// consumeESCSeq consumes an ESC-prefixed sequence (s starts with 0x1b).
func consumeESCSeq(s string) string {
	rest, ok := consumeESCBody(s)
	if ok {
		return rest
	}
	return s[1:] // unterminated: strip only the ESC byte
}

func consumeESCBody(s string) (string, bool) {
	switch s[1] {
	case '[':
		return consumeToFinal(s[2:])
	case ']':
		return consumeToST(s[2:])
	case 'P', '^', '_', 'X':
		return consumeToST(s[2:])
	case '\\':
		return s[2:], true
	default:
		if s[1] < 0x80 {
			return s[2:], true // two-character sequence
		}
		return "", false // ESC before multi-byte char: strip only ESC
	}
}

// consumeC1Seq consumes a C1 escape sequence (s starts with a C1 byte).
func consumeC1Seq(s string) string {
	switch s[0] {
	case 0x9b:
		if rest, ok := consumeToFinal(s[1:]); ok {
			return rest
		}
	case 0x9c:
		return s[1:]
	default:
		if rest, ok := consumeToST(s[1:]); ok {
			return rest
		}
	}
	return s[1:]
}

// consumeToFinal consumes up to and including the CSI final byte (letter or ~).
func consumeToFinal(s string) (string, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x40 && c <= 0x7e {
			return s[i+1:], true
		}
	}
	return "", false
}

// consumeToST consumes up to and including the OSC/DCS terminator
// (BEL, ST 0x9C, or the two-byte ESC \ form).
func consumeToST(s string) (string, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x07 || c == 0x9c {
			return s[i+1:], true
		}
		if c == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
			return s[i+2:], true
		}
	}
	return "", false
}
