package db

import (
	"errors"
	"fmt"
)

type ProposalStatus string

const (
	ProposalStatusDetected ProposalStatus = "detected"
	ProposalStatusDrafted  ProposalStatus = "drafted"
	ProposalStatusApproved ProposalStatus = "approved"
	ProposalStatusRejected ProposalStatus = "rejected"
	ProposalStatusArchived ProposalStatus = "archived"
)

type InvalidTransitionError struct {
	CurrentStatus   ProposalStatus
	RequestedStatus ProposalStatus
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("invalid status transition from '%s' to '%s'", e.CurrentStatus, e.RequestedStatus)
}

var validTransitions = map[ProposalStatus][]ProposalStatus{
	ProposalStatusDetected: {ProposalStatusDrafted},
	ProposalStatusDrafted:  {ProposalStatusApproved, ProposalStatusRejected},
	ProposalStatusApproved: {ProposalStatusArchived},
	ProposalStatusRejected: {ProposalStatusArchived},
}

func ValidTransition(currentStatus, newStatus ProposalStatus) error {
	if currentStatus == newStatus {
		return nil
	}

	allowed, exists := validTransitions[currentStatus]
	if !exists {
		return &InvalidTransitionError{
			CurrentStatus:   currentStatus,
			RequestedStatus: newStatus,
		}
	}

	for _, status := range allowed {
		if status == newStatus {
			return nil
		}
	}

	return &InvalidTransitionError{
		CurrentStatus:   currentStatus,
		RequestedStatus: newStatus,
	}
}

func IsValidProposalStatus(status string) bool {
	switch ProposalStatus(status) {
	case ProposalStatusDetected, ProposalStatusDrafted, ProposalStatusApproved,
		ProposalStatusRejected, ProposalStatusArchived:
		return true
	default:
		return false
	}
}

func IsInvalidTransitionError(err error) bool {
	var invalidErr *InvalidTransitionError
	return errors.As(err, &invalidErr)
}
