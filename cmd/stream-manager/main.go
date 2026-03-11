package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/chuckha/birdcam-automation/internal/auth"
	"github.com/chuckha/birdcam-automation/internal/config"
	"github.com/chuckha/birdcam-automation/internal/downloader"
	"github.com/chuckha/birdcam-automation/internal/event"
	"github.com/chuckha/birdcam-automation/internal/manager"
	"github.com/chuckha/birdcam-automation/internal/processor"
	"github.com/chuckha/birdcam-automation/internal/scheduler"
	"github.com/chuckha/birdcam-automation/internal/uploader"
	"github.com/chuckha/birdcam-automation/internal/youtube"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	oauthConfig, err := auth.LoadConfig(cfg.OAuthClientSecretFile)
	if err != nil {
		log.Fatalf("loading oauth config: %v", err)
	}
	httpClient, err := auth.NewClient(ctx, oauthConfig, cfg.OAuthTokenFile)
	if err != nil {
		log.Fatalf("creating oauth client: %v", err)
	}

	ytClient, err := youtube.New(httpClient)
	if err != nil {
		log.Fatalf("creating youtube client: %v", err)
	}

	up, err := uploader.New(httpClient)
	if err != nil {
		log.Fatalf("creating uploader: %v", err)
	}

	dl := downloader.New(cfg.YtdlpPath)
	proc := processor.New(cfg.PythonPath, cfg.HighlightsScript)
	sched := scheduler.New(cfg.Latitude, cfg.Longitude, cfg.TimeZone)

	events := make(chan event.Event, 16)
	mgr := manager.New(ytClient, dl, proc, up, cfg.DataDir, events)

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return sched.Run(ctx, events)
	})

	g.Go(func() error {
		return mgr.Run(ctx)
	})

	log.Println("stream-manager started (pid:", os.Getpid(), ")")
	if err := g.Wait(); err != nil && err != context.Canceled {
		log.Fatalf("stream-manager exited with error: %v", err)
	}
	log.Println("stream-manager stopped")
}
