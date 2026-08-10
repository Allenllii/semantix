package main

import "strings"

// stripESC removes ANSI/OSC escape sequences from untrusted content before
// terminal output. Malicious session files must not be able to rewrite the
// user's terminal (cursor moves, title changes, OSC 52 clipboard writes).
func stripESC(s string) string {
	// fast path: no escape bytes (ESC / C1 CSI / C1 OSC). IndexByte is
	// byte-level — ContainsRune would treat lone 0x9B/0x9D as invalid UTF-8
	// (RuneError) and miss them.
	if strings.IndexByte(s, 0x1b) < 0 && strings.IndexByte(s, 0x9b) < 0 && strings.IndexByte(s, 0x9d) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == 0x9b || s[i] == 0x9d {
			// C1 CSI (0x9B) terminates on a final byte (letter/~);
			// C1 OSC (0x9D) terminates on BEL or ST — mirror the ESC forms.
			isOSC := s[i] == 0x9d
			for i++; i < len(s); i++ {
				c := s[i]
				if isOSC {
					if c == 0x07 {
						break
					}
					if c == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
						i++
						break
					}
					continue
				}
				if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '~' || c == 0x07 {
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
