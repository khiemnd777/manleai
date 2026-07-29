package conversation

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

const (
	defaultAnswerContextTTL        = 45 * time.Second
	answerContextFenceLoadAttempts = 3

	answerContextCacheStatusHit  = "hit"
	answerContextCacheStatusMiss = "miss"

	answerContextRefreshReasonNone          = "none"
	answerContextRefreshReasonCold          = "cold"
	answerContextRefreshReasonTTLExpired    = "ttl_expired"
	answerContextRefreshReasonFenceMismatch = "fence_mismatch"

	answerContextRetryReasonNone              = "none"
	answerContextRetryReasonReadinessMismatch = "readiness_mismatch"
	answerContextRetryReasonFenceChanged      = "fence_changed_during_load"

	answerContextLoadOutcomeCacheHit       = "cache_hit"
	answerContextLoadOutcomeRefreshed      = "refreshed"
	answerContextLoadOutcomeFailClosed     = "refreshed_fail_closed"
	answerContextLoadOutcomeError          = "load_error"
	answerContextLoadOutcomeRetryExhausted = "retry_exhausted"

	answerContextTimingAuthority     = "answer_context_authority"
	answerContextTimingCacheStatus   = "answer_context_cache_status"
	answerContextTimingRefreshReason = "answer_context_refresh_reason"
	answerContextTimingRetryReason   = "answer_context_retry_reason"
	answerContextTimingAttempts      = "answer_context_attempts"
	answerContextTimingOutcome       = "answer_context_outcome"
	answerContextTimingReady         = "answer_context_ready"
)

type AIAnswerContext struct {
	SchedulingAuthority string
	Services            []ServiceOption
	ServiceAliases      []ServiceAlias
	CategoryAliases     []ServiceCategoryAlias
	Staff               []StaffOption
	ActiveStaff         []StaffOption
	Knowledge           []KnowledgeSnippet
	BusinessHours       []BusinessHourPeriod
	CacheHit            bool
}

// ownerFirstAnswerContextStore keeps canonical and internal-calendar catalog
// reads separate from provider-backed reads. Provider mappings remain the
// source of truth for external_provider and are deliberately not consulted by
// either owner-first authority.
type ownerFirstAnswerContextStore interface {
	GetManleAICalendarAnswerContextEvidence(ctx context.Context, salonID string) (manleAICalendarAnswerContextEvidence, error)
	ListCanonicalGuidanceServices(ctx context.Context, salonID string) ([]ServiceOption, error)
	ListCanonicalActiveStaff(ctx context.Context, salonID string) ([]StaffOption, error)
	ListCanonicalServiceAliases(ctx context.Context, salonID string) ([]ServiceAlias, error)
	ListCanonicalServiceCategoryAliases(ctx context.Context, salonID string) ([]ServiceCategoryAlias, error)
	ListManleAICalendarBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error)
	ListManleAICalendarBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error)
	ListOwnerManagedBusinessHourPeriods(ctx context.Context, salonID string) ([]BusinessHourPeriod, error)
	ListManleAICalendarBusinessHourPeriods(ctx context.Context, salonID string) ([]BusinessHourPeriod, error)
}

type answerContextCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]answerContextCacheEntry
}

type answerContextCacheEntry struct {
	context   AIAnswerContext
	fence     AnswerContextFence
	expiresAt time.Time
}

type answerContextLoadDiagnostics struct {
	authority          string
	cacheStatus        string
	refreshReason      string
	retryReason        string
	attempts           int
	outcome            string
	ready              bool
	readinessEvaluated bool
}

func (d answerContextLoadDiagnostics) timingAttributes() map[string]string {
	attributes := map[string]string{
		answerContextTimingCacheStatus:   d.cacheStatus,
		answerContextTimingRefreshReason: d.refreshReason,
		answerContextTimingRetryReason:   d.retryReason,
		answerContextTimingAttempts:      strconv.Itoa(d.attempts),
		answerContextTimingOutcome:       d.outcome,
	}
	if authority := answerContextDiagnosticAuthority(d.authority); authority != "" {
		attributes[answerContextTimingAuthority] = authority
	}
	if d.readinessEvaluated {
		attributes[answerContextTimingReady] = strconv.FormatBool(d.ready)
	}
	return attributes
}

func answerContextDiagnosticAuthority(authority string) string {
	switch strings.TrimSpace(authority) {
	case booking.SchedulingAuthorityOwnerManual,
		booking.SchedulingAuthorityManleAICalendar,
		booking.SchedulingAuthorityExternalProvider:
		return strings.TrimSpace(authority)
	default:
		return ""
	}
}

func newAnswerContextCache(ttl time.Duration) *answerContextCache {
	if ttl <= 0 {
		ttl = defaultAnswerContextTTL
	}
	return &answerContextCache{
		ttl:     ttl,
		entries: map[string]answerContextCacheEntry{},
	}
}

func (c *answerContextCache) get(salonID string, fence AnswerContextFence) (*AIAnswerContext, bool) {
	ctx, status := c.lookup(salonID, fence)
	return ctx, status == answerContextCacheStatusHit
}

func (c *answerContextCache) lookup(salonID string, fence AnswerContextFence) (*AIAnswerContext, string) {
	if c == nil {
		return nil, answerContextRefreshReasonCold
	}
	key := strings.TrimSpace(salonID)
	if key == "" {
		return nil, answerContextRefreshReasonCold
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, answerContextRefreshReasonCold
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return nil, answerContextRefreshReasonTTLExpired
	}
	if entry.fence != fence {
		delete(c.entries, key)
		return nil, answerContextRefreshReasonFenceMismatch
	}
	ctx := cloneAIAnswerContext(&entry.context)
	ctx.CacheHit = true
	return ctx, answerContextCacheStatusHit
}

func (c *answerContextCache) set(salonID string, fence AnswerContextFence, ctx AIAnswerContext) {
	if c == nil {
		return
	}
	key := strings.TrimSpace(salonID)
	if key == "" {
		return
	}
	ctx.CacheHit = false
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = answerContextCacheEntry{
		context:   *cloneAIAnswerContext(&ctx),
		fence:     fence,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *answerContextCache) clear(salonID string) {
	if c == nil {
		return
	}
	key := strings.TrimSpace(salonID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if key == "" {
		c.entries = map[string]answerContextCacheEntry{}
		return
	}
	delete(c.entries, key)
}

func cloneAIAnswerContext(ctx *AIAnswerContext) *AIAnswerContext {
	if ctx == nil {
		return &AIAnswerContext{}
	}
	return &AIAnswerContext{
		SchedulingAuthority: ctx.SchedulingAuthority,
		Services:            append([]ServiceOption(nil), ctx.Services...),
		ServiceAliases:      append([]ServiceAlias(nil), ctx.ServiceAliases...),
		CategoryAliases:     append([]ServiceCategoryAlias(nil), ctx.CategoryAliases...),
		Staff:               append([]StaffOption(nil), ctx.Staff...),
		ActiveStaff:         append([]StaffOption(nil), ctx.ActiveStaff...),
		Knowledge:           append([]KnowledgeSnippet(nil), ctx.Knowledge...),
		BusinessHours:       append([]BusinessHourPeriod(nil), ctx.BusinessHours...),
		CacheHit:            ctx.CacheHit,
	}
}

func (s *Service) loadAnswerContext(ctx context.Context, salonID string) (*AIAnswerContext, error) {
	answerCtx, _, err := s.loadAnswerContextWithDiagnostics(ctx, salonID)
	return answerCtx, err
}

func (s *Service) loadAnswerContextWithDiagnostics(ctx context.Context, salonID string) (*AIAnswerContext, answerContextLoadDiagnostics, error) {
	diagnostics := answerContextLoadDiagnostics{
		cacheStatus:   answerContextCacheStatusMiss,
		refreshReason: answerContextRefreshReasonCold,
		retryReason:   answerContextRetryReasonNone,
	}
	for attempt := 0; attempt < answerContextFenceLoadAttempts; attempt++ {
		diagnostics.attempts = attempt + 1
		fence, err := s.store.GetAnswerContextFence(ctx, salonID)
		if err != nil {
			diagnostics.outcome = answerContextLoadOutcomeError
			return nil, diagnostics, err
		}
		diagnostics.authority = fence.SchedulingAuthority
		cached, cacheLookup := s.answerContextCache.lookup(salonID, fence)
		if cacheLookup == answerContextCacheStatusHit {
			diagnostics.cacheStatus = answerContextCacheStatusHit
			diagnostics.refreshReason = answerContextRefreshReasonNone
			diagnostics.outcome = answerContextLoadOutcomeCacheHit
			diagnostics.readinessEvaluated = false
			return cached, diagnostics, nil
		}
		if attempt == 0 || diagnostics.refreshReason == answerContextRefreshReasonCold {
			diagnostics.refreshReason = cacheLookup
		}
		ready, evidenceMatches, err := s.loadAnswerContextReadiness(ctx, salonID, fence)
		if err != nil {
			diagnostics.outcome = answerContextLoadOutcomeError
			return nil, diagnostics, err
		}
		diagnostics.readinessEvaluated = true
		diagnostics.ready = ready
		if !evidenceMatches {
			diagnostics.retryReason = answerContextRetryReasonReadinessMismatch
			s.answerContextCache.clear(salonID)
			continue
		}

		answerCtx, err := s.loadFreshAnswerContext(ctx, salonID, fence)
		if err != nil {
			diagnostics.outcome = answerContextLoadOutcomeError
			return nil, diagnostics, err
		}
		verifiedFence, err := s.store.GetAnswerContextFence(ctx, salonID)
		if err != nil {
			diagnostics.outcome = answerContextLoadOutcomeError
			return nil, diagnostics, err
		}
		if fence != verifiedFence {
			diagnostics.retryReason = answerContextRetryReasonFenceChanged
			s.answerContextCache.clear(salonID)
			continue
		}
		if !ready {
			failClosedSchedulingContext(answerCtx)
			diagnostics.outcome = answerContextLoadOutcomeFailClosed
		} else {
			diagnostics.outcome = answerContextLoadOutcomeRefreshed
		}
		s.answerContextCache.set(salonID, verifiedFence, *answerCtx)
		return cloneAIAnswerContext(answerCtx), diagnostics, nil
	}
	diagnostics.outcome = answerContextLoadOutcomeRetryExhausted
	return nil, diagnostics, errors.New("conversation answer context readiness changed while loading")
}

func (s *Service) loadAnswerContextReadiness(ctx context.Context, salonID string, fence AnswerContextFence) (bool, bool, error) {
	authority := strings.TrimSpace(fence.SchedulingAuthority)
	if authority == "" {
		// Legacy test stores predate the persisted authority fence. Production
		// repository reads always return the protocol token.
		authority = booking.SchedulingAuthorityExternalProvider
	}
	switch authority {
	case booking.SchedulingAuthorityOwnerManual:
		return true, true, nil
	case booking.SchedulingAuthorityExternalProvider:
		return externalProviderAnswerContextReady(fence), true, nil
	case booking.SchedulingAuthorityManleAICalendar:
		store, ok := s.store.(ownerFirstAnswerContextStore)
		if !ok {
			return false, false, errors.New("owner-first answer-context store is unavailable")
		}
		evidence, err := store.GetManleAICalendarAnswerContextEvidence(ctx, salonID)
		if err != nil {
			return false, false, err
		}
		return evidence.Ready, manleAICalendarEvidenceMatchesFence(evidence, fence), nil
	default:
		return false, true, nil
	}
}

func externalProviderAnswerContextReady(fence AnswerContextFence) bool {
	return strings.TrimSpace(fence.ActiveProvider) != "" &&
		fence.ConnectionStatus == "active" &&
		strings.TrimSpace(fence.LocationID) != "" &&
		fence.SnapshotGeneration > 0 &&
		strings.TrimSpace(fence.LastSyncAtRFC3339) != ""
}

func manleAICalendarEvidenceMatchesFence(evidence manleAICalendarAnswerContextEvidence, fence AnswerContextFence) bool {
	return evidence.SchedulingAuthority == fence.SchedulingAuthority &&
		evidence.SchedulingAuthorityVersion == fence.SchedulingAuthorityVersion &&
		evidence.CalendarConfigVersion == fence.CalendarConfigVersion &&
		evidence.CalendarActivatedVersion == fence.CalendarActivatedVersion
}

func failClosedSchedulingContext(answerCtx *AIAnswerContext) {
	if answerCtx == nil {
		return
	}
	clearBookingReadyFlags(answerCtx.Services)
	answerCtx.Staff = nil
	answerCtx.ActiveStaff = nil
	answerCtx.BusinessHours = nil
}

func (s *Service) loadFreshAnswerContext(ctx context.Context, salonID string, fence AnswerContextFence) (*AIAnswerContext, error) {
	authority := strings.TrimSpace(fence.SchedulingAuthority)
	if authority == "" {
		// Legacy test stores predate the persisted authority fence. Production
		// repository reads always return the protocol token.
		authority = booking.SchedulingAuthorityExternalProvider
	}
	var ownerFirstStore ownerFirstAnswerContextStore
	if authority == booking.SchedulingAuthorityOwnerManual || authority == booking.SchedulingAuthorityManleAICalendar {
		var ok bool
		ownerFirstStore, ok = s.store.(ownerFirstAnswerContextStore)
		if !ok {
			return nil, errors.New("owner-first answer-context store is unavailable")
		}
	}

	var services []ServiceOption
	var err error
	if ownerFirstStore != nil {
		services, err = ownerFirstStore.ListCanonicalGuidanceServices(ctx, salonID)
	} else {
		services, err = s.store.ListGuidanceServices(ctx, salonID)
	}
	if err != nil {
		return nil, err
	}

	var bookableServices []ServiceOption
	switch authority {
	case booking.SchedulingAuthorityExternalProvider:
		bookableServices, err = s.store.ListBookableServices(ctx, salonID)
	case booking.SchedulingAuthorityManleAICalendar:
		bookableServices, err = ownerFirstStore.ListManleAICalendarBookableServices(ctx, salonID)
	case booking.SchedulingAuthorityOwnerManual:
		markOwnerRequestableServices(services)
	default:
		clearBookingReadyFlags(services)
	}
	if err != nil {
		return nil, err
	}
	if authority != booking.SchedulingAuthorityOwnerManual {
		markBookableServices(services, bookableServices)
	}
	var serviceAliases []ServiceAlias
	if ownerFirstStore != nil {
		serviceAliases, err = ownerFirstStore.ListCanonicalServiceAliases(ctx, salonID)
	} else {
		serviceAliases, err = s.store.ListActiveServiceAliases(ctx, salonID)
	}
	if err != nil {
		return nil, err
	}
	var categoryAliases []ServiceCategoryAlias
	if ownerFirstStore != nil {
		categoryAliases, err = ownerFirstStore.ListCanonicalServiceCategoryAliases(ctx, salonID)
	} else {
		categoryAliases, err = s.store.ListActiveServiceCategoryAliases(ctx, salonID)
	}
	if err != nil {
		return nil, err
	}
	var activeStaff []StaffOption
	if ownerFirstStore != nil {
		activeStaff, err = ownerFirstStore.ListCanonicalActiveStaff(ctx, salonID)
	} else {
		activeStaff, err = s.store.ListActiveStaff(ctx, salonID)
	}
	if err != nil {
		return nil, err
	}
	var staff []StaffOption
	switch authority {
	case booking.SchedulingAuthorityExternalProvider:
		staff, err = s.store.ListBookableStaff(ctx, salonID)
	case booking.SchedulingAuthorityManleAICalendar:
		staff, err = ownerFirstStore.ListManleAICalendarBookableStaff(ctx, salonID)
	case booking.SchedulingAuthorityOwnerManual:
		staff = ownerRequestableStaff(activeStaff)
	}
	if err != nil {
		return nil, err
	}
	knowledge, err := s.store.ListActiveKnowledge(ctx, salonID)
	if err != nil {
		return nil, err
	}
	var hours []BusinessHourPeriod
	switch authority {
	case booking.SchedulingAuthorityOwnerManual:
		hours, err = ownerFirstStore.ListOwnerManagedBusinessHourPeriods(ctx, salonID)
	case booking.SchedulingAuthorityManleAICalendar:
		hours, err = ownerFirstStore.ListManleAICalendarBusinessHourPeriods(ctx, salonID)
	case booking.SchedulingAuthorityExternalProvider:
		hours, err = s.store.ListExternalProviderBusinessHourPeriods(ctx, salonID)
	}
	if err != nil {
		return nil, err
	}
	answerCtx := AIAnswerContext{
		SchedulingAuthority: authority,
		Services:            services,
		ServiceAliases:      serviceAliases,
		CategoryAliases:     categoryAliases,
		Staff:               staff,
		ActiveStaff:         activeStaff,
		Knowledge:           knowledge,
		BusinessHours:       hours,
	}
	return &answerCtx, nil
}

func markOwnerRequestableServices(services []ServiceOption) {
	for index := range services {
		services[index].BookingReady = strings.TrimSpace(services[index].ID) != "" && services[index].DurationMinutes > 0
	}
}

func ownerRequestableStaff(active []StaffOption) []StaffOption {
	items := make([]StaffOption, 0, len(active))
	for _, item := range active {
		if strings.TrimSpace(item.ID) != "" && item.AIBookable {
			items = append(items, item)
		}
	}
	return items
}

func markBookableServices(guidanceServices []ServiceOption, bookableServices []ServiceOption) {
	bookableIDs := make(map[string]struct{}, len(bookableServices))
	for _, service := range bookableServices {
		if id := strings.TrimSpace(service.ID); id != "" {
			bookableIDs[id] = struct{}{}
		}
	}
	for index := range guidanceServices {
		_, guidanceServices[index].BookingReady = bookableIDs[strings.TrimSpace(guidanceServices[index].ID)]
	}
}

func clearBookingReadyFlags(services []ServiceOption) {
	for index := range services {
		services[index].BookingReady = false
	}
}

func (s *Service) InvalidateAnswerContext(salonID string) {
	s.answerContextCache.clear(salonID)
}
