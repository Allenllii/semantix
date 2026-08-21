package scheddemo

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// concurrencyProbe records the peak number of fixture tools executing at once.
// A run shares one probe across all its tools; ON and OFF arms use fresh probes
// so their peaks are independent. Measuring concurrency inside the tools (rather
// than inferring it from event timestamps) keeps the metric deterministic under
// -race and immune to millisecond rounding.
//
// Peak is a whole-run maximum, not per-round. Every current scenario keeps its
// highest concurrency in the round that matters (multi-round scenarios warm up
// with single-tool rounds), so the run peak equals the measured round's peak. A
// future scenario that parallelizes a non-headline round would break that
// assumption — measure per round then.
type concurrencyProbe struct {
	mu   sync.Mutex
	cur  int
	peak int
}

func (p *concurrencyProbe) enter() {
	p.mu.Lock()
	p.cur++
	if p.cur > p.peak {
		p.peak = p.cur
	}
	p.mu.Unlock()
}

func (p *concurrencyProbe) leave() {
	p.mu.Lock()
	p.cur--
	p.mu.Unlock()
}

// Peak returns the highest observed concurrency.
func (p *concurrencyProbe) Peak() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.peak
}

// toolSpec declares one fixture tool for a scenario. Durations are synthetic
// (a stand-in for real tool latency); readOnly and the failFirst warmup are the
// inputs the real scheduler reasons about.
type toolSpec struct {
	name      string
	readOnly  bool
	dur       time.Duration
	failFirst int // fail this tool's first N executions (seeds the behavior gate)
}

// fixtureTool is a tool.Tool whose only job is to occupy a measurable slice of
// wall-clock and, optionally, fail a warmup so the scheduler's behavior gate has
// history to act on. Instances are built fresh per run so failFirst and the
// concurrency probe never leak across arms.
type fixtureTool struct {
	spec     toolSpec
	probe    *concurrencyProbe
	failsMu  sync.Mutex
	failsLet int
}

func newFixtureTool(spec toolSpec, probe *concurrencyProbe) *fixtureTool {
	return &fixtureTool{spec: spec, probe: probe, failsLet: spec.failFirst}
}

func (t *fixtureTool) Name() string            { return t.spec.name }
func (t *fixtureTool) Description() string     { return "scheddemo fixture tool" }
func (t *fixtureTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *fixtureTool) ReadOnly() bool          { return t.spec.readOnly }

func (t *fixtureTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	t.probe.enter()
	defer t.probe.leave()
	if t.spec.dur > 0 {
		select {
		case <-time.After(t.spec.dur):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	t.failsMu.Lock()
	fail := t.failsLet > 0
	if fail {
		t.failsLet--
	}
	t.failsMu.Unlock()
	if fail {
		return "", errors.New("simulated tool failure (behavior-gate warmup)")
	}
	return "ok", nil
}
