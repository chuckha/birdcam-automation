package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/chuckha/birdcam-automation/internal/auth"
	"github.com/chuckha/birdcam-automation/internal/downloader"
	"github.com/chuckha/birdcam-automation/internal/processor"
	"github.com/chuckha/birdcam-automation/internal/uploader"
	"github.com/chuckha/birdcam-automation/internal/youtube"
)

func main() {
	var fromStr, toStr string
	var dryRun bool
	var uploadOnly string
	flag.StringVar(&fromStr, "from", "", "start date (YYYY-MM-DD, required)")
	flag.StringVar(&toStr, "to", "", "end date inclusive (YYYY-MM-DD, required)")
	flag.BoolVar(&dryRun, "dry-run", false, "list what would be done without executing")
	flag.StringVar(&uploadOnly, "upload-only", "", "upload an existing highlights file (path) for the --from date, skip download/process")
	flag.Parse()

	if fromStr == "" || toStr == "" {
		fmt.Fprintln(os.Stderr, "both --from and --to are required")
		flag.Usage()
		os.Exit(1)
	}

	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		log.Fatalf("invalid --from date: %v", err)
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		log.Fatalf("invalid --to date: %v", err)
	}
	if to.Before(from) {
		log.Fatal("--to must not be before --from")
	}

	clientSecretFile := requireEnv("OAUTH_CLIENT_SECRET_FILE")
	oauthFile := requireEnv("OAUTH_TOKEN_FILE")
	dataDir := envDefault("DATA_DIR", "/data")
	pythonPath := envDefault("PYTHON_PATH", "python3")
	highlightsScript := envDefault("HIGHLIGHTS_SCRIPT", "detect_birds.py")
	ytdlpPath := envDefault("YTDLP_PATH", "yt-dlp")
	playlistID := os.Getenv("YOUTUBE_PLAYLIST_ID")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	oauthConfig, err := auth.LoadConfig(clientSecretFile)
	if err != nil {
		log.Fatalf("loading oauth config: %v", err)
	}
	httpClient, err := auth.NewClient(ctx, oauthConfig, oauthFile)
	if err != nil {
		log.Fatalf("creating oauth client: %v", err)
	}

	ytClient, err := youtube.New(httpClient)
	if err != nil {
		log.Fatalf("creating youtube client: %v", err)
	}

	broadcasts, err := ytClient.ListCompletedBroadcasts(ctx)
	if err != nil {
		log.Fatalf("listing completed broadcasts: %v", err)
	}

	// Index broadcasts by their scheduled start date (YYYY-MM-DD in UTC).
	dateIndex := make(map[string]youtube.Broadcast, len(broadcasts))
	for _, b := range broadcasts {
		dateIndex[b.Start.UTC().Format("2006-01-02")] = b
	}

	if dryRun {
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			date := d.Format("2006-01-02")
			b, ok := dateIndex[date]
			if !ok {
				log.Printf("[dry-run] %s: no broadcast found", date)
				continue
			}
			log.Printf("[dry-run] %s: found broadcast %s (%q), would download, process, and upload", date, b.ID, b.Title)
		}
		return
	}

	dl := downloader.New(ytdlpPath)
	proc := processor.New(pythonPath, highlightsScript)
	up, err := uploader.New(httpClient, playlistID)
	if err != nil {
		log.Fatalf("creating uploader: %v", err)
	}

	// Schedule uploads one per day at 8 AM UTC, starting tomorrow.
	now := time.Now().UTC()
	nextPublish := time.Date(now.Year(), now.Month(), now.Day()+1, 8, 0, 0, 0, time.UTC)
	publishIndex := 0

	if uploadOnly != "" {
		date := from.Format("2006-01-02")
		title := "Birdcam Highlights " + date
		publishAt := nextPublish
		videoID, err := up.Upload(ctx, uploadOnly, title, publishAt)
		if err != nil {
			log.Fatalf("%s: uploading: %v", date, err)
		}
		log.Printf("%s: uploaded as %s (publishes %s)", date, videoID, publishAt.Format(time.RFC3339))
		return
	}

	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")

		b, ok := dateIndex[date]
		if !ok {
			log.Printf("%s: no broadcast found, skipping", date)
			continue
		}
		log.Printf("%s: processing broadcast %s (%q)", date, b.ID, b.Title)

		highlightsFile := filepath.Join(dataDir, date+"_highlights.mp4")

		// Use existing VOD file if already downloaded.
		vodFile := findExisting(dataDir, date+"_day")
		if vodFile != "" {
			log.Printf("%s: using existing file %s", date, vodFile)
		} else {
			dest := filepath.Join(dataDir, date+"_day.mp4")
			var dlErr error
			vodFile, dlErr = dl.Download(ctx, b.ID, dest)
			if dlErr != nil {
				log.Fatalf("%s: downloading VOD: %v", date, dlErr)
			}
		}
		actualVodFile := vodFile

		if err := proc.ProcessSingle(ctx, actualVodFile, highlightsFile); err != nil {
			if errors.Is(err, processor.ErrNoBirds) {
				log.Printf("%s: no bird activity, skipping upload", date)
				continue
			}
			log.Fatalf("%s: processing highlights: %v", date, err)
		}

		publishAt := nextPublish.Add(time.Duration(publishIndex) * 24 * time.Hour)
		highlightsTitle := "Birdcam Highlights " + date
		videoID, err := up.Upload(ctx, highlightsFile, highlightsTitle, publishAt)
		if err != nil {
			log.Fatalf("%s: uploading highlights: %v", date, err)
		}
		log.Printf("%s: uploaded highlights as %s (publishes %s)", date, videoID, publishAt.Format(time.RFC3339))
		publishIndex++
	}

	log.Println("backfill complete")
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

// findExisting looks for a file matching base.* in dir (e.g. "2026-03-04_day.*").
func findExisting(dir, base string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, base+".*"))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

func envDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}
