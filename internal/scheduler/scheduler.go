package scheduler

import (
	"context"
	"time"

	"github.com/chuckha/birdcam-automation/internal/event"
	sunrise "github.com/nathan-osman/go-sunrise"
)

type Scheduler struct {
	lat, lon float64
	loc      *time.Location
	now      func() time.Time
}

func New(lat, lon float64, loc *time.Location) *Scheduler {
	return &Scheduler{
		lat: lat,
		lon: lon,
		loc: loc,
		now: time.Now,
	}
}

func (s *Scheduler) Run(ctx context.Context, events chan<- event.Event) error {
	for {
		now := s.now()
		rise, set := s.nextSunriseAndSunset(now)

		if rise.Before(set) {
			if err := s.waitAndSend(ctx, events, rise, event.Sunrise); err != nil {
				return err
			}
			if err := s.waitAndSend(ctx, events, set, event.Sunset); err != nil {
				return err
			}
		} else {
			if err := s.waitAndSend(ctx, events, set, event.Sunset); err != nil {
				return err
			}
			if err := s.waitAndSend(ctx, events, rise, event.Sunrise); err != nil {
				return err
			}
		}
	}
}

func (s *Scheduler) nextSunriseAndSunset(now time.Time) (time.Time, time.Time) {
	rise, set := sunrise.SunriseSunset(s.lat, s.lon, now.Year(), now.Month(), now.Day())
	rise = rise.In(s.loc)
	set = set.In(s.loc)

	if rise.Before(now) {
		tomorrow := now.AddDate(0, 0, 1)
		rise, _ = sunrise.SunriseSunset(s.lat, s.lon, tomorrow.Year(), tomorrow.Month(), tomorrow.Day())
		rise = rise.In(s.loc)
	}
	if set.Before(now) {
		tomorrow := now.AddDate(0, 0, 1)
		_, set = sunrise.SunriseSunset(s.lat, s.lon, tomorrow.Year(), tomorrow.Month(), tomorrow.Day())
		set = set.In(s.loc)
	}

	return rise, set
}

func (s *Scheduler) waitAndSend(ctx context.Context, events chan<- event.Event, t time.Time, kind event.Kind) error {
	d := time.Until(t)
	if d < 0 {
		d = 0
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		date := t.Format("2006-01-02")
		events <- event.Event{
			Kind:    kind,
			Time:    t,
			Payload: map[string]string{"date": date},
		}
		return nil
	}
}
