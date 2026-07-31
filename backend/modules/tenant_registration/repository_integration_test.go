package tenant_registration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/internal/databasecontext"
	"github.com/manleai/ai-receptionist/internal/middleware"
	"github.com/manleai/ai-receptionist/modules/access"
)

func TestRepositoryRegistrationReplayAuditRetentionAndImmutability(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	adminDB, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	runtimeDB, err := database.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open context database: %v", err)
	}
	t.Cleanup(func() { _ = runtimeDB.Close() })

	actorID := insertRegistrationPlatformActor(t, adminDB, "platform_admin")
	actor := middleware.ActorContext{UserID: actorID, PrincipalScope: middleware.PrincipalScopePlatform}
	platformCtx := databasecontext.WithActor(context.Background(), actorID)
	publicCtx := databasecontext.WithScope(context.Background(), databasecontext.ScopePublic)
	workerCtx := databasecontext.WithScope(context.Background(), databasecontext.ScopeWorker)
	repository := NewRepository(runtimeDB)
	service := NewService(repository, registrationAuthorizer{allowed: access.CapabilityRegistrationManage})

	request := validSubmission()
	request.SubmissionKey = uuid.NewString()
	request.ContactEmail = "registration-" + uuid.NewString() + "@example.test"
	created, err := service.Submit(publicCtx, request)
	if err != nil || created.Replayed || created.RequestReference == "" {
		t.Fatalf("create=%#v error=%v", created, err)
	}
	replay, err := service.Submit(publicCtx, request)
	if err != nil || !replay.Replayed || replay.RequestReference != created.RequestReference {
		t.Fatalf("replay=%#v error=%v", replay, err)
	}
	changed := request
	changed.Notes = "changed payload"
	if _, err := service.Submit(publicCtx, changed); !errors.Is(err, ErrSubmissionConflict) {
		t.Fatalf("changed submission key error=%v", err)
	}

	duplicate := request
	duplicate.SubmissionKey = uuid.NewString()
	duplicate.SalonName = "Separate Duplicate Evidence Salon"
	duplicateResult, err := service.Submit(publicCtx, duplicate)
	if err != nil || duplicateResult.Replayed || duplicateResult.RequestReference == created.RequestReference {
		t.Fatalf("separate duplicate=%#v error=%v", duplicateResult, err)
	}
	items, _, _, err := repository.List(platformCtx, ListFilter{Query: created.RequestReference, Limit: 10})
	if err != nil || len(items) != 1 {
		t.Fatalf("list items=%#v error=%v", items, err)
	}
	item := items[0]
	if item.ContactEmailMasked == request.ContactEmail || item.ContactPhoneMasked == request.ContactPhone || item.SalonPhoneMasked == request.SalonPhone {
		t.Fatalf("list leaked raw contact data: %#v", item)
	}
	duplicateItems, _, _, err := repository.List(platformCtx, ListFilter{Query: duplicateResult.RequestReference, Limit: 10})
	if err != nil || len(duplicateItems) != 1 || !duplicateItems[0].PossibleDuplicate || duplicateItems[0].ID == item.ID {
		t.Fatalf("duplicate evidence items=%#v error=%v", duplicateItems, err)
	}

	noteRequest := AddNoteRequest{ActionKey: "note-" + uuid.NewString(), ExpectedVersion: 1, Content: "Verified callback window only."}
	note, err := service.AddNote(platformCtx, actor, item.ID, noteRequest)
	if err != nil || note.Replayed || note.Version != 2 {
		t.Fatalf("add note=%#v error=%v", note, err)
	}
	noteReplay, err := service.AddNote(platformCtx, actor, item.ID, noteRequest)
	if err != nil || !noteReplay.Replayed || noteReplay.NoteID != note.NoteID {
		t.Fatalf("note replay=%#v error=%v", noteReplay, err)
	}
	noteChanged := noteRequest
	noteChanged.Content = "Changed action-key payload."
	if _, err := service.AddNote(platformCtx, actor, item.ID, noteChanged); !errors.Is(err, ErrActionConflict) {
		t.Fatalf("changed note action error=%v", err)
	}
	status := StatusDeclined
	if _, err := service.Mutate(platformCtx, actor, item.ID, MutationRequest{ActionKey: "stale-" + uuid.NewString(), ExpectedVersion: 1, Status: &status}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale mutation error=%v", err)
	}
	converted := StatusConverted
	if _, err := service.Mutate(platformCtx, actor, item.ID, MutationRequest{ActionKey: "generic-convert-" + uuid.NewString(), ExpectedVersion: 2, Status: &converted}); !errors.Is(err, ErrTransition) {
		t.Fatalf("generic converted transition error=%v", err)
	}
	terminal, err := service.Mutate(platformCtx, actor, item.ID, MutationRequest{ActionKey: "decline-" + uuid.NewString(), ExpectedVersion: 2, Status: &status})
	if err != nil || terminal.Version != 3 || terminal.Status != StatusDeclined {
		t.Fatalf("terminal mutation=%#v error=%v", terminal, err)
	}
	empty := ""
	if _, err := service.Mutate(platformCtx, actor, item.ID, MutationRequest{ActionKey: "reopen-" + uuid.NewString(), ExpectedVersion: 3, AssignedToUserID: &empty}); !errors.Is(err, ErrTerminal) {
		t.Fatalf("terminal generic patch error=%v", err)
	}
	if _, err := service.AddNote(platformCtx, actor, item.ID, AddNoteRequest{ActionKey: "terminal-note-" + uuid.NewString(), ExpectedVersion: 3, Content: "must fail"}); !errors.Is(err, ErrTerminal) {
		t.Fatalf("terminal note error=%v", err)
	}

	if _, err := adminDB.Exec(`UPDATE tenant_registration_request_events SET details='{}' WHERE request_id=$1`, item.ID); err == nil {
		t.Fatal("immutable event accepted an update")
	}
	if _, err := adminDB.Exec(`UPDATE tenant_registration_request_notes SET content='changed' WHERE id=$1`, note.NoteID); err == nil {
		t.Fatal("immutable note accepted an update")
	}
	var auditText string
	if err := adminDB.QueryRow(`SELECT string_agg(details::text,' ') FROM tenant_registration_request_events WHERE request_id=$1`, item.ID).Scan(&auditText); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	for _, forbidden := range []string{request.ContactEmail, request.ContactPhone, request.SalonPhone, noteRequest.Content} {
		if strings.Contains(auditText, forbidden) {
			t.Fatalf("audit details leaked %q: %s", forbidden, auditText)
		}
	}

	if _, err := adminDB.Exec(`UPDATE tenant_registration_requests SET retention_expires_at=now()-interval '1 second' WHERE id=$1`, item.ID); err != nil {
		t.Fatalf("make terminal request due: %v", err)
	}
	if count, err := service.RedactDue(workerCtx, 100); err != nil || count < 1 {
		t.Fatalf("redact count=%d error=%v", count, err)
	}
	redacted, err := repository.Get(platformCtx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if redacted.RedactedAt == nil || redacted.ContactEmail != "" || redacted.ContactPhone != "" || redacted.SalonName != "" || len(redacted.InternalNotes) != 1 || redacted.InternalNotes[0].Content != "" || redacted.InternalNotes[0].RedactedAt == nil {
		t.Fatalf("terminal PII was not fully redacted: %#v", redacted)
	}
	if count, err := service.RedactDue(workerCtx, 100); err != nil || count != 0 {
		t.Fatalf("repeated redaction count=%d error=%v", count, err)
	}
	active, err := repository.Get(platformCtx, duplicateItems[0].ID)
	if err != nil || active.RedactedAt != nil || active.ContactEmail == "" {
		t.Fatalf("active request was redacted: %#v error=%v", active, err)
	}
}

func insertRegistrationPlatformActor(t *testing.T, db *sql.DB, role string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`INSERT INTO users(email,password_hash,full_name,status,principal_scope) VALUES($1,'integration-test','Registration Platform Actor','active','platform') RETURNING id::text`, "registration-platform-"+uuid.NewString()+"@example.test").Scan(&id); err != nil {
		t.Fatalf("insert platform actor: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO platform_role_assignments(user_id,role_id,created_by_user_id,updated_by_user_id) SELECT $1,id,$1,$1 FROM roles WHERE name=$2`, id, role); err != nil {
		t.Fatalf("assign platform role: %v", err)
	}
	return id
}
