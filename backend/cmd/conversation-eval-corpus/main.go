package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/manleai/ai-receptionist/internal/conversationeval"
)

func main() {
	var corpusPath string
	var reviewPath string
	var pilotCorpusPath string
	flag.StringVar(&corpusPath, "corpus", "modules/conversation/testdata/receptionist_semantic_scenarios.json", "output path for the generated scenario corpus")
	flag.StringVar(&reviewPath, "review", "modules/conversation/testdata/receptionist_semantic_review.json", "output path for the deterministic 100-round review report")
	flag.StringVar(&pilotCorpusPath, "pilot-corpus", "modules/conversation/testdata/receptionist_semantic_pilot_50.json", "output path for the directly authored 50-scenario model pilot")
	flag.Parse()

	corpus := conversationeval.GenerateCorpus()
	if problems := conversationeval.ValidateCorpus(corpus); len(problems) > 0 {
		fatalf("generated corpus failed validation: %s", problems[0])
	}
	review := conversationeval.ReviewCorpus(corpus)
	if !review.Passed {
		fatalf("generated corpus failed the 100-round review")
	}
	if err := writeJSON(corpusPath, corpus); err != nil {
		fatalf("write corpus: %v", err)
	}
	if err := writeJSON(reviewPath, review); err != nil {
		fatalf("write review: %v", err)
	}
	pilot := conversationeval.GeneratePilotCorpus()
	if problems := conversationeval.ValidatePilotCorpus(pilot); len(problems) > 0 {
		fatalf("generated pilot corpus failed validation: %s", problems[0])
	}
	if err := writeJSON(pilotCorpusPath, pilot); err != nil {
		fatalf("write pilot corpus: %v", err)
	}
	fmt.Printf("generated %d semantic-turn scenarios, %d completed structural review records, and %d directly authored pilot executions; no model calls were made\n", len(corpus.Scenarios), len(review.Rounds), len(pilot.Scenarios))
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
