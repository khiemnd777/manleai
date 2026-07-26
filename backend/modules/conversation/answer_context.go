package conversation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
)

const (
	defaultAnswerContextTTL        = 45 * time.Second
	answerContextFenceLoadAttempts = 3
)

type AIAnswerContext struct {
	Services        []ServiceOption
	ServiceAliases  []ServiceAlias
	CategoryAliases []ServiceCategoryAlias
	Staff           []StaffOption
	ActiveStaff     []StaffOption
	Knowledge       []KnowledgeSnippet
	BusinessHours   []BusinessHourPeriod
	CacheHit        bool
}

// ownerFirstAnswerContextStore keeps canonical and internal-calendar catalog
// reads separate from provider-backed reads. Provider mappings remain the
// source of truth for external_provider and are deliberately not consulted by
// either owner-first authority.
type ownerFirstAnswerContextStore interface {
	ListCanonicalGuidanceServices(ctx context.Context, salonID string) ([]ServiceOption, error)
	ListCanonicalActiveStaff(ctx context.Context, salonID string) ([]StaffOption, error)
	ListCanonicalServiceAliases(ctx context.Context, salonID string) ([]ServiceAlias, error)
	ListCanonicalServiceCategoryAliases(ctx context.Context, salonID string) ([]ServiceCategoryAlias, error)
	ListManleAICalendarBookableServices(ctx context.Context, salonID string) ([]ServiceOption, error)
	ListManleAICalendarBookableStaff(ctx context.Context, salonID string) ([]StaffOption, error)
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
	if c == nil {
		return nil, false
	}
	key := strings.TrimSpace(salonID)
	if key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return nil, false
	}
	if entry.fence != fence {
		delete(c.entries, key)
		return nil, false
	}
	ctx := cloneAIAnswerContext(&entry.context)
	ctx.CacheHit = true
	return ctx, true
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
		Services:        append([]ServiceOption(nil), ctx.Services...),
		ServiceAliases:  append([]ServiceAlias(nil), ctx.ServiceAliases...),
		CategoryAliases: append([]ServiceCategoryAlias(nil), ctx.CategoryAliases...),
		Staff:           append([]StaffOption(nil), ctx.Staff...),
		ActiveStaff:     append([]StaffOption(nil), ctx.ActiveStaff...),
		Knowledge:       append([]KnowledgeSnippet(nil), ctx.Knowledge...),
		BusinessHours:   append([]BusinessHourPeriod(nil), ctx.BusinessHours...),
		CacheHit:        ctx.CacheHit,
	}
}

func (s *Service) loadAnswerContext(ctx context.Context, salonID string) (*AIAnswerContext, error) {
	for attempt := 0; attempt < answerContextFenceLoadAttempts; attempt++ {
		fence, err := s.store.GetAnswerContextFence(ctx, salonID)
		if err != nil {
			return nil, err
		}
		if cached, ok := s.answerContextCache.get(salonID, fence); ok {
			return cached, nil
		}

		answerCtx, err := s.loadFreshAnswerContext(ctx, salonID, fence)
		if err != nil {
			return nil, err
		}
		verifiedFence, err := s.store.GetAnswerContextFence(ctx, salonID)
		if err != nil {
			return nil, err
		}
		if fence != verifiedFence {
			s.answerContextCache.clear(salonID)
			continue
		}
		if !verifiedFence.Ready {
			failClosedSchedulingContext(answerCtx)
		}
		s.answerContextCache.set(salonID, verifiedFence, *answerCtx)
		return cloneAIAnswerContext(answerCtx), nil
	}
	return nil, errors.New("conversation answer context readiness changed while loading")
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
	case booking.SchedulingAuthorityManleAICalendar:
		hours, err = ownerFirstStore.ListManleAICalendarBusinessHourPeriods(ctx, salonID)
	default:
		hours, err = s.store.ListBusinessHourPeriods(ctx, salonID)
	}
	if err != nil {
		return nil, err
	}
	answerCtx := AIAnswerContext{
		Services:        services,
		ServiceAliases:  serviceAliases,
		CategoryAliases: categoryAliases,
		Staff:           staff,
		ActiveStaff:     activeStaff,
		Knowledge:       knowledge,
		BusinessHours:   hours,
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
