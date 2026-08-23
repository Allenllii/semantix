package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"semantix/harness/agent"
	"semantix/harness/control"
	"semantix/harness/event"
	"semantix/harness/i18n"
)

type planDecisionCall struct {
	id       string
	action   control.PlanDecisionAction
	feedback string
}

type recoveryResolveCall struct {
	id       string
	action   agent.RecoveryAction
	feedback string
}

// planCardCtrl records which resolution API the approval card reaches for.
// A real controller is embedded rather than a nil interface: these tests drive
// the whole Update path, and rendering the status line alone reads a dozen
// controller fields. Only the three resolve methods are shadowed — that is the
// distinction under test, and a boolean Approve cannot express it.
type planCardCtrl struct {
	control.SessionAPI
	planCalls     []planDecisionCall
	approveCalls  int
	recoveryCalls []recoveryResolveCall
}

func newPlanCardCtrl(t *testing.T) *planCardCtrl {
	t.Helper()
	real := control.New(control.Options{})
	t.Cleanup(real.Close)
	return &planCardCtrl{SessionAPI: real}
}

func (c *planCardCtrl) ResolvePlanDecision(id string, action control.PlanDecisionAction, feedback string) error {
	c.planCalls = append(c.planCalls, planDecisionCall{id: id, action: action, feedback: feedback})
	return nil
}

func (c *planCardCtrl) Approve(string, bool, bool, bool) { c.approveCalls++ }

func (c *planCardCtrl) ResolveRecovery(id string, action agent.RecoveryAction, feedback string) error {
	c.recoveryCalls = append(c.recoveryCalls, recoveryResolveCall{id: id, action: action, feedback: feedback})
	return nil
}

func newPlanCardTUI(t *testing.T, ctrl *planCardCtrl) chatTUI {
	t.Helper()
	m := newTestChatTUI()
	m.ctrl = ctrl
	m.planMode = true
	m.ctrl.SetPlanMode(true)
	m.pendingApproval = &event.Approval{ID: "plan", Tool: planApprovalTool}
	return m
}

// TestReviseRowsPromptForText pins which rows open the note field. Only the
// revise row does: it is the one decision whose meaning is incomplete without
// the user saying what to change. Start, exit and the plain deny rows resolve
// on the keystroke as they always have.
func TestReviseRowsPromptForText(t *testing.T) {
	tests := []struct {
		name     string
		approval *event.Approval
		want     []bool
	}{
		{
			name:     "plan card",
			approval: &event.Approval{Tool: planApprovalTool},
			want:     []bool{false, true, false},
		},
		{
			name:     "recovery card",
			approval: &event.Approval{Kind: "recovery", Recovery: &event.RecoveryApproval{}},
			want:     []bool{false, true},
		},
		{
			name: "recovery card with task grant",
			approval: &event.Approval{
				Kind: "recovery", Recovery: &event.RecoveryApproval{CanGrantTask: true},
			},
			want: []bool{false, false, true},
		},
		{
			name:     "ordinary tool card prompts for nothing",
			approval: &event.Approval{Tool: "bash", Subject: "echo hi"},
			want:     []bool{false, false, false, false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := approvalChoices(tt.approval)
			if len(got) != len(tt.want) {
				t.Fatalf("choices = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].promptsForText != tt.want[i] {
					t.Errorf("row %d promptsForText = %v, want %v", i, got[i].promptsForText, tt.want[i])
				}
			}
		})
	}
}

func pressKeys(t *testing.T, m chatTUI, keys ...tea.KeyPressMsg) chatTUI {
	t.Helper()
	for _, k := range keys {
		next, _ := m.Update(k)
		m = next.(chatTUI)
	}
	return m
}

func typeRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// TestPlanReviseRowOpensNoteFieldInsteadOfResolving: picking "revise" must hold
// the card open and hand input to the composer. Resolving on the keystroke is
// what left the revision note arriving as a fresh, unattached user turn.
func TestPlanReviseRowOpensNoteFieldInsteadOfResolving(t *testing.T) {
	ctrl := newPlanCardCtrl(t)
	m := pressKeys(t, newPlanCardTUI(t, ctrl), typeRune('2'))

	if len(ctrl.planCalls) != 0 || ctrl.approveCalls != 0 {
		t.Fatalf("revise resolved immediately: plan=%v approve=%d", ctrl.planCalls, ctrl.approveCalls)
	}
	if m.pendingApproval == nil {
		t.Error("approval was cleared; the card must stay up while the note is typed")
	}
	if !m.approvalTyping {
		t.Error("approvalTyping = false, want true")
	}
	if m.hideComposer() {
		t.Error("hideComposer() = true; the composer owns input while the note is typed")
	}
	if !m.planMode {
		t.Error("plan mode must stay on while revising")
	}
}

// TestApprovalNoteTypingSendsRowKeysToComposer guards the whole resolving
// keymap, not just the letters: 1 and y and a and n each answer a row when the
// card has focus, so every one of them must become text instead.
func TestApprovalNoteTypingSendsRowKeysToComposer(t *testing.T) {
	ctrl := newPlanCardCtrl(t)
	m := pressKeys(t, newPlanCardTUI(t, ctrl), typeRune('2'))
	m = pressKeys(t, m, typeRune('1'), typeRune('y'), typeRune('a'), typeRune('n'))

	if len(ctrl.planCalls) != 0 || ctrl.approveCalls != 0 {
		t.Fatalf("a row key resolved while typing: plan=%v approve=%d", ctrl.planCalls, ctrl.approveCalls)
	}
	if got := m.input.Value(); got != "1yan" {
		t.Errorf("composer value = %q, want %q", got, "1yan")
	}
	if !m.approvalTyping {
		t.Error("typing mode ended unexpectedly")
	}
}

// TestApprovalNoteEnterSubmitsTrimmedText: the note rides the decision itself.
func TestApprovalNoteEnterSubmitsTrimmedText(t *testing.T) {
	ctrl := newPlanCardCtrl(t)
	m := pressKeys(t, newPlanCardTUI(t, ctrl), typeRune('2'))
	m.input.SetValue("  widen the retry window  ")
	m = pressKeys(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(ctrl.planCalls) != 1 {
		t.Fatalf("ResolvePlanDecision calls = %d, want 1", len(ctrl.planCalls))
	}
	got := ctrl.planCalls[0]
	if got.action != control.PlanDecisionRevisePlan || got.feedback != "widen the retry window" {
		t.Errorf("resolved %+v, want revise_plan with the trimmed note", got)
	}
	if m.approvalTyping || m.pendingApproval != nil {
		t.Error("card still open after the note was submitted")
	}
	if !m.planMode {
		t.Error("revising must keep plan mode on")
	}
}

// TestApprovalNoteEmptySubmitKeepsPlanning matches what n and Esc already do:
// no note is a valid revise, not a different decision.
func TestApprovalNoteEmptySubmitKeepsPlanning(t *testing.T) {
	ctrl := newPlanCardCtrl(t)
	m := pressKeys(t, newPlanCardTUI(t, ctrl), typeRune('2'), tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(ctrl.planCalls) != 1 {
		t.Fatalf("ResolvePlanDecision calls = %d, want 1", len(ctrl.planCalls))
	}
	if got := ctrl.planCalls[0]; got.action != control.PlanDecisionRevisePlan || got.feedback != "" {
		t.Errorf("resolved %+v, want revise_plan with an empty note", got)
	}
	if !m.planMode {
		t.Error("plan mode must stay on")
	}
}

// TestApprovalNoteEscReturnsToRows: backing out of the field is not a decision.
func TestApprovalNoteEscReturnsToRows(t *testing.T) {
	ctrl := newPlanCardCtrl(t)
	m := pressKeys(t, newPlanCardTUI(t, ctrl), typeRune('2'))
	m.input.SetValue("half-written note")
	m = pressKeys(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if len(ctrl.planCalls) != 0 || ctrl.approveCalls != 0 {
		t.Fatalf("Esc resolved the approval: plan=%v approve=%d", ctrl.planCalls, ctrl.approveCalls)
	}
	if m.approvalTyping {
		t.Error("approvalTyping = true, want false after Esc")
	}
	if m.pendingApproval == nil {
		t.Error("Esc must return to the rows, not dismiss the card")
	}
	if !m.hideComposer() {
		t.Error("hideComposer() = false; the rows own input again after Esc")
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("abandoned note left in the composer: %q", got)
	}
}

// TestRecoveryReviseRowForwardsNoteToResolveRecovery: the Auto Guard card's
// transport has taken feedback since it shipped; the TUI was passing "".
func TestRecoveryReviseRowForwardsNoteToResolveRecovery(t *testing.T) {
	ctrl := newPlanCardCtrl(t)
	m := newTestChatTUI()
	m.ctrl = ctrl
	m.pendingApproval = &event.Approval{
		ID: "rec", Kind: "recovery", Recovery: &event.RecoveryApproval{},
	}
	m = pressKeys(t, m, typeRune('2'))
	if !m.approvalTyping {
		t.Fatal("recovery revise did not open the note field")
	}
	m.input.SetValue("  keep the original file  ")
	m = pressKeys(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(ctrl.recoveryCalls) != 1 {
		t.Fatalf("ResolveRecovery calls = %d, want 1", len(ctrl.recoveryCalls))
	}
	got := ctrl.recoveryCalls[0]
	if got.action != agent.RecoveryActionRevise || got.feedback != "keep the original file" {
		t.Errorf("resolved %+v, want revise with the trimmed note", got)
	}
}

// TestApprovalNoteSurfacesItsHintWhileTyping: the row shortcuts stop working the
// moment the composer takes over, so the chrome must stop advertising them.
func TestApprovalNoteSurfacesItsHintWhileTyping(t *testing.T) {
	ctrl := newPlanCardCtrl(t)
	m := newPlanCardTUI(t, ctrl)
	m.width = 120

	rows := ansi.Strip(m.renderApprovalBanner())
	if !strings.Contains(rows, "y/a/p/n") {
		t.Fatalf("row-selection banner lost its shortcut hint:\n%s", rows)
	}

	m = pressKeys(t, m, typeRune('2'))
	typing := ansi.Strip(m.renderApprovalBanner())
	if !strings.Contains(typing, i18n.M.ApprovalNoteHint) {
		t.Errorf("typing banner missing %q:\n%s", i18n.M.ApprovalNoteHint, typing)
	}
	if strings.Contains(typing, "y/a/p/n") {
		t.Errorf("typing banner still advertises inert row shortcuts:\n%s", typing)
	}
	if got := m.input.Placeholder; got != i18n.M.ApprovalNoteHint {
		t.Errorf("composer placeholder = %q, want %q", got, i18n.M.ApprovalNoteHint)
	}

	footer := ansi.Strip(m.primaryStatusLine(" Plan ", false, false))
	if !strings.Contains(footer, i18n.M.ChatStatusApprovalNote) {
		t.Errorf("footer missing %q:\n%s", i18n.M.ChatStatusApprovalNote, footer)
	}
}

// TestPlanApprovalRowsResolveThroughPlanDecisionAPI pins the receipt fix: every
// plan row must name its own outcome through ResolvePlanDecision. The generic
// boolean Approve cannot express "exit without executing" — it classifies any
// denial on the plan tool as revise_plan — so reaching for it here is what
// wrote the wrong decision into every durable receipt.
func TestPlanApprovalRowsResolveThroughPlanDecisionAPI(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyPressMsg
		want control.PlanDecisionAction
	}{
		{name: "start execution", key: tea.KeyPressMsg{Code: '1'}, want: control.PlanDecisionStartExecution},
		{name: "exit without executing", key: tea.KeyPressMsg{Code: '3'}, want: control.PlanDecisionExitPlan},
		{name: "legacy n keeps planning", key: tea.KeyPressMsg{Code: 'n'}, want: control.PlanDecisionRevisePlan},
		{name: "escape keeps planning", key: tea.KeyPressMsg{Code: tea.KeyEscape}, want: control.PlanDecisionRevisePlan},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := newPlanCardCtrl(t)
			m := newPlanCardTUI(t, ctrl)

			next, _ := m.handleApprovalKey(tt.key)
			m = next.(chatTUI)

			if ctrl.approveCalls != 0 {
				t.Errorf("Approve called %d times; the plan card must resolve through ResolvePlanDecision",
					ctrl.approveCalls)
			}
			if len(ctrl.planCalls) != 1 {
				t.Fatalf("ResolvePlanDecision calls = %d, want 1", len(ctrl.planCalls))
			}
			got := ctrl.planCalls[0]
			if got.id != "plan" || got.action != tt.want {
				t.Errorf("resolved %+v, want id %q action %q", got, "plan", tt.want)
			}
			if m.pendingApproval != nil {
				t.Error("plan approval was not cleared")
			}
		})
	}
}
