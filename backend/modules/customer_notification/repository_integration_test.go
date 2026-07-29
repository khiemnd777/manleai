package customernotification

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	appdatabase "github.com/manleai/ai-receptionist/internal/database"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
)

func TestRepositoryPostgresConsentTransitionsReplayAndStopRace(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := appdatabase.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	ownerID, salonID, sessionID := seedCustomerNotificationConsentFixture(t, ctx, db)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, ownerID)
	})
	repo := NewRepository(db)
	service := NewService(repo)
	destination := "+13125550123"

	pending, replayed, err := repo.RecordConsentRequested(ctx, salonID, sessionID, destination, "request-1", sessionID)
	if err != nil || replayed || pending.Status != ConsentPending || pending.RequestedAt == nil || pending.ConsentedAt != nil || pending.DeclinedAt != nil {
		t.Fatalf("pending=%#v replayed=%t err=%v", pending, replayed, err)
	}
	declined, replayed, err := repo.RecordConversationConsent(ctx, RecordConversationConsentRequest{
		SalonID: salonID, CallSessionID: sessionID, Destination: destination,
		Granted: false, EventKey: "response-no-1", EvidenceReference: "turn-no-1",
	})
	if err != nil || replayed || declined.Status != ConsentDeclined || declined.DeclinedAt == nil || declined.ConsentedAt != nil || declined.OptedOutAt != nil {
		t.Fatalf("declined=%#v replayed=%t err=%v", declined, replayed, err)
	}
	if _, _, err := service.AttestConsent(ctx, salonID, ownerID, AttestConsentRequest{Destination: destination, ActionKey: "false-attestation", Attested: false}); !errors.Is(err, ErrValidation) {
		t.Fatalf("false owner attestation error=%v", err)
	}
	consented, replayed, err := service.AttestConsent(ctx, salonID, ownerID, AttestConsentRequest{Destination: destination, ActionKey: "explicit-attestation", Attested: true})
	if err != nil || replayed || consented.Status != ConsentConsented || consented.ConsentedAt == nil || consented.DeclinedAt != nil || consented.OptedOutAt != nil {
		t.Fatalf("consented=%#v replayed=%t err=%v", consented, replayed, err)
	}
	if _, _, err := repo.RecordConsentRequested(ctx, salonID, sessionID, destination, "request-after-consent", sessionID); !errors.Is(err, ErrConflict) {
		t.Fatalf("consented to pending error=%v", err)
	}

	if err := service.ApplyInboundOptOut(ctx, salonID, destination, "+13125550999", "+13125550999", "STOP", "SMSTOP1", strings.Repeat("a", 64)); err != nil {
		t.Fatalf("STOP: %v", err)
	}
	optedOut, err := repo.ConsentForDestination(ctx, salonID, destination)
	if err != nil || optedOut.Status != ConsentOptedOut || optedOut.OptedOutAt == nil || optedOut.ConsentedAt != nil || optedOut.DeclinedAt != nil {
		t.Fatalf("opted out=%#v err=%v", optedOut, err)
	}
	if _, _, err := service.AttestConsent(ctx, salonID, ownerID, AttestConsentRequest{Destination: destination, ActionKey: "cannot-lift-stop", Attested: true}); !errors.Is(err, ErrConflict) {
		t.Fatalf("owner lifted STOP error=%v", err)
	}
	versionBeforeHelp := optedOut.Version
	if err := service.ApplyInboundOptOut(ctx, salonID, destination, "+13125550999", "+13125550999", "HELP", "SMHELP1", strings.Repeat("b", 64)); err != nil {
		t.Fatalf("HELP: %v", err)
	}
	afterHelp, _ := repo.ConsentForDestination(ctx, salonID, destination)
	if afterHelp.Version != versionBeforeHelp || afterHelp.Status != ConsentOptedOut || afterHelp.Source != optedOut.Source {
		t.Fatalf("HELP mutated state before=%#v after=%#v", optedOut, afterHelp)
	}
	if err := service.ApplyInboundOptOut(ctx, salonID, destination, "+13125550999", "+13125550999", "START", "SMSTART1", strings.Repeat("c", 64)); err != nil {
		t.Fatalf("START: %v", err)
	}
	afterStart, _ := repo.ConsentForDestination(ctx, salonID, destination)
	if afterStart.Status != ConsentConsented || afterStart.Source != ConsentSourceTwilio || afterStart.ConsentedAt == nil || afterStart.OptedOutAt != nil {
		t.Fatalf("START state=%#v", afterStart)
	}
	assertSignedSTARTTransitions(t, ctx, service, repo, salonID, sessionID)

	assertConcurrentConsentReplay(t, ctx, repo, salonID, ownerID)
	assertStopWinsConcurrentLocalConsent(t, ctx, service, repo, salonID, ownerID, sessionID)
	assertPredispatchSTOPAndPolicyDisableAreSerialized(t, ctx, service, repo, salonID, ownerID, destination)
}

func TestRepositoryPostgresExternalVersionZeroUnknownOutcomeAndBoundedRequeue(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := appdatabase.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	ownerID, salonID, _ := seedCustomerNotificationConsentFixture(t, ctx, db)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, ownerID)
	})
	repo := NewRepository(db)
	service := NewService(repo)
	destination := "+13125550131"
	if _, _, err := service.AttestConsent(ctx, salonID, ownerID, AttestConsentRequest{Destination: destination, ActionKey: "v0-consent", Attested: true}); err != nil {
		t.Fatalf("consent: %v", err)
	}
	serviceID, staffID := seedCustomerNotificationCatalog(t, ctx, db, salonID)
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	providerID := "customer-sms-v0-" + uuid.NewString()
	var attemptID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO booking_attempts(
			salon_id,source,status,pos_provider,pos_booking_id,pos_booking_version,pos_idempotency_key,
			operation_key,request_fingerprint,operation_type,provider_outcome,retry_policy,reconciliation_status,
			customer_name,customer_phone,service_id,staff_id,staff_selection_mode,requested_start_time,requested_end_time,
			provider_location_id,provider_snapshot_generation,scheduling_authority,authority_provider,
			authority_appointment_id,authority_appointment_version,authority_idempotency_key,
			authority_location_id,authority_snapshot_generation
		) VALUES($1,'owner_dashboard','confirmed','square',$2,0,$3,$4,$5,'book','succeeded','none','not_required',
		         'Version Zero Customer',$6,$7,$8,'specific',$9,$10,'location-v0',1,'external_provider','square',$2,0,$3,'location-v0',1)
		RETURNING id::text
	`, salonID, providerID, uuid.NewString(), "customer-sms-v0-op-"+uuid.NewString(), strings.Repeat("7", 64), destination, serviceID, staffID, start, start.Add(45*time.Minute)).Scan(&attemptID); err != nil {
		t.Fatalf("insert v0 attempt: %v", err)
	}
	var appointmentID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO appointments(
			salon_id,booking_attempt_id,pos_provider,pos_appointment_id,pos_appointment_version,status,
			customer_name,customer_phone,service_id,staff_id,staff_selection_mode,start_time,end_time,
			pos_sync_status,last_pos_synced_at,scheduling_authority,authority_provider,
			authority_appointment_id,authority_appointment_version,confirmed_at,confirmation_source
		) VALUES($1,$2,'square',$3,0,'confirmed','Version Zero Customer',$4,$5,$6,'specific',$7,$8,
		         'synced',now(),'external_provider','square',$3,0,now(),'external_provider')
		RETURNING id::text
	`, salonID, attemptID, providerID, destination, serviceID, staffID, start, start.Add(45*time.Minute)).Scan(&appointmentID); err != nil {
		t.Fatalf("insert external v0 appointment: %v", err)
	}
	var triggerDeliveryID string
	var sourceVersion int
	if err := db.QueryRowContext(ctx, `SELECT id::text,source_version FROM customer_notification_deliveries WHERE salon_id=$1 AND appointment_id=$2 AND notification_type='confirmed'`, salonID, appointmentID).Scan(&triggerDeliveryID, &sourceVersion); err != nil || sourceVersion != 0 {
		t.Fatalf("v0 trigger delivery=%q version=%d err=%v", triggerDeliveryID, sourceVersion, err)
	}
	items, err := repo.ClaimBatch(ctx, 1, DeliveryLeaseDuration)
	if err != nil || len(items) != 1 || items[0].ID != triggerDeliveryID {
		t.Fatalf("claim version-zero delivery=%#v err=%v", items, err)
	}
	claimed := items[0]
	if err := repo.MarkDispatchStarted(ctx, claimed); err != nil {
		t.Fatalf("mark version-zero dispatch: %v", err)
	}
	providerMessageID := "SMCUSTOMERMONOTONIC" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := repo.RecordProviderResult(ctx, claimed, notificationdelivery.SendResult{
		ProviderMessageID: providerMessageID,
		ProviderStatus:    "accepted",
		DeliveryStatus:    StatusProviderAccepted,
		StatusRank:        10,
	}); err != nil {
		t.Fatalf("record provider acceptance: %v", err)
	}
	deliveredAt := time.Now().UTC().Add(time.Second)
	if err := repo.ApplyProviderCallback(ctx, notificationdelivery.ProviderCallback{
		Provider: notificationdelivery.ProviderTwilio, ProviderMessageID: providerMessageID,
		ProviderStatus: "delivered", DeliveryStatus: StatusDelivered, StatusRank: 40,
		EventKey: "monotonic-delivered", EventFingerprint: strings.Repeat("a", 64), OccurredAt: deliveredAt,
	}); err != nil {
		t.Fatalf("apply delivered callback: %v", err)
	}
	if err := repo.ApplyProviderCallback(ctx, notificationdelivery.ProviderCallback{
		Provider: notificationdelivery.ProviderTwilio, ProviderMessageID: providerMessageID,
		ProviderStatus: "sent", DeliveryStatus: StatusSent, StatusRank: 30,
		EventKey: "monotonic-late-sent", EventFingerprint: strings.Repeat("b", 64), OccurredAt: deliveredAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("apply late sent callback: %v", err)
	}
	var finalStatus, attemptOutcome string
	var callbackEvents int
	if err := db.QueryRowContext(ctx, `
		SELECT delivery.delivery_status,attempt.outcome,
		       (SELECT count(*) FROM customer_notification_delivery_events event
		        WHERE event.customer_notification_delivery_id=delivery.id AND event.event_type='status_callback')
		FROM customer_notification_deliveries delivery
		JOIN customer_notification_delivery_attempts attempt
		  ON attempt.customer_notification_delivery_id=delivery.id AND attempt.provider_message_id=$2
		WHERE delivery.id=$1
	`, triggerDeliveryID, providerMessageID).Scan(&finalStatus, &attemptOutcome, &callbackEvents); err != nil || finalStatus != StatusDelivered || attemptOutcome != "delivered" || callbackEvents != 2 {
		t.Fatalf("monotonic callback status=%q attempt=%q events=%d err=%v", finalStatus, attemptOutcome, callbackEvents, err)
	}

	ambiguousDeliveryID := seedCustomerAppointmentDelivery(t, ctx, db, salonID, appointmentID, attemptID, destination, "ambiguous-outcome", 0)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO customer_notification_delivery_attempts(
			salon_id,customer_notification_delivery_id,attempt_number,claim_token,provider,outcome,
			error_code,started_at,dispatch_started_at,finished_at
		) VALUES($1,$2,5,$3,'twilio','outcome_unknown','DELIVERY_OUTCOME_UNKNOWN',now(),now(),now())
	`, salonID, ambiguousDeliveryID, uuid.NewString()); err != nil {
		t.Fatalf("insert ambiguous attempt: %v", err)
	}
	detail, err := service.DetailForAppointment(ctx, salonID, ownerID, appointmentID)
	if err != nil {
		t.Fatalf("appointment detail: %v", err)
	}
	ambiguousDetail := findCustomerDelivery(t, detail, ambiguousDeliveryID)
	if ambiguousDetail.CanRequeue || ambiguousDetail.RequeueBlockedReason != "delivery_outcome_unknown" {
		t.Fatalf("ambiguous detail=%#v", detail)
	}
	if _, _, err := service.Requeue(ctx, salonID, ownerID, appointmentID, ambiguousDeliveryID, RequeueRequest{ActionKey: "ambiguous-requeue"}); !errors.Is(err, ErrRequeueBlocked) {
		t.Fatalf("ambiguous requeue error=%v", err)
	}

	requestID, requestDeliveryID := seedCustomerRequestDeliveryForDetail(t, ctx, db, salonID, destination, "request-detail")
	requestDetail, err := service.DetailForRequest(ctx, salonID, ownerID, requestID)
	if err != nil {
		t.Fatalf("request detail: %v", err)
	}
	requestDelivery := findCustomerDelivery(t, requestDetail, requestDeliveryID)
	if requestDelivery.NotificationType != "request_received" {
		t.Fatalf("request delivery=%#v", requestDelivery)
	}

	boundedDeliveryID := seedCustomerAppointmentDelivery(t, ctx, db, salonID, appointmentID, attemptID, destination, "bounded-requeue", 0)
	type requeueResult struct {
		key      string
		replayed bool
		err      error
	}
	results := make(chan requeueResult, 2)
	var wg sync.WaitGroup
	for index := range 2 {
		key := "bounded-action-" + string(rune('a'+index))
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, replayed, err := service.Requeue(ctx, salonID, ownerID, appointmentID, boundedDeliveryID, RequeueRequest{ActionKey: key})
			results <- requeueResult{key: key, replayed: replayed, err: err}
		}()
	}
	wg.Wait()
	close(results)
	winner := ""
	blocked := 0
	for result := range results {
		if result.err == nil {
			winner = result.key
		} else if errors.Is(result.err, ErrRequeueBlocked) {
			blocked++
		} else {
			t.Fatalf("concurrent requeue %s: %v", result.key, result.err)
		}
	}
	if winner == "" || blocked != 1 {
		t.Fatalf("winner=%q blocked=%d", winner, blocked)
	}
	if _, replayed, err := service.Requeue(ctx, salonID, ownerID, appointmentID, boundedDeliveryID, RequeueRequest{ActionKey: winner}); err != nil || !replayed {
		t.Fatalf("exact requeue replay=%t err=%v", replayed, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE customer_notification_deliveries SET delivery_status='dead_letter',dead_lettered_at=now(),last_delivery_error_code='TWILIO_30003' WHERE id=$1`, boundedDeliveryID); err != nil {
		t.Fatalf("dead letter bounded delivery: %v", err)
	}
	if _, _, err := service.Requeue(ctx, salonID, ownerID, appointmentID, boundedDeliveryID, RequeueRequest{ActionKey: "over-limit"}); !errors.Is(err, ErrRequeueBlocked) {
		t.Fatalf("over-limit requeue error=%v", err)
	}
}

func findCustomerDelivery(t *testing.T, detail *Detail, deliveryID string) Delivery {
	t.Helper()
	if detail == nil {
		t.Fatal("customer notification detail is nil")
	}
	for _, delivery := range detail.Deliveries {
		if delivery.ID == deliveryID {
			return delivery
		}
	}
	t.Fatalf("customer notification delivery %s not found in %#v", deliveryID, detail)
	return Delivery{}
}

func seedCustomerNotificationCatalog(t *testing.T, ctx context.Context, db *sql.DB, salonID string) (serviceID, staffID string) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `INSERT INTO services(salon_id,pos_provider,pos_service_id,pos_service_version,name,duration_minutes,active,ai_bookable) VALUES($1,'square',$2,1,'Customer SMS Service',45,true,true) RETURNING id::text`, salonID, "customer-sms-service-"+uuid.NewString()).Scan(&serviceID); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO staff(salon_id,pos_provider,pos_staff_id,name,active,ai_bookable) VALUES($1,'square',$2,'Customer SMS Staff',true,true) RETURNING id::text`, salonID, "customer-sms-staff-"+uuid.NewString()).Scan(&staffID); err != nil {
		t.Fatalf("insert staff: %v", err)
	}
	return serviceID, staffID
}

func seedCustomerAppointmentDelivery(t *testing.T, ctx context.Context, db *sql.DB, salonID, appointmentID, attemptID, destination, suffix string, sourceVersion int) string {
	t.Helper()
	var deliveryID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer_notification_deliveries(
			salon_id,customer_sms_consent_id,appointment_id,booking_attempt_id,notification_type,
			source_version,dedupe_key,message_body,destination_e164,destination_masked,destination_hash,
			consent_version,policy_version,delivery_status,delivery_attempts,dead_lettered_at,last_delivery_error_code
		)
		SELECT $1,consent.id,$2,$3,'confirmed',$4,$5,'Bounded retry fixture. Reply STOP to opt out.',
		       consent.normalized_destination,consent.destination_masked,encode(digest(consent.normalized_destination,'sha256'),'hex'),
		       consent.version,settings.customer_sms_policy_version,'dead_letter',5,now(),'TWILIO_30003'
		FROM customer_sms_consents consent JOIN salon_settings settings ON settings.salon_id=consent.salon_id
		WHERE consent.salon_id=$1 AND consent.normalized_destination=$6
		RETURNING id::text
	`, salonID, appointmentID, attemptID, sourceVersion, "customer-sms-manual-"+suffix+"-"+uuid.NewString(), destination).Scan(&deliveryID); err != nil {
		t.Fatalf("insert customer appointment delivery: %v", err)
	}
	return deliveryID
}

func assertSignedSTARTTransitions(t *testing.T, ctx context.Context, service *Service, repo *Repository, salonID, sessionID string) {
	t.Helper()
	tests := []struct {
		name, destination string
		prepare           func() error
	}{
		{name: "missing", destination: "+13125550126", prepare: func() error { return nil }},
		{name: "pending", destination: "+13125550127", prepare: func() error {
			_, _, err := repo.RecordConsentRequested(ctx, salonID, sessionID, "+13125550127", "start-pending-request", sessionID)
			return err
		}},
		{name: "declined", destination: "+13125550128", prepare: func() error {
			if _, _, err := repo.RecordConsentRequested(ctx, salonID, sessionID, "+13125550128", "start-declined-request", sessionID); err != nil {
				return err
			}
			_, _, err := repo.RecordConversationConsent(ctx, RecordConversationConsentRequest{SalonID: salonID, CallSessionID: sessionID, Destination: "+13125550128", Granted: false, EventKey: "start-declined-response", EvidenceReference: "decline-before-start"})
			return err
		}},
	}
	for index, test := range tests {
		if err := test.prepare(); err != nil {
			t.Fatalf("prepare START %s: %v", test.name, err)
		}
		if err := service.ApplyInboundOptOut(ctx, salonID, test.destination, "+13125550999", "+13125550999", "START", "SMSTARTCASE"+string(rune('A'+index)), strings.Repeat(string(rune('1'+index)), 64)); err != nil {
			t.Fatalf("START %s: %v", test.name, err)
		}
		consent, err := repo.ConsentForDestination(ctx, salonID, test.destination)
		if err != nil || consent.Status != ConsentConsented || consent.Source != ConsentSourceTwilio || consent.ConsentedAt == nil || consent.DeclinedAt != nil || consent.OptedOutAt != nil {
			t.Fatalf("START %s consent=%#v err=%v", test.name, consent, err)
		}
	}
}

func assertPredispatchSTOPAndPolicyDisableAreSerialized(t *testing.T, ctx context.Context, service *Service, repo *Repository, salonID, ownerID, destination string) {
	t.Helper()
	runRace := func(label string, mutate func() error) {
		deliveryID := seedCustomerRequestDelivery(t, ctx, repo.db, salonID, destination, label)
		items, err := repo.ClaimBatch(ctx, 1, DeliveryLeaseDuration)
		if err != nil || len(items) != 1 || items[0].ID != deliveryID {
			t.Fatalf("%s claim=%#v err=%v", label, items, err)
		}
		item := items[0]
		start := make(chan struct{})
		markResult := make(chan error, 1)
		mutationResult := make(chan error, 1)
		go func() { <-start; markResult <- repo.MarkDispatchStarted(ctx, item) }()
		go func() { <-start; mutationResult <- mutate() }()
		close(start)
		markErr, mutationErr := <-markResult, <-mutationResult
		if mutationErr != nil {
			t.Fatalf("%s mutation: %v", label, mutationErr)
		}
		var dispatchStarted sql.NullTime
		if err := repo.db.QueryRowContext(ctx, `SELECT delivery_dispatch_started_at FROM customer_notification_deliveries WHERE id=$1`, deliveryID).Scan(&dispatchStarted); err != nil {
			t.Fatalf("%s dispatch evidence: %v", label, err)
		}
		switch {
		case markErr == nil && dispatchStarted.Valid:
			if err := repo.RecordOutcomeUnknown(ctx, item, "provider-not-called-in-test"); err != nil {
				t.Fatalf("%s finish marked delivery: %v", label, err)
			}
		case errors.Is(markErr, ErrDispatchBlocked) && !dispatchStarted.Valid:
			if err := repo.RecordSuppressed(ctx, item, "TEST_PRE_DISPATCH_MUTATION"); err != nil {
				t.Fatalf("%s suppress blocked delivery: %v", label, err)
			}
		default:
			t.Fatalf("%s markErr=%v dispatchStarted=%t; stale dispatch mark must never follow the mutation", label, markErr, dispatchStarted.Valid)
		}
	}

	runRace("stop-race", func() error {
		return repo.ApplyInboundOptOut(ctx, InboundOptOut{
			SalonID: salonID, From: destination, To: "+13125550999", ConfiguredSender: "+13125550999",
			OptOutType: "STOP", ProviderMessageID: "SMPREDISPATCHSTOP", EventFingerprint: strings.Repeat("e", 64),
		})
	})
	if err := service.ApplyInboundOptOut(ctx, salonID, destination, "+13125550999", "+13125550999", "START", "SMPREDISPATCHSTART", strings.Repeat("f", 64)); err != nil {
		t.Fatalf("restore after STOP race: %v", err)
	}
	policy, err := service.GetPolicy(ctx, salonID, ownerID)
	if err != nil {
		t.Fatalf("policy before disable race: %v", err)
	}
	runRace("policy-disable-race", func() error {
		_, err := service.UpdatePolicy(ctx, salonID, ownerID, UpdatePolicyRequest{
			Enabled: false, QuietStart: policy.QuietStart, QuietEnd: policy.QuietEnd, ExpectedVersion: policy.Version,
		})
		return err
	})
}

func seedCustomerRequestDelivery(t *testing.T, ctx context.Context, db *sql.DB, salonID, destination, label string) string {
	t.Helper()
	_, deliveryID := seedCustomerRequestDeliveryForDetail(t, ctx, db, salonID, destination, label)
	return deliveryID
}

func seedCustomerRequestDeliveryForDetail(t *testing.T, ctx context.Context, db *sql.DB, salonID, destination, label string) (string, string) {
	t.Helper()
	operationKey := "customer-sms-" + label + "-" + uuid.NewString()
	var requestID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO scheduling_requests(
			salon_id,scheduling_authority,operation_key,request_fingerprint,operation_type,source,
			status,version,customer_name,customer_phone,requested_timezone,party_size,
			requested_start_time,requested_end_time
		) VALUES($1,'owner_manual',$2,$3,'book','customer_sms_integration','pending',1,
		         'Customer', $4, 'America/Chicago',1,now()+interval '1 day',now()+interval '1 day 1 hour')
		RETURNING id::text
	`, salonID, operationKey, strings.Repeat("9", 64), destination).Scan(&requestID); err != nil {
		t.Fatalf("%s insert scheduling request: %v", label, err)
	}
	var deliveryID string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM customer_notification_deliveries WHERE salon_id=$1 AND scheduling_request_id=$2`, salonID, requestID).Scan(&deliveryID); err != nil {
		t.Fatalf("%s customer delivery: %v", label, err)
	}
	return requestID, deliveryID
}

func assertConcurrentConsentReplay(t *testing.T, ctx context.Context, repo *Repository, salonID, ownerID string) {
	t.Helper()
	destination := "+13125550124"
	type result struct {
		replayed bool
		err      error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, replayed, err := repo.RecordOwnerAttestation(ctx, salonID, ownerID, destination, "same-owner-event", true)
			results <- result{replayed: replayed, err: err}
		}()
	}
	wg.Wait()
	close(results)
	replays, successes := 0, 0
	for item := range results {
		if item.err != nil {
			t.Fatalf("concurrent replay: %v", item.err)
		}
		successes++
		if item.replayed {
			replays++
		}
	}
	if successes != 2 || replays != 1 {
		t.Fatalf("successes=%d replays=%d, want 2/1", successes, replays)
	}
	var events int
	if err := repo.db.QueryRowContext(ctx, `SELECT count(*) FROM customer_sms_consent_events event JOIN customer_sms_consents consent ON consent.id=event.customer_sms_consent_id WHERE consent.salon_id=$1 AND consent.normalized_destination=$2`, salonID, destination).Scan(&events); err != nil || events != 1 {
		t.Fatalf("replay events=%d err=%v", events, err)
	}
}

func assertStopWinsConcurrentLocalConsent(t *testing.T, ctx context.Context, service *Service, repo *Repository, salonID, ownerID, sessionID string) {
	t.Helper()
	destination := "+13125550125"
	if _, _, err := repo.RecordConsentRequested(ctx, salonID, sessionID, destination, "race-request", sessionID); err != nil {
		t.Fatalf("race pending: %v", err)
	}
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _, err := service.AttestConsent(ctx, salonID, ownerID, AttestConsentRequest{Destination: destination, ActionKey: "race-owner", Attested: true})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		errs <- repo.ApplyInboundOptOut(ctx, InboundOptOut{
			SalonID: salonID, From: destination, To: "+13125550999", ConfiguredSender: "+13125550999",
			OptOutType: "STOP", ProviderMessageID: "SMRACESTOP", EventFingerprint: strings.Repeat("d", 64),
		})
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrConflict) {
			t.Fatalf("race error=%v", err)
		}
	}
	final, err := repo.ConsentForDestination(ctx, salonID, destination)
	if err != nil || final.Status != ConsentOptedOut {
		t.Fatalf("race final=%#v err=%v", final, err)
	}
}

func seedCustomerNotificationConsentFixture(t *testing.T, ctx context.Context, db *sql.DB) (ownerID, salonID, sessionID string) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,full_name) VALUES($1,'test','Customer SMS Owner') RETURNING id::text`, "customer-sms-"+uuid.NewString()+"@example.test").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO salons(name,phone,timezone,owner_user_id) VALUES('Customer SMS Salon','+13125550999','America/Chicago',$1) RETURNING id::text`, ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO salon_settings(salon_id,scheduling_authority,customer_sms_enabled,customer_sms_quiet_start,customer_sms_quiet_end) VALUES($1,'owner_manual',true,'21:00','08:00')`, salonID); err != nil {
		t.Fatalf("insert settings: %v", err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO call_sessions(salon_id,channel,status,intent,outcome,customer_name,customer_phone,inbound_phone,outbound_phone,summary) VALUES($1,'phone','completed','book','completed','Customer','+13125550123','+13125550123','+13125550999','consent evidence') RETURNING id::text`, salonID).Scan(&sessionID); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	return ownerID, salonID, sessionID
}
