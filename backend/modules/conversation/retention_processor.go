package conversation

import "context"

type RetentionStore interface {
	RedactExpiredSessions(ctx context.Context, limit int) (int, error)
}

type RetentionProcessor struct {
	store RetentionStore
}

func NewRetentionProcessor(store RetentionStore) *RetentionProcessor {
	return &RetentionProcessor{store: store}
}

func (p *RetentionProcessor) ProcessOnce(ctx context.Context, limit int) (int, error) {
	if p == nil || p.store == nil {
		return 0, nil
	}
	return p.store.RedactExpiredSessions(ctx, clampRetentionLimit(limit))
}
