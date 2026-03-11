# CLAUDE.md — birdcam-automation

## What this project does

Automates a birdcam YouTube channel: runs day/night live broadcasts keyed to sunrise/sunset, downloads VODs via yt-dlp, detects bird activity with a Python script (`detect_birds.py`), and uploads highlight compilations.

## Commands

- `go run ./cmd/stream-manager` — long-running daemon (needs all env vars)
- `go run ./cmd/backfill --from YYYY-MM-DD --to YYYY-MM-DD` — backfill past dates
- `go run ./cmd/login` — OAuth token refresh

## Running locally

```bash
set -a && source .env && set +a
```

Python venv lives at `./venv/`. The `PYTHON_PATH` env var should point to `./venv/bin/python3`.

## Architecture

Event-driven: `scheduler` emits Sunrise/Sunset events, `manager` handles the pipeline (broadcast -> download -> detect -> upload). Each step emits the next event.

Key interfaces are defined in `internal/manager/manager.go` (Broadcaster, Downloader, Processor, Uploader). Tests use fakes, not mocks.

## Project conventions

- All env var loading happens in `internal/config/` (stream-manager) or directly in `main.go` (backfill, login)
- `internal/auth/` handles OAuth: loading client secrets, token files (supports both Go and Python token formats), and auto-refresh
- `downloader.Download` returns the actual filepath (yt-dlp picks the extension)
- `processor.ErrNoBirds` (exit code 2 from detect_birds.py) means no bird activity — callers should skip upload, not treat as failure
- Broadcasts are matched by scheduled start date, not title (titles can be identical across days)
- Highlight uploads are set to private with a scheduled publish time

## Build and verify

```bash
go build ./...
go test ./...
go vet ./...
```

## Files not in git

- `.env`, `token.json`, `client_secret.json` — credentials
- `*.mp4` — video files
- `venv/` — Python virtualenv
