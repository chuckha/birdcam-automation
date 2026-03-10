package manager

import (
	"context"
	"testing"
	"time"

	"github.com/chuckha/birdcam-automation/internal/event"
)

type fakeBroadcaster struct {
	calls []broadcasterCall
}

type broadcasterCall struct {
	method      string
	broadcastID string
	title       string
}

func (f *fakeBroadcaster) CreateBroadcast(_ context.Context, title string, _ time.Time) (string, error) {
	id := "broadcast-" + title
	f.calls = append(f.calls, broadcasterCall{method: "CreateBroadcast", title: title, broadcastID: id})
	return id, nil
}

func (f *fakeBroadcaster) GoLive(_ context.Context, broadcastID string) error {
	f.calls = append(f.calls, broadcasterCall{method: "GoLive", broadcastID: broadcastID})
	return nil
}

func (f *fakeBroadcaster) EndBroadcast(_ context.Context, broadcastID string) error {
	f.calls = append(f.calls, broadcasterCall{method: "EndBroadcast", broadcastID: broadcastID})
	return nil
}

type fakeDownloader struct {
	calls []downloaderCall
}

type downloaderCall struct {
	broadcastID string
	dest        string
}

func (f *fakeDownloader) Download(_ context.Context, broadcastID string, dest string) error {
	f.calls = append(f.calls, downloaderCall{broadcastID: broadcastID, dest: dest})
	return nil
}

type fakeProcessor struct {
	calls []processorCall
}

type processorCall struct {
	dayFile   string
	nightFile string
	outFile   string
}

func (f *fakeProcessor) Highlights(_ context.Context, dayFile, nightFile, outFile string) error {
	f.calls = append(f.calls, processorCall{dayFile: dayFile, nightFile: nightFile, outFile: outFile})
	return nil
}

type fakeUploader struct {
	calls []uploaderCall
}

type uploaderCall struct {
	filePath string
	title    string
}

func (f *fakeUploader) Upload(_ context.Context, filePath, title string, _ time.Time) (string, error) {
	id := "video-" + title
	f.calls = append(f.calls, uploaderCall{filePath: filePath, title: title})
	return id, nil
}

func TestManager_Sunrise_CreatesDayBroadcast(t *testing.T) {
	bc := &fakeBroadcaster{}
	events := make(chan event.Event, 10)
	m := New(bc, &fakeDownloader{}, &fakeProcessor{}, &fakeUploader{}, "/data", events)

	ctx := context.Background()
	ev := event.Event{
		Kind:    event.Sunrise,
		Time:    time.Date(2026, 3, 9, 6, 30, 0, 0, time.UTC),
		Payload: map[string]string{"date": "2026-03-09"},
	}

	err := m.handle(ctx, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bc.calls) != 2 {
		t.Fatalf("expected 2 broadcaster calls, got %d", len(bc.calls))
	}
	if bc.calls[0].method != "CreateBroadcast" {
		t.Errorf("expected CreateBroadcast, got %s", bc.calls[0].method)
	}
	if bc.calls[0].title != "Birdcam Day 2026-03-09" {
		t.Errorf("expected title 'Birdcam Day 2026-03-09', got %q", bc.calls[0].title)
	}
	if bc.calls[1].method != "GoLive" {
		t.Errorf("expected GoLive, got %s", bc.calls[1].method)
	}
}

func TestManager_Sunrise_EndsPreviousNightBroadcast(t *testing.T) {
	bc := &fakeBroadcaster{}
	dl := &fakeDownloader{}
	events := make(chan event.Event, 10)
	m := New(bc, dl, &fakeProcessor{}, &fakeUploader{}, "/data", events)
	m.activeBroadcast = "night-broadcast-1"
	m.broadcastType = "night"

	ctx := context.Background()
	ev := event.Event{
		Kind:    event.Sunrise,
		Time:    time.Date(2026, 3, 9, 6, 30, 0, 0, time.UTC),
		Payload: map[string]string{"date": "2026-03-09"},
	}

	err := m.handle(ctx, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bc.calls) != 3 {
		t.Fatalf("expected 3 broadcaster calls, got %d", len(bc.calls))
	}
	if bc.calls[0].method != "EndBroadcast" {
		t.Errorf("expected EndBroadcast first, got %s", bc.calls[0].method)
	}
	if bc.calls[0].broadcastID != "night-broadcast-1" {
		t.Errorf("expected ending night-broadcast-1, got %s", bc.calls[0].broadcastID)
	}

	// Wait briefly for the download goroutine to register.
	time.Sleep(10 * time.Millisecond)
	if len(dl.calls) != 1 {
		t.Fatalf("expected 1 download call, got %d", len(dl.calls))
	}
	if dl.calls[0].broadcastID != "night-broadcast-1" {
		t.Errorf("expected download of night-broadcast-1, got %s", dl.calls[0].broadcastID)
	}
}

func TestManager_Sunset_EndsDayBroadcastAndCreatesNight(t *testing.T) {
	bc := &fakeBroadcaster{}
	dl := &fakeDownloader{}
	events := make(chan event.Event, 10)
	m := New(bc, dl, &fakeProcessor{}, &fakeUploader{}, "/data", events)
	m.activeBroadcast = "day-broadcast-1"
	m.broadcastType = "day"

	ctx := context.Background()
	ev := event.Event{
		Kind:    event.Sunset,
		Time:    time.Date(2026, 3, 9, 18, 30, 0, 0, time.UTC),
		Payload: map[string]string{"date": "2026-03-09"},
	}

	err := m.handle(ctx, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bc.calls) != 3 {
		t.Fatalf("expected 3 broadcaster calls, got %d", len(bc.calls))
	}
	if bc.calls[0].method != "EndBroadcast" {
		t.Errorf("expected EndBroadcast first, got %s", bc.calls[0].method)
	}
	if bc.calls[1].method != "CreateBroadcast" {
		t.Errorf("expected CreateBroadcast second, got %s", bc.calls[1].method)
	}
	if bc.calls[1].title != "Birdcam Night 2026-03-09" {
		t.Errorf("expected night broadcast title, got %q", bc.calls[1].title)
	}
	if bc.calls[2].method != "GoLive" {
		t.Errorf("expected GoLive third, got %s", bc.calls[2].method)
	}
}

func TestManager_DownloadComplete_BothFiles_EmitsDayFilesReady(t *testing.T) {
	events := make(chan event.Event, 10)
	m := New(&fakeBroadcaster{}, &fakeDownloader{}, &fakeProcessor{}, &fakeUploader{}, "/data", events)

	ctx := context.Background()

	// First download: day file.
	err := m.handle(ctx, event.Event{
		Kind: event.DownloadComplete,
		Time: time.Now(),
		Payload: map[string]string{
			"broadcast_id": "b1",
			"file_path":    "/data/2026-03-09_day.mp4",
			"logical_day":  "2026-03-09",
			"type":         "day",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No DayFilesReady event yet.
	if len(events) != 0 {
		t.Fatalf("expected no events yet, got %d", len(events))
	}

	// Second download: night file.
	err = m.handle(ctx, event.Event{
		Kind: event.DownloadComplete,
		Time: time.Now(),
		Payload: map[string]string{
			"broadcast_id": "b2",
			"file_path":    "/data/2026-03-09_night.mp4",
			"logical_day":  "2026-03-09",
			"type":         "night",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 DayFilesReady event, got %d", len(events))
	}

	ready := <-events
	if ready.Kind != event.DayFilesReady {
		t.Errorf("expected DayFilesReady, got %s", ready.Kind)
	}
	if ready.Payload["day_file"] != "/data/2026-03-09_day.mp4" {
		t.Errorf("unexpected day_file: %s", ready.Payload["day_file"])
	}
	if ready.Payload["night_file"] != "/data/2026-03-09_night.mp4" {
		t.Errorf("unexpected night_file: %s", ready.Payload["night_file"])
	}
}

func TestManager_DayFilesReady_RunsHighlights(t *testing.T) {
	proc := &fakeProcessor{}
	events := make(chan event.Event, 10)
	m := New(&fakeBroadcaster{}, &fakeDownloader{}, proc, &fakeUploader{}, "/data", events)

	ctx := context.Background()
	err := m.handle(ctx, event.Event{
		Kind: event.DayFilesReady,
		Time: time.Now(),
		Payload: map[string]string{
			"logical_day": "2026-03-09",
			"day_file":    "/data/2026-03-09_day.mp4",
			"night_file":  "/data/2026-03-09_night.mp4",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(proc.calls) != 1 {
		t.Fatalf("expected 1 processor call, got %d", len(proc.calls))
	}
	if proc.calls[0].dayFile != "/data/2026-03-09_day.mp4" {
		t.Errorf("unexpected dayFile: %s", proc.calls[0].dayFile)
	}
	if proc.calls[0].outFile != "/data/2026-03-09_highlights.mp4" {
		t.Errorf("unexpected outFile: %s", proc.calls[0].outFile)
	}

	if len(events) != 1 {
		t.Fatalf("expected HighlightsReady event")
	}
	ev := <-events
	if ev.Kind != event.HighlightsReady {
		t.Errorf("expected HighlightsReady, got %s", ev.Kind)
	}
}

func TestManager_HighlightsReady_UploadsVideo(t *testing.T) {
	up := &fakeUploader{}
	events := make(chan event.Event, 10)
	m := New(&fakeBroadcaster{}, &fakeDownloader{}, &fakeProcessor{}, up, "/data", events)

	ctx := context.Background()
	err := m.handle(ctx, event.Event{
		Kind: event.HighlightsReady,
		Time: time.Now(),
		Payload: map[string]string{
			"logical_day":     "2026-03-09",
			"highlights_file": "/data/2026-03-09_highlights.mp4",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(up.calls) != 1 {
		t.Fatalf("expected 1 upload call, got %d", len(up.calls))
	}
	if up.calls[0].filePath != "/data/2026-03-09_highlights.mp4" {
		t.Errorf("unexpected filePath: %s", up.calls[0].filePath)
	}
	if up.calls[0].title != "Birdcam Highlights 2026-03-09" {
		t.Errorf("unexpected title: %s", up.calls[0].title)
	}

	if len(events) != 1 {
		t.Fatalf("expected UploadComplete event")
	}
	ev := <-events
	if ev.Kind != event.UploadComplete {
		t.Errorf("expected UploadComplete, got %s", ev.Kind)
	}
}

func TestManager_UploadComplete_EmitsDayComplete(t *testing.T) {
	events := make(chan event.Event, 10)
	m := New(&fakeBroadcaster{}, &fakeDownloader{}, &fakeProcessor{}, &fakeUploader{}, "/data", events)

	// Seed pending files to verify cleanup.
	m.pendingFiles["2026-03-09"] = map[string]string{
		"day":   "/data/2026-03-09_day.mp4",
		"night": "/data/2026-03-09_night.mp4",
	}

	err := m.handle(context.Background(), event.Event{
		Kind: event.UploadComplete,
		Time: time.Now(),
		Payload: map[string]string{
			"video_id":    "vid-123",
			"logical_day": "2026-03-09",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected DayComplete event")
	}
	ev := <-events
	if ev.Kind != event.DayComplete {
		t.Errorf("expected DayComplete, got %s", ev.Kind)
	}

	if _, ok := m.pendingFiles["2026-03-09"]; ok {
		t.Error("expected pending files to be cleaned up")
	}
}
