package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	operationshealth "github.com/manleai/ai-receptionist/modules/operations_health"
)

const (
	defaultJobHeartbeatInterval = 15 * time.Second
	defaultJobLeaseDuration     = 90 * time.Second
	defaultJobFinishTimeout     = 5 * time.Second
)

type recurringJob struct {
	name     string
	interval time.Duration
	run      func(context.Context) (int, error)
}

type recurringJobTicker interface {
	Ticks() <-chan time.Time
	Stop()
}

type recurringJobRunRecorder interface {
	StartRun(context.Context, operationshealth.StartRunInput) (string, error)
	HeartbeatRun(context.Context, string, string, string, time.Duration) error
	FinishRun(context.Context, operationshealth.FinishRunInput) error
}

type recurringJobScheduler struct {
	newTicker         func(time.Duration) recurringJobTicker
	recorder          recurringJobRunRecorder
	workerInstanceID  string
	heartbeatInterval time.Duration
	leaseDuration     time.Duration
	finishTimeout     time.Duration
}

type standardRecurringJobTicker struct{ ticker *time.Ticker }

func newRecurringJobScheduler(recorder recurringJobRunRecorder) recurringJobScheduler {
	return recurringJobScheduler{
		newTicker: func(interval time.Duration) recurringJobTicker {
			return standardRecurringJobTicker{ticker: time.NewTicker(interval)}
		},
		recorder: recorder, workerInstanceID: uuid.NewString(),
		heartbeatInterval: defaultJobHeartbeatInterval,
		leaseDuration:     defaultJobLeaseDuration,
		finishTimeout:     defaultJobFinishTimeout,
	}
}

func (t standardRecurringJobTicker) Ticks() <-chan time.Time { return t.ticker.C }
func (t standardRecurringJobTicker) Stop()                   { t.ticker.Stop() }

// Run starts one independent loop per job and waits for every loop to stop.
// The database recorder is also a distributed lease: local synchronous loops
// prevent overlap in one process, while StartRun fences concurrent replicas.
func (s recurringJobScheduler) Run(ctx context.Context, jobs ...recurringJob) {
	var workers sync.WaitGroup
	for _, job := range jobs {
		if job.run == nil || job.interval <= 0 {
			continue
		}
		job := job
		workers.Add(1)
		go func() { defer workers.Done(); s.runJob(ctx, job) }()
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
		s.executeRun(ctx, job)
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

func (s recurringJobScheduler) executeRun(ctx context.Context, job recurringJob) {
	if s.recorder == nil {
		_, _, _, _ = invokeJob(ctx, job.run)
		return
	}
	leaseDuration := s.leaseDuration
	if leaseDuration <= 0 {
		leaseDuration = defaultJobLeaseDuration
	}
	runID, err := s.recorder.StartRun(ctx, operationshealth.StartRunInput{
		JobName: job.name, WorkerInstanceID: s.workerInstanceID,
		Interval: job.interval, StaleAfter: staleAfterFor(job.interval),
		LeaseDuration: leaseDuration,
	})
	if errors.Is(err, operationshealth.ErrJobLeaseHeld) || err != nil {
		return
	}

	jobCtx, cancelJob := context.WithCancel(ctx)
	defer cancelJob()
	heartbeatsDone := make(chan struct{})
	heartbeatFailure := make(chan error, 1)
	go func() {
		defer close(heartbeatsDone)
		interval := s.heartbeatInterval
		if interval <= 0 {
			interval = defaultJobHeartbeatInterval
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				heartbeatCtx, heartbeatCancel := context.WithTimeout(context.WithoutCancel(jobCtx), interval)
				heartbeatErr := s.recorder.HeartbeatRun(heartbeatCtx, job.name, runID, s.workerInstanceID, leaseDuration)
				heartbeatCancel()
				if heartbeatErr != nil {
					heartbeatFailure <- heartbeatErr
					cancelJob()
					return
				}
			}
		}
	}()

	processed, runErr, panicked, cancelled := invokeJob(jobCtx, job.run)
	cancelJob()
	<-heartbeatsDone
	var heartbeatErr error
	select {
	case heartbeatErr = <-heartbeatFailure:
	default:
	}
	if errors.Is(heartbeatErr, operationshealth.ErrRunFenced) {
		return
	}
	status, errorClass, errorCode := classifyRunResult(runErr, panicked, cancelled || errors.Is(ctx.Err(), context.Canceled))
	if heartbeatErr != nil && !panicked && ctx.Err() == nil {
		status, errorClass, errorCode = operationshealth.RunStatusFailed, "worker", "JOB_HEARTBEAT_FAILED"
	}
	if processed < 0 {
		processed = 0
	}
	if processed > operationshealth.MaxProcessedCount {
		processed = operationshealth.MaxProcessedCount
	}
	finishTimeout := s.finishTimeout
	if finishTimeout <= 0 {
		finishTimeout = defaultJobFinishTimeout
	}
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer finishCancel()
	_ = s.recorder.FinishRun(finishCtx, operationshealth.FinishRunInput{
		JobName: job.name, RunID: runID, WorkerInstanceID: s.workerInstanceID,
		Status: status, ProcessedCount: processed, ErrorClass: errorClass, ErrorCode: errorCode,
	})
}

func invokeJob(ctx context.Context, run func(context.Context) (int, error)) (processed int, err error, panicked bool, cancelled bool) {
	defer func() {
		if recover() != nil {
			panicked = true
			err = nil
		}
		cancelled = errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled)
	}()
	processed, err = run(ctx)
	return
}

func classifyRunResult(err error, panicked, cancelled bool) (status, class, code string) {
	switch {
	case panicked:
		return operationshealth.RunStatusPanicked, "worker", "JOB_PANIC"
	case cancelled:
		return operationshealth.RunStatusCancelled, "cancellation", "JOB_CANCELLED"
	case errors.Is(err, context.DeadlineExceeded):
		return operationshealth.RunStatusFailed, "timeout", "JOB_DEADLINE_EXCEEDED"
	case err != nil:
		return operationshealth.RunStatusFailed, "dependency", "JOB_RUN_FAILED"
	default:
		return operationshealth.RunStatusSucceeded, "", ""
	}
}

func staleAfterFor(interval time.Duration) time.Duration {
	stale := 3 * interval
	if stale < 2*time.Minute {
		stale = 2 * time.Minute
	}
	if stale > 30*24*time.Hour {
		stale = 30 * 24 * time.Hour
	}
	return stale
}
