package schedulingload

import (
	"context"
	"sync"
	"time"
)

type sampleResult struct {
	Success          bool
	Replayed         bool
	ExpectedConflict bool
	UnexpectedError  bool
}

func runConcurrent(
	ctx context.Context,
	operations int,
	concurrency int,
	call func(context.Context, int) sampleResult,
) []operationSample {
	samples := make([]operationSample, operations)
	semaphore := make(chan struct{}, concurrency)
	var wait sync.WaitGroup
	for index := 0; index < operations; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			started := time.Now()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				samples[index] = operationSample{latency: time.Since(started), unexpectedError: true}
				return
			}
			result := call(ctx, index)
			samples[index] = operationSample{
				latency:          time.Since(started),
				success:          result.Success,
				replayed:         result.Replayed,
				expectedConflict: result.ExpectedConflict,
				unexpectedError:  result.UnexpectedError,
			}
		}()
	}
	wait.Wait()
	return samples
}
