package ratelimit

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var (
	ErrInvalidPolicy = errors.New("rate limit policy is invalid")
	ErrUnavailable   = errors.New("rate limit dependency is unavailable")
	policyName       = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

type Policy struct {
	Name   string
	Rate   int
	Window time.Duration
	Burst  int
}

func (p Policy) Validate() error {
	if !policyName.MatchString(p.Name) || p.Rate <= 0 || p.Window < time.Millisecond || p.Burst <= 0 {
		return ErrInvalidPolicy
	}
	return nil
}

type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
	ResetAfter time.Duration
}

type Taker interface {
	Take(ctx context.Context, identity string, policy Policy) (Decision, error)
}
