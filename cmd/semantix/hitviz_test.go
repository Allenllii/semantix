package main

import (
	"bytes"
	"testing"
)

// U30: zone 图标映射 —— 🟢 hit / 🟡 grey / ⚪ miss（未知回落 ⚪，故障安全）。
func TestZoneIcon(t *testing.T) {
	for _, tc := range []struct {
		zone string
		want string
	}{
		{"hit", "🟢"},
		{"grey", "🟡"},
		{"miss", "⚪"},
		{"bogus", "⚪"},
		{"", "⚪"},
	} {
		if got := verdictIcon(tc.zone); got != tc.want {
			t.Errorf("verdictIcon(%q) = %q, want %q", tc.zone, got, tc.want)
		}
	}
}

// U30: 命中摘要行 —— hits 数 = zone==hit 行数，sessions = 去重非空来源会话。
func TestWriteHitsSummary(t *testing.T) {
	cases := []struct {
		name string
		rows []hitRow
		want string
	}{
		{"empty list prints nothing", nil, ""},
		{"single hit, one session", []hitRow{{Zone: "hit", SourceSession: "sess-a"}}, "🎯 1/1 hits in 1 sessions\n"},
		{"mixed zones, two sessions", []hitRow{
			{Zone: "hit", SourceSession: "sess-a"},
			{Zone: "grey", SourceSession: "sess-a"},
			{Zone: "miss", SourceSession: "sess-b"},
		}, "🎯 1/3 hits in 2 sessions\n"},
		{"unknown session not counted", []hitRow{
			{Zone: "hit", SourceSession: ""},
			{Zone: "hit", SourceSession: "sess-a"},
		}, "🎯 2/2 hits in 1 sessions\n"},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		writeHitsSummary(&buf, tc.rows)
		if got := buf.String(); got != tc.want {
			t.Errorf("%s: summary = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// U30: 命中行格式 —— 位置 + 图标 + score/zone/id/scope；from:<session> 仅在已知时输出。
func TestFormatHitLine(t *testing.T) {
	got := formatHitLine(2, "🟢", "0.850000", "hit", "s1", "project", "sess-a")
	want := "2. 🟢 score=0.850000 zone=hit id=s1 scope=project from:sess-a"
	if got != want {
		t.Errorf("formatHitLine = %q, want %q", got, want)
	}
	got = formatHitLine(1, "⚪", "0.000000", "miss", "s9", "project", "")
	want = "1. ⚪ score=0.000000 zone=miss id=s9 scope=project"
	if got != want {
		t.Errorf("formatHitLine (no session) = %q, want %q", got, want)
	}
}
