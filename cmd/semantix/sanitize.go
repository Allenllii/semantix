package main

import "semantix/kernel/sanitize"

// stripESC removes ANSI/OSC escape sequences from untrusted content before
// terminal output. Malicious session files must not be able to rewrite the
// user's terminal (cursor moves, title changes, OSC 52 clipboard writes,
// tmux passthrough).
//
// Single implementation: delegates to kernel/sanitize.EscapeSequences
// (issue #8 acceptance ④ keeps one authoritative escape scanner for the
// terminal path and the sanitization pipeline — they must never drift
// apart; Issue #278 moved the scanner from kernel/judge to kernel/sanitize).
func stripESC(s string) string {
	return sanitize.EscapeSequences(s)
}
