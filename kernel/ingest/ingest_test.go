package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/event"
	"semantix/kernel/slice"
)

const sampleSession = `{"role":"user","content":"修复 go 测试失败"}
{"role":"assistant","content":"","tool_calls":[{"id":"c1","name":"grep"},{"id":"c2","name":"readFile"}]}
{"role":"tool","content":"找到 2 个文件"}
{"role":"tool","content":"断言失败 want 5 got 4"}
{"role":"assistant","content":"","tool_calls":[{"id":"c3","name":"editFile"}]}
{"role":"tool","content":"已修复"}
{"role":"assistant","content":"修复完成"}
{"role":"user","content":"加 CI"}
{"role":"assistant","content":"","tool_calls":[{"id":"c4","name":"writeFile"}]}
{"role":"tool","content":"已创建 ci.yml"}
{"role":"assistant","content":"CI 配置完成"}
`

func writeSample(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(sampleSession), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestJSONLSourceProducesEventStream(t *testing.T) {
	dir := t.TempDir()
	writeSample(t, dir, "sess-a.jsonl")

	src, err := NewJSONLSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	se, err := src.Next()
	if err != nil {
		t.Fatal(err)
	}
	if se.SessionID != "sess-a" {
		t.Fatalf("session id = %q, want sess-a", se.SessionID)
	}

	var turns, dispatches, results, rounds int
	for _, e := range se.Events {
		switch e.Kind {
		case event.TurnStarted:
			turns++
		case event.ToolDispatch:
			dispatches++
		case event.ToolResult:
			results++
		case event.ToolRoundEnd:
			rounds++
		}
	}
	if turns != 2 {
		t.Errorf("TurnStarted = %d, want 2", turns)
	}
	if dispatches != 4 {
		t.Errorf("ToolDispatch = %d, want 4 (c1-c4)", dispatches)
	}
	if results != 4 {
		t.Errorf("ToolResult = %d, want 4", results)
	}
	if rounds != 2 {
		t.Errorf("ToolRoundEnd = %d, want 2 (one per turn)", rounds)
	}

	if _, err := src.Next(); !IsEOF(err) {
		t.Fatalf("second Next() = %v, want EOF", err)
	}
}

func TestJSONLSourceToleratesCorruptLines(t *testing.T) {
	dir := t.TempDir()
	p := writeSample(t, dir, "sess-a.jsonl")
	raw, _ := os.ReadFile(p)
	corrupt := string(raw) + "{not json}\n"
	if err := os.WriteFile(p, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
	src, _ := NewJSONLSource(p)
	se, err := src.Next()
	if err != nil {
		t.Fatalf("Next() = %v, want nil (corrupt line skipped)", err)
	}
	if len(se.Events) == 0 {
		t.Fatal("no events produced")
	}
}

func TestJSONLSourcePreservesAndProjectsClosedLoopEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "closed-loop.jsonl")
	lines := []string{
		`{"kind":5,"session_id":"closed-loop","turn":1,"at":"2026-08-18T00:00:00Z","data":{"layer":"L3","slice_ids":["slice-hit"]}}`,
		`{"kind":6,"session_id":"closed-loop","turn":1,"at":"2026-08-18T00:00:00Z","data":{"slice_ids":["slice-inject"],"bytes":128}}`,
		`{"kind":8,"session_id":"closed-loop","turn":1,"at":"2026-08-18T00:00:00Z","data":{"targets":["slice-a"]}}`,
		`{"kind":9,"session_id":"closed-loop","turn":2,"at":"2026-08-18T00:00:01Z","data":{"targets":["slice-b"]}}`,
		`{"kind":11,"session_id":"closed-loop","turn":2,"at":"2026-08-18T00:00:02Z","data":{"params":{"tau_l2":0.55,"prefetch_conf":0.6}}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := NewJSONLSource(path)
	if err != nil {
		t.Fatal(err)
	}
	se, err := src.Next()
	if err != nil {
		t.Fatal(err)
	}
	if len(se.Events) != 5 {
		t.Fatalf("events=%d, want 5", len(se.Events))
	}
	text := string(se.Transcript)
	for _, phrase := range []string{"slice hit", "slice inject", "prefetch hit", "prefetch waste", "evolution tick"} {
		if !strings.Contains(text, phrase) {
			t.Fatalf("transcript missing %q:\n%s", phrase, text)
		}
	}
}

func TestPipelineExtractsAndPersists(t *testing.T) {
	dir := t.TempDir()
	writeSample(t, dir, "sess-a.jsonl")
	writeSample(t, dir, "sess-b.jsonl")

	src, err := NewJSONLSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := slice.NewFileStore(filepath.Join(t.TempDir(), "db.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c, ok := store.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	})
	idx := bm25.New()
	p := Pipeline{
		Extractor: slice.NewExtractor(),
		Store:     store,
		Index:     idx,
		Scope:     slice.Project,
		Project:   "demo",
	}
	stats, err := p.Run(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("sessions processed = %d, want 2 (%v)", len(stats), stats)
	}
	for _, n := range stats {
		if n == 0 {
			t.Fatalf("session produced 0 slices: %v", stats)
		}
	}
	// The ingested corpus must be searchable end-to-end.
	hits, err := idx.Search("修复 go 测试", 3, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || !strings.Contains(string(hits[0].Slice.Content), "修复 go 测试") {
		t.Fatalf("search after ingest: top hit = %q, want the repair-testing slice", firstContent(hits))
	}
}

func firstContent(hits []slice.Hit) string {
	if len(hits) == 0 {
		return ""
	}
	return string(hits[0].Slice.Content)
}
