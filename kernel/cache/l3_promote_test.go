package cache

import (
	"context"
	"errors"
	"testing"

	"semantix/kernel/judge"
	"semantix/kernel/promote"
	"semantix/kernel/slice"
)

// stubPromote records promotion decision calls (Issue #280 test double).
type stubPromote struct {
	lookup     bool
	promoteErr error
	lookups    int
	writes     []promote.Entry
	rejected   []string // reasons
}

func (s *stubPromote) Lookup(sourceSliceID, query, currentVersion string, now int64) bool {
	s.lookups++
	return s.lookup
}

func (s *stubPromote) Promote(e promote.Entry) error {
	s.writes = append(s.writes, e)
	return s.promoteErr
}

func (s *stubPromote) Rejected(sourceSliceID, query, reason string, now int64) {
	s.rejected = append(s.rejected, reason)
}

var _ Promote = (*stubPromote)(nil)

// variantMock is a judge with a controllable second perspective (the
// consensus gate's rephrased rubric; Issue #280).
type variantMock struct {
	primary,
	secondary bool
	secondaryCalls int
}

func (m *variantMock) Confirm(_ context.Context, _ judge.Candidate) (bool, error) {
	return m.primary, nil
}

func (m *variantMock) ConfirmSecondary(_ context.Context, _ judge.Candidate) (bool, error) {
	m.secondaryCalls++
	return m.secondary, nil
}

var _ judge.VariantJudge = (*variantMock)(nil)

// A promotion hit skips the judge entirely (same query+version, TTL open).
func TestDecideL3PromoteHitSkipsJudge(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	j := &mockJudge{confirm: true}
	p := &stubPromote{lookup: true}
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: j, Promote: p}
	var acc ObsAccum
	d.Obs = &acc
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil || res == nil {
		t.Fatalf("promotion hit must reuse (res=%v err=%v)", res, err)
	}
	if j.got != nil {
		t.Fatal("judge must be skipped on a promotion hit")
	}
	o := acc.Snapshot()
	if o.PromoteHit != 1 {
		t.Fatalf("PromoteHit = %d, want 1", o.PromoteHit)
	}
}

// Judge approval + consensus approval writes the promotion entry.
func TestDecideL3PromoteWrittenAfterConsensus(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	j := &mockJudge{confirm: true}
	p := &stubPromote{}
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: j,
		Consensus: &variantMock{primary: true, secondary: true}, Promote: p}
	var acc ObsAccum
	d.Obs = &acc
	if res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project}); err != nil || res == nil {
		t.Fatalf("approved reuse (res=%v err=%v)", res, err)
	}
	if len(p.writes) != 1 {
		t.Fatalf("promotion writes = %d, want 1", len(p.writes))
	}
	if p.writes[0].Query != "修复 go 测试失败" || p.writes[0].SourceSliceID != "l3-1" {
		t.Fatalf("entry = %+v", p.writes[0])
	}
	o := acc.Snapshot()
	if o.PromoteWritten != 1 {
		t.Fatalf("PromoteWritten = %d, want 1", o.PromoteWritten)
	}
}

// Consensus failure blocks the promotion write but NOT the L3 reuse (the
// primary judge already approved; consensus only gates eligibility), and
// records a consensus_failed lesson.
func TestDecideL3ConsensusRejectBlocksPromote(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	j := &mockJudge{confirm: true}
	p := &stubPromote{}
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: j,
		Consensus: &variantMock{primary: true, secondary: false}, Promote: p}
	var acc ObsAccum
	d.Obs = &acc
	res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project})
	if err != nil || res == nil {
		t.Fatalf("primary-approved reuse must still succeed (res=%v err=%v)", res, err)
	}
	if len(p.writes) != 0 {
		t.Fatalf("consensus-rejected candidate must not be promoted, wrote %d", len(p.writes))
	}
	if len(p.rejected) != 1 || p.rejected[0] != "consensus_failed" {
		t.Fatalf("rejected lessons = %v, want [consensus_failed]", p.rejected)
	}
	o := acc.Snapshot()
	if o.PromoteRejected != 1 {
		t.Fatalf("PromoteRejected = %d, want 1", o.PromoteRejected)
	}
}

// A blacklisted write is counted and the entry is not stored.
func TestDecideL3BlacklistBlocksPromoteWrite(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	j := &mockJudge{confirm: true}
	p := &stubPromote{promoteErr: promote.ErrBlacklisted}
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: j,
		Consensus: &variantMock{primary: true, secondary: true}, Promote: p}
	var acc ObsAccum
	d.Obs = &acc
	if res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project}); err != nil || res == nil {
		t.Fatalf("reuse unaffected by blacklist (res=%v err=%v)", res, err)
	}
	o := acc.Snapshot()
	if o.PromoteBlacklisted != 1 {
		t.Fatalf("PromoteBlacklisted = %d, want 1", o.PromoteBlacklisted)
	}
	if o.PromoteWritten != 0 {
		t.Fatalf("PromoteWritten = %d, want 0", o.PromoteWritten)
	}
}

// A judge decline records a judge_declined lesson (failure memory).
func TestDecideL3JudgeDeclineRecordsRejection(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	j := &mockJudge{confirm: false}
	p := &stubPromote{}
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: j, Promote: p}
	if res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project}); err != nil || res != nil {
		t.Fatalf("declined candidate must not reuse (res=%v err=%v)", res, err)
	}
	if len(p.rejected) != 1 || p.rejected[0] != "judge_declined" {
		t.Fatalf("rejected lessons = %v, want [judge_declined]", p.rejected)
	}
}

// Judge error is unavailability (Issue #245): no lesson recorded, no
// promotion written, reuse fails closed.
func TestDecideL3JudgeErrorNoLesson(t *testing.T) {
	root, idx, _, _ := buildTestLib(t)
	j := &mockJudge{confirm: true, err: errors.New("timeout")}
	p := &stubPromote{}
	d := &L3Decider{Index: idx, Root: root, Zones: greyZones(), Judge: j, Promote: p}
	if res, err := d.DecideL3(context.Background(), Query{UserInput: "修复 go 测试失败", Scope: slice.Project}); err != nil || res != nil {
		t.Fatalf("judge error must reject (res=%v err=%v)", res, err)
	}
	if len(p.rejected) != 0 {
		t.Fatalf("judge error is not a lesson, got %v", p.rejected)
	}
	if len(p.writes) != 0 {
		t.Fatalf("judge error must not promote, wrote %d", len(p.writes))
	}
}
