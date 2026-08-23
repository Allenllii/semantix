package tool

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestSemantixLookupSchemaAndName(t *testing.T) {
	tl := semantixLookup{}
	if tl.Name() != "semantix_lookup" {
		t.Errorf("name: %s", tl.Name())
	}
	if !tl.ReadOnly() {
		t.Error("must be read-only")
	}
	var s map[string]any
	if err := json.Unmarshal(tl.Schema(), &s); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if s["type"] != "object" {
		t.Errorf("schema type: %v", s["type"])
	}
}

// TestSemantixLookupBudget pins the production subprocess deadline. The 3s
// default is part of the fail-soft contract — an overrun degrades to an empty
// result — so a test that widens the budget for its own assertions must not be
// able to move it. The zero value means production.
func TestSemantixLookupBudget(t *testing.T) {
	if got := (semantixLookup{}).budget(); got != 3*time.Second {
		t.Errorf("production budget: got %s, want 3s", got)
	}
	if got := (semantixLookup{timeout: 250 * time.Millisecond}).budget(); got != 250*time.Millisecond {
		t.Errorf("injected budget: got %s, want 250ms", got)
	}
	if got := (semantixLookup{timeout: -1}).budget(); got != 3*time.Second {
		t.Errorf("negative budget must fall back to production: got %s", got)
	}
}

func TestSemantixLookupRequiresQuery(t *testing.T) {
	noKernelPATH(t)
	_, err := semantixLookup{}.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want error for missing query")
	}
	// The clamp path runs on to the subprocess; with no kernel reachable it
	// degrades soft, which is the assertion — argument handling must not
	// surface an error of its own.
	_, err = semantixLookup{}.Execute(context.Background(), json.RawMessage(`{"query":"x","limit":999}`))
	if err != nil {
		t.Fatalf("limit clamped, no error expected: %v", err)
	}
}

// TestSemantixLookupFailsSoft: neither an absent kernel binary nor a broken
// one may surface an error — a kernel outage must never break the agent's
// turn. Both branches are constructed rather than assumed: the machine
// running these tests is a semantix checkout and may well have a working
// kernel on PATH.
func TestSemantixLookupFailsSoft(t *testing.T) {
	t.Run("binary absent", func(t *testing.T) {
		noKernelPATH(t)
		out, err := semantixLookup{}.Execute(context.Background(),
			json.RawMessage(`{"query":"anything"}`))
		if err != nil {
			t.Fatalf("soft degrade violated: %v", err)
		}
		if out != "" {
			t.Errorf("want empty output, got %q", out)
		}
	})

	t.Run("binary exits non-zero", func(t *testing.T) {
		fakeKernelPATH(t, fakeKernelFail)
		out, err := semantixLookup{}.Execute(context.Background(),
			json.RawMessage(`{"query":"anything"}`))
		if err != nil {
			t.Fatalf("soft degrade violated: %v", err)
		}
		if out != "" {
			t.Errorf("want empty output, got %q", out)
		}
	})
}

// TestSemantixLookupBudgetIsHonored proves Execute deadlines the subprocess on
// the value budget() resolved, not on a constant of its own. The kernel here
// answers happily, so an empty result can only mean the 1ns budget expired —
// and no fork+exec on any machine meets 1ns, which makes the assertion
// immune to the scheduling latency that made this test file flaky to begin
// with. Under a hardcoded 3s the fake would answer and this would fail.
func TestSemantixLookupBudgetIsHonored(t *testing.T) {
	fakeKernelPATH(t, fakeKernelArgv)
	out, err := (semantixLookup{timeout: time.Nanosecond}).Execute(context.Background(),
		json.RawMessage(`{"query":"anything"}`))
	if err != nil {
		t.Fatalf("soft degrade violated: %v", err)
	}
	if out != "" {
		t.Errorf("budget ignored: kernel answered %q", out)
	}
}

// TestSemantixLookupUnresponsiveKernel covers the other end of the deadline:
// a kernel that accepts the call and never replies must be killed and
// degraded, not waited on. The ceiling is deliberately far above any
// plausible exec latency — it separates "a deadline fired" from "no deadline
// at all" (the fake sleeps for two minutes) without asserting on wall clock.
func TestSemantixLookupUnresponsiveKernel(t *testing.T) {
	fakeKernelPATH(t, fakeKernelHang)
	start := time.Now()
	out, err := (semantixLookup{timeout: 200 * time.Millisecond}).Execute(context.Background(),
		json.RawMessage(`{"query":"anything"}`))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("soft degrade violated: %v", err)
	}
	if out != "" {
		t.Errorf("want empty output, got %q", out)
	}
	if elapsed > 30*time.Second {
		t.Errorf("waited %s: the subprocess deadline never fired", elapsed)
	}
}

// TestSemantixLookupSubprocess verifies the exact argv handed to the kernel
// binary, including that a non-ASCII query survives the process boundary
// unmangled.
func TestSemantixLookupSubprocess(t *testing.T) {
	fakeKernelPATH(t, fakeKernelArgv)
	// Assert argv, not the production time budget. The fixture's first exec of
	// a freshly written binary is a cold start whose latency is unbounded — a
	// saturated `go test ./... -race` run has pushed it past the 3s production
	// budget, silently degrading Execute to an empty result mid-assert. Widen
	// the budget here instead of weakening it in production; budget() keeps
	// the 3s default honest.
	out, err := (semantixLookup{timeout: time.Minute}).Execute(context.Background(),
		json.RawMessage(`{"query":"修复 go 测试","limit":7}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "lookup --query 修复 go 测试 --limit 7 --json\n"
	if out != want {
		t.Errorf("argv mismatch:\n got %q\nwant %q", out, want)
	}
}
