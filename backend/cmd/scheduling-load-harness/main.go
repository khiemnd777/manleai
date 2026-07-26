package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/manleai/ai-receptionist/internal/schedulingload"
)

func main() {
	os.Exit(run())
}

func run() int {
	databaseURLEnv := flag.String("database-url-env", "SCHEDULING_LOAD_DATABASE_URL", "environment variable containing the isolated database URL")
	expectedDatabase := flag.String("expected-database", "", "exact isolated database name")
	expectedDatabaseUser := flag.String("expected-database-user", "", "exact dedicated database role")
	databasePrefix := flag.String("database-prefix", schedulingload.DefaultDatabasePrefix, "required isolated database name prefix")
	attestation := flag.String("attestation", "", "exact non-production isolation attestation")
	release := flag.String("release", "", "release identifier under verification")
	runID := flag.String("run-id", "", "optional UUID; generated when omitted")
	seed := flag.Int64("seed", 1, "stable synthetic workload seed")
	concurrency := flag.Int("concurrency", 8, "bounded concurrent operations")
	operations := flag.Int("operations", 16, "bounded operations per workload")
	duration := flag.Duration("duration", 2*time.Minute, "maximum total run duration")
	baseTimeText := flag.String("base-time", "2030-01-07T14:00:00Z", "fixed RFC3339 workload clock")
	flag.Parse()

	databaseURL, ok := os.LookupEnv(*databaseURLEnv)
	if !ok || databaseURL == "" {
		fmt.Fprintf(os.Stderr, "database URL environment variable %q is not set\n", *databaseURLEnv)
		return 2
	}
	baseTime, err := time.Parse(time.RFC3339, *baseTimeText)
	if err != nil {
		fmt.Fprintln(os.Stderr, "base-time must be RFC3339")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	report, runErr := schedulingload.Run(ctx, schedulingload.Config{
		DatabaseURL: databaseURL, ExpectedDatabaseName: *expectedDatabase, ExpectedDatabaseUser: *expectedDatabaseUser, DatabasePrefix: *databasePrefix,
		Attestation: *attestation, Release: *release, RunID: *runID, Seed: *seed,
		Concurrency: *concurrency, OperationsPerWorkload: *operations, Duration: *duration, BaseTime: baseTime,
	})
	if report.SchemaVersion != "" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintln(os.Stderr, "encode load report failed")
			return 1
		}
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "scheduling load harness failed:", runErr)
		return 1
	}
	if !report.Passed {
		fmt.Fprintln(os.Stderr, "scheduling load harness invariant gate failed")
		return 1
	}
	return 0
}
