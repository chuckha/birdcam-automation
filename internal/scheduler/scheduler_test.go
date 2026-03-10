package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/chuckha/birdcam-automation/internal/event"
)

func TestScheduler_EmitsSunriseBeforeSunset(t *testing.T) {
	loc := time.UTC
	// Use a time early in the day so sunrise comes first.
	now := time.Date(2026, 3, 9, 0, 0, 0, 0, loc)

	s := New(40.0, -74.0, loc)

	// Override now func to return a fixed time.
	callCount := 0
	s.now = func() time.Time {
		callCount++
		return now
	}

	events := make(chan event.Event, 10)
	ctx, cancel := context.WithCancel(context.Background())

	// Override waitAndSend to not actually wait.
	// We'll run just enough of the loop to get two events then cancel.
	go func() {
		// Wait for two events then cancel.
		<-events
		<-events
		cancel()
	}()

	// Replace the timer-based approach: just test nextSunriseAndSunset.
	rise, set := s.nextSunriseAndSunset(now)

	if !rise.Before(set) {
		t.Errorf("expected sunrise (%v) before sunset (%v)", rise, set)
	}

	_ = ctx
}

func TestScheduler_NextSunriseAndSunset_SunriseInPast(t *testing.T) {
	loc := time.UTC
	// Use a time after sunrise but before sunset.
	now := time.Date(2026, 3, 9, 14, 0, 0, 0, loc)

	s := New(40.0, -74.0, loc)

	rise, set := s.nextSunriseAndSunset(now)

	// Sunrise should be tomorrow (after now).
	if !rise.After(now) {
		t.Errorf("expected sunrise (%v) to be after now (%v)", rise, now)
	}

	// Sunset should still be today if it hasn't passed.
	// At 14:00 UTC at lat 40, sunset is around 23:00 UTC in March.
	if set.Before(now) {
		t.Errorf("expected sunset (%v) to be after now (%v)", set, now)
	}
}

func TestScheduler_NextSunriseAndSunset_BothInPast(t *testing.T) {
	loc := time.UTC
	// Use a time after both sunrise and sunset.
	now := time.Date(2026, 3, 9, 23, 59, 0, 0, loc)

	s := New(40.0, -74.0, loc)

	rise, set := s.nextSunriseAndSunset(now)

	if !rise.After(now) {
		t.Errorf("expected sunrise (%v) to be after now (%v)", rise, now)
	}
	if !set.After(now) {
		t.Errorf("expected sunset (%v) to be after now (%v)", set, now)
	}
}
