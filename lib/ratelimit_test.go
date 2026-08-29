package tinyfish

import (
	"context"
	"testing"
	"time"
)

func TestSlidingWindowLimiterWeightedWindow(t *testing.T) {
	t.Parallel()

	limiter := newSlidingWindowLimiter(3, time.Minute)
	start := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	if wait, ok := limiter.acquireAt(start, 2); !ok || wait != 0 {
		t.Fatalf("first acquire = (%v, %v), want (0, true)", wait, ok)
	}
	if wait, ok := limiter.acquireAt(start.Add(10*time.Second), 1); !ok || wait != 0 {
		t.Fatalf("second acquire = (%v, %v), want (0, true)", wait, ok)
	}
	if wait, ok := limiter.acquireAt(start.Add(20*time.Second), 1); ok || wait != 40*time.Second {
		t.Fatalf("limited acquire = (%v, %v), want (40s, false)", wait, ok)
	}
	if wait, ok := limiter.acquireAt(start.Add(time.Minute), 2); !ok || wait != 0 {
		t.Fatalf("boundary acquire = (%v, %v), want (0, true)", wait, ok)
	}
	if limiter.used != 3 {
		t.Fatalf("used = %d, want 3", limiter.used)
	}
}

func TestSlidingWindowLimiterRejectsImpossibleWeight(t *testing.T) {
	t.Parallel()

	limiter := newSlidingWindowLimiter(2, time.Minute)
	if err := limiter.WaitN(context.Background(), 3); err == nil {
		t.Fatal("WaitN() error = nil")
	}
}
