package tenant_registration

import "context"

const DefaultRetentionBatch = 100

type RetentionProcessor struct{ service *Service }

func NewRetentionProcessor(repository *Repository) *RetentionProcessor {
	return &RetentionProcessor{service: NewService(repository, nil)}
}

func (p *RetentionProcessor) ProcessOnce(ctx context.Context, limit int) (int, error) {
	if p == nil || p.service == nil {
		return 0, ErrValidation
	}
	return p.service.RedactDue(ctx, limit)
}
