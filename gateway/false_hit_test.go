package gateway

import (
	"testing"
	"time"

	"semantix/kernel/cache"
	"semantix/kernel/usage"
)

// --- normalizedEditRatio (Issue #262 §3.3) ---

func TestNormalizedEditRatio(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"", "", 1},
		{"", "abc", 0},
		{"abc", "", 0},
		{"修复 go 测试失败", "修复 go 测试失败", 1},
		{"修复 go 测试失败", "修复 go 测试失败请重试", 1 - 3.0/13}, // 3 inserted runes / max len 13
		{"abc", "xyz", 0}, // full substitution: 3 edits / 3
		{"kitten", "sitting", 1 - 3.0/7},
	}
	for _, c := range cases {
		got := normalizedEditRatio(c.a, c.b)
		if got < c.want-1e-9 || got > c.want+1e-9 {
			t.Errorf("normalizedEditRatio(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestNormalizedEditRatioBounded(t *testing.T) {
	long := make([]rune, maxEditLen+1)
	for i := range long {
		long[i] = 'a'
	}
	// Over-long inputs never trigger a false-hit retry (bounded work).
	if got := normalizedEditRatio(string(long), string(long)); got != 0 {
		t.Fatalf("over-long identical strings ratio = %v, want 0 (bounded)", got)
	}
}

// --- false-hit retry detection (Issue #262 §3.3) ---

func newObsGateway() *Gateway {
	return &Gateway{
		cfg:      &Config{Cache: CacheConfig{FalseHitSim: DefaultFalseHitSim}},
		l3Reuses: make(map[string]l3ReuseEntry),
		now:      time.Now,
	}
}

func TestFalseHitRetryDetection(t *testing.T) {
	g := newObsGateway()
	q := "修复 go 测试失败"

	// No prior reuse → no false hit.
	if g.detectFalseHit("s1", q) {
		t.Fatal("no prior reuse must not be a false hit")
	}

	// Serve once, then a near-identical retry in the SAME session → hit.
	g.recordL3Reuse("s1", q, "l3-1")
	if !g.detectFalseHit("s1", "修复 go 测试失败") {
		t.Fatal("same-session near-identical retry must be a suspected false hit")
	}
	// The record is consumed: a second retry is not flagged again.
	if g.detectFalseHit("s1", q) {
		t.Fatal("record must be consumed after one detection")
	}

	// Different session → no hit (the record is session-scoped).
	g.recordL3Reuse("s2", q, "l3-1")
	if g.detectFalseHit("s3", q) {
		t.Fatal("different session must not be a false hit")
	}

	// Unrelated query → similarity below threshold → no hit.
	g.recordL3Reuse("s4", q, "l3-1")
	if g.detectFalseHit("s4", "完全不相关的主题") {
		t.Fatal("low-similarity query must not be a false hit")
	}
}

func TestFalseHitDetectionDisabled(t *testing.T) {
	g := newObsGateway()
	g.cfg.Cache.FalseHitSim = -1 // explicit disable
	g.recordL3Reuse("s1", "修复 go 测试失败", "l3-1")
	if g.detectFalseHit("s1", "修复 go 测试失败") {
		t.Fatal("false_hit_sim=-1 must disable retry detection")
	}

	g2 := newObsGateway()
	g2.cfg.Cache.FalseHitSim = 0 // unspecified → default
	if got := g2.falseHitSim(); got != DefaultFalseHitSim {
		t.Fatalf("falseHitSim() = %v, want default %v", got, DefaultFalseHitSim)
	}
}

func TestL3ReuseMapBounded(t *testing.T) {
	g := newObsGateway()
	for i := 0; i < maxL3ReuseEntries+50; i++ {
		g.recordL3Reuse("session-"+itoa(i), "query", "l3-1")
	}
	if len(g.l3Reuses) > maxL3ReuseEntries {
		t.Fatalf("reuse map grew to %d entries, bound is %d", len(g.l3Reuses), maxL3ReuseEntries)
	}
	// The most recent session is still tracked.
	if !g.detectFalseHit("session-"+itoa(maxL3ReuseEntries+49), "query") {
		t.Fatal("most recent reuse must survive eviction")
	}
}

// withL3Obs must map the decision snapshot onto the usage event fields.
func TestWithL3Obs(t *testing.T) {
	g := newObsGateway()
	e := g.withL3Obs(
		usage.Event{},
		cache.Obs{Grey: 2, RulesReject: 1, JudgeReject: 1, JudgeApproved: 1, FingerprintReject: 1, IsolatedReject: 1},
		true,
	)
	if !e.L3FalseHit || e.L3GreyCandidates != 2 || e.L3JudgeReject != 1 ||
		e.L3JudgeApproved != 1 || e.L3RulesReject != 1 ||
		e.L3FingerprintReject != 1 || e.L3IsolatedReject != 1 {
		t.Fatalf("mapped event = %+v", e)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
