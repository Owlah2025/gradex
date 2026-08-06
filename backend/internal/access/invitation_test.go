package access

import (
	"testing"
)

func TestInvitationStateMachine(t *testing.T) {
	t.Run("state validity", func(t *testing.T) {
		validStates := []State{
			StatePendingStudentAcceptance,
			StatePendingAdminApproval,
			StateApproved,
			StateRejected,
			StateCancelled,
		}
		for _, s := range validStates {
			if !s.Valid() {
				t.Errorf("state %s should be valid", s)
			}
		}

		if State("INVALID").Valid() {
			t.Error("INVALID state should not be valid")
		}
	})

	t.Run("terminal states", func(t *testing.T) {
		terminals := []State{StateApproved, StateRejected, StateCancelled}
		for _, s := range terminals {
			if !s.IsTerminal() {
				t.Errorf("state %s should be terminal", s)
			}
			if s.CanAccept() {
				t.Errorf("terminal state %s must refuse acceptance", s)
			}
		}

		nonTerminals := []State{StatePendingStudentAcceptance, StatePendingAdminApproval}
		for _, s := range nonTerminals {
			if s.IsTerminal() {
				t.Errorf("state %s should not be terminal", s)
			}
		}
	})

	t.Run("can accept state", func(t *testing.T) {
		if !StatePendingStudentAcceptance.CanAccept() {
			t.Error("StatePendingStudentAcceptance should allow acceptance")
		}

		if StatePendingAdminApproval.CanAccept() {
			t.Error("StatePendingAdminApproval should refuse acceptance")
		}
	})
}
