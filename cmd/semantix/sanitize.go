package main

import "strings"

// stripESC removes ANSI/OSC escape sequences from untrusted content before
// terminal output. Malicious session files must not be able to rewrite the
// user's terminal (cursor moves, title changes, OSC 52 clipboard writes,
// tmux passthrough).
//
// Covered escape families (both ESC-prefixed and C1 single-byte forms):
//   CSI   ESC [ ... final-byte | 0x9B ... final-byte
//   OSC   ESC ] ... BEL/ST      | 0x9D ... BEL/ST
//   DCS   ESC P ... ST/BEL      | 0x90 ... ST/BEL
//   PM    ESC ^ ... ST/BEL      | 0x9E ... ST/BEL
//   APC   ESC _ ... ST/BEL      | 0x9F ... ST/BEL
//   SOS   ESC X ... ST/BEL      | 0x98 ... ST/BEL
//   ST    ESC \                 | 0x9C (stripped alone)
//   two-char  ESC <byte>        (e.g. ESC 7 DECSC) — consumed, 2 bytes only
func stripESC(s string) string {
	// fast path: byte-level (ContainsRune would treat lone C1 bytes as
	// invalid UTF-8 / RuneError and miss them).
	if strings.IndexByte(s, 0x1b) < 0 &&
		strings.IndexByte(s, 0x90) < 0 && strings.IndexByte(s, 0x98) < 0 &&
		strings.IndexByte(s, 0x9b) < 0 && strings.IndexByte(s, 0x9c) < 0 &&
		strings.IndexByte(s, 0x9d) < 0 && strings.IndexByte(s, 0x9e) < 0 &&
		strings.IndexByte(s, 0x9f) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if isC1Start(s[i]) {
			// CSI (0x9B) terminates on a final byte (letter/~); every other
			// C1 start (OSC/DCS/PM/APC/SOS) terminates on BEL or ST.
			isCSI := s[i] == 0x9b
			for i++; i < len(s); i++ {
				c := s[i]
				if isCSI {
					if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '~' || c == 0x07 {
						break
					}
					continue
				}
				if c == 0x07 || c == 0x9c {
					break
				}
				if c == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
			}
			continue
		}
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		switch s[i] {
		case ']': // OSC
			for i++; i < len(s); i++ {
				if s[i] == 0x07 {
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
			}
		case '[': // CSI
			for i++; i < len(s); i++ {
				c := s[i]
				if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '~' || c == 0x07 {
					break
				}
			}
		case 'P', '^', '_', 'X': // DCS / PM / APC / SOS
			for i++; i < len(s); i++ {
				if s[i] == 0x07 || s[i] == 0x9c {
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
			}
		case '\\': // ST alone
			// consumed (the ESC itself was consumed at loop top)
		default:
			// two-character sequence (ESC 7 DECSC, ESC = DECKPAM, ...):
			// consume exactly this one byte, do not run past it.
		}
	}
	return b.String()
}

// isC1Start reports whether b is a C1 control byte that starts an escape
// sequence (or is the standalone ST terminator 0x9C).
func isC1Start(b byte) bool {
	switch b {
	case 0x90, 0x98, 0x9b, 0x9c, 0x9d, 0x9e, 0x9f:
		return true
	}
	return false
}
