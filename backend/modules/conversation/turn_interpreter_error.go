package conversation

import (
	"errors"
	"strings"
)

const (
	TurnInterpreterOutcomeAccepted         = "accepted"
	TurnInterpreterOutcomeProviderDisabled = "provider_disabled"
	TurnInterpreterOutcomeProviderError    = "provider_error"
	TurnInterpreterOutcomeTimeout          = "timeout"
	TurnInterpreterOutcomeEmptyOutput      = "empty_output"
	TurnInterpreterOutcomeSchemaInvalid    = "schema_invalid"
	TurnInterpreterOutcomeLowConfidence    = "low_confidence"
	TurnInterpreterOutcomeCatalogRejected  = "catalog_rejected"
	TurnInterpreterOutcomeSourceUngrounded = "source_ungrounded"
)

type TurnInterpreterError struct {
	Outcome     string
	Diagnostics map[string]string
	Err         error
}

func (e *TurnInterpreterError) Error() string {
	if e == nil || e.Err == nil {
		return "turn interpreter failed"
	}
	return e.Err.Error()
}

func (e *TurnInterpreterError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewTurnInterpreterError(outcome string, err error) error {
	return NewTurnInterpreterErrorWithDiagnostics(outcome, err, nil)
}

func NewTurnInterpreterErrorWithDiagnostics(outcome string, err error, diagnostics map[string]string) error {
	if err == nil {
		return nil
	}
	return &TurnInterpreterError{Outcome: strings.TrimSpace(outcome), Diagnostics: safeTurnInterpreterDiagnostics(diagnostics), Err: err}
}

func turnInterpreterErrorOutcome(err error) string {
	var typed *TurnInterpreterError
	if errors.As(err, &typed) && strings.TrimSpace(typed.Outcome) != "" {
		return strings.TrimSpace(typed.Outcome)
	}
	return TurnInterpreterOutcomeProviderError
}

func turnInterpreterErrorDiagnostics(err error) map[string]string {
	var typed *TurnInterpreterError
	if errors.As(err, &typed) {
		return safeTurnInterpreterDiagnostics(typed.Diagnostics)
	}
	return nil
}

func safeTurnInterpreterDiagnostics(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	allowed := map[string]bool{
		"provider": true, "failure_stage": true, "http_status": true,
		"http_status_class": true, "request_id": true,
	}
	output := map[string]string{}
	for key, value := range input {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if allowed[key] && value != "" && len(value) <= 128 {
			output[key] = value
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
