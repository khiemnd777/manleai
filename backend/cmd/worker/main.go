package main

import (
	"log/slog"
	"os"
)

func main() {
	slog.New(slog.NewJSONHandler(os.Stdout, nil)).Info("worker entrypoint started", "scope", "reminders and async POS retries begin in later milestones")
}
