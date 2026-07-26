package conversation

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/middleware"
	schedulingretention "github.com/manleai/ai-receptionist/modules/scheduling_retention"
)

const (
	redactionSeedCustomerName = "Priya Redaction Seed"
	redactionSeedDescription  = "Priya's private Tuesday appointment description"
	redactionSeedGuestLabel   = "private guest Priya"
)

func TestPostgresOwnerScopedSessionRedactionClearsConversationJSONBAndRepairsHistoricalRows(t *testing.T) {
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

	ownerA, salonA, sessionA := seedJSONBRedactionSession(t, ctx, db, "a")
	ownerB, salonB, sessionB := seedJSONBRedactionSession(t, ctx, db, "b")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id IN ($1,$2)`, salonA, salonB)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, ownerA, ownerB)
	})

	repository := NewRepository(db)
	app := redactionTestApp(ownerA, repository)

	redactResponse := executeRedactionRequest(t, app, http.MethodPost, "/salons/"+salonA+"/conversation-sessions/"+sessionA+"/redact")
	assertRedactionHTTPStatus(t, redactResponse, fiber.StatusOK)
	redacted := decodeRedactionSession(t, redactResponse)
	assertRedactedSessionResponse(t, redacted)
	assertRedactedSessionRows(t, ctx, db, sessionA)

	getResponse := executeRedactionRequest(t, app, http.MethodGet, "/salons/"+salonA+"/conversation-sessions/"+sessionA)
	assertRedactionHTTPStatus(t, getResponse, fiber.StatusOK)
	assertRedactedSessionResponse(t, decodeRedactionSession(t, getResponse))

	listResponse := executeRedactionRequest(t, app, http.MethodGet, "/salons/"+salonA+"/conversation-sessions?lifecycle_status=redacted")
	assertRedactionHTTPStatus(t, listResponse, fiber.StatusOK)
	var list ListSessionsResponse
	decodeRedactionResponse(t, listResponse, &list)
	if len(list.Sessions) != 1 || list.Sessions[0].ID != sessionA {
		t.Fatalf("redacted session list = %#v", list.Sessions)
	}
	assertRedactedSessionResponse(t, &list.Sessions[0])

	var firstRedactedAt time.Time
	if err := db.QueryRowContext(ctx, `SELECT redacted_at FROM call_sessions WHERE id=$1`, sessionA).Scan(&firstRedactedAt); err != nil {
		t.Fatalf("load first redacted_at: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE call_sessions
		SET dialog_state = $1::jsonb,
		    party_plan = $2::jsonb,
		    booking_segments = $3::jsonb,
		    reschedule_candidates = $4::jsonb
		WHERE id = $5 AND salon_id = $6
	`, redactionDialogStateJSON(), redactionPartyPlanJSON(), redactionBookingSegmentsJSON(), redactionCandidatesJSON(), sessionA, salonA); err != nil {
		t.Fatalf("seed historical residual JSONB: %v", err)
	}

	repairResponse := executeRedactionRequest(t, app, http.MethodPost, "/salons/"+salonA+"/conversation-sessions/"+sessionA+"/redact")
	assertRedactionHTTPStatus(t, repairResponse, fiber.StatusOK)
	assertRedactedSessionResponse(t, decodeRedactionSession(t, repairResponse))
	assertRedactedSessionRows(t, ctx, db, sessionA)
	var repairedRedactedAt time.Time
	if err := db.QueryRowContext(ctx, `SELECT redacted_at FROM call_sessions WHERE id=$1`, sessionA).Scan(&repairedRedactedAt); err != nil {
		t.Fatalf("load repaired redacted_at: %v", err)
	}
	if !repairedRedactedAt.Equal(firstRedactedAt) {
		t.Fatalf("redacted_at changed on idempotent repair: first=%s repaired=%s", firstRedactedAt, repairedRedactedAt)
	}

	crossTenantResponse := executeRedactionRequest(t, app, http.MethodPost, "/salons/"+salonB+"/conversation-sessions/"+sessionB+"/redact")
	assertRedactionHTTPStatus(t, crossTenantResponse, fiber.StatusNotFound)
	assertSeededSessionRows(t, ctx, db, sessionB)
}

func redactionTestApp(ownerUserID string, repository *Repository) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalUserID, ownerUserID)
		return c.Next()
	})
	handler := NewHandler(NewService(repository, nil))
	app.Get("/salons/:id/conversation-sessions", handler.List)
	app.Get("/salons/:id/conversation-sessions/:session_id", handler.Get)
	app.Post("/salons/:id/conversation-sessions/:session_id/redact", handler.Redact)
	return app
}

func executeRedactionRequest(t *testing.T, app *fiber.App, method string, path string) *http.Response {
	t.Helper()
	response, err := app.Test(httptest.NewRequest(method, path, nil))
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return response
}

func assertRedactionHTTPStatus(t *testing.T, response *http.Response, want int) {
	t.Helper()
	if response.StatusCode == want {
		return
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	t.Fatalf("status=%d want=%d body=%s", response.StatusCode, want, body)
}

func decodeRedactionSession(t *testing.T, response *http.Response) *Session {
	t.Helper()
	var session Session
	decodeRedactionResponse(t, response, &session)
	return &session
}

func decodeRedactionResponse(t *testing.T, response *http.Response, destination any) {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	_ = response.Body.Close()
	for _, privateValue := range []string{redactionSeedCustomerName, redactionSeedDescription, redactionSeedGuestLabel} {
		if bytes.Contains(body, []byte(privateValue)) {
			t.Fatalf("response leaked seeded private value %q: %s", privateValue, body)
		}
	}
	if err := json.Unmarshal(body, destination); err != nil {
		t.Fatalf("decode response %s: %v", body, err)
	}
}

func assertRedactedSessionResponse(t *testing.T, session *Session) {
	t.Helper()
	if session == nil {
		t.Fatal("redacted session response is nil")
	}
	if session.LifecycleStatus != LifecycleRedacted || session.RedactedAt == nil ||
		session.CustomerName != "" || session.CustomerPhone != "" || session.CustomerEmail != "" ||
		session.InboundPhone != "" || session.OutboundPhone != "" {
		t.Fatalf("redacted session identity/lifecycle = %#v", session)
	}
	if len(session.RescheduleCandidates) != 0 || len(session.BookingSegments) != 0 || session.PartyPlan != nil {
		t.Fatalf("redacted session retained party/reschedule state: candidates=%#v segments=%#v party=%#v", session.RescheduleCandidates, session.BookingSegments, session.PartyPlan)
	}
	state := session.DialogState
	if state.Pending != nil || state.ManualTarget != nil || state.LastMutation != nil || len(state.MutationHistory) != 0 ||
		state.Consultation != nil || state.Guidance != nil || state.CustomerSMSConsent != nil || state.ProgressFingerprint != "" {
		t.Fatalf("redacted session retained dialog state: %#v", state)
	}
	for _, message := range session.Transcript {
		if message.Body != redactedTranscriptBody || !boolPayload(message.Metadata, "redacted") {
			t.Fatalf("redacted transcript = %#v", message)
		}
	}
	if session.Handoff != nil && (session.Handoff.CustomerName != "" || session.Handoff.CustomerPhone != "" || session.Handoff.Summary != redactedSummaryBody) {
		t.Fatalf("redacted handoff = %#v", session.Handoff)
	}
	if session.PartyRequest != nil && (session.PartyRequest.RepresentativeName != "" || session.PartyRequest.RepresentativePhone != "" ||
		len(session.PartyRequest.GuestServiceRequests) != 0 || session.PartyRequest.FlexibilityNotes != "" || session.PartyRequest.Summary != redactedSummaryBody) {
		t.Fatalf("redacted party request = %#v", session.PartyRequest)
	}
}

func seedJSONBRedactionSession(t *testing.T, ctx context.Context, db *sql.DB, label string) (string, string, string) {
	t.Helper()
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,full_name)
		VALUES($1,'redaction-jsonb-test',$2) RETURNING id::text
	`, "redaction-jsonb-"+label+"-"+uuid.NewString()+"@example.test", "Redaction owner "+label).Scan(&ownerID); err != nil {
		t.Fatalf("insert owner %s: %v", label, err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons(name,phone,timezone,owner_user_id)
		VALUES($1,$2,'America/Chicago',$3) RETURNING id::text
	`, "Redaction JSONB salon "+label, "+13125550"+map[string]string{"a": "141", "b": "142"}[label], ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon %s: %v", label, err)
	}
	var sessionID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO call_sessions(
			salon_id,channel,status,intent,outcome,booking_action,
			customer_name,customer_phone,customer_email,inbound_phone,outbound_phone,summary,
			dialog_state,party_plan,booking_segments,reschedule_candidates,offered_slots
		) VALUES(
			$1,'phone','completed','booking','owner_review_pending','reschedule',
			$2,'+13125550141','private@example.test','+13125550141','+13125550999','private session summary',
			$3::jsonb,$4::jsonb,$5::jsonb,$6::jsonb,
			'[{"staff_name":"Private Staff","service_name":"Private Service","start_time":"2026-07-28T15:00:00Z","end_time":"2026-07-28T15:45:00Z"}]'::jsonb
		) RETURNING id::text
	`, salonID, redactionSeedCustomerName, redactionDialogStateJSON(), redactionPartyPlanJSON(), redactionBookingSegmentsJSON(), redactionCandidatesJSON()).Scan(&sessionID); err != nil {
		t.Fatalf("insert call session %s: %v", label, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO call_transcript_messages(session_id,salon_id,speaker,body,metadata,sequence)
		VALUES($1,$2,'customer',$3,'{"customer_name":"Priya Redaction Seed"}'::jsonb,1)
	`, sessionID, salonID, "My name is "+redactionSeedCustomerName); err != nil {
		t.Fatalf("insert transcript %s: %v", label, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO handoff_requests(salon_id,call_session_id,status,reason,customer_name,customer_phone,summary)
		VALUES($1,$2,'pending','owner_review',$3,'+13125550141','private handoff summary')
	`, salonID, sessionID, redactionSeedCustomerName); err != nil {
		t.Fatalf("insert handoff %s: %v", label, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO party_booking_requests(
			salon_id,call_session_id,event_key,status,party_size,representative_name,
			representative_phone,guest_service_requests,flexibility_notes,summary
		) VALUES($1,$2,$3,'pending',2,$4,'+13125550141',$5::jsonb,'private flexibility','private party summary')
	`, salonID, sessionID, "redaction-jsonb-"+label+"-"+uuid.NewString(), redactionSeedCustomerName,
		`[{"guest_reference":"`+redactionSeedGuestLabel+`","service_name":"Private Service","notes":"private guest note"}]`); err != nil {
		t.Fatalf("insert party request %s: %v", label, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO voice_audio_outputs(
			salon_id,call_session_id,provider,provider_call_id,content_type,audio_data,expires_at
		) VALUES($1,$2,'openai',$3,'audio/mpeg',$4,now()+interval '1 hour')
	`, salonID, sessionID, "redaction-jsonb-audio-"+label+"-"+uuid.NewString(), []byte("private voice bytes for "+label)); err != nil {
		t.Fatalf("insert voice audio %s: %v", label, err)
	}
	return ownerID, salonID, sessionID
}

func redactionDialogStateJSON() string {
	return `{"version":6,"phase":"clarifying","pending":{"kind":"customer_name","value":"` + redactionSeedCustomerName + `","prompt_key":"customer_name"},"manual_appointment_target":{"description":"` + redactionSeedDescription + `"},"progress_fingerprint":"private-progress","draft_revision":3,"reviewed_revision":0,"authorized_revision":0}`
}

func redactionPartyPlanJSON() string {
	return `{"party_size":2,"groups":[{"label":"` + redactionSeedGuestLabel + `","count":2}],"evidence":[{"kind":"caller_text","text":"` + redactionSeedDescription + `"}]}`
}

func redactionBookingSegmentsJSON() string {
	return `[{"service_id":"private-service","guest_reference":"` + redactionSeedGuestLabel + `","quantity":1}]`
}

func redactionCandidatesJSON() string {
	return `[{"appointment_id":"private-appointment","service_label":"Private Service for ` + redactionSeedCustomerName + `","staff_label":"Private Staff","start_time":"2026-07-28T15:00:00Z","end_time":"2026-07-28T15:45:00Z"}]`
}

func assertRedactedSessionRows(t *testing.T, ctx context.Context, db *sql.DB, sessionID string) {
	t.Helper()
	var name, phone, email, inbound, outbound sql.NullString
	var dialogState, partyPlan, bookingSegments, candidates, offeredSlots []byte
	if err := db.QueryRowContext(ctx, `
		SELECT customer_name,customer_phone,customer_email,inbound_phone,outbound_phone,
		       dialog_state,party_plan,booking_segments,reschedule_candidates,offered_slots
		FROM call_sessions WHERE id=$1
	`, sessionID).Scan(&name, &phone, &email, &inbound, &outbound, &dialogState, &partyPlan, &bookingSegments, &candidates, &offeredSlots); err != nil {
		t.Fatalf("load redacted call session: %v", err)
	}
	if name.Valid || phone.Valid || email.Valid || inbound.Valid || outbound.Valid ||
		strings.TrimSpace(string(dialogState)) != "{}" || strings.TrimSpace(string(partyPlan)) != "{}" ||
		strings.TrimSpace(string(bookingSegments)) != "[]" || strings.TrimSpace(string(candidates)) != "[]" || strings.TrimSpace(string(offeredSlots)) != "[]" {
		t.Fatalf("redacted rows retained state: identity=%v/%v/%v/%v/%v dialog=%s party=%s segments=%s candidates=%s offered=%s",
			name, phone, email, inbound, outbound, dialogState, partyPlan, bookingSegments, candidates, offeredSlots)
	}
	var unsafeAudioCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM voice_audio_outputs
		WHERE call_session_id=$1
		  AND (octet_length(audio_data) <> 0 OR redacted_at IS NULL OR redaction_version <> 1)
	`, sessionID).Scan(&unsafeAudioCount); err != nil {
		t.Fatalf("load redacted voice audio: %v", err)
	}
	if unsafeAudioCount != 0 {
		t.Fatalf("redacted call session retained %d unsafe voice audio rows", unsafeAudioCount)
	}
}

func assertSeededSessionRows(t *testing.T, ctx context.Context, db *sql.DB, sessionID string) {
	t.Helper()
	var lifecycleStatus string
	var name string
	var dialogState, partyPlan, bookingSegments, candidates []byte
	if err := db.QueryRowContext(ctx, `
		SELECT lifecycle_status,customer_name,dialog_state,party_plan,booking_segments,reschedule_candidates
		FROM call_sessions WHERE id=$1
	`, sessionID).Scan(&lifecycleStatus, &name, &dialogState, &partyPlan, &bookingSegments, &candidates); err != nil {
		t.Fatalf("load cross-tenant session: %v", err)
	}
	if lifecycleStatus == LifecycleRedacted || name != redactionSeedCustomerName ||
		strings.TrimSpace(string(dialogState)) == "{}" || strings.TrimSpace(string(partyPlan)) == "{}" ||
		strings.TrimSpace(string(bookingSegments)) == "[]" || strings.TrimSpace(string(candidates)) == "[]" {
		t.Fatalf("cross-tenant session was modified: lifecycle=%q name=%q dialog=%s party=%s segments=%s candidates=%s",
			lifecycleStatus, name, dialogState, partyPlan, bookingSegments, candidates)
	}
	var privateAudioCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM voice_audio_outputs
		WHERE call_session_id=$1
		  AND octet_length(audio_data) > 0
		  AND redacted_at IS NULL
	`, sessionID).Scan(&privateAudioCount); err != nil {
		t.Fatalf("load cross-tenant voice audio: %v", err)
	}
	if privateAudioCount != 1 {
		t.Fatalf("cross-tenant voice audio changed: private rows=%d", privateAudioCount)
	}
}

func TestPostgresManualSessionRedactionClearsFutureAudioAndExpiryWorkerIsIdempotent(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	var ownerID, salonID, sessionID, audioID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,full_name)
		VALUES($1,'retention-test','Early Audio Owner') RETURNING id::text
	`, "early-audio-"+uuid.NewString()+"@example.test").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons(name,phone,timezone,owner_user_id)
		VALUES('Early Audio Salon','+13125550119','America/Chicago',$1) RETURNING id::text
	`, ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, ownerID)
	})
	if err := db.QueryRowContext(ctx, `
		INSERT INTO call_sessions(
			salon_id,channel,status,intent,outcome,customer_name,customer_phone,inbound_phone,outbound_phone,summary
		) VALUES($1,'phone','completed','book','completed','Mai Nguyen','+13125550119','+13125550119','+13125550999','private call summary')
		RETURNING id::text
	`, salonID).Scan(&sessionID); err != nil {
		t.Fatalf("insert call session: %v", err)
	}
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.QueryRowContext(ctx, `
		INSERT INTO voice_audio_outputs(
			salon_id,call_session_id,provider,provider_call_id,content_type,audio_data,expires_at,created_at
		) VALUES($1,$2,'openai','call-safe-evidence','audio/mpeg',$3,$4,$5)
		RETURNING id::text
	`, salonID, sessionID, []byte("future private speech"), expiresAt, createdAt).Scan(&audioID); err != nil {
		t.Fatalf("insert voice audio: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin redaction: %v", err)
	}
	if err := redactSessionInTx(ctx, tx, sessionID, salonID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("redact session: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit redaction: %v", err)
	}

	assertEarlyAudioRedacted(t, ctx, db, audioID, expiresAt, createdAt)
	if _, err := db.ExecContext(ctx, `UPDATE voice_audio_outputs SET expires_at=now()-interval '1 second' WHERE id=$1`, audioID); err != nil {
		t.Fatalf("move already-redacted audio past expiry: %v", err)
	}
	if _, err := schedulingretention.NewRepository(db).RedactNext(ctx, schedulingretention.KindVoiceAudio); err != nil {
		t.Fatalf("expiry worker after manual redaction: %v", err)
	}
	assertEarlyAudioRedacted(t, ctx, db, audioID, time.Time{}, createdAt)
	if _, err := db.ExecContext(ctx, `UPDATE voice_audio_outputs SET audio_data='restored'::bytea WHERE id=$1`, audioID); err == nil {
		t.Fatal("manual audio redaction was reversible")
	}
}

func assertEarlyAudioRedacted(t *testing.T, ctx context.Context, db *sql.DB, audioID string, expectedExpiry, expectedCreated time.Time) {
	t.Helper()
	var body []byte
	var provider, providerCallID, contentType string
	var expiresAt, createdAt time.Time
	var redactedAt sql.NullTime
	var version sql.NullInt64
	if err := db.QueryRowContext(ctx, `
		SELECT audio_data,provider,provider_call_id,content_type,expires_at,created_at,redacted_at,redaction_version
		FROM voice_audio_outputs WHERE id=$1
	`, audioID).Scan(&body, &provider, &providerCallID, &contentType, &expiresAt, &createdAt, &redactedAt, &version); err != nil {
		t.Fatalf("load redacted audio: %v", err)
	}
	if len(body) != 0 || provider != "openai" || providerCallID != "call-safe-evidence" || contentType != "audio/mpeg" ||
		!redactedAt.Valid || !version.Valid || version.Int64 != schedulingretention.PolicyVersion {
		t.Fatalf("audio redaction bytes=%d provider=%q call=%q content=%q redacted=%v version=%v", len(body), provider, providerCallID, contentType, redactedAt.Valid, version)
	}
	if !expectedExpiry.IsZero() && !expiresAt.Equal(expectedExpiry) {
		t.Fatalf("audio expiry=%v want %v", expiresAt, expectedExpiry)
	}
	if !createdAt.Equal(expectedCreated) {
		t.Fatalf("audio created_at=%v want %v", createdAt, expectedCreated)
	}
}
