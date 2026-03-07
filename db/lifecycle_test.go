package db

import (
	"testing"
)

func TestProposalStatusTransitionValidation(t *testing.T) {
	tests := []struct {
		name        string
		current     ProposalStatus
		newStatus   ProposalStatus
		shouldError bool
	}{
		{"detected to drafted", ProposalStatusDetected, ProposalStatusDrafted, false},
		{"drafted to approved", ProposalStatusDrafted, ProposalStatusApproved, false},
		{"drafted to rejected", ProposalStatusDrafted, ProposalStatusRejected, false},
		{"approved to archived", ProposalStatusApproved, ProposalStatusArchived, false},
		{"rejected to archived", ProposalStatusRejected, ProposalStatusArchived, false},
		{"same status (detected)", ProposalStatusDetected, ProposalStatusDetected, false},
		{"same status (approved)", ProposalStatusApproved, ProposalStatusApproved, false},
		{"invalid: detected to approved", ProposalStatusDetected, ProposalStatusApproved, true},
		{"invalid: detected to rejected", ProposalStatusDetected, ProposalStatusRejected, true},
		{"invalid: approved to drafted", ProposalStatusApproved, ProposalStatusDrafted, true},
		{"invalid: approved to rejected", ProposalStatusApproved, ProposalStatusRejected, true},
		{"invalid: rejected to approved", ProposalStatusRejected, ProposalStatusApproved, true},
		{"invalid: archived to anything", ProposalStatusArchived, ProposalStatusDrafted, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidTransition(tt.current, tt.newStatus)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error for transition %s -> %s, got nil", tt.current, tt.newStatus)
				} else if !IsInvalidTransitionError(err) {
					t.Errorf("Expected InvalidTransitionError, got %T", err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for transition %s -> %s: %v", tt.current, tt.newStatus, err)
				}
			}
		})
	}
}

func TestIsValidProposalStatus(t *testing.T) {
	valid := []string{"detected", "drafted", "approved", "rejected", "archived"}
	invalid := []string{"pending", "in_progress", "completed", "", "DETECTED", "Drafted"}

	for _, status := range valid {
		if !IsValidProposalStatus(status) {
			t.Errorf("Expected status '%s' to be valid", status)
		}
	}

	for _, status := range invalid {
		if IsValidProposalStatus(status) {
			t.Errorf("Expected status '%s' to be invalid", status)
		}
	}
}

func TestInvalidTransitionError(t *testing.T) {
	err := &InvalidTransitionError{
		CurrentStatus:   ProposalStatusDetected,
		RequestedStatus: ProposalStatusApproved,
	}

	expected := "invalid status transition from 'detected' to 'approved'"
	if err.Error() != expected {
		t.Errorf("Expected error message '%s', got '%s'", expected, err.Error())
	}

	if !IsInvalidTransitionError(err) {
		t.Error("Expected IsInvalidTransitionError to return true")
	}
}
