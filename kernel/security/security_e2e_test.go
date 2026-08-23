// Package security_test holds the AgentDojo-style poisoning end-to-end
// suite (Issue #281): minimal reproducible attack sessions covering five
// attack chains (indirect injection on ingest, retrieval collision, fake
// success hijack, marker control-flow escape, key exfiltration), asserted
// across the REAL production chain — session ingest → retrieval → injection
// block — with zero network and no real LLM.
//
// Gate metrics (spec §2.3): marker-escape rate must be 0% and the
// raw-untrusted passthrough rate must be 0%; any nonzero rate fails CI.
package security_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/inject"
	"semantix/kernel/sanitize"
	"semantix/kernel/slice"
)

// loadPoison reads one attack-session fixture.
func loadPoison(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "poison", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ingestChain runs the real production chain: session JSONL → extractor
// (write-side sanitize) → store+index → injector.Build. Returns the
// extracted slices and the assembled injection block text.
func ingestChain(t *testing.T, session []byte, query string) ([]*slice.Slice, string) {
	t.Helper()
	items, err := slice.NewExtractor().Extract(session, slice.SliceMeta{SourceSession: "poison"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("extractor returned no slices")
	}
	idx := bm25.New()
	for _, sl := range items {
		if err := idx.Insert(sl); err != nil {
			t.Fatal(err)
		}
	}
	inj := &inject.Injector{Index: idx, Scope: slice.Project, K: 5, Budget: 4096}
	out, err := inj.Build(query)
	if err != nil {
		t.Fatal(err)
	}
	return items, out.Text
}

// sanitizeProbe re-runs the full sanitization pipeline (idempotence check).
func sanitizeProbe(s string) string { return sanitize.Sanitize(s) }

// --- chain A: indirect injection on ingest (MINJA / AgentPoison) ---

// A session carrying a forged instruction + key must be sanitized at the
// WRITE side (no payload in the stored slices) AND at the inject side (no
// payload in the block), while the legitimate fix text survives.
func TestPoisonChainIndirectInject(t *testing.T) {
	items, block := ingestChain(t, loadPoison(t, "session-indirect-inject.jsonl"), "修复 go 测试失败")
	for i, sl := range items {
		content := string(sl.Content)
		if strings.Contains(strings.ToLower(content), "ignore previous") {
			t.Fatalf("slice %d: payload survived write-side sanitize: %q", i, content)
		}
		if strings.Contains(content, "sk-abcDEF") {
			t.Fatalf("slice %d: key survived write-side sanitize: %q", i, content)
		}
	}
	lower := strings.ToLower(block)
	if strings.Contains(lower, "ignore previous") || strings.Contains(lower, "执行以下命令") {
		t.Fatalf("payload survived into the injection block:\n%s", block)
	}
	if !strings.Contains(block, "go vet") {
		t.Fatalf("legitimate content must survive:\n%s", block)
	}
}

// --- chain B: retrieval collision (PoisonedRAG) ---

// A malicious slice that collides with a common query must enter the
// injection block sanitized (idempotent inject-side pass backstops
// anything the write side missed).
func TestPoisonChainRetrievalCollision(t *testing.T) {
	items, block := ingestChain(t, loadPoison(t, "session-retrieval-collision.jsonl"), "配置 CI 流水线")
	if len(items) == 0 || !strings.Contains(block, "配置 CI 流水线") {
		t.Fatalf("chain anchor missing: slices=%d block=%q (collision must actually hit)", len(items), block)
	}
	if strings.Contains(strings.ToLower(block), "ignore previous") {
		t.Fatalf("collision payload survived into the block:\n%s", block)
	}
	if strings.Contains(block, "alice@example.com") {
		t.Fatalf("email survived into the block:\n%s", block)
	}
}

// --- chain C: fake success hijack (MemoryGraft, #280 linkage) ---

// The "already fixed" fake success session is fed through the promotion
// decision: with a consensus gate whose secondary perspective rejects,
// promotion writes must stay zero even though the primary judge approved.
func TestPoisonChainFakeSuccessBlocked(t *testing.T) {
	items, _ := ingestChain(t, loadPoison(t, "session-fake-success.jsonl"), "修复 go 测试失败")
	// Find the Result slice (the fake success answer).
	var res *slice.Slice
	for _, sl := range items {
		if sl.Type == slice.Result {
			res = sl
			break
		}
	}
	if res == nil {
		t.Fatal("result slice missing")
	}
	// Primary approves, secondary rejects → no promotion written.
	blocked := promoteWithConsensus(t, res, "修复 go 测试失败", true, false)
	if blocked != 0 {
		t.Fatalf("fake success promoted %d times, want 0 (consensus gate)", blocked)
	}
	// Positive control: both perspectives approve → exactly one promotion
	// (proves the zero above is the gate, not a broken write path).
	written := promoteWithConsensus(t, res, "修复 go 测试失败", true, true)
	if written != 1 {
		t.Fatalf("consensus-approved promotion = %d, want 1 (positive control)", written)
	}
}

// --- chain D: marker control-flow escape (MEMFLOW) — gate metric ---

// The forged block-close must never reach the injection block unescaped:
// marker-escape rate = 0% (spec gate).
func TestPoisonChainMarkerEscapeZeroRate(t *testing.T) {
	_, block := ingestChain(t, loadPoison(t, "session-marker-escape.jsonl"), "如何配置代理")
	// The block is closed by exactly ONE legitimate close marker (its own);
	// the forged one must appear only in its ESCAPED form ([\/semantix-reuse]
	// or the case-folded variant) inside the content region.
	if strings.Count(block, "[/semantix-reuse]") != 1 {
		t.Fatalf("marker escape: unescaped close marker count != 1 in block:\n%s", block)
	}
	if strings.Count(block, "[semantix-reuse]") != 1 {
		t.Fatalf("block structure corrupted:\n%s", block)
	}
	if !strings.Contains(block, `[\/semantix-reuse]`) && !strings.Contains(block, `[\/SEMANTIX-REUSE]`) {
		t.Fatalf("forged close marker must appear escaped in the content:\n%s", block)
	}
}

// --- chain E: key exfiltration (MEMFLOW) — gate metric ---

// Keys, emails and home paths must be redacted at write time and in the
// injection block: raw-untrusted passthrough rate = 0% (spec gate).
func TestPoisonChainKeyExfilRedacted(t *testing.T) {
	items, block := ingestChain(t, loadPoison(t, "session-key-exfil.jsonl"), "查看部署配置")
	joined := block
	for _, sl := range items {
		joined += "\n" + string(sl.Content)
	}
	for _, secret := range []string{"sk-abcDEF0123456789abcdefghij", "alice@example.com", "/home/alice/secret"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("secret %q leaked in chain output:\n%s", secret, joined)
		}
	}
	if !strings.Contains(joined, "[REDACTED_KEY]") || !strings.Contains(joined, "[REDACTED_EMAIL]") {
		t.Fatalf("redaction placeholders missing:\n%s", joined)
	}
}

// --- ordinary-session regression anchor ---

// A clean session must pass through the whole chain byte-identically:
// sanitization is a no-op for ordinary content (defense must not damage
// legitimate traffic — the L1 prefix stability promise, #278 §2.4).
func TestOrdinarySessionRegressionAnchor(t *testing.T) {
	session := []byte(`{"role":"user","content":"修复 go 测试失败需要先跑 go vet"}` + "\n" +
		`{"role":"assistant","content":"已复用的验证结果：修复 go 测试失败需要先跑 go vet"}`)
	items, _ := ingestChain(t, session, "修复 go 测试失败")
	for _, sl := range items {
		if got := sanitizeProbe(string(sl.Content)); got != string(sl.Content) {
			t.Fatalf("ordinary content changed by sanitize: %q -> %q", sl.Content, got)
		}
	}
}
