package tinyfish

import (
	"context"
	"errors"
	"sync"
	"time"
)

type requestLimiter interface {
	WaitN(context.Context, int) error
}

type windowEvent struct {
	timestamp time.Time
	weight    int
}

// slidingWindowLimiter enforces an exact weighted limit over the immediately
// preceding window. A Fetch request records one unit per URL, while Search
// records one unit per request.
type slidingWindowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	events []windowEvent
	used   int
	now    func() time.Time
}

func newSlidingWindowLimiter(limit int, window time.Duration) *slidingWindowLimiter {
	return &slidingWindowLimiter{
		limit:  limit,
		window: window,
		now:    time.Now,
	}
}

func (limiter *slidingWindowLimiter) WaitN(ctx context.Context, weight int) error {
	if weight <= 0 {
		return nil
	}
	if weight > limiter.limit {
		return errors.New("tinyfish: request weight exceeds the sliding-window rate limit")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		wait, acquired := limiter.acquireAt(limiter.now(), weight)
		if acquired {
			return nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// acquireAt is separated from WaitN so the window arithmetic can be tested
// deterministically without sleeping.
func (limiter *slidingWindowLimiter) acquireAt(now time.Time, weight int) (time.Duration, bool) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	limiter.prune(now)
	if limiter.used+weight <= limiter.limit {
		limiter.events = append(limiter.events, windowEvent{timestamp: now, weight: weight})
		limiter.used += weight
		return 0, true
	}

	required := limiter.used + weight - limiter.limit
	released := 0
	for _, event := range limiter.events {
		released += event.weight
		if released >= required {
			wait := event.timestamp.Add(limiter.window).Sub(now)
			if wait < 0 {
				wait = 0
			}
			return wait, false
		}
	}

	return limiter.window, false
}

func (limiter *slidingWindowLimiter) prune(now time.Time) {
	firstActive := 0
	for firstActive < len(limiter.events) {
		event := limiter.events[firstActive]
		if event.timestamp.Add(limiter.window).After(now) {
			break
		}
		limiter.used -= event.weight
		firstActive++
	}
	if firstActive == 0 {
		return
	}
	copy(limiter.events, limiter.events[firstActive:])
	limiter.events = limiter.events[:len(limiter.events)-firstActive]
	if len(limiter.events) == 0 {
		limiter.events = nil
	}
}
