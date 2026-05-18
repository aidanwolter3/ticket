package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateTicketTransition_NewStatuses(t *testing.T) {
	tests := []struct {
		from    Status
		to      Status
		wantErr bool
	}{
		{StatusPreparing, StatusInProgress, false},
		{StatusPreparing, StatusReady, false},
		{StatusTearingDown, StatusDraft, false},
		{StatusTearingDown, StatusMerged, false},
		{StatusTearingDown, StatusApproved, false},
		{StatusReady, StatusPreparing, false},
		{StatusApproved, StatusTearingDown, false},
		// invalid transitions
		{StatusPreparing, StatusMerged, true},
		{StatusTearingDown, StatusReady, true},
		{StatusTearingDown, StatusInProgress, true},
	}
	for _, tc := range tests {
		err := ValidateTicketTransition(tc.from, tc.to)
		if tc.wantErr {
			assert.Error(t, err, "%s → %s should be invalid", tc.from, tc.to)
		} else {
			assert.NoError(t, err, "%s → %s should be valid", tc.from, tc.to)
		}
	}
}
