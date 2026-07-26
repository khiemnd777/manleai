package schedulingload

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/scheduling"
	"github.com/manleai/ai-receptionist/modules/scheduling_owner_manual"
)

func runOwnerManualWorkload(
	ctx context.Context,
	db *sql.DB,
	config Config,
	seed seededRun,
	violations *InvariantViolations,
) ([]WorkloadReport, error) {
	service := scheduling_owner_manual.NewService(scheduling_owner_manual.NewRepository(db))
	request := scheduling.ActionRequest{
		OperationType:      scheduling.OperationKindBook,
		OperationKey:       "load-owner-create-" + config.RunID,
		Source:             booking.SourceOwnerDashboard,
		CustomerName:       "Synthetic Customer",
		CustomerPhone:      syntheticPhone(config.RunID, "customer-owner"),
		CustomerEmail:      syntheticEmail(config.RunID, "customer-owner"),
		RequestedTimezone:  "America/Chicago",
		PartySize:          1,
		RequestedStartTime: config.workloadTime().Add(24 * time.Hour),
		Segments: []scheduling.ActionSegment{{
			ServiceID: seed.OwnerManual.ServiceID, StaffSelectionMode: booking.StaffSelectionAnyone,
			GuestReference: "synthetic-guest", Quantity: 1,
		}},
	}

	var requestID string
	var requestIDMutex sync.Mutex
	started := time.Now()
	createSamples := runConcurrent(ctx, config.OperationsPerWorkload, config.Concurrency, func(callCtx context.Context, _ int) sampleResult {
		created, replayed, err := service.CreateRequest(callCtx, seed.OwnerManual.SalonID, seed.OwnerID, request)
		if err != nil || created == nil {
			return sampleResult{UnexpectedError: true}
		}
		requestIDMutex.Lock()
		if requestID == "" {
			requestID = created.ID
		} else if requestID != created.ID {
			violations.Idempotency++
		}
		requestIDMutex.Unlock()
		return sampleResult{Success: true, Replayed: replayed}
	})
	reports := []WorkloadReport{summarizeWorkload("owner_manual_create_replay", started, createSamples)}
	if requestID == "" {
		return reports, errors.New("owner_manual workload did not create a request")
	}

	changed := request
	changed.Notes = "different synthetic payload"
	if _, _, err := service.CreateRequest(ctx, seed.OwnerManual.SalonID, seed.OwnerID, changed); !errors.Is(err, booking.ErrOperationConflict) {
		violations.Idempotency++
	}
	if _, _, err := service.CreateRequest(ctx, seed.OwnerManual.SalonID, seed.OtherOwnerID, request); !errors.Is(err, pos.ErrNotFound) {
		violations.Tenant++
	}

	transitionStarted := time.Now()
	winnerAction := ""
	var winnerMutex sync.Mutex
	transitionSamples := runConcurrent(ctx, config.OperationsPerWorkload, config.Concurrency, func(callCtx context.Context, index int) sampleResult {
		actionKey := fmt.Sprintf("load-owner-transition-%s-%d", config.RunID, index)
		_, replayed, err := service.Transition(callCtx, seed.OwnerManual.SalonID, seed.OwnerID, requestID, scheduling.TransitionSchedulingRequest{
			ActionKey: actionKey, ExpectedVersion: 1, Status: scheduling.SchedulingRequestStatusContacted,
			Note: "synthetic transition",
		})
		if err == nil {
			winnerMutex.Lock()
			if winnerAction == "" {
				winnerAction = actionKey
			} else if !replayed {
				violations.Idempotency++
			}
			winnerMutex.Unlock()
			return sampleResult{Success: true, Replayed: replayed}
		}
		if errors.Is(err, scheduling.ErrSchedulingRequestVersion) {
			return sampleResult{ExpectedConflict: true}
		}
		return sampleResult{UnexpectedError: true}
	})
	reports = append(reports, summarizeWorkload("owner_manual_transition_cas", transitionStarted, transitionSamples))
	if winnerAction == "" {
		return reports, errors.New("owner_manual workload did not win transition CAS")
	}
	if _, replayed, err := service.Transition(ctx, seed.OwnerManual.SalonID, seed.OwnerID, requestID, scheduling.TransitionSchedulingRequest{
		ActionKey: winnerAction, ExpectedVersion: 1, Status: scheduling.SchedulingRequestStatusContacted,
		Note: "synthetic transition",
	}); err != nil || !replayed {
		violations.Idempotency++
	}

	var requests, segments, createdEvents, statusEvents, notifications int
	if err := db.QueryRowContext(ctx, `
		SELECT
			count(*) FILTER (WHERE request.id IS NOT NULL),
			count(DISTINCT segment.id),
			count(DISTINCT event.id) FILTER (WHERE event.event_type='request_created'),
			count(DISTINCT event.id) FILTER (WHERE event.event_type='status_changed'),
			count(DISTINCT notification.id)
		FROM scheduling_requests request
		LEFT JOIN scheduling_request_segments segment ON segment.scheduling_request_id=request.id
		LEFT JOIN scheduling_request_events event ON event.scheduling_request_id=request.id
		LEFT JOIN owner_notifications notification ON notification.scheduling_request_id=request.id
		WHERE request.salon_id::text=$1 AND request.operation_key=$2
	`, seed.OwnerManual.SalonID, request.OperationKey).Scan(&requests, &segments, &createdEvents, &statusEvents, &notifications); err != nil {
		return reports, fmt.Errorf("inspect owner_manual invariants: %w", err)
	}
	// Joined row multiplication means only child counts are DISTINCT. The root is
	// checked independently to keep duplicate evidence unambiguous.
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM scheduling_requests WHERE salon_id::text=$1 AND operation_key=$2`, seed.OwnerManual.SalonID, request.OperationKey).Scan(&requests); err != nil {
		return reports, err
	}
	if requests != 1 {
		violations.Duplicates += absoluteDifference(requests, 1)
	}
	if segments != 1 || createdEvents != 1 || statusEvents != 1 || notifications != 1 {
		violations.Orphans += absoluteDifference(segments, 1) + absoluteDifference(createdEvents, 1) + absoluteDifference(statusEvents, 1) + absoluteDifference(notifications, 1)
	}
	return reports, nil
}

func absoluteDifference(actual int, expected int) int {
	if actual > expected {
		return actual - expected
	}
	return expected - actual
}
