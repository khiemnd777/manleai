package ratelimit

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRedisStoreEnforcesOneAtomicBucketAcrossInstances(t *testing.T) {
	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL is not configured")
	}
	first, err := NewRedisStore(redisURL)
	if err != nil {
		t.Fatalf("first store: %v", err)
	}
	defer first.Close()
	second, err := NewRedisStore(redisURL)
	if err != nil {
		t.Fatalf("second store: %v", err)
	}
	defer second.Close()

	ctx := context.Background()
	if err := first.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	policy := Policy{Name: "integration", Rate: 1, Window: time.Second, Burst: 7}
	identity := "test-" + uuid.NewString()
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for index := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := first
			if index%2 == 1 {
				store = second
			}
			decision, err := store.Take(ctx, identity, policy)
			if err != nil {
				t.Errorf("take: %v", err)
				return
			}
			if decision.Allowed {
				allowed.Add(1)
			} else if decision.RetryAfter <= 0 {
				t.Errorf("denial omitted retry evidence: %#v", decision)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != int64(policy.Burst) {
		t.Fatalf("allowed=%d, want atomic burst=%d", got, policy.Burst)
	}
}
