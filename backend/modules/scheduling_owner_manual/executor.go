package scheduling_owner_manual

import (
	"context"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type requestCreator interface {
	CreateRequest(ctx context.Context, salonID string, ownerUserID string, req scheduling.ActionRequest) (*scheduling.SchedulingRequest, bool, error)
}

type Executor struct {
	requests requestCreator
}

func NewExecutor(requests requestCreator) *Executor {
	return &Executor{requests: requests}
}

func (e *Executor) SchedulingAuthority() string {
	return booking.SchedulingAuthorityOwnerManual
}

func (e *Executor) CheckAvailability(context.Context, string, string, booking.AvailabilityRequest) (*scheduling.AvailabilityResult, error) {
	return &scheduling.AvailabilityResult{
		Kind:                scheduling.AvailabilityKindRequestOnly,
		SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
	}, nil
}

func (e *Executor) ExecuteAction(ctx context.Context, salonID string, ownerUserID string, req scheduling.ActionRequest) (*scheduling.ActionResult, error) {
	request, _, err := e.requests.CreateRequest(ctx, salonID, ownerUserID, req)
	if err != nil {
		return nil, err
	}
	if request == nil || request.ID == "" {
		return nil, scheduling.ErrInvalidSchedulingResult
	}
	return &scheduling.ActionResult{
		Kind:                scheduling.ActionKindPendingOwnerReview,
		OperationType:       req.OperationType,
		SchedulingAuthority: booking.SchedulingAuthorityOwnerManual,
		PendingOwnerReview: &scheduling.PendingOwnerReviewResult{
			SchedulingRequestID: request.ID,
			Status:              string(request.Status),
			Version:             request.Version,
			Request:             request,
		},
	}, nil
}

var _ scheduling.NeutralExecutor = (*Executor)(nil)
