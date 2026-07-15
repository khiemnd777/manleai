package main

import (
	"context"
	"sync"
	"time"
)

type recurringJob struct {
	name     string
	interval time.Duration
	run      func(context.Context)
}

type recurringJobTicker interface {
	Ticks() <-chan time.Time
	Stop()
}

type recurringJobScheduler struct {
	newTicker func(time.Duration) recurringJobTicker
}

type standardRecurringJobTicker struct {
	ticker *time.Ticker
}

func newRecurringJobScheduler() recurringJobScheduler {
	return recurringJobScheduler{
		newTicker: func(interval time.Duration) recurringJobTicker {
			return standardRecurringJobTicker{ticker: time.NewTicker(interval)}
		},
	}
}

func (t standardRecurringJobTicker) Ticks() <-chan time.Time {
	return t.ticker.C
}

func (t standardRecurringJobTicker) Stop() {
	t.ticker.Stop()
}

// Run starts one independent loop per job and waits for every loop to stop.
// A job runs immediately on startup, then at its configured cadence. Each loop
// invokes its job synchronously, so one job can never overlap itself while a
// slow job cannot delay any other job's cadence.
func (s recurringJobScheduler) Run(ctx context.Context, jobs ...recurringJob) {
	var workers sync.WaitGroup
	for _, job := range jobs {
		if job.run == nil || job.interval <= 0 {
			continue
		}

		job := job
		workers.Add(1)
		go func() {
			defer workers.Done()
			s.runJob(ctx, job)
		}()
	}

	workers.Wait()
}

func (s recurringJobScheduler) runJob(ctx context.Context, job recurringJob) {
	if ctx.Err() != nil {
		return
	}

	ticker := s.newTicker(job.interval)
	defer ticker.Stop()

	for {
		job.run(ctx)
		if ctx.Err() != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.Ticks():
		}
	}
}
