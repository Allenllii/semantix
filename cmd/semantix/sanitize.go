package main

import "strings"

// stripESC removes ANSI/OSC escape sequences from untrusted content before
// terminal output. Malicious session files must not be able to rewrite the
// user's terminal (cursor moves, title changes, OSC 52 clipboard writes).
func stripESC(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s // fast path: no ESC byte
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i < len(s) && s[i] == ']' {
			// OSC sequence: consume until BEL (0x07) or ST (ESC \)
			for i++; i < len(s); i++ {
				if s[i] == 0x07 {
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
			}
			continue
		}
		// CSI or two-character sequence: consume until a final byte
		for ; i < len(s); i++ {
			c := s[i]
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '~' || c == 0x07 {
				break
			}
		}
	}
	return b.String()
}
