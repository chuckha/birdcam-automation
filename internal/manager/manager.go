package manager

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/chuckha/birdcam-automation/internal/event"
)

type Broadcaster interface {
	CreateBroadcast(ctx context.Context, title string, scheduledStart time.Time) (broadcastID string, err error)
	GoLive(ctx context.Context, broadcastID string) error
	EndBroadcast(ctx context.Context, broadcastID string) error
}

type Downloader interface {
	Download(ctx context.Context, broadcastID string, dest string) error
}

type Processor interface {
	Highlights(ctx context.Context, dayFile, nightFile, outFile string) error
}

type Uploader interface {
	Upload(ctx context.Context, filePath, title string, publishAt time.Time) (videoID string, err error)
}

type Manager struct {
	broadcaster Broadcaster
	downloader  Downloader
	processor   Processor
	uploader    Uploader
	dataDir     string
	events      chan event.Event

	mu               sync.Mutex
	activeBroadcast  string // current live broadcast ID
	broadcastType    string // "day" or "night"
	pendingFiles     map[string]map[string]string // logicalDay -> {"day": path, "night": path}
}

func New(
	broadcaster Broadcaster,
	downloader Downloader,
	processor Processor,
	uploader Uploader,
	dataDir string,
	events chan event.Event,
) *Manager {
	return &Manager{
		broadcaster:  broadcaster,
		downloader:   downloader,
		processor:    processor,
		uploader:     uploader,
		dataDir:      dataDir,
		events:       events,
		pendingFiles: make(map[string]map[string]string),
	}
}

func (m *Manager) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-m.events:
			if err := m.handle(ctx, ev); err != nil {
				log.Printf("error handling %s event: %v", ev.Kind, err)
			}
		}
	}
}

func (m *Manager) handle(ctx context.Context, ev event.Event) error {
	switch ev.Kind {
	case event.Sunrise:
		return m.handleSunrise(ctx, ev)
	case event.Sunset:
		return m.handleSunset(ctx, ev)
	case event.DownloadComplete:
		return m.handleDownloadComplete(ctx, ev)
	case event.DayFilesReady:
		return m.handleDayFilesReady(ctx, ev)
	case event.HighlightsReady:
		return m.handleHighlightsReady(ctx, ev)
	case event.UploadComplete:
		return m.handleUploadComplete(ev)
	default:
		log.Printf("unhandled event kind: %s", ev.Kind)
		return nil
	}
}

func (m *Manager) handleSunrise(ctx context.Context, ev event.Event) error {
	date := ev.Payload["date"]

	// End previous night broadcast if active.
	if m.activeBroadcast != "" && m.broadcastType == "night" {
		prevBroadcastID := m.activeBroadcast
		if err := m.broadcaster.EndBroadcast(ctx, prevBroadcastID); err != nil {
			return fmt.Errorf("ending night broadcast: %w", err)
		}
		// Start downloading the previous night VOD in the background.
		m.startDownload(ctx, prevBroadcastID, date, "night")
	}

	// Create and go live with day broadcast.
	title := fmt.Sprintf("Birdcam Day %s", date)
	broadcastID, err := m.broadcaster.CreateBroadcast(ctx, title, ev.Time)
	if err != nil {
		return fmt.Errorf("creating day broadcast: %w", err)
	}
	if err := m.broadcaster.GoLive(ctx, broadcastID); err != nil {
		return fmt.Errorf("going live with day broadcast: %w", err)
	}

	m.activeBroadcast = broadcastID
	m.broadcastType = "day"
	return nil
}

func (m *Manager) handleSunset(ctx context.Context, ev event.Event) error {
	date := ev.Payload["date"]

	// End day broadcast if active.
	if m.activeBroadcast != "" && m.broadcastType == "day" {
		prevBroadcastID := m.activeBroadcast
		if err := m.broadcaster.EndBroadcast(ctx, prevBroadcastID); err != nil {
			return fmt.Errorf("ending day broadcast: %w", err)
		}
		// Start downloading the day VOD in the background.
		m.startDownload(ctx, prevBroadcastID, date, "day")
	}

	// Create and go live with night broadcast.
	title := fmt.Sprintf("Birdcam Night %s", date)
	broadcastID, err := m.broadcaster.CreateBroadcast(ctx, title, ev.Time)
	if err != nil {
		return fmt.Errorf("creating night broadcast: %w", err)
	}
	if err := m.broadcaster.GoLive(ctx, broadcastID); err != nil {
		return fmt.Errorf("going live with night broadcast: %w", err)
	}

	m.activeBroadcast = broadcastID
	m.broadcastType = "night"
	return nil
}

func (m *Manager) startDownload(ctx context.Context, broadcastID, logicalDay, typ string) {
	dest := filepath.Join(m.dataDir, fmt.Sprintf("%s_%s.mp4", logicalDay, typ))
	go func() {
		err := m.downloader.Download(ctx, broadcastID, dest)
		if err != nil {
			log.Printf("download failed for %s %s: %v", typ, broadcastID, err)
			return
		}
		m.events <- event.Event{
			Kind: event.DownloadComplete,
			Time: time.Now(),
			Payload: map[string]string{
				"broadcast_id": broadcastID,
				"file_path":    dest,
				"logical_day":  logicalDay,
				"type":         typ,
			},
		}
	}()
}

func (m *Manager) handleDownloadComplete(_ context.Context, ev event.Event) error {
	logicalDay := ev.Payload["logical_day"]
	typ := ev.Payload["type"]
	filePath := ev.Payload["file_path"]

	m.mu.Lock()
	if m.pendingFiles[logicalDay] == nil {
		m.pendingFiles[logicalDay] = make(map[string]string)
	}
	m.pendingFiles[logicalDay][typ] = filePath
	files := m.pendingFiles[logicalDay]
	dayFile := files["day"]
	nightFile := files["night"]
	m.mu.Unlock()

	if dayFile != "" && nightFile != "" {
		m.events <- event.Event{
			Kind: event.DayFilesReady,
			Time: time.Now(),
			Payload: map[string]string{
				"logical_day": logicalDay,
				"day_file":    dayFile,
				"night_file":  nightFile,
			},
		}
	} else {
		log.Printf("download complete for %s/%s, waiting for other file", logicalDay, typ)
	}
	return nil
}

func (m *Manager) handleDayFilesReady(ctx context.Context, ev event.Event) error {
	logicalDay := ev.Payload["logical_day"]
	dayFile := ev.Payload["day_file"]
	nightFile := ev.Payload["night_file"]
	outFile := filepath.Join(m.dataDir, fmt.Sprintf("%s_highlights.mp4", logicalDay))

	if err := m.processor.Highlights(ctx, dayFile, nightFile, outFile); err != nil {
		return fmt.Errorf("generating highlights for %s: %w", logicalDay, err)
	}

	m.events <- event.Event{
		Kind: event.HighlightsReady,
		Time: time.Now(),
		Payload: map[string]string{
			"logical_day":     logicalDay,
			"highlights_file": outFile,
		},
	}
	return nil
}

func (m *Manager) handleHighlightsReady(ctx context.Context, ev event.Event) error {
	logicalDay := ev.Payload["logical_day"]
	highlightsFile := ev.Payload["highlights_file"]
	title := fmt.Sprintf("Birdcam Highlights %s", logicalDay)

	videoID, err := m.uploader.Upload(ctx, highlightsFile, title, time.Now().Add(24*time.Hour))
	if err != nil {
		return fmt.Errorf("uploading highlights for %s: %w", logicalDay, err)
	}

	m.events <- event.Event{
		Kind: event.UploadComplete,
		Time: time.Now(),
		Payload: map[string]string{
			"video_id":    videoID,
			"logical_day": logicalDay,
		},
	}
	return nil
}

func (m *Manager) handleUploadComplete(ev event.Event) error {
	logicalDay := ev.Payload["logical_day"]
	videoID := ev.Payload["video_id"]
	log.Printf("day %s complete: highlights uploaded as %s", logicalDay, videoID)

	m.mu.Lock()
	delete(m.pendingFiles, logicalDay)
	m.mu.Unlock()

	m.events <- event.Event{
		Kind:    event.DayComplete,
		Time:    time.Now(),
		Payload: map[string]string{"logical_day": logicalDay},
	}
	return nil
}
