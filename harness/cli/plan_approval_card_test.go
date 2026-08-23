package cli

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"semantix/harness/agent"
	"semantix/harness/control"
	"semantix/harness/event"
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

// planCardCtrl records the resolution calls the approval card makes. Every
// other SessionAPI method panics through the embedded nil interface, which is
// what keeps these tests pinned to the resolution path rather than drifting
// into whatever else a real controller would do.
type planCardCtrl struct {
	control.SessionAPI
	planCalls     []planDecisionCall
	approveCalls  int
	recoveryCalls []recoveryResolveCall
	planMode      bool
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

func (c *planCardCtrl) SetPlanMode(on bool) { c.planMode = on }
func (c *planCardCtrl) PlanMode() bool      { return c.planMode }
func (c *planCardCtrl) Cancel()             {}

func newPlanCardTUI(ctrl *planCardCtrl) chatTUI {
	m := newTestChatTUI()
	m.ctrl = ctrl
	m.planMode = true
	ctrl.planMode = true
	m.pendingApproval = &event.Approval{ID: "plan", Tool: planApprovalTool}
	return m
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
			ctrl := &planCardCtrl{}
			m := newPlanCardTUI(ctrl)

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
