package conversation

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/database"
)

type informationalContractTenant struct {
	ownerID string
	salonID string
}

type informationalCatalogFixture struct {
	categoryID  string
	serviceID   string
	staffID     string
	knowledgeID string
}

func TestPostgresConversationMessageOwnerManualCommonResourceFreshness(t *testing.T) {
	db := openInformationalContractDatabase(t)
	ctx := context.Background()
	tenant := insertInformationalContractTenant(t, ctx, db, "owner-resources")
	other := insertInformationalContractTenant(t, ctx, db, "owner-isolation")

	repository := NewRepository(db)
	toolA := newOwnerManualSchedulingTool("unused-owner-a")
	toolB := newOwnerManualSchedulingTool("unused-owner-b")
	serviceA := NewService(repository, toolA)
	serviceB := NewService(repository, toolB)
	serviceA.now = fixedNow
	serviceB.now = fixedNow

	if err := serviceA.PrewarmAnswerContext(ctx, tenant.salonID); err != nil {
		t.Fatalf("prewarm owner service A: %v", err)
	}
	if err := serviceB.PrewarmAnswerContext(ctx, tenant.salonID); err != nil {
		t.Fatalf("prewarm owner service B: %v", err)
	}

	fixture := insertInformationalCatalogFixture(t, ctx, db, tenant.salonID)
	inserted, _, _ := askInformationalQuestion(
		t, ctx, serviceA, tenant, ChannelSimulator,
		"Please describe the treatment menu your studio maintains.",
		testQuestionUnderstanding(ConversationQuestionCatalog, ConversationQuestionModeList),
		"owner-service-insert",
	)
	assertReplyContains(t, inserted, "Quartz Manicure")
	assertAnswerMetadata(t, inserted, "owner-service-insert", answerSourceServiceCatalog, "service_menu", "service_menu", true)

	if _, err := db.ExecContext(ctx, `
		UPDATE services
		SET name = 'Mineral Manicure', price_from = 53, price_display = '$53 and up'
		WHERE id = $1 AND salon_id = $2
	`, fixture.serviceID, tenant.salonID); err != nil {
		t.Fatalf("update owner service: %v", err)
	}
	updated, _, _ := askInformationalQuestion(
		t, ctx, serviceB, tenant, ChannelSimulator,
		"What does the refreshed manicure option cost?",
		testQuestionUnderstanding(ConversationQuestionPrice, "", fixture.serviceID),
		"owner-service-update",
	)
	assertReplyContains(t, updated, "Mineral Manicure is $53 and up")
	assertAnswerMetadata(t, updated, "owner-service-update", answerSourceServiceCatalog, "service_price", "service_price", true)

	if _, err := db.ExecContext(ctx, `
		UPDATE service_aliases
		SET status = 'archived'
		WHERE salon_id = $1 AND service_id = $2
	`, tenant.salonID, fixture.serviceID); err != nil {
		t.Fatalf("archive prior owner service alias: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_aliases (salon_id, service_id, alias, normalized_alias, source, status)
		VALUES ($1, $2, 'crystal refresh', 'crystal refresh', 'owner', 'active')
	`, tenant.salonID, fixture.serviceID); err != nil {
		t.Fatalf("insert replacement owner service alias: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE service_categories SET name = 'Hand Artistry' WHERE id = $1 AND salon_id = $2`, fixture.categoryID, tenant.salonID); err != nil {
		t.Fatalf("update owner category: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE service_category_aliases
		SET status = 'archived'
		WHERE salon_id = $2 AND category_id = $1
	`, fixture.categoryID, tenant.salonID); err != nil {
		t.Fatalf("archive prior owner category alias: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_category_aliases (salon_id, category_id, alias, normalized_alias, source, status)
		VALUES ($1, $2, 'hand rituals', 'hand rituals', 'owner', 'active')
	`, tenant.salonID, fixture.categoryID); err != nil {
		t.Fatalf("insert replacement owner category alias: %v", err)
	}
	aliasAnswer, _, aliasInterpreter := askInformationalQuestion(
		t, ctx, serviceA, tenant, ChannelSimulator,
		"Could you explain the crystal refresh listed with your hand rituals?",
		testQuestionUnderstanding(ConversationQuestionCatalog, ConversationQuestionModeDetails, fixture.serviceID),
		"owner-alias-category-update",
	)
	assertReplyContains(t, aliasAnswer, "Mineral Manicure")
	assertInterpreterCatalogEvidence(t, aliasInterpreter.request, fixture.serviceID, "crystal refresh", fixture.categoryID, "Hand Artistry", "hand rituals")

	if _, err := db.ExecContext(ctx, `
		UPDATE staff SET name = 'Rina Cole' WHERE id = $1 AND salon_id = $2
	`, fixture.staffID, tenant.salonID); err != nil {
		t.Fatalf("update owner staff: %v", err)
	}
	staffAnswerSession, _, _ := askInformationalQuestion(
		t, ctx, serviceB, tenant, ChannelSimulator,
		"Which technicians are part of the current team roster?",
		testQuestionUnderstanding(ConversationQuestionStaff, ""),
		"owner-staff-update",
	)
	assertReplyContains(t, staffAnswerSession, "Rina Cole")
	assertAnswerMetadata(t, staffAnswerSession, "owner-staff-update", answerSourceStaff, "staff_question", "staff_question", true)

	if _, err := db.ExecContext(ctx, `
		UPDATE knowledge_items
		SET body = 'Guests may use the covered garage behind the library after 4 PM.'
		WHERE id = $1 AND salon_id = $2
	`, fixture.knowledgeID, tenant.salonID); err != nil {
		t.Fatalf("update owner knowledge: %v", err)
	}
	knowledgeAnswer, _, _ := askInformationalQuestion(
		t, ctx, serviceA, tenant, ChannelSimulator,
		"What is the current parking policy for guests using the garage?",
		testQuestionUnderstanding(ConversationQuestionPolicy, ""),
		"owner-knowledge-update",
	)
	assertReplyContains(t, knowledgeAnswer, "covered garage behind the library after 4 PM")
	assertAnswerMetadata(t, knowledgeAnswer, "owner-knowledge-update", answerSourceKnowledge, "knowledge_match", "knowledge_question", true)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time,
			end_at_midnight, source, provider_period_index
		) VALUES
			($1, 4, TIME '08:30', TIME '12:15', false, 'local_override', 1),
			($1, 4, TIME '13:45', TIME '19:15', false, 'local_override', 2),
			($1, 6, TIME '17:30', TIME '00:00', true, 'local_override', 1)
	`, tenant.salonID); err != nil {
		t.Fatalf("insert owner local hours: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time, source, provider_period_index
		) VALUES ($1, 4, TIME '10:15', TIME '15:45', 'local_override', 1)
	`, other.salonID); err != nil {
		t.Fatalf("insert isolated tenant local hours: %v", err)
	}
	ownerHours, _, _ := askInformationalQuestion(
		t, ctx, serviceB, tenant, ChannelSimulator,
		"Please share the Thursday operating schedule.",
		testQuestionUnderstanding(ConversationQuestionHours, ""),
		"owner-hours-split",
	)
	assertReplyContains(t, ownerHours, "8:30 AM to 12:15 PM and 1:45 PM to 7:15 PM")
	assertAnswerMetadata(t, ownerHours, "owner-hours-split", answerSourceBusinessHours, "business_hours", "hours_question", true)

	midnightHours, _, _ := askInformationalQuestion(
		t, ctx, serviceA, tenant, ChannelSimulator,
		"How late does the Saturday operating schedule run?",
		testQuestionUnderstanding(ConversationQuestionHours, ""),
		"owner-hours-midnight",
	)
	assertReplyContains(t, midnightHours, "5:30 PM to 12:00 AM")

	closedDay, _, _ := askInformationalQuestion(
		t, ctx, serviceB, tenant, ChannelSimulator,
		"What operating schedule is listed for Wednesday?",
		testQuestionUnderstanding(ConversationQuestionHours, ""),
		"owner-hours-closed",
	)
	assertReplyContains(t, closedDay, "do not see open business hours for Wednesday")

	otherRepository := NewRepository(db)
	otherService := NewService(otherRepository, newOwnerManualSchedulingTool("unused-owner-other"))
	otherService.now = fixedNow
	isolatedHours, _, _ := askInformationalQuestion(
		t, ctx, otherService, other, ChannelSimulator,
		"Please share the Thursday operating schedule.",
		testQuestionUnderstanding(ConversationQuestionHours, ""),
		"owner-hours-isolation",
	)
	assertReplyContains(t, isolatedHours, "10:15 AM to 3:45 PM")
	assertReplyNotContains(t, isolatedHours, "8:30 AM")

	if _, err := db.ExecContext(ctx, `
		DELETE FROM salon_business_hour_periods WHERE salon_id = $1 AND source = 'local_override'
	`, tenant.salonID); err != nil {
		t.Fatalf("delete owner local hours: %v", err)
	}
	missingHours, _, _ := askInformationalQuestion(
		t, ctx, serviceA, tenant, ChannelSimulator,
		"Has the studio published an operating schedule yet?",
		testQuestionUnderstanding(ConversationQuestionHours, ""),
		"owner-hours-missing",
	)
	assertReplyContains(t, missingHours, "Business hours have not been configured for this salon yet")
	assertReplyNotContains(t, missingHours, "POS")

	if _, err := db.ExecContext(ctx, `
		UPDATE services SET archived_at = now() WHERE id = $1 AND salon_id = $2
	`, fixture.serviceID, tenant.salonID); err != nil {
		t.Fatalf("archive owner service: %v", err)
	}
	archived, _, _ := askInformationalQuestion(
		t, ctx, serviceB, tenant, ChannelSimulator,
		"Please describe the treatment menu your studio maintains.",
		testQuestionUnderstanding(ConversationQuestionCatalog, ConversationQuestionModeList),
		"owner-service-archive",
	)
	assertReplyNotContains(t, archived, "Mineral Manicure")

	assertNoInformationalSchedulingCalls(t, toolA)
	assertNoInformationalSchedulingCalls(t, toolB)
}

func TestPostgresConversationMessageRuntimeGreetingToneAndProfileFreshness(t *testing.T) {
	db := openInformationalContractDatabase(t)
	ctx := context.Background()
	tenant := insertInformationalContractTenant(t, ctx, db, "runtime-profile")
	fixture := insertInformationalCatalogFixture(t, ctx, db, tenant.salonID)

	repository := NewRepository(db)
	toolA := newOwnerManualSchedulingTool("unused-runtime-a")
	toolB := newOwnerManualSchedulingTool("unused-runtime-b")
	serviceA := NewService(repository, toolA)
	serviceB := NewService(repository, toolB)
	if err := serviceA.PrewarmAnswerContext(ctx, tenant.salonID); err != nil {
		t.Fatalf("prewarm runtime service A: %v", err)
	}
	if err := serviceB.PrewarmAnswerContext(ctx, tenant.salonID); err != nil {
		t.Fatalf("prewarm runtime service B: %v", err)
	}

	initialSession, err := serviceA.Start(ctx, tenant.salonID, tenant.ownerID, StartSessionRequest{Channel: ChannelSimulator})
	if err != nil {
		t.Fatalf("start initial runtime session: %v", err)
	}
	assertReplyContains(t, initialSession, "Prism Nail Studio")
	assertReplyContains(t, initialSession, "How may I help")

	firstGenerator := &fakeReplyGenerator{message: "Which current nail system best describes what you have?"}
	serviceA.SetReplyGenerator(firstGenerator)
	serviceA.SetTurnInterpreter(&fakeConversationActInterpreter{turn: consultationFreshnessUnderstanding()})
	firstConsultation, err := serviceA.Message(ctx, tenant.salonID, tenant.ownerID, initialSession.ID, MessageRequest{
		Message:  "I need guidance for shortening a structured set with easier upkeep.",
		EventKey: "runtime-profile-before",
	})
	if err != nil {
		t.Fatalf("initial consultation Message: %v", err)
	}
	if firstGenerator.lastConsultationRequest.AITone != "professional_warm" ||
		firstGenerator.lastConsultationRequest.Question.ProfileRevisions[fixture.serviceID] != 1 {
		t.Fatalf("initial tone/profile request = %#v", firstGenerator.lastConsultationRequest)
	}
	assertReplyContains(t, firstConsultation, firstGenerator.message)

	if _, err := db.ExecContext(ctx, `UPDATE salons SET name = 'Aurora Nail Atelier' WHERE id = $1`, tenant.salonID); err != nil {
		t.Fatalf("mutate runtime salon name: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE salon_settings
		SET ai_greeting = 'Welcome. What can I help you explore today?', ai_tone = 'concise_calm'
		WHERE salon_id = $1
	`, tenant.salonID); err != nil {
		t.Fatalf("mutate runtime greeting/tone: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE service_consultation_profiles
		SET compatible_current_systems = '["gel","dip"]'::jsonb,
		    revision = 2,
		    owner_approved_summary = 'Owner-approved guidance updated for the current catalog.'
		WHERE salon_id = $1 AND service_id = $2
	`, tenant.salonID, fixture.serviceID); err != nil {
		t.Fatalf("mutate runtime consultation profile: %v", err)
	}

	updatedSession, err := serviceB.Start(ctx, tenant.salonID, tenant.ownerID, StartSessionRequest{Channel: ChannelSimulator})
	if err != nil {
		t.Fatalf("start updated runtime session: %v", err)
	}
	assertReplyContains(t, updatedSession, "Aurora Nail Atelier")
	assertReplyContains(t, updatedSession, "What can I help you explore today")
	assertReplyNotContains(t, updatedSession, "Prism Nail Studio")

	secondGenerator := &fakeReplyGenerator{message: "Is the current system gel or dip?"}
	serviceB.SetReplyGenerator(secondGenerator)
	serviceB.SetTurnInterpreter(&fakeConversationActInterpreter{turn: consultationFreshnessUnderstanding()})
	updatedConsultation, err := serviceB.Message(ctx, tenant.salonID, tenant.ownerID, updatedSession.ID, MessageRequest{
		Message:  "Could you advise me on a shorter, lower-maintenance structured service?",
		EventKey: "runtime-profile-after",
	})
	if err != nil {
		t.Fatalf("updated consultation Message: %v", err)
	}
	request := secondGenerator.lastConsultationRequest
	if request.AITone != "concise_calm" || request.Question.ProfileRevisions[fixture.serviceID] != 2 ||
		!sameStrings(request.Question.Options, []string{ConsultationSystemGel, ConsultationSystemDip}) {
		t.Fatalf("updated tone/profile request = %#v", request)
	}
	assertReplyContains(t, updatedConsultation, secondGenerator.message)
	assertNoInformationalSchedulingCalls(t, toolA)
	assertNoInformationalSchedulingCalls(t, toolB)
}

func TestPostgresConversationMessageAuthorityHoursMatrixAndReplay(t *testing.T) {
	db := openInformationalContractDatabase(t)
	ctx := context.Background()

	t.Run("external provider and authority switch", func(t *testing.T) {
		tenant := insertInformationalContractTenant(t, ctx, db, "external-matrix")
		fixture := insertExternalProviderFixture(t, ctx, db, tenant)
		repository := NewRepository(db)
		toolA := newOwnerManualSchedulingTool("unused-external-a")
		toolB := newOwnerManualSchedulingTool("unused-external-b")
		serviceA := NewService(repository, toolA)
		serviceB := NewService(repository, toolB)
		serviceA.now = fixedNow
		serviceB.now = fixedNow
		if err := serviceA.PrewarmAnswerContext(ctx, tenant.salonID); err != nil {
			t.Fatalf("prewarm external service A: %v", err)
		}
		if err := serviceB.PrewarmAnswerContext(ctx, tenant.salonID); err != nil {
			t.Fatalf("prewarm external service B: %v", err)
		}

		externalCatalog, _, externalCatalogInterpreter := askInformationalQuestion(
			t, ctx, serviceA, tenant, ChannelSimulator,
			"Could you explain the provider polish care under the provider hand menu?",
			testQuestionUnderstanding(ConversationQuestionCatalog, ConversationQuestionModeDetails, fixture.serviceID),
			"external-catalog",
		)
		assertReplyContains(t, externalCatalog, "Provider Mineral Care")
		assertInterpreterCatalogEvidence(t, externalCatalogInterpreter.request, fixture.serviceID, "provider polish care", fixture.categoryID, "Provider Hands", "provider hand menu")

		externalStaff, _, _ := askInformationalQuestion(
			t, ctx, serviceB, tenant, ChannelSimulator,
			"Which technicians are enabled in the current provider roster?",
			testQuestionUnderstanding(ConversationQuestionStaff, ""),
			"external-staff",
		)
		assertReplyContains(t, externalStaff, "Laila Monroe")
		assertAnswerMetadata(t, externalStaff, "external-staff", answerSourceStaff, "staff_question", "staff_question", true)

		externalKnowledge, _, _ := askInformationalQuestion(
			t, ctx, serviceA, tenant, ChannelSimulator,
			"What is the studio's current late-arrival policy?",
			testQuestionUnderstanding(ConversationQuestionPolicy, ""),
			"external-knowledge",
		)
		assertReplyContains(t, externalKnowledge, "ten-minute grace period")

		simulator, _, _ := askInformationalQuestion(
			t, ctx, serviceA, tenant, ChannelSimulator,
			"Please provide Friday's operating schedule.",
			testQuestionUnderstanding(ConversationQuestionHours, ""),
			"external-hours-simulator",
		)
		assertReplyContains(t, simulator, "8:00 AM to 5:00 PM")
		assertReplyNotContains(t, simulator, "11:00 AM")

		phone, _, phoneInterpreter := askInformationalQuestion(
			t, ctx, serviceA, tenant, ChannelPhone,
			"Please provide Friday's operating schedule.",
			testQuestionUnderstanding(ConversationQuestionHours, ""),
			"external-hours-phone",
		)
		if lastAIReply(phone) != lastAIReply(simulator) {
			t.Fatalf("simulator/phone informational parity mismatch: simulator=%q phone=%q", lastAIReply(simulator), lastAIReply(phone))
		}
		beforeReplayCount := len(phone.Transcript)
		replayed, err := serviceA.Message(ctx, tenant.salonID, tenant.ownerID, phone.ID, MessageRequest{
			Message: "This duplicate wording must not create another turn.", EventKey: "external-hours-phone",
		})
		if err != nil {
			t.Fatalf("replay external phone event: %v", err)
		}
		if replayed.ReplayAIMessage != lastAIReply(phone) || len(replayed.Transcript) != beforeReplayCount || phoneInterpreter.calls != 1 {
			t.Fatalf("exact replay evidence = reply %q transcript %d/%d interpreter_calls=%d", replayed.ReplayAIMessage, len(replayed.Transcript), beforeReplayCount, phoneInterpreter.calls)
		}

		if _, err := db.ExecContext(ctx, `
			UPDATE salon_business_hour_periods
			SET end_local_time = TIME '13:30'
			WHERE salon_id = $1 AND source = 'local_override'
		`, tenant.salonID); err != nil {
			t.Fatalf("mutate authority-irrelevant local hours: %v", err)
		}
		localIgnored, _, _ := askInformationalQuestion(
			t, ctx, serviceB, tenant, ChannelSimulator,
			"Please provide Friday's operating schedule.",
			testQuestionUnderstanding(ConversationQuestionHours, ""),
			"external-local-ignored",
		)
		assertReplyContains(t, localIgnored, "8:00 AM to 5:00 PM")
		assertMetadataBool(t, localIgnored, "external-local-ignored", "answer_context_cache_hit", true)

		if _, err := db.ExecContext(ctx, `
			UPDATE pos_connections SET status = 'syncing' WHERE salon_id = $1 AND provider = 'square'
		`, tenant.salonID); err != nil {
			t.Fatalf("set provider syncing: %v", err)
		}
		providerNotReady, _, _ := askInformationalQuestion(
			t, ctx, serviceA, tenant, ChannelSimulator,
			"What operating schedule is currently verified for Friday?",
			testQuestionUnderstanding(ConversationQuestionHours, ""),
			"external-provider-not-ready",
		)
		assertReplyContains(t, providerNotReady, "do not have synced business hours from the POS yet")
		assertReplyNotContains(t, providerNotReady, "11:00 AM")

		if _, err := db.ExecContext(ctx, `
			INSERT INTO salon_business_hour_periods (
				salon_id, day_of_week, start_local_time, end_local_time, source,
				provider, provider_location_id, provider_period_index, last_synced_at
			) VALUES ($1, 5, TIME '09:15', TIME '16:45', 'imported', 'square', 'location-west', 1, now())
		`, tenant.salonID); err != nil {
			t.Fatalf("insert replacement provider hours: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE pos_connections
			SET status = 'active', location_id = 'location-west', snapshot_generation = 12, last_sync_at = now()
			WHERE salon_id = $1 AND provider = 'square'
		`, tenant.salonID); err != nil {
			t.Fatalf("switch provider snapshot location: %v", err)
		}
		newSnapshot, _, _ := askInformationalQuestion(
			t, ctx, serviceB, tenant, ChannelSimulator,
			"What operating schedule is currently verified for Friday?",
			testQuestionUnderstanding(ConversationQuestionHours, ""),
			"external-location-switch",
		)
		assertReplyContains(t, newSnapshot, "9:15 AM to 4:45 PM")
		assertReplyNotContains(t, newSnapshot, "8:00 AM")

		if _, err := db.ExecContext(ctx, `
			UPDATE salon_settings
			SET scheduling_authority = 'owner_manual', booking_mode = 'pending_approval'
			WHERE salon_id = $1
		`, tenant.salonID); err != nil {
			t.Fatalf("switch external tenant to owner manual: %v", err)
		}
		ownerAfterSwitch, _, _ := askInformationalQuestion(
			t, ctx, serviceA, tenant, ChannelSimulator,
			"What operating schedule is currently verified for Friday?",
			testQuestionUnderstanding(ConversationQuestionHours, ""),
			"external-to-owner-switch",
		)
		assertReplyContains(t, ownerAfterSwitch, "11:00 AM to 1:30 PM")
		assertReplyNotContains(t, ownerAfterSwitch, "9:15 AM")
		if fixture.serviceID == "" || fixture.staffID == "" {
			t.Fatal("external fixture did not persist provider catalog evidence")
		}
		assertNoInformationalSchedulingCalls(t, toolA)
		assertNoInformationalSchedulingCalls(t, toolB)
	})

	t.Run("ManleAI Calendar activation fence", func(t *testing.T) {
		tenant := insertInformationalContractTenant(t, ctx, db, "calendar-matrix")
		resourcePoolID := insertReadyManleAICalendarFixture(t, ctx, db, tenant)
		repository := NewRepository(db)
		toolA := newOwnerManualSchedulingTool("unused-calendar-a")
		toolB := newOwnerManualSchedulingTool("unused-calendar-b")
		serviceA := NewService(repository, toolA)
		serviceB := NewService(repository, toolB)
		serviceA.now = fixedNow
		serviceB.now = fixedNow
		if err := serviceA.PrewarmAnswerContext(ctx, tenant.salonID); err != nil {
			t.Fatalf("prewarm calendar service A: %v", err)
		}
		if err := serviceB.PrewarmAnswerContext(ctx, tenant.salonID); err != nil {
			t.Fatalf("prewarm calendar service B: %v", err)
		}

		internalCatalog, _, _ := askInformationalQuestion(
			t, ctx, serviceA, tenant, ChannelSimulator,
			"Please describe the treatment menu maintained for this studio.",
			testQuestionUnderstanding(ConversationQuestionCatalog, ConversationQuestionModeList),
			"calendar-catalog-ready",
		)
		assertReplyContains(t, internalCatalog, "Internal Structure Care")

		ready, _, _ := askInformationalQuestion(
			t, ctx, serviceA, tenant, ChannelSimulator,
			"Please provide Saturday's operating schedule.",
			testQuestionUnderstanding(ConversationQuestionHours, ""),
			"calendar-hours-ready",
		)
		assertReplyContains(t, ready, "10:00 AM to 6:00 PM")

		staffReady, _, _ := askInformationalQuestion(
			t, ctx, serviceB, tenant, ChannelSimulator,
			"Which technicians are configured for the internal schedule?",
			testQuestionUnderstanding(ConversationQuestionStaff, ""),
			"calendar-staff-ready",
		)
		assertReplyContains(t, staffReady, "Noelle Park")

		if _, err := db.ExecContext(ctx, `
			INSERT INTO manleai_calendar_exceptions (
				salon_id, scope_type, resource_pool_id, effect, starts_at, ends_at,
				capacity_override, reason, created_by_user_id
			) VALUES (
				$1, 'resource', $2, 'capacity_override', now() + interval '3 days',
				now() + interval '3 days 2 hours', 1, 'Bounded acceptance fixture', $3
			)
		`, tenant.salonID, resourcePoolID, tenant.ownerID); err != nil {
			t.Fatalf("stale calendar activation: %v", err)
		}
		stale, _, _ := askInformationalQuestion(
			t, ctx, serviceA, tenant, ChannelSimulator,
			"What does the internal Saturday operating schedule say now?",
			testQuestionUnderstanding(ConversationQuestionHours, ""),
			"calendar-hours-stale",
		)
		assertReplyContains(t, stale, "Current internal business hours are not available yet")
		assertReplyNotContains(t, stale, "10:00 AM")

		if _, err := db.ExecContext(ctx, `
			UPDATE manleai_calendar_configs
			SET activated_at = now(), activated_by_user_id = $2
			WHERE salon_id = $1
		`, tenant.salonID, tenant.ownerID); err != nil {
			t.Fatalf("reactivate calendar configuration: %v", err)
		}
		reactivated, _, _ := askInformationalQuestion(
			t, ctx, serviceB, tenant, ChannelSimulator,
			"What does the internal Saturday operating schedule say now?",
			testQuestionUnderstanding(ConversationQuestionHours, ""),
			"calendar-hours-reactivated",
		)
		assertReplyContains(t, reactivated, "10:00 AM to 6:00 PM")
		assertNoInformationalSchedulingCalls(t, toolA)
		assertNoInformationalSchedulingCalls(t, toolB)
	})
}

type fenceMutationStore struct {
	*Repository
	db         *sql.DB
	salonID    string
	mutateOnce sync.Once
	fenceReads int
}

func (s *fenceMutationStore) GetAnswerContextFence(ctx context.Context, salonID string) (AnswerContextFence, error) {
	s.fenceReads++
	return s.Repository.GetAnswerContextFence(ctx, salonID)
}

func (s *fenceMutationStore) ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error) {
	var mutationErr error
	s.mutateOnce.Do(func() {
		_, mutationErr = s.db.ExecContext(ctx, `
			UPDATE knowledge_items
			SET body = 'Guests should arrive twelve minutes early for the preparation policy.'
			WHERE salon_id = $1 AND title = 'Arrival preparation'
		`, s.salonID)
	})
	if mutationErr != nil {
		return nil, mutationErr
	}
	return s.Repository.ListActiveKnowledge(ctx, salonID)
}

func TestPostgresConversationMessageRetriesMutationBetweenFenceReads(t *testing.T) {
	db := openInformationalContractDatabase(t)
	ctx := context.Background()
	tenant := insertInformationalContractTenant(t, ctx, db, "fence-retry")
	if _, err := db.ExecContext(ctx, `
		INSERT INTO knowledge_items (salon_id, title, category, body, status, source)
		VALUES ($1, 'Arrival preparation', 'policy', 'Guests should arrive five minutes early.', 'active', 'owner')
	`, tenant.salonID); err != nil {
		t.Fatalf("insert initial retry knowledge: %v", err)
	}
	store := &fenceMutationStore{Repository: NewRepository(db), db: db, salonID: tenant.salonID}
	tool := newOwnerManualSchedulingTool("unused-retry")
	service := NewService(store, tool)

	answer, _, _ := askInformationalQuestion(
		t, ctx, service, tenant, ChannelSimulator,
		"What preparation policy applies before arrival?",
		testQuestionUnderstanding(ConversationQuestionPolicy, ""),
		"fence-retry-knowledge",
	)
	assertReplyContains(t, answer, "arrive twelve minutes early")
	assertReplyNotContains(t, answer, "five minutes")
	if store.fenceReads < 4 {
		t.Fatalf("double-read retry used %d fence reads, want at least 4", store.fenceReads)
	}
	assertMetadataBool(t, answer, "fence-retry-knowledge", "answer_context_cache_hit", false)
	assertNoInformationalSchedulingCalls(t, tool)
}

func openInformationalContractDatabase(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate PostgreSQL: %v", err)
	}
	return db
}

func insertInformationalContractTenant(t *testing.T, ctx context.Context, db *sql.DB, label string) informationalContractTenant {
	t.Helper()
	ownerID, salonID := insertConversationHoursTenant(t, ctx, db, label)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID)
	})
	if _, err := db.ExecContext(ctx, `
		UPDATE salons
		SET name = 'Prism Nail Studio', timezone = 'America/Chicago', ai_enabled = true
		WHERE id = $1
	`, salonID); err != nil {
		t.Fatalf("configure informational salon %s: %v", label, err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE salon_settings
		SET ai_greeting = 'Welcome. How may I help today?', ai_tone = 'professional_warm',
		    consultation_enabled = true
		WHERE salon_id = $1
	`, salonID); err != nil {
		t.Fatalf("configure informational settings %s: %v", label, err)
	}
	return informationalContractTenant{ownerID: ownerID, salonID: salonID}
}

func insertInformationalCatalogFixture(t *testing.T, ctx context.Context, db *sql.DB, salonID string) informationalCatalogFixture {
	t.Helper()
	var fixture informationalCatalogFixture
	suffix := uuid.NewString()
	if err := db.QueryRowContext(ctx, `
		INSERT INTO service_categories (salon_id, name, slug, source)
		VALUES ($1, 'Hand Care', $2, 'manual')
		RETURNING id::text
	`, salonID, "hand-care-"+suffix).Scan(&fixture.categoryID); err != nil {
		t.Fatalf("insert informational category: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (
			salon_id, pos_provider, pos_service_id, name, description, ai_description,
			duration_minutes, price_from, price_display, ai_bookable, active, source,
			sync_status, service_category_id, service_category_source
		) VALUES (
			$1, 'square', $2, 'Quartz Manicure', 'Detailed fallback description',
			'Natural shaping with a mineral finish', 55, 47.50, '$47.50', true, true,
			'local', 'local_only', $3, 'manual'
		) RETURNING id::text
	`, salonID, "quartz-"+suffix, fixture.categoryID).Scan(&fixture.serviceID); err != nil {
		t.Fatalf("insert informational service: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_aliases (salon_id, service_id, alias, normalized_alias, source, status)
		VALUES ($1, $2, 'opal finish', 'opal finish', 'owner', 'active')
	`, salonID, fixture.serviceID); err != nil {
		t.Fatalf("insert informational service alias: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_category_aliases (salon_id, category_id, alias, normalized_alias, source, status)
		VALUES ($1, $2, 'hand treatments', 'hand treatments', 'owner', 'active')
	`, salonID, fixture.categoryID); err != nil {
		t.Fatalf("insert informational category alias: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_consultation_profiles (
			salon_id, service_id, status, recommended_outcomes, compatible_current_systems,
			length_capabilities, priority_tags, finish_options, maintenance_note,
			owner_approved_summary, revision
		) VALUES (
			$1, $2, 'ready', '["shorten"]'::jsonb, '["acrylic","extension"]'::jsonb,
			'["shorten"]'::jsonb, '["lower_maintenance"]'::jsonb, '["glossy"]'::jsonb,
			'Return based on owner guidance.', 'Owner-approved structured service guidance.', 1
		)
	`, salonID, fixture.serviceID); err != nil {
		t.Fatalf("insert informational profile: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (
			salon_id, pos_provider, pos_staff_id, name, ai_bookable, active, source, sync_status
		) VALUES ($1, 'square', $2, 'Dana Ruiz', true, true, 'local', 'local_only')
		RETURNING id::text
	`, salonID, "dana-"+suffix).Scan(&fixture.staffID); err != nil {
		t.Fatalf("insert informational staff: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO knowledge_items (salon_id, title, category, body, status, source)
		VALUES ($1, 'Guest parking', 'policy', 'Street parking is available beside the studio.', 'active', 'owner')
		RETURNING id::text
	`, salonID).Scan(&fixture.knowledgeID); err != nil {
		t.Fatalf("insert informational knowledge: %v", err)
	}
	return fixture
}

func insertExternalProviderFixture(t *testing.T, ctx context.Context, db *sql.DB, tenant informationalContractTenant) informationalCatalogFixture {
	t.Helper()
	var fixture informationalCatalogFixture
	suffix := uuid.NewString()
	if _, err := db.ExecContext(ctx, `
		UPDATE salon_settings
		SET scheduling_authority = 'external_provider', booking_mode = 'confirmed_booking'
		WHERE salon_id = $1
	`, tenant.salonID); err != nil {
		t.Fatalf("select external authority: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections (
			salon_id, provider, status, location_id, snapshot_generation, last_sync_at
		) VALUES ($1, 'square', 'active', 'location-east', 11, now())
	`, tenant.salonID); err != nil {
		t.Fatalf("configure external provider connection: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO service_categories (salon_id, name, slug, source)
		VALUES ($1, 'Provider Hands', $2, 'imported') RETURNING id::text
	`, tenant.salonID, "provider-hands-"+suffix).Scan(&fixture.categoryID); err != nil {
		t.Fatalf("insert external category: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (
			salon_id, pos_provider, pos_service_id, pos_service_version, name,
			duration_minutes, price_from, ai_bookable, active, source, sync_status,
			last_synced_at, service_category_id, service_category_source
		) VALUES (
			$1, 'square', $2, 7, 'Provider Mineral Care', 50, 61, true, true,
			'imported', 'synced', now(), $3, 'imported'
		) RETURNING id::text
	`, tenant.salonID, "provider-service-"+suffix, fixture.categoryID).Scan(&fixture.serviceID); err != nil {
		t.Fatalf("insert external service: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (
			salon_id, pos_provider, pos_staff_id, name, ai_bookable, active,
			source, sync_status, last_synced_at
		) VALUES ($1, 'square', $2, 'Laila Monroe', true, true, 'imported', 'synced', now())
		RETURNING id::text
	`, tenant.salonID, "provider-staff-"+suffix).Scan(&fixture.staffID); err != nil {
		t.Fatalf("insert external staff: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_entity_links (
			salon_id, entity_type, entity_id, provider, provider_entity_id,
			provider_version, sync_status, last_synced_at
		) VALUES
			($1, 'service', $2, 'square', $4, 7, 'synced', now()),
			($1, 'staff', $3, 'square', $5, 7, 'synced', now())
	`, tenant.salonID, fixture.serviceID, fixture.staffID, "provider-service-id-"+suffix, "provider-staff-id-"+suffix); err != nil {
		t.Fatalf("insert external provider links: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_aliases (salon_id, service_id, alias, normalized_alias, source, status)
		VALUES ($1, $2, 'provider polish care', 'provider polish care', 'owner', 'active')
	`, tenant.salonID, fixture.serviceID); err != nil {
		t.Fatalf("insert external service alias: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO service_category_aliases (salon_id, category_id, alias, normalized_alias, source, status)
		VALUES ($1, $2, 'provider hand menu', 'provider hand menu', 'owner', 'active')
	`, tenant.salonID, fixture.categoryID); err != nil {
		t.Fatalf("insert external category alias: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time, source,
			provider, provider_location_id, provider_period_index, last_synced_at
		) VALUES
			($1, 5, TIME '08:00', TIME '17:00', 'imported', 'square', 'location-east', 1, now()),
			($1, 5, TIME '11:00', TIME '14:00', 'local_override', '', '', 1, NULL)
	`, tenant.salonID); err != nil {
		t.Fatalf("insert external hours projections: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO knowledge_items (salon_id, title, category, body, status, source)
		VALUES ($1, 'Late arrival', 'policy', 'The studio offers a ten-minute grace period for late arrivals.', 'active', 'owner')
		RETURNING id::text
	`, tenant.salonID).Scan(&fixture.knowledgeID); err != nil {
		t.Fatalf("insert external owner-managed knowledge: %v", err)
	}
	return fixture
}

func insertReadyManleAICalendarFixture(t *testing.T, ctx context.Context, db *sql.DB, tenant informationalContractTenant) string {
	t.Helper()
	suffix := uuid.NewString()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_configs (
			salon_id, slot_step_minutes, minimum_booking_notice_minutes,
			booking_horizon_days, max_party_size
		) VALUES ($1, 15, 30, 60, 2)
	`, tenant.salonID); err != nil {
		t.Fatalf("insert ManleAI Calendar config: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE salon_settings SET scheduling_authority = 'manleai_calendar' WHERE salon_id = $1
	`, tenant.salonID); err != nil {
		t.Fatalf("select ManleAI Calendar authority: %v", err)
	}
	var serviceID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (
			salon_id, pos_provider, pos_service_id, name, duration_minutes,
			price_from, ai_bookable, active, source, sync_status
		) VALUES ($1, 'square', $2, 'Internal Structure Care', 60, 70, true, true, 'local', 'local_only')
		RETURNING id::text
	`, tenant.salonID, "internal-service-"+suffix).Scan(&serviceID); err != nil {
		t.Fatalf("insert internal service: %v", err)
	}
	var staffID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (
			salon_id, pos_provider, pos_staff_id, name, ai_bookable, active, source, sync_status
		) VALUES ($1, 'square', $2, 'Noelle Park', true, true, 'local', 'local_only')
		RETURNING id::text
	`, tenant.salonID, "internal-staff-"+suffix).Scan(&staffID); err != nil {
		t.Fatalf("insert internal staff: %v", err)
	}
	var resourcePoolID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO manleai_calendar_resource_pools (salon_id, name, capacity)
		VALUES ($1, 'Acceptance chairs', 2) RETURNING id::text
	`, tenant.salonID).Scan(&resourcePoolID); err != nil {
		t.Fatalf("insert internal resource pool: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_business_hour_periods (
			salon_id, day_of_week, start_local_time, end_local_time, source, provider_period_index
		) VALUES ($1, 6, TIME '10:00', TIME '18:00', 'local_override', 1)
	`, tenant.salonID); err != nil {
		t.Fatalf("insert internal local hours: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_policies (salon_id, service_id, enabled, capacity_mode)
		VALUES ($1, $2, true, 'staff_only')
	`, tenant.salonID, serviceID); err != nil {
		t.Fatalf("insert internal service policy: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_service_staff (salon_id, service_id, staff_id)
		VALUES ($1, $2, $3)
	`, tenant.salonID, serviceID, staffID); err != nil {
		t.Fatalf("insert internal staff eligibility: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO manleai_calendar_staff_weekly_periods (
			salon_id, staff_id, day_of_week, start_minute, end_minute
		) VALUES ($1, $2, 6, 600, 1080)
	`, tenant.salonID, staffID); err != nil {
		t.Fatalf("insert internal weekly schedule: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE manleai_calendar_configs
		SET activated_at = now(), activated_by_user_id = $2
		WHERE salon_id = $1
	`, tenant.salonID, tenant.ownerID); err != nil {
		t.Fatalf("activate internal calendar fixture: %v", err)
	}
	return resourcePoolID
}

func askInformationalQuestion(
	t *testing.T,
	ctx context.Context,
	service *Service,
	tenant informationalContractTenant,
	channel string,
	message string,
	understanding TurnUnderstanding,
	eventKey string,
) (*Session, *TranscriptMessage, *fakeConversationActInterpreter) {
	t.Helper()
	interpreter := &fakeConversationActInterpreter{turn: understanding}
	service.SetTurnInterpreter(interpreter)
	var session *Session
	var err error
	if channel == ChannelPhone {
		session, err = service.StartPhoneCall(ctx, tenant.salonID, tenant.ownerID, StartPhoneCallRequest{
			Provider: "twilio", ProviderCallID: "CA" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			FromPhone: "+13125550111", ToPhone: "+13125550222",
		})
	} else {
		session, err = service.Start(ctx, tenant.salonID, tenant.ownerID, StartSessionRequest{Channel: ChannelSimulator})
	}
	if err != nil {
		t.Fatalf("start %s informational session: %v", channel, err)
	}
	updated, err := service.Message(ctx, tenant.salonID, tenant.ownerID, session.ID, MessageRequest{Message: message, EventKey: eventKey})
	if err != nil {
		t.Fatalf("%s informational Message %q: %v", channel, message, err)
	}
	ai := transcriptAIMessageForEvent(updated, eventKey)
	if ai == nil {
		t.Fatalf("AI transcript for event %q not found: %#v", eventKey, updated.Transcript)
	}
	return updated, ai, interpreter
}

func consultationFreshnessUnderstanding() TurnUnderstanding {
	needs := ConsultationNeedProfile{
		DesiredOutcome: ConsultationOutcomeShorten,
		LengthChange:   ConsultationLengthShorten,
		Confidence:     0.97,
		Reason:         "acceptance_fixture_structured_guidance",
	}
	return TurnUnderstanding{
		Goal: "consultation", Confidence: 0.97, Consultation: needs,
		ConsultationMutations: initialConsultationMutations(needs, 0.97),
	}
}

func transcriptAIMessageForEvent(session *Session, eventKey string) *TranscriptMessage {
	if session == nil {
		return nil
	}
	for index := len(session.Transcript) - 1; index >= 0; index-- {
		message := &session.Transcript[index]
		if message.Speaker != SpeakerAI {
			continue
		}
		if eventKey == "" || strings.TrimSpace(informationalMetadataString(message.Metadata, "event_key")) == eventKey {
			return message
		}
	}
	return nil
}

func lastAIReply(session *Session) string {
	if message := transcriptAIMessageForEvent(session, ""); message != nil {
		return message.Body
	}
	return ""
}

func assertReplyContains(t *testing.T, session *Session, expected string) {
	t.Helper()
	if reply := lastAIReply(session); !strings.Contains(reply, expected) {
		metadata := map[string]any(nil)
		if message := transcriptAIMessageForEvent(session, ""); message != nil {
			metadata = message.Metadata
		}
		t.Fatalf("reply %q does not contain %q; metadata=%#v transcript=%#v", reply, expected, metadata, session.Transcript)
	}
}

func assertReplyNotContains(t *testing.T, session *Session, unexpected string) {
	t.Helper()
	if reply := lastAIReply(session); strings.Contains(reply, unexpected) {
		t.Fatalf("reply %q unexpectedly contains %q", reply, unexpected)
	}
}

func assertAnswerMetadata(t *testing.T, session *Session, eventKey string, source string, reason string, intent string, expectSourceIDs bool) {
	t.Helper()
	message := transcriptAIMessageForEvent(session, eventKey)
	if message == nil {
		t.Fatalf("missing AI transcript metadata for event %q", eventKey)
	}
	metadata := message.Metadata
	if informationalMetadataString(metadata, "answer_source") != source || informationalMetadataString(metadata, "answer_source_reason") != reason ||
		informationalMetadataString(metadata, "router_intent") != intent {
		t.Fatalf("answer metadata source/reason/intent = %#v", metadata)
	}
	if _, ok := metadata["answer_source_confidence"].(float64); !ok {
		t.Fatalf("answer_source_confidence missing or non-numeric: %#v", metadata)
	}
	if _, ok := metadata["answer_context_cache_hit"].(bool); !ok {
		t.Fatalf("answer_context_cache_hit missing or non-boolean: %#v", metadata)
	}
	ids, hasIDs := metadata["source_record_ids"].([]any)
	if expectSourceIDs && (!hasIDs || len(ids) == 0) {
		t.Fatalf("source_record_ids missing: %#v", metadata)
	}
}

func assertMetadataBool(t *testing.T, session *Session, eventKey string, key string, expected bool) {
	t.Helper()
	message := transcriptAIMessageForEvent(session, eventKey)
	if message == nil {
		t.Fatalf("missing AI transcript for event %q", eventKey)
	}
	actual, ok := message.Metadata[key].(bool)
	if !ok || actual != expected {
		t.Fatalf("metadata %s = %#v, want %t; metadata=%#v", key, message.Metadata[key], expected, message.Metadata)
	}
}

func informationalMetadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func assertInterpreterCatalogEvidence(t *testing.T, request TurnInterpretationRequest, serviceID string, serviceAlias string, categoryID string, categoryName string, categoryAlias string) {
	t.Helper()
	serviceFound := false
	for _, service := range request.CatalogServices {
		if service.ServiceID == serviceID && service.CategoryID == categoryID && service.CategoryName == categoryName {
			serviceFound = true
		}
	}
	aliasFound := false
	for _, alias := range request.CatalogServiceAliases {
		if alias.ServiceID == serviceID && alias.Alias == serviceAlias {
			aliasFound = true
		}
	}
	categoryFound := false
	for _, category := range request.CatalogCategories {
		if category.CategoryID == categoryID && category.CategoryName == categoryName && containsString(category.Aliases, categoryAlias) {
			categoryFound = true
		}
	}
	if !serviceFound || !aliasFound || !categoryFound {
		t.Fatalf("semantic catalog evidence service=%t alias=%t category=%t request=%#v", serviceFound, aliasFound, categoryFound, request)
	}
}

func assertNoInformationalSchedulingCalls(t *testing.T, tool *fakeNeutralSchedulingTool) {
	t.Helper()
	if tool == nil {
		t.Fatal("nil scheduling test tool")
	}
	if tool.authorityChecks != 0 || tool.availabilityChecks != 0 || tool.actionCalls != 0 ||
		tool.fakeBookingTool.availabilityCalls != 0 || tool.fakeBookingTool.calls != 0 {
		t.Fatalf(
			"informational flow called scheduling/POS: authority=%d availability=%d action=%d provider_availability=%d provider_booking=%d",
			tool.authorityChecks, tool.availabilityChecks, tool.actionCalls,
			tool.fakeBookingTool.availabilityCalls, tool.fakeBookingTool.calls,
		)
	}
}

var _ Store = (*fenceMutationStore)(nil)
var _ NeutralSchedulingTool = (*fakeNeutralSchedulingTool)(nil)
