package voice

import (
	"errors"
	"strings"
	"testing"
)

func TestProviderRequestErrorNeverExposesWrappedProviderDetail(t *testing.T) {
	detail := "private caller wording with api-key-secret"
	err := &ProviderRequestError{
		Provider: "openai",
		Stage:    "realtime_connect",
		Err:      errors.New(detail),
	}
	if strings.Contains(err.Error(), detail) || strings.Contains(err.Error(), "api-key-secret") {
		t.Fatalf("ProviderRequestError leaked wrapped detail: %q", err.Error())
	}
	if got := err.SafeDiagnostics(); got["provider"] != "openai" || got["failure_stage"] != "realtime_connect" {
		t.Fatalf("safe diagnostics = %#v", got)
	}
}
