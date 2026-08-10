package main

import (
	"strings"
	"unicode/utf8"
)

// stripESC removes ANSI/OSC escape sequences from untrusted content before
// terminal output. Malicious session files must not be able to rewrite the
// user's terminal (cursor moves, title changes, OSC 52 clipboard writes,
// tmux passthrough).
//
// Entry checks are rune-level: a C1 control (U+0090 etc.) only counts when it
// appears as its own rune. C1 bytes (0x80-0x9F) are valid UTF-8 continuation
// bytes inside multi-byte characters (e.g. 亐 U+4E90 = E4 BA 90), and byte
// scanning would corrupt them — rune decoding keeps legal text intact.
//
// Covered escape families (ESC-prefixed and C1 forms):
//   CSI   ESC [ ... final | 0x9B ... final        (final = letter/~)
//   OSC   ESC ] ... BEL/ST | 0x9D ... BEL/ST
//   DCS   ESC P ... BEL/ST | 0x90 ... BEL/ST
//   PM    ESC ^ ... BEL/ST | 0x9E ... BEL/ST
//   APC   ESC _ ... BEL/ST | 0x9F ... BEL/ST
//   SOS   ESC X ... BEL/ST | 0x98 ... BEL/ST
//   ST    ESC \            | 0x9C (stripped alone)
//   two-char  ESC <byte>   (e.g. ESC 7 DECSC) — consumed, 2 bytes only
//
// Sequence payloads are ASCII by spec, so inner scanning is byte-level.
func stripESC(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			// Not valid UTF-8. A lone control byte (C1 range or ESC) is an
			// escape start — strip it; any other invalid byte is kept as-is
			// (never widen damage).
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
		case 0x1b: // ESC
			if len(s) < 2 {
				return b.String() // trailing bare ESC
			}
			s = consumeESCSeq(s)
		case 0x90, 0x98, 0x9b, 0x9c, 0x9d, 0x9e, 0x9f: // C1 controls (own rune)
			s = consumeC1Seq(s)
		default:
			b.WriteString(s[:size])
			s = s[size:]
		}
	}
	return b.String()
}

// consumeESCSeq consumes an ESC-prefixed sequence (s starts with 0x1b) and
// returns the remainder.
func consumeESCSeq(s string) string {
	rest, ok := consumeESCBody(s)
	if ok {
		return rest
	}
	// unterminated: strip only the ESC byte
	return s[1:]
}

// consumeESCBody dispatches by the byte after ESC and returns whether a
// full terminator was found.
func consumeESCBody(s string) (string, bool) {
	switch s[1] {
	case '[': // CSI
		return consumeToFinal(s[2:])
	case ']': // OSC
		return consumeToST(s[2:])
	case 'P', '^', '_', 'X': // DCS / PM / APC / SOS
		return consumeToST(s[2:])
	case '\\': // ST alone
		return s[2:], true
	default:
		if s[1] < 0x80 {
			// two-character sequence (ESC 7 DECSC, ESC = DECKPAM, ...)
			return s[2:], true
		}
		// ESC directly before a multi-byte UTF-8 character: the second byte
		// is a lead byte, not a sequence — strip only ESC so the character
		// (and its C1-valued continuation bytes) survives intact.
		return "", false
	}
}

// consumeC1Seq consumes a C1 escape sequence (s starts with a C1 control
// byte or rune) and returns the remainder.
func consumeC1Seq(s string) string {
	switch s[0] {
	case 0x9b: // C1 CSI
		if rest, ok := consumeToFinal(s[1:]); ok {
			return rest
		}
	case 0x9c: // C1 ST alone
		return s[1:]
	default: // C1 OSC / DCS / PM / APC / SOS
		if rest, ok := consumeToST(s[1:]); ok {
			return rest
		}
	}
	// unterminated: strip only the C1 byte
	return s[1:]
}

// consumeToFinal consumes bytes until a CSI final byte (letter, ~, or BEL).
// Returns the remainder and whether a terminator was found. If no terminator
// exists, the caller strips only the escape start (an unterminated sequence
// is inert on terminals) instead of eating the rest of the text.
func consumeToFinal(s string) (string, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '~' || c == 0x07 {
			return s[i+1:], true
		}
	}
	return "", false
}

// consumeToST consumes bytes until BEL (0x07) or ST (0x9C or ESC \).
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
