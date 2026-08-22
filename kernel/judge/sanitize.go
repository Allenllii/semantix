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
		// C1 control values are intentionally NOT matched here: per the design
		// note above, a C1 byte is only an escape when it appears as its own raw
		// byte (handled on the RuneError path). A legally UTF-8-encoded C1 rune
		// (e.g. 0xC2 0x9B → U+009B) is ordinary content and must survive — the
		// old case re-entered consumeC1Seq with the UTF-8 lead byte at s[0],
		// mis-dispatching and silently deleting unrelated text (Issue #358).
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
