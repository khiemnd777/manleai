package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/conversationeval"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/internal/encryption"
	"github.com/manleai/ai-receptionist/internal/openairuntime"
	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
)

func main() {
	var mode, corpusPath, outputPath, checkpointPath, configSalonID string
	var maxModelCalls, transientRetries int
	var requestTimeout time.Duration
	flag.StringVar(&mode, "mode", "structural", "mode: write-corpus, structural, deterministic-runtime, or live-canary")
	flag.StringVar(&corpusPath, "corpus", "modules/conversation/testdata/receptionist_real_salon_100.json", "real-salon corpus path")
	flag.StringVar(&outputPath, "output", "", "retained JSON output path")
	flag.StringVar(&checkpointPath, "checkpoint", "", "live-canary checkpoint path; defaults to <output>.checkpoint.json")
	flag.StringVar(&configSalonID, "config-salon-id", "", "salon whose database-backed OpenAI integration config is used for live canaries")
	flag.IntVar(&maxModelCalls, "max-model-calls", 0, "required live-canary paid-call ceiling; maximum 60")
	flag.IntVar(&transientRetries, "transient-retries", 0, "bounded transient retries per paid call (0-3)")
	flag.DurationVar(&requestTimeout, "request-timeout", 45*time.Second, "per paid model-call timeout")
	flag.Parse()

	mode = strings.TrimSpace(mode)
	if mode == "write-corpus" {
		if strings.TrimSpace(outputPath) == "" {
			outputPath = corpusPath
		}
		corpus := conversationeval.DefaultRealSalonCorpus()
		if problems := conversationeval.ValidateRealSalonCorpus(corpus); len(problems) != 0 {
			fatalf("authored corpus is invalid: %s", problems[0])
		}
		if err := writeJSON(outputPath, corpus); err != nil {
			fatalf("write authored corpus: %v", err)
		}
		fmt.Printf("mode=write-corpus journeys=%d output=%s\n", len(corpus.Journeys), outputPath)
		return
	}
	corpus, err := readCorpus(corpusPath)
	if err != nil {
		fatalf("read real-salon corpus: %v", err)
	}
	if problems := conversationeval.ValidateRealSalonCorpus(corpus); len(problems) != 0 {
		fatalf("real-salon corpus validation failed with %d problems; first: %s", len(problems), problems[0])
	}
	if strings.TrimSpace(outputPath) == "" {
		fatalf("%s mode requires -output so its evidence is retained", mode)
	}
	var report conversationeval.RealSalonReport
	switch mode {
	case "structural":
		report = conversationeval.ReviewRealSalonStructure(corpus)
	case "deterministic-runtime":
		report = conversationeval.RunRealSalonDeterministic(context.Background(), corpus)
	case "live-canary":
		if strings.TrimSpace(configSalonID) == "" {
			fatalf("live-canary requires -config-salon-id for database-backed OpenAI configuration")
		}
		if maxModelCalls <= 0 || maxModelCalls > conversationeval.RealSalonLiveModelCallLimit {
			fatalf("live-canary requires -max-model-calls between 1 and %d", conversationeval.RealSalonLiveModelCallLimit)
		}
		if strings.TrimSpace(checkpointPath) == "" {
			checkpointPath = outputPath + ".checkpoint.json"
		}
		cfg := config.Load()
		db, err := database.Open(context.Background(), cfg.DatabaseURL)
		if err != nil {
			fatalf("open database for strict OpenAI config resolution: %v", err)
		}
		defer db.Close()
		cipher, err := encryption.NewTokenCipher(cfg.EncryptionKey)
		if err != nil {
			fatalf("create database secret cipher: %v", err)
		}
		integrationService := integrationconfig.NewService(integrationconfig.NewRepository(db), cipher, cfg)
		storedConfig, err := integrationService.ResolveOpenAIRuntimeConfig(context.Background(), strings.TrimSpace(configSalonID))
		if err != nil {
			fatalf("resolve database-backed OpenAI integration config: %v", err)
		}
		if !storedConfig.Enabled {
			fatalf("database-backed OpenAI integration is disabled for config salon")
		}
		model := conversationeval.NewOpenAIDirectModel(pinnedResolver{runtimeSalonID: "real-salon-evaluation", resolved: storedConfig})
		report, err = conversationeval.RunRealSalonLive(
			context.Background(), corpus, model,
			conversationeval.JSONRealSalonCheckpointStore{Path: checkpointPath},
			conversationeval.RealSalonLiveOptions{
				SalonID: "real-salon-evaluation", ModelCallBudget: maxModelCalls,
				RequestTimeout: requestTimeout, TransientRetries: transientRetries,
			},
		)
		if err != nil {
			fatalf("run real-salon live canary: %v", err)
		}
	default:
		fatalf("mode must be write-corpus, structural, deterministic-runtime, or live-canary")
	}
	if err := writeJSON(outputPath, report); err != nil {
		fatalf("write real-salon report: %v", err)
	}
	fmt.Printf("mode=%s journeys=%d structural=%d runtime=%d model=%d reviewed=%d failed=%d not_run=%d calls=%d passed=%t\n",
		report.Mode, report.JourneyCount, report.StructuralValidated, report.RuntimeExecuted,
		report.ModelExecuted, report.ReviewPassed, report.Failed, report.NotRun,
		report.ModelCallCount, report.Passed,
	)
	if report.Failed > 0 {
		os.Exit(1)
	}
}

type pinnedResolver struct {
	runtimeSalonID string
	resolved       openairuntime.ResolvedConfig
}

func (r pinnedResolver) ResolveOpenAIRuntimeConfig(_ context.Context, salonID string) (openairuntime.ResolvedConfig, error) {
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(salonID) != strings.TrimSpace(r.runtimeSalonID) {
		return openairuntime.ResolvedConfig{}, openairuntime.ErrInvalidSalon
	}
	resolved := r.resolved
	resolved.SalonID = strings.TrimSpace(r.runtimeSalonID)
	return resolved, nil
}

func readCorpus(path string) (conversationeval.RealSalonCorpus, error) {
	var corpus conversationeval.RealSalonCorpus
	file, err := os.Open(strings.TrimSpace(path))
	if err != nil {
		return corpus, err
	}
	defer file.Close()
	err = json.NewDecoder(io.LimitReader(file, 64*1024*1024)).Decode(&corpus)
	return corpus, err
}

func writeJSON(path string, value any) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("output path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".conversation-eval-real-output-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
