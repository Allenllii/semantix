package main

import "semantix/kernel/judge"

// stripESC removes ANSI/OSC escape sequences from untrusted content before
// terminal output. Malicious session files must not be able to rewrite the
// user's terminal (cursor moves, title changes, OSC 52 clipboard writes,
// tmux passthrough).
//
// Single implementation: delegates to kernel/judge.Sanitize (issue #8
// acceptance ④ keeps one authoritative escape scanner for both the judge
// input path and the terminal path — the two must never drift apart).
func stripESC(s string) string {
	return judge.Sanitize(s)
}
