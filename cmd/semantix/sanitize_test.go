package main

import "testing"

func TestStripESC(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain text", "plain text"},
		{"\x1b[31mred\x1b[0m", "red"},                 // ANSI color
		{"a\x1b[2Jb", "ab"},                           // clear screen
		{"\x1b]0;newtitle\x07rest", "rest"},           // OSC title + BEL
		{"\x1b[1;2Hc", "c"},                           // cursor move
		{"multi\x1b[1m\x1b[2mline", "multiline"},      // adjacent sequences
		{"\x1b", ""},                                  // bare ESC at end
		{"ESC\x1b[31m-ok", "ESC-ok"},                  // embedded in text
		{"\x9b31mred\x9b0m", "red"},                   // C1 CSI (single-byte)
		{"\x9d0;title\x07rest", "rest"},               // C1 OSC + BEL
		{"plain\x9bl", "plain"},                       // C1 CSI final byte 'l' consumed
		{"\x90tmux;stuff\x9crest", "rest"},            // C1 DCS passthrough + ST
		{"\x9e0;pm\x9crest", "rest"},                  // C1 PM + ST
		{"\x9f0;apc\x9crest", "rest"},                 // C1 APC + ST
		{"\x98sos\x9crest", "rest"},                   // C1 SOS + ST
		{"\x1b7keep", "keep"},                         // ESC 7 DECSC: two-char, keep rest
		{"\x1bPtmux;\x1b\\rest", "rest"},              // ESC-form DCS + ST
		{"\x1b=keep2", "keep2"},                       // ESC = DECKPAM two-char
		{"亐修复", "亐修复"},                             // U+4E90 (UTF-8 E4 BA 90): C1-valued continuation byte must survive
		{"修复\x90测试", "修复测试"},                     // C1 0x90 as own rune stripped, CJK around it intact
		{"\u0090tmux;\u009crest", "rest"},             // C1 DCS via rune form
		{"\x1b耀keep", "耀keep"},                       // ESC before multi-byte lead: strip ESC only, character survives
		{"\x1b7keep", "keep"},                         // ASCII two-char sequence still consumed
	}
	for _, c := range cases {
		if got := stripESC(c.in); got != c.want {
			t.Errorf("stripESC(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
