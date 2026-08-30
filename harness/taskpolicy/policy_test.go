package taskpolicy

import (
	"strings"
	"testing"

	"semantix/harness/agentpreset"
	"semantix/harness/taskintent"
)

func TestDeriveLightSimpleIsDirect(t *testing.T) {
	p := Derive(Input{
		Raw:    "what is a mutex?",
		Preset: agentpreset.Light,
	})
	if p.Intent != taskintent.Conversation && p.Intent != taskintent.Advisory {
		t.Fatalf("intent = %v", p.Intent)
	}
	if p.Route != RouteDirect {
		t.Fatalf("route = %v, want direct", p.Route)
	}
	if p.Review != ReviewNone {
		t.Fatalf("review = %v, want none", p.Review)
	}
}

func TestDeriveLightHighRiskElevates(t *testing.T) {
	p := Derive(Input{
		Raw:    "fix the authentication bypass in production login",
		Preset: agentpreset.Light,
	})
	if p.Risk < RiskHigh && !p.SecurityClass {
		t.Fatalf("expected high risk or security class, got risk=%v security=%v", p.Risk, p.SecurityClass)
	}
	if p.Review != ReviewForcedSecurity {
		t.Fatalf("review = %v, want forced-security", p.Review)
	}
	if p.Verification != VerifyFull {
		t.Fatalf("verification = %v, want full", p.Verification)
	}
	if p.Route != RouteFullPlan {
		t.Fatalf("route = %v, want full plan", p.Route)
	}
}

func TestDeriveDeliveryLowRiskNoReviewer(t *testing.T) {
	p := Derive(Input{
		Raw:      "fix the typo in README.md",
		Preset:   agentpreset.Delivery,
		Anchored: true,
	})
	if p.Risk != RiskLow {
		// typo fix is low risk
		t.Logf("risk = %v (may be medium if multi-file heuristics fire)", p.Risk)
	}
	if p.Risk == RiskLow && p.RequiresIndependentReview() {
		t.Fatal("delivery low-risk must not force independent reviewer")
	}
	if !p.RequireAtomicContract {
		t.Fatal("delivery mutation should require atomic contract")
	}
}

func TestDeriveDeliveryMediumForcesReview(t *testing.T) {
	p := Derive(Input{
		Raw:          "refactor the payment module and update its callers",
		Preset:       agentpreset.Delivery,
		MultiFile:    true,
		CrossSurface: true,
	})
	if p.Risk < RiskMedium {
		t.Fatalf("risk = %v, want at least medium", p.Risk)
	}
	if !p.RequiresIndependentReview() {
		t.Fatal("delivery medium/high must force independent reviewer")
	}
}

func TestConstraintsNoMutation(t *testing.T) {
	p := Derive(Input{
		Raw:    "只分析这段代码的问题，不要修改",
		Preset: agentpreset.Balanced,
	})
	if p.AllowsMutation() {
		t.Fatal("must forbid mutation")
	}
}

func TestConstraintsNoTests(t *testing.T) {
	p := Derive(Input{
		Raw:    "fix the bug but don't run tests",
		Preset: agentpreset.Balanced,
	})
	if p.AllowsTests() {
		t.Fatal("must forbid tests")
	}
}

func TestConstraintsOnlyRun(t *testing.T) {
	p := Derive(Input{
		Raw:    "fix the parser, only run go test ./internal/parser",
		Preset: agentpreset.Balanced,
	})
	if !p.AllowsCommand("go test ./internal/parser") {
		t.Fatal("allowed check should pass")
	}
	if p.AllowsCommand("npm test") {
		t.Fatal("other checks should be blocked")
	}
	if p.AllowsCommand("go test ./internal/parser && npm test") {
		t.Fatal("a second shell command must not inherit the go test allowance")
	}
}

func TestConstraintsNoPush(t *testing.T) {
	p := Derive(Input{
		Raw:    "fix the bug, don't push",
		Preset: agentpreset.Balanced,
	})
	if !p.AllowsMutation() {
		t.Fatal("local mutation should still be allowed")
	}
	if p.AllowsExternal() {
		t.Fatal("push must be forbidden")
	}
}

func TestQuotedConstraintsIgnored(t *testing.T) {
	raw := "please fix the bug\n```\ndon't modify anything\n```\n"
	p := Derive(Input{
		Raw:         raw,
		Instruction: StripQuotedConstraints(raw),
		Preset:      agentpreset.Balanced,
	})
	if !p.AllowsMutation() {
		t.Fatal("quoted no-modify must not bind the host")
	}
}

func TestQualifiedNoModifyDoesNotFreezeWorkspace(t *testing.T) {
	// SWE-bench style framing: a scoped prohibition names its object; the fix
	// itself requires edits, so a workspace-wide write freeze is wrong.
	for _, raw := range []string{
		"Resolve the issue below.\n- Do NOT modify existing test files.\n- Do not commit.",
		"fix the bug, but don't change the public API",
		"修复这个问题，但不要修改测试文件",
	} {
		p := Derive(Input{
			Raw:         raw,
			Instruction: StripQuotedConstraints(raw),
			Preset:      agentpreset.Balanced,
		})
		if !p.AllowsMutation() {
			t.Fatalf("qualified prohibition froze workspace: %q", raw)
		}
	}
}

func TestGlobalNoModifyStillBinds(t *testing.T) {
	for _, raw := range []string{
		"review this, do not modify anything",
		"don't change the code, just explain it",
		"分析问题，不要改代码",
		"look at the failure. do not modify.",
	} {
		p := Derive(Input{
			Raw:         raw,
			Instruction: StripQuotedConstraints(raw),
			Preset:      agentpreset.Balanced,
		})
		if p.AllowsMutation() {
			t.Fatalf("global prohibition must bind: %q", raw)
		}
	}
}

func TestMutationTriggerSurfacedForDiagnostics(t *testing.T) {
	// Issue #400 expectation 3: the trigger phrase must be visible in the
	// execution-policy block so a wrongly-frozen turn can be traced back to
	// the instruction that caused it.
	p := Derive(Input{Raw: "review this, do not modify anything", Preset: agentpreset.Balanced})
	if p.Constraints.MutationTrigger != "do not modify" {
		t.Fatalf("MutationTrigger = %q, want \"do not modify\"", p.Constraints.MutationTrigger)
	}
	block := ExecutionPolicyBlock(p)
	if !strings.Contains(block, `constraint=no-mutation (trigger: do not modify)`) {
		t.Fatalf("block missing trigger: %s", block)
	}
	// A scoped prohibition leaves both the workspace and the trigger empty.
	q := Derive(Input{
		Raw:         "- Do NOT modify existing test files.",
		Instruction: StripQuotedConstraints("- Do NOT modify existing test files."),
		Preset:      agentpreset.Balanced,
	})
	if !q.AllowsMutation() || q.Constraints.MutationTrigger != "" {
		t.Fatalf("scoped prohibition must not freeze or set a trigger: %+v", q.Constraints)
	}
}

func TestUnbalancedInlineCodeDoesNotSwallowLaterLines(t *testing.T) {
	// A stray backtick on one line used to flip inline-code state for the rest
	// of the input, silently dropping every later constraint line.
	raw := "the failing key is `WIDGET_SETTING\nnow don't modify anything, analysis only"
	stripped := StripQuotedConstraints(raw)
	if !strings.Contains(strings.ToLower(stripped), "analysis only") {
		t.Fatalf("later line was swallowed: %q", stripped)
	}
	p := Derive(Input{Raw: raw, Instruction: stripped, Preset: agentpreset.Balanced})
	if p.AllowsMutation() {
		t.Fatal("constraint after unbalanced backtick must still bind")
	}
}

func TestUnbalancedQuoteDoesNotSwallowLaterLines(t *testing.T) {
	raw := "error says \"unterminated\nread only please: analysis only"
	if s := StripQuotedConstraints(raw); !strings.Contains(s, "analysis only") {
		t.Fatalf("later line was swallowed: %q", s)
	}
}

func TestTagWrappedCitationDoesNotBind(t *testing.T) {
	// Constraint-looking prose inside an embedded block (issue body, log) is
	// citation, not an instruction to the host.
	raw := "Resolve the issue below.\n<issue>\nI made no changes to ax1, analysis only shows the bug.\n</issue>\nImplement a fix."
	p := Derive(Input{
		Raw:         raw,
		Instruction: StripQuotedConstraints(raw),
		Preset:      agentpreset.Balanced,
	})
	if !p.AllowsMutation() {
		t.Fatal("cited text inside <issue> must not freeze the workspace")
	}
	// The same phrase outside any block still binds.
	raw2 := "Look at this, analysis only.\n<issue>\nsome bug\n</issue>"
	p2 := Derive(Input{Raw: raw2, Instruction: StripQuotedConstraints(raw2), Preset: agentpreset.Balanced})
	if p2.AllowsMutation() {
		t.Fatal("host constraint outside the block must bind")
	}
	// An unmatched angle-bracket placeholder is plain text, not a citation.
	raw3 := "rename <old> to <new> everywhere, do not modify anything else manually; use the script"
	if s := StripQuotedConstraints(raw3); !strings.Contains(s, "do not modify") {
		t.Fatalf("placeholder text was stripped: %q", s)
	}
}

func TestRiskOnlyRatchetsUp(t *testing.T) {
	p := Derive(Input{Raw: "explain this", Preset: agentpreset.Balanced})
	if p.Risk != RiskLow {
		t.Fatalf("start risk = %v", p.Risk)
	}
	p.RaiseRisk(RiskMedium)
	if p.Risk != RiskMedium {
		t.Fatalf("raised = %v", p.Risk)
	}
	p.RaiseRisk(RiskLow)
	if p.Risk != RiskMedium {
		t.Fatal("risk must not decrease")
	}
}

func TestPlanModeForbidsMutation(t *testing.T) {
	p := Derive(Input{
		Raw:      "implement the feature",
		Preset:   agentpreset.Delivery,
		PlanMode: true,
	})
	if p.AllowsMutation() {
		t.Fatal("plan mode must forbid mutation")
	}
}

func TestExecutionPolicyBlockStable(t *testing.T) {
	p := Derive(Input{Raw: "fix it", Preset: agentpreset.Balanced})
	block := ExecutionPolicyBlock(p)
	if !strings.Contains(block, `preset="balanced"`) {
		t.Fatalf("block missing preset: %s", block)
	}
	if !strings.Contains(block, `version="1"`) {
		t.Fatalf("block missing version: %s", block)
	}
	if !strings.HasPrefix(block, "<execution-policy") || !strings.HasSuffix(block, "</execution-policy>") {
		t.Fatalf("bad block shape: %s", block)
	}
}

func TestMatrixPlanningRoutes(t *testing.T) {
	// Balanced multi-file → light plan
	p := Derive(Input{
		Raw:       "update the API and its tests",
		Preset:    agentpreset.Balanced,
		MultiFile: true,
	})
	if p.Route != RouteLightPlan && p.Route != RouteFullPlan {
		t.Fatalf("balanced multi-file route = %v", p.Route)
	}
	// Delivery multi-file medium → full plan
	d := Derive(Input{
		Raw:        "update the API and its tests across packages",
		Preset:     agentpreset.Delivery,
		MultiFile:  true,
		Structured: true,
	})
	if d.Route != RouteFullPlan {
		t.Fatalf("delivery structured multi-file route = %v, want full", d.Route)
	}
}
