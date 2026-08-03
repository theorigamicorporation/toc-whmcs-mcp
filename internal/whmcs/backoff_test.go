package whmcs_test

import (
	"testing"
	"time"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/whmcs"
)

func TestBackoffIsBounded(t *testing.T) {
	// The delay governs how long a billing call is held open, so it must stay
	// bounded for every attempt number, including ones the retry loop should
	// never produce.
	const maxWithJitter = 6 * time.Second

	for _, attempt := range []int{-1000, -1, 0, 1, 2, 3, 4, 5, 10, 63, 64, 65, 1 << 20} {
		d := whmcs.BackoffForTest(attempt)
		if d < 0 {
			t.Errorf("backoff(%d) = %s, want a non-negative delay", attempt, d)
		}
		if d > maxWithJitter {
			t.Errorf("backoff(%d) = %s, over the %s bound", attempt, d, maxWithJitter)
		}
	}
}

func TestBackoffGrowsThenPlateaus(t *testing.T) {
	// Jitter is random, so assert on the range each attempt can fall in rather
	// than on sampled minimums, which would make this flaky.
	//
	// backoff(n) = base(n) + [0, base(n)/2], base(n) = min(250ms * 2^(n-1), 4s).
	inRange := func(attempt int, lo, hi time.Duration) {
		t.Helper()
		for range 200 {
			d := whmcs.BackoffForTest(attempt)
			if d < lo || d > hi {
				t.Fatalf("backoff(%d) = %s, want within [%s, %s]", attempt, d, lo, hi)
			}
		}
	}

	inRange(1, 250*time.Millisecond, 375*time.Millisecond)
	inRange(2, 500*time.Millisecond, 750*time.Millisecond)
	inRange(3, time.Second, 1500*time.Millisecond)

	// From the cap onwards every attempt draws from the same range, so a long
	// retry loop cannot creep upwards.
	for _, attempt := range []int{5, 10, 20, 1000} {
		inRange(attempt, 4*time.Second, 6*time.Second)
	}
}
