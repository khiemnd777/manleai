package booking

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/modules/pos"
)

type externalClaimedInterval struct {
	ResourceKind string
	ResourceID   string
	StartTime    time.Time
	EndTime      time.Time
}

// GetExternalSchedulingSafety returns only current, database-backed provider
// evidence for the exact salon/provider/location configuration. Absence and
// expiry are represented as ErrSchedulingAuthorityNotReady so callers fail
// closed before acquiring an outbound slot claim.
func (r *Repository) GetExternalSchedulingSafety(ctx context.Context, salonID string, provider string, fence pos.ProviderFence) (*ExternalSchedulingSafety, error) {
	salonID = strings.TrimSpace(salonID)
	provider = strings.TrimSpace(provider)
	locationID := strings.TrimSpace(fence.LocationID)
	if salonID == "" || provider == "" || locationID == "" || fence.SnapshotGeneration <= 0 {
		return nil, ErrValidation
	}

	var safety ExternalSchedulingSafety
	err := r.db.QueryRowContext(ctx, `
		SELECT evidence.id::text, version.version,
		       evidence.atomic_create_no_overlap,
		       evidence.atomic_reschedule_no_overlap,
		       evidence.concrete_staff_assignment,
		       evidence.resource_capacity_enforced,
		       evidence.atomic_party_create,
		       evidence.verified_at, evidence.expires_at
		FROM external_provider_scheduling_capability_evidence evidence
		JOIN salon_integration_configs config
		  ON config.salon_id=evidence.salon_id
		 AND config.id=evidence.integration_config_id
		 AND config.provider=evidence.provider
		 AND config.enabled=true
		JOIN technical_resource_versions version
		  ON version.salon_id=config.salon_id
		 AND version.resource_type='integration_config'
		 AND version.resource_id=config.provider
		 AND version.version=evidence.config_version
		JOIN pos_connections connection
		  ON connection.salon_id=evidence.salon_id
		 AND connection.provider=evidence.provider
		 AND connection.id=evidence.connection_id
		 AND connection.booking_write_capability_version=evidence.connection_capability_version
		 AND connection.status='active'
		 AND connection.last_sync_at IS NOT NULL
		 AND connection.location_id=evidence.provider_location_id
		 AND connection.snapshot_generation=$4
		WHERE evidence.salon_id=$1
		  AND evidence.provider=$2
		  AND evidence.provider_location_id=$3
		  AND evidence.verification_contract_version='square-buyer-single-create-v1'
		  AND evidence.write_permission_mode='buyer_write'
		  AND evidence.provider_api_version=config.settings->>'api_version'
		  AND evidence.oauth_scope_fingerprint=public.square_oauth_scope_fingerprint(connection.scopes)
		  AND evidence.verified_at <= now()
		  AND evidence.expires_at > now()
		ORDER BY evidence.config_version DESC, evidence.verified_at DESC
		LIMIT 1
	`, salonID, provider, locationID, fence.SnapshotGeneration).Scan(
		&safety.EvidenceID,
		&safety.ConfigVersion,
		&safety.AtomicCreateNoOverlap,
		&safety.AtomicRescheduleNoOverlap,
		&safety.ConcreteStaffAssignment,
		&safety.ResourceCapacityEnforced,
		&safety.AtomicPartyCreate,
		&safety.VerifiedAt,
		&safety.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSchedulingAuthorityNotReady
	}
	if err != nil {
		return nil, err
	}
	return &safety, nil
}

// FilterExternalClaimedSlots is advisory availability subtraction. The
// exclusion constraint used by ClaimPending* remains the commit-time source of
// truth; this avoids repeatedly offering slots held by another external flow.
func (r *Repository) FilterExternalClaimedSlots(ctx context.Context, salonID string, provider string, fence pos.ProviderFence, targetAppointmentID string, slots []AvailabilitySlot) ([]AvailabilitySlot, error) {
	if len(slots) == 0 {
		return []AvailabilitySlot{}, nil
	}
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(provider) == "" || !validProviderFence(fence) {
		return nil, ErrValidation
	}
	windowStart, windowEnd := slots[0].StartTime.UTC(), slots[0].EndTime.UTC()
	for _, slot := range slots[1:] {
		if slot.StartTime.Before(windowStart) {
			windowStart = slot.StartTime.UTC()
		}
		if slot.EndTime.After(windowEnd) {
			windowEnd = slot.EndTime.UTC()
		}
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT interval.resource_kind,interval.resource_id,
		       interval.occupied_start_time,interval.occupied_end_time
		FROM external_slot_claim_intervals interval
		JOIN external_slot_claims claim
		  ON claim.salon_id=interval.salon_id AND claim.id=interval.claim_id
		WHERE interval.salon_id=$1
		  AND interval.provider=$2
		  AND interval.provider_location_id=$3
		  AND interval.released_at IS NULL
		  AND interval.activation_pending=false
		  AND interval.occupied_start_time < $5
		  AND interval.occupied_end_time > $4
		  AND ($6='' OR NOT EXISTS (
		      SELECT 1 FROM appointments appointment
		      WHERE appointment.salon_id=interval.salon_id
		        AND appointment.id=NULLIF($6,'')::uuid
		        AND appointment.booking_attempt_id=claim.booking_attempt_id
		  ))
		ORDER BY interval.occupied_start_time,interval.id
	`, salonID, provider, fence.LocationID, windowStart, windowEnd, strings.TrimSpace(targetAppointmentID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claimed := make([]externalClaimedInterval, 0)
	for rows.Next() {
		var interval externalClaimedInterval
		if err := rows.Scan(&interval.ResourceKind, &interval.ResourceID, &interval.StartTime, &interval.EndTime); err != nil {
			return nil, err
		}
		claimed = append(claimed, interval)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	available := make([]AvailabilitySlot, 0, len(slots))
	for _, slot := range slots {
		if !externalAvailabilitySlotOverlapsClaim(slot, claimed) {
			available = append(available, slot)
		}
	}
	return available, nil
}

func externalAvailabilitySlotOverlapsClaim(slot AvailabilitySlot, claimed []externalClaimedInterval) bool {
	for _, segment := range slot.Segments {
		startTime := segment.OccupiedStartTime.UTC()
		endTime := segment.OccupiedEndTime.UTC()
		if startTime.IsZero() {
			startTime = segment.ScheduledStartTime.UTC()
		}
		if endTime.IsZero() {
			endTime = segment.ScheduledEndTime.UTC()
		}
		for _, interval := range claimed {
			if interval.ResourceKind == "staff" && interval.ResourceID == segment.StaffID &&
				startTime.Before(interval.EndTime) && interval.StartTime.Before(endTime) {
				return true
			}
		}
	}
	return false
}
