package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	operationshealth "github.com/manleai/ai-receptionist/modules/operations_health"
)

type manualRecurringJobTicker struct {
	ticks   chan time.Time
	stopped atomic.Bool
}

func newManualRecurringJobTicker() *manualRecurringJobTicker {
	return &manualRecurringJobTicker{ticks: make(chan time.Time, 4)}
}

func (t *manualRecurringJobTicker) Ticks() <-chan time.Time {
	return t.ticks
}

func (t *manualRecurringJobTicker) Stop() {
	t.stopped.Store(true)
}

func TestRecurringJobSchedulerBlockedWebhookDoesNotStarveLeaseSweep(t *testing.T) {
	const (
		webhookInterval = 31 * time.Second
		leaseInterval   = 29 * time.Second
	)

	webhookTicker := newManualRecurringJobTicker()
	leaseTicker := newManualRecurringJobTicker()
	scheduler := recurringJobScheduler{
		newTicker: func(interval time.Duration) recurringJobTicker {
			switch interval {
			case webhookInterval:
				return webhookTicker
			case leaseInterval:
				return leaseTicker
			default:
				t.Errorf("unexpected interval %s", interval)
				return newManualRecurringJobTicker()
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	webhookStarted := make(chan struct{})
	leaseRuns := make(chan struct{}, 2)
	var webhookStartedOnce sync.Once
	var webhookRunCount atomic.Int32

	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx,
			recurringJob{
				name:     "square_webhooks",
				interval: webhookInterval,
				run: func(ctx context.Context) (int, error) {
					webhookRunCount.Add(1)
					webhookStartedOnce.Do(func() { close(webhookStarted) })
					<-ctx.Done()
					return 0, ctx.Err()
				},
			},
			recurringJob{
				name:     "booking_lease_sweep",
				interval: leaseInterval,
				run: func(context.Context) (int, error) {
					leaseRuns <- struct{}{}
					return 1, nil
				},
			},
		)
		close(done)
	}()

	waitForSignal(t, webhookStarted, "startup webhook run")
	waitForSignal(t, leaseRuns, "startup lease sweep")
	leaseTicker.ticks <- time.Now()
	waitForSignal(t, leaseRuns, "second lease sweep while webhook is blocked")

	if got := webhookRunCount.Load(); got != 1 {
		t.Fatalf("webhook runs = %d, want 1 blocked run", got)
	}

	cancel()
	waitForSignal(t, done, "scheduler shutdown")
	if !webhookTicker.stopped.Load() || !leaseTicker.stopped.Load() {
		t.Fatal("scheduler did not stop every job ticker")
	}
}

func TestRecurringJobSchedulerDoesNotOverlapSameJob(t *testing.T) {
	const interval = 17 * time.Second
	ticker := newManualRecurringJobTicker()
	scheduler := recurringJobScheduler{
		newTicker: func(got time.Duration) recurringJobTicker {
			if got != interval {
				t.Errorf("interval = %s, want %s", got, interval)
			}
			return ticker
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runStarted := make(chan struct{}, 2)
	releaseFirst := make(chan struct{})
	var runCount atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32

	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx, recurringJob{
			name:     "booking_lease_sweep",
			interval: interval,
			run: func(ctx context.Context) (int, error) {
				current := active.Add(1)
				for {
					maximum := maxActive.Load()
					if current <= maximum || maxActive.CompareAndSwap(maximum, current) {
						break
					}
				}
				run := runCount.Add(1)
				runStarted <- struct{}{}
				if run == 1 {
					select {
					case <-releaseFirst:
					case <-ctx.Done():
					}
				}
				active.Add(-1)
				return 1, nil
			},
		})
		close(done)
	}()

	waitForSignal(t, runStarted, "first job run")
	ticker.ticks <- time.Now()

	select {
	case <-runStarted:
		t.Fatal("same job overlapped while its first run was blocked")
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseFirst)
	waitForSignal(t, runStarted, "queued second job run")
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("maximum concurrent runs = %d, want 1", got)
	}

	cancel()
	waitForSignal(t, done, "scheduler shutdown")
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

type recordingJobRecorder struct {
	mu       sync.Mutex
	starts   []operationshealth.StartRunInput
	finishes []operationshealth.FinishRunInput
	startErr error
}

func (r *recordingJobRecorder) StartRun(_ context.Context, input operationshealth.StartRunInput) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, input)
	if r.startErr != nil {
		return "", r.startErr
	}
	return uuid.NewString(), nil
}

func (r *recordingJobRecorder) HeartbeatRun(context.Context, string, string, string, time.Duration) error {
	return nil
}

func (r *recordingJobRecorder) FinishRun(_ context.Context, input operationshealth.FinishRunInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishes = append(r.finishes, input)
	return nil
}

func TestRecurringJobSchedulerRecordsPanicAndContinues(t *testing.T) {
	recorder := &recordingJobRecorder{}
	scheduler := newRecurringJobScheduler(recorder)
	scheduler.executeRun(context.Background(), recurringJob{
		name: "panic_job", interval: 30 * time.Second,
		run: func(context.Context) (int, error) { panic("sensitive panic detail") },
	})
	scheduler.executeRun(context.Background(), recurringJob{
		name: "panic_job", interval: 30 * time.Second,
		run: func(context.Context) (int, error) { return 2, nil },
	})
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.finishes) != 2 {
		t.Fatalf("finishes=%d, want 2", len(recorder.finishes))
	}
	if got := recorder.finishes[0]; got.Status != operationshealth.RunStatusPanicked || got.ErrorCode != "JOB_PANIC" || got.ErrorClass != "worker" {
		t.Fatalf("panic finish=%#v", got)
	}
	if got := recorder.finishes[1]; got.Status != operationshealth.RunStatusSucceeded || got.ProcessedCount != 2 {
		t.Fatalf("recovery finish=%#v", got)
	}
}

func TestRecurringJobSchedulerRecordsCancellationAfterContextStops(t *testing.T) {
	recorder := &recordingJobRecorder{}
	scheduler := newRecurringJobScheduler(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		scheduler.executeRun(ctx, recurringJob{
			name: "cancel_job", interval: time.Minute,
			run: func(jobCtx context.Context) (int, error) {
				close(started)
				<-jobCtx.Done()
				return 3, jobCtx.Err()
			},
		})
		close(done)
	}()
	waitForSignal(t, started, "cancel job start")
	cancel()
	waitForSignal(t, done, "cancel job finish")
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.finishes) != 1 {
		t.Fatalf("finishes=%d, want 1", len(recorder.finishes))
	}
	finish := recorder.finishes[0]
	if finish.Status != operationshealth.RunStatusCancelled || finish.ErrorClass != "cancellation" || finish.ErrorCode != "JOB_CANCELLED" || finish.ProcessedCount != 3 {
		t.Fatalf("cancel finish=%#v", finish)
	}
}

func TestRecurringJobSchedulerSkipsExecutionWhenAnotherWorkerOwnsLease(t *testing.T) {
	recorder := &recordingJobRecorder{startErr: operationshealth.ErrJobLeaseHeld}
	scheduler := newRecurringJobScheduler(recorder)
	var runs atomic.Int32
	scheduler.executeRun(context.Background(), recurringJob{
		name: "leased_job", interval: time.Minute,
		run: func(context.Context) (int, error) { runs.Add(1); return 1, errors.New("must not run") },
	})
	if runs.Load() != 0 {
		t.Fatal("leased job executed")
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.finishes) != 0 {
		t.Fatal("leased job recorded a finish")
	}
}

func TestClassifyRunResultKeepsDeadlineSeparateFromCancellation(t *testing.T) {
	status, class, code := classifyRunResult(context.DeadlineExceeded, false, false)
	if status != operationshealth.RunStatusFailed || class != "timeout" || code != "JOB_DEADLINE_EXCEEDED" {
		t.Fatalf("deadline classification=%q/%q/%q", status, class, code)
	}
	status, class, code = classifyRunResult(context.Canceled, false, true)
	if status != operationshealth.RunStatusCancelled || class != "cancellation" || code != "JOB_CANCELLED" {
		t.Fatalf("cancellation classification=%q/%q/%q", status, class, code)
	}
}
