package conversation

import (
	"context"
	"strings"
	"sync"
	"time"
)

const defaultAnswerContextTTL = 45 * time.Second

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

type answerContextCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]answerContextCacheEntry
}

type answerContextCacheEntry struct {
	context   AIAnswerContext
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

func (c *answerContextCache) get(salonID string) (*AIAnswerContext, bool) {
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
	ctx := cloneAIAnswerContext(&entry.context)
	ctx.CacheHit = true
	return ctx, true
}

func (c *answerContextCache) set(salonID string, ctx AIAnswerContext) {
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
	if cached, ok := s.answerContextCache.get(salonID); ok {
		return cached, nil
	}
	services, err := s.store.ListBookableServices(ctx, salonID)
	if err != nil {
		return nil, err
	}
	serviceAliases, err := s.store.ListActiveServiceAliases(ctx, salonID)
	if err != nil {
		return nil, err
	}
	categoryAliases, err := s.store.ListActiveServiceCategoryAliases(ctx, salonID)
	if err != nil {
		return nil, err
	}
	staff, err := s.store.ListBookableStaff(ctx, salonID)
	if err != nil {
		return nil, err
	}
	activeStaff, err := s.store.ListActiveStaff(ctx, salonID)
	if err != nil {
		return nil, err
	}
	knowledge, err := s.store.ListActiveKnowledge(ctx, salonID)
	if err != nil {
		return nil, err
	}
	hours, err := s.store.ListBusinessHourPeriods(ctx, salonID)
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
	s.answerContextCache.set(salonID, answerCtx)
	return cloneAIAnswerContext(&answerCtx), nil
}

func (s *Service) InvalidateAnswerContext(salonID string) {
	s.answerContextCache.clear(salonID)
}
