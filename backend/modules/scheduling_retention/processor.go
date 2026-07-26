package schedulingretention

import "context"

type Processor struct {
	store Store
}

func NewProcessor(store Store) *Processor {
	return &Processor{store: store}
}

func (p *Processor) ProcessOnce(ctx context.Context, limit int) (int, error) {
	if p == nil || p.store == nil {
		return 0, nil
	}
	limit = clampBatch(limit)
	kinds := []string{
		KindOwnerRetentionExpiry,
		KindCustomerRetentionExpiry,
		KindSchedulingRequest,
		KindOwnerNotification,
		KindCustomerNotification,
		KindVoiceAudio,
	}
	processed := 0
	for processed < limit {
		madeProgress := false
		for _, kind := range kinds {
			if processed >= limit {
				break
			}
			changed, err := p.store.ProcessNext(ctx, kind)
			if err != nil {
				return processed, err
			}
			if changed {
				processed++
				madeProgress = true
			}
		}
		if !madeProgress {
			break
		}
	}
	return processed, nil
}

func clampBatch(limit int) int {
	if limit <= 0 {
		return DefaultProcessBatch
	}
	if limit > MaxProcessBatch {
		return MaxProcessBatch
	}
	return limit
}
