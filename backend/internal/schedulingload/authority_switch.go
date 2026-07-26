package schedulingload

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
	switching "github.com/manleai/ai-receptionist/modules/scheduling_authority_switch"
)

type fakeExternalReadiness struct {
	evidence scheduling.TargetReadiness
}

func newFakeExternalReadiness(config Config) *fakeExternalReadiness {
	digest := sha256.Sum256([]byte(ReportSchemaVersion + ":" + config.Release + ":" + config.RunID))
	return &fakeExternalReadiness{evidence: scheduling.TargetReadiness{
		TargetSchedulingAuthority:    booking.SchedulingAuthorityExternalProvider,
		Ready:                        true,
		AvailabilityReady:            true,
		ExecutionReady:               true,
		AuthorityVersion:             1,
		ReadinessEvidenceVersion:     1,
		ReadinessEvidenceFingerprint: hex.EncodeToString(digest[:]),
		EligibleServiceCount:         1,
		Checks: []scheduling.TargetReadinessCheck{{
			Code: "SYNTHETIC_EXTERNAL_BOUNDARY_READY", Ready: true, Scope: "load_harness_fake",
		}},
		Blockers:             []scheduling.TargetReadinessBlocker{},
		AvailabilityBlockers: []scheduling.TargetReadinessBlocker{},
		ExecutionBlockers:    []scheduling.TargetReadinessBlocker{},
	}}
}

func (fake *fakeExternalReadiness) SchedulingTargetReadiness(context.Context, string, string) (scheduling.TargetReadiness, error) {
	return fake.evidence, nil
}

func (fake *fakeExternalReadiness) SchedulingTargetReadinessTx(context.Context, *sql.Tx, string, string) (scheduling.TargetReadiness, error) {
	return fake.evidence, nil
}

func runAuthoritySwitchWorkload(
	ctx context.Context,
	db *sql.DB,
	config Config,
	seed seededRun,
	violations *InvariantViolations,
) ([]WorkloadReport, error) {
	external := newFakeExternalReadiness(config)
	service := switching.NewService(switching.NewRepository(db), nil, external, true)
	reports, err := runAuthoritySwitchReplay(ctx, service, config, seed, violations)
	if err != nil {
		return reports, err
	}
	raceReport, err := runAuthoritySwitchRace(ctx, service, config, seed, violations)
	reports = append(reports, raceReport...)
	if err != nil {
		return reports, err
	}
	if err := inspectSwitchInvariants(ctx, db, config, seed, violations); err != nil {
		return reports, err
	}
	return reports, nil
}

func runAuthoritySwitchReplay(
	ctx context.Context,
	service *switching.Service,
	config Config,
	seed seededRun,
	violations *InvariantViolations,
) ([]WorkloadReport, error) {
	request := switching.PreviewRequest{
		OperationKey:                   "load-switch-replay-preview-" + config.RunID,
		SourceSchedulingAuthority:      booking.SchedulingAuthorityOwnerManual,
		TargetSchedulingAuthority:      booking.SchedulingAuthorityExternalProvider,
		ExpectedSourceAuthorityVersion: 1,
	}
	var switchRunID string
	var switchRunMutex sync.Mutex
	started := time.Now()
	previewSamples := runConcurrent(ctx, config.OperationsPerWorkload, config.Concurrency, func(callCtx context.Context, _ int) sampleResult {
		response, err := service.Preview(callCtx, seed.SwitchReplay.SalonID, seed.OwnerID, request)
		if err != nil || response == nil || response.SwitchRun == nil || response.SwitchRun.Status != switching.StatusPreviewReady {
			return sampleResult{UnexpectedError: true}
		}
		switchRunMutex.Lock()
		if switchRunID == "" {
			switchRunID = response.SwitchRun.ID
		} else if switchRunID != response.SwitchRun.ID {
			violations.Idempotency++
		}
		switchRunMutex.Unlock()
		return sampleResult{Success: true, Replayed: response.Replayed}
	})
	reports := []WorkloadReport{summarizeWorkload("authority_switch_preview_replay", started, previewSamples)}
	if switchRunID == "" {
		return reports, errors.New("authority switch replay workload did not create a preview")
	}

	changed := request
	changed.TargetSchedulingAuthority = booking.SchedulingAuthorityManleAICalendar
	if _, err := service.Preview(ctx, seed.SwitchReplay.SalonID, seed.OwnerID, changed); !errors.Is(err, switching.ErrOperationConflict) {
		violations.Idempotency++
	}
	if _, err := service.Get(ctx, seed.SwitchReplay.SalonID, seed.OtherOwnerID, switchRunID); !errors.Is(err, switching.ErrNotFound) {
		violations.Tenant++
	}

	actionKey := "load-switch-replay-commit-" + config.RunID
	commitStarted := time.Now()
	commitSamples := runConcurrent(ctx, config.OperationsPerWorkload, config.Concurrency, func(callCtx context.Context, _ int) sampleResult {
		response, err := service.Commit(callCtx, seed.SwitchReplay.SalonID, seed.OwnerID, switchRunID, switching.CommitRequest{ActionKey: actionKey})
		if err != nil || response == nil || response.SwitchRun == nil || response.SwitchRun.Status != "committed" {
			return sampleResult{UnexpectedError: true}
		}
		return sampleResult{Success: true, Replayed: response.Replayed}
	})
	reports = append(reports, summarizeWorkload("authority_switch_commit_replay", commitStarted, commitSamples))
	return reports, nil
}

func runAuthoritySwitchRace(
	ctx context.Context,
	service *switching.Service,
	config Config,
	seed seededRun,
	violations *InvariantViolations,
) ([]WorkloadReport, error) {
	runIDs := make([]string, config.OperationsPerWorkload)
	previewStarted := time.Now()
	previewSamples := runConcurrent(ctx, config.OperationsPerWorkload, config.Concurrency, func(callCtx context.Context, index int) sampleResult {
		response, err := service.Preview(callCtx, seed.SwitchRace.SalonID, seed.OwnerID, switching.PreviewRequest{
			OperationKey:                   fmt.Sprintf("load-switch-race-preview-%s-%d", config.RunID, index),
			SourceSchedulingAuthority:      booking.SchedulingAuthorityOwnerManual,
			TargetSchedulingAuthority:      booking.SchedulingAuthorityExternalProvider,
			ExpectedSourceAuthorityVersion: 1,
		})
		if err != nil || response == nil || response.SwitchRun == nil || response.Replayed {
			return sampleResult{UnexpectedError: true}
		}
		runIDs[index] = response.SwitchRun.ID
		return sampleResult{Success: true}
	})
	reports := []WorkloadReport{summarizeWorkload("authority_switch_independent_previews", previewStarted, previewSamples)}
	for _, runID := range runIDs {
		if runID == "" {
			return reports, errors.New("authority switch race workload has a missing preview")
		}
	}

	commitStarted := time.Now()
	winnerAction := ""
	var winnerMutex sync.Mutex
	commitSamples := runConcurrent(ctx, len(runIDs), config.Concurrency, func(callCtx context.Context, index int) sampleResult {
		actionKey := fmt.Sprintf("load-switch-race-commit-%s-%d", config.RunID, index)
		response, err := service.Commit(callCtx, seed.SwitchRace.SalonID, seed.OwnerID, runIDs[index], switching.CommitRequest{ActionKey: actionKey})
		if err == nil && response != nil && response.SwitchRun != nil && response.SwitchRun.Status == "committed" {
			winnerMutex.Lock()
			if winnerAction == "" {
				winnerAction = actionKey
			} else if !response.Replayed {
				violations.Idempotency++
			}
			winnerMutex.Unlock()
			return sampleResult{Success: true, Replayed: response.Replayed}
		}
		if errors.Is(err, switching.ErrVersionConflict) || errors.Is(err, switching.ErrStateConflict) {
			return sampleResult{ExpectedConflict: true}
		}
		return sampleResult{UnexpectedError: true}
	})
	reports = append(reports, summarizeWorkload("authority_switch_commit_cas", commitStarted, commitSamples))
	if winnerAction == "" {
		return reports, errors.New("authority switch race workload had no commit winner")
	}
	return reports, nil
}

func inspectSwitchInvariants(ctx context.Context, db *sql.DB, config Config, seed seededRun, violations *InvariantViolations) error {
	type expected struct {
		salonID string
		runs    int
		events  int
	}
	for _, item := range []expected{
		{salonID: seed.SwitchReplay.SalonID, runs: 1, events: 2},
		{salonID: seed.SwitchRace.SalonID, runs: config.OperationsPerWorkload, events: config.OperationsPerWorkload + 1},
	} {
		var runs, events, committed, authorityVersion int
		var authority string
		if err := db.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE status='committed') FROM scheduling_authority_switch_runs WHERE salon_id::text=$1`, item.salonID).Scan(&runs, &committed); err != nil {
			return err
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM scheduling_authority_switch_events WHERE salon_id::text=$1`, item.salonID).Scan(&events); err != nil {
			return err
		}
		if err := db.QueryRowContext(ctx, `SELECT scheduling_authority, scheduling_authority_version FROM salon_settings WHERE salon_id::text=$1`, item.salonID).Scan(&authority, &authorityVersion); err != nil {
			return err
		}
		if runs != item.runs {
			violations.Duplicates += absoluteDifference(runs, item.runs)
		}
		if events != item.events {
			violations.Orphans += absoluteDifference(events, item.events)
		}
		if committed != 1 || authority != booking.SchedulingAuthorityExternalProvider || authorityVersion != 2 {
			violations.Safety += absoluteDifference(committed, 1) + absoluteDifference(authorityVersion, 2)
			if authority != booking.SchedulingAuthorityExternalProvider {
				violations.Safety++
			}
		}
	}
	var duplicateOperations, orphanEvents int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM (
			SELECT salon_id, operation_key FROM scheduling_authority_switch_runs
			WHERE salon_id::text IN ($1,$2) GROUP BY salon_id, operation_key HAVING count(*) > 1
		) duplicate
	`, seed.SwitchReplay.SalonID, seed.SwitchRace.SalonID).Scan(&duplicateOperations); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM scheduling_authority_switch_events event
		LEFT JOIN scheduling_authority_switch_runs run ON run.id=event.switch_run_id AND run.salon_id=event.salon_id
		WHERE event.salon_id::text IN ($1,$2) AND run.id IS NULL
	`, seed.SwitchReplay.SalonID, seed.SwitchRace.SalonID).Scan(&orphanEvents); err != nil {
		return err
	}
	violations.Duplicates += duplicateOperations
	violations.Orphans += orphanEvents
	return nil
}
