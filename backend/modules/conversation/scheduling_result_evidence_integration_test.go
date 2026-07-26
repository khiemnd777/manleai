package conversation

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func TestPostgresSchedulingResultEvidenceOwnerScopeAndExactExternalGraph(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	ownerID := insertEvidenceTestOwner(t, ctx, db, "owner")
	otherOwnerID := insertEvidenceTestOwner(t, ctx, db, "other-owner")
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons(name,phone,timezone,owner_user_id)
		VALUES($1,$2,'America/Chicago',$3)
		RETURNING id::text
	`, "Calls evidence "+uuid.NewString(), "+13125550123", ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, ownerID, otherOwnerID)
	})

	var serviceID, staffID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services(salon_id,pos_provider,pos_service_id,pos_service_version,name,duration_minutes)
		VALUES($1,'square',$2,7,'Evidence Manicure',45)
		RETURNING id::text
	`, salonID, "evidence-service-"+uuid.NewString()).Scan(&serviceID); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff(salon_id,pos_provider,pos_staff_id,name)
		VALUES($1,'square',$2,'Evidence Technician')
		RETURNING id::text
	`, salonID, "evidence-staff-"+uuid.NewString()).Scan(&staffID); err != nil {
		t.Fatalf("insert staff: %v", err)
	}

	validSessionID := insertExternalEvidenceFixture(t, ctx, db, salonID, serviceID, staffID, true)
	partialSessionID := insertExternalEvidenceFixture(t, ctx, db, salonID, serviceID, staffID, false)
	repository := NewRepository(db)

	valid, err := repository.GetSessionForOwner(ctx, salonID, ownerID, validSessionID)
	if err != nil {
		t.Fatalf("get valid session: %v", err)
	}
	if evidence := valid.SchedulingResultEvidence; evidence == nil || !evidence.Complete ||
		evidence.SchedulingAuthority != "external_provider" || evidence.Kind != SchedulingEvidenceKindCompletedOperation ||
		!evidence.IsCurrent || evidence.ResultChildCount != 1 || evidence.CurrentActiveChildCount != 1 {
		t.Fatalf("valid external evidence = %#v", evidence)
	}

	partial, err := repository.GetSessionForOwner(ctx, salonID, ownerID, partialSessionID)
	if err != nil {
		t.Fatalf("get partial session: %v", err)
	}
	if evidence := partial.SchedulingResultEvidence; evidence == nil || evidence.Complete ||
		evidence.Kind != SchedulingEvidenceKindIncomplete || evidence.IncompleteReason != evidenceReasonExternalResultInvalid {
		t.Fatalf("partial external evidence = %#v", evidence)
	}

	sessions, err := repository.ListSessions(ctx, salonID, ownerID, LifecycleActive, 10, 0)
	if err != nil {
		t.Fatalf("list owner sessions: %v", err)
	}
	evidenceBySession := make(map[string]*SchedulingResultEvidence, len(sessions))
	for index := range sessions {
		evidenceBySession[sessions[index].ID] = sessions[index].SchedulingResultEvidence
	}
	if evidenceBySession[validSessionID] == nil || !evidenceBySession[validSessionID].Complete ||
		evidenceBySession[partialSessionID] == nil || evidenceBySession[partialSessionID].Complete {
		t.Fatalf("listed evidence = %#v", evidenceBySession)
	}

	if _, err := repository.GetSessionForOwner(ctx, salonID, otherOwnerID, validSessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner get error = %v, want ErrNotFound", err)
	}
	otherSessions, err := repository.ListSessions(ctx, salonID, otherOwnerID, LifecycleActive, 10, 0)
	if err != nil || len(otherSessions) != 0 {
		t.Fatalf("cross-owner list = %#v, error=%v", otherSessions, err)
	}
}

func insertEvidenceTestOwner(t *testing.T, ctx context.Context, db *sql.DB, label string) string {
	t.Helper()
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,full_name)
		VALUES($1,'calls-evidence-test',$2)
		RETURNING id::text
	`, "calls-evidence-"+label+"-"+uuid.NewString()+"@example.test", "Calls evidence "+label).Scan(&ownerID); err != nil {
		t.Fatalf("insert %s: %v", label, err)
	}
	return ownerID
}

func insertExternalEvidenceFixture(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	salonID string,
	serviceID string,
	staffID string,
	exactGraph bool,
) string {
	t.Helper()
	sessionID := uuid.NewString()
	attemptID := uuid.NewString()
	appointmentID := uuid.NewString()
	providerBookingID := "calls-evidence-booking-" + uuid.NewString()
	providerServiceID := "calls-evidence-service-" + uuid.NewString()
	providerStaffID := "calls-evidence-staff-" + uuid.NewString()
	startTime := time.Now().UTC().Add(7 * 24 * time.Hour).Truncate(time.Second)
	endTime := startTime.Add(45 * time.Minute)
	operationKey := "conversation:" + sessionID + ":book"

	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_attempts(
			id,salon_id,source,status,pos_provider,pos_booking_id,pos_booking_version,
			scheduling_authority,authority_provider,authority_appointment_id,authority_appointment_version,
			operation_key,operation_type,provider_outcome,retry_policy,reconciliation_status,
			customer_name,customer_phone,service_id,staff_id,staff_selection_mode,
			requested_start_time,requested_end_time
		) VALUES(
			$1,$2,'ai_voice_call','confirmed','square',$3,4,
			'external_provider','square',$3,4,
			$4,'book','succeeded','none','not_required',
			'Evidence Caller','+13125550124',$5,$6,'specific',$7,$8
		)
	`, attemptID, salonID, providerBookingID, operationKey, serviceID, staffID, startTime, endTime); err != nil {
		t.Fatalf("insert external attempt: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO appointments(
			id,salon_id,booking_attempt_id,pos_provider,pos_appointment_id,pos_appointment_version,
			scheduling_authority,authority_provider,authority_appointment_id,authority_appointment_version,
			status,customer_name,customer_phone,service_id,staff_id,staff_selection_mode,
			start_time,end_time,pos_sync_status,last_pos_synced_at,confirmed_at,confirmation_source
		) VALUES(
			$1,$2,$3,'square',$4,4,
			'external_provider','square',$4,4,
			'confirmed','Evidence Caller','+13125550124',$5,$6,'specific',
			$7,$8,'synced',now(),now(),'external_provider'
		)
	`, appointmentID, salonID, attemptID, providerBookingID, serviceID, staffID, startTime, endTime); err != nil {
		t.Fatalf("insert external appointment: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO booking_attempt_segments(
			salon_id,booking_attempt_id,service_id,staff_id,staff_selection_mode,
			pos_service_id,pos_service_version,pos_staff_id,scheduling_authority,
			authority_provider,authority_service_id,authority_service_version,authority_staff_id,
			name,duration_minutes,sort_order
		) VALUES(
			$1,$2,$3,$4,'specific',$5,7,$6,'external_provider',
			'square',$5,7,$6,'Evidence Manicure',45,1
		)
	`, salonID, attemptID, serviceID, staffID, providerServiceID, providerStaffID); err != nil {
		t.Fatalf("insert external attempt child: %v", err)
	}
	if !exactGraph {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO booking_attempt_segments(
				salon_id,booking_attempt_id,service_id,staff_id,staff_selection_mode,
				pos_service_id,pos_service_version,pos_staff_id,scheduling_authority,
				authority_provider,authority_service_id,authority_service_version,authority_staff_id,
				name,duration_minutes,sort_order
			) VALUES(
				$1,$2,$3,$4,'specific',$5,7,$6,'external_provider',
				'square',$5,7,$6,'Unmirrored Evidence Manicure',45,2
			)
		`, salonID, attemptID, serviceID, staffID, providerServiceID+"-extra", providerStaffID); err != nil {
			t.Fatalf("insert unmatched attempt child: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO appointment_services(
			salon_id,appointment_id,service_id,staff_id,staff_selection_mode,
			pos_service_id,pos_service_version,pos_staff_id,scheduling_authority,
			authority_provider,authority_service_id,authority_service_version,authority_staff_id,
			name,duration_minutes,sort_order
		) VALUES(
			$1,$2,$3,$4,'specific',$5,7,$6,'external_provider',
			'square',$5,7,$6,'Evidence Manicure',45,1
		)
	`, salonID, appointmentID, serviceID, staffID, providerServiceID, providerStaffID); err != nil {
		t.Fatalf("insert external appointment child: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO call_sessions(
			id,salon_id,channel,status,intent,outcome,booking_action,
			customer_name,customer_phone,service_id,staff_id,staff_selection_mode,
			requested_start_time,booking_attempt_id,appointment_id
		) VALUES(
			$1,$2,'simulator','completed','booking','booking_confirmed','book',
			'Evidence Caller','+13125550124',$3,$4,'specific',$5,$6,$7
		)
	`, sessionID, salonID, serviceID, staffID, startTime, attemptID, appointmentID); err != nil {
		t.Fatalf("insert conversation session: %v", err)
	}
	return sessionID
}
