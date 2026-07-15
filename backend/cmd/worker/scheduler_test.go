package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
				run: func(ctx context.Context) {
					webhookRunCount.Add(1)
					webhookStartedOnce.Do(func() { close(webhookStarted) })
					<-ctx.Done()
				},
			},
			recurringJob{
				name:     "booking_lease_sweep",
				interval: leaseInterval,
				run: func(context.Context) {
					leaseRuns <- struct{}{}
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
			run: func(ctx context.Context) {
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
