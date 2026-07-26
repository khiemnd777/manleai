package ratelimit

import (
	"errors"
	"testing"
	"time"
)

func TestPolicyValidationFailsClosed(t *testing.T) {
	valid := Policy{Name: "auth_login", Rate: 10, Window: time.Minute, Burst: 5}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid policy: %v", err)
	}
	invalid := []Policy{
		{Name: "Auth Login", Rate: 10, Window: time.Minute, Burst: 5},
		{Name: "auth_login", Rate: 0, Window: time.Minute, Burst: 5},
		{Name: "auth_login", Rate: 10, Window: 0, Burst: 5},
		{Name: "auth_login", Rate: 10, Window: time.Minute, Burst: 0},
	}
	for _, policy := range invalid {
		if err := policy.Validate(); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("policy=%#v error=%v", policy, err)
		}
	}
}
