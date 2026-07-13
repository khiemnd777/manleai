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
)

type TurnInterpreterError struct {
	Outcome string
	Err     error
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
	if err == nil {
		return nil
	}
	return &TurnInterpreterError{Outcome: strings.TrimSpace(outcome), Err: err}
}

func turnInterpreterErrorOutcome(err error) string {
	var typed *TurnInterpreterError
	if errors.As(err, &typed) && strings.TrimSpace(typed.Outcome) != "" {
		return strings.TrimSpace(typed.Outcome)
	}
	return TurnInterpreterOutcomeProviderError
}
