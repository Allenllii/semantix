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
	}
	for _, c := range cases {
		if got := stripESC(c.in); got != c.want {
			t.Errorf("stripESC(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
