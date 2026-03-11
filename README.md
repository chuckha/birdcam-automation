# birdcam-automation

Automates a birdcam live stream on YouTube. Runs day/night broadcasts on a sunrise/sunset schedule, downloads VODs, detects bird activity with a Python script, and uploads highlight videos.

## Commands

### stream-manager

Long-running daemon that manages the full lifecycle:

1. Starts a "day" broadcast at sunrise, "night" broadcast at sunset
2. Downloads each VOD after the broadcast ends
3. Runs `detect_birds.py` on both day and night videos
4. Concatenates highlights and uploads to YouTube (private, publishes 24h later)

```bash
go run ./cmd/stream-manager
```

### backfill

One-off tool to process past broadcasts. Finds completed broadcasts by date, downloads VODs, runs bird detection, and uploads highlights.

```bash
go run ./cmd/backfill --from 2026-03-04 --to 2026-03-08 --dry-run
go run ./cmd/backfill --from 2026-03-04 --to 2026-03-08
```

- Skips download if a matching file already exists in `DATA_DIR`
- Skips upload if no bird activity is detected
- Uploads are scheduled one per day at 8 AM UTC

### login

OAuth login flow. Run this to get a fresh token when yours expires.

```bash
go run ./cmd/login
```

## Setup

1. Create a Google Cloud project with the YouTube Data API v3 enabled
2. Create OAuth 2.0 credentials (Desktop app) and download `client_secret.json`
3. Copy `.env.example` to `.env` and fill in the values
4. Create a Python venv and install dependencies:
   ```bash
   python3 -m venv venv
   venv/bin/pip install opencv-python numpy Pillow
   ```
5. Run `go run ./cmd/login` to authenticate
6. Source your env and run:
   ```bash
   set -a && source .env && set +a
   go run ./cmd/stream-manager
   ```

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `OAUTH_CLIENT_SECRET_FILE` | yes | | Path to Google OAuth client secret JSON |
| `OAUTH_TOKEN_FILE` | yes | | Path to save/load the OAuth token |
| `DATA_DIR` | no | `/data` | Directory for downloaded VODs and highlights |
| `PYTHON_PATH` | no | `python3` | Python interpreter (use venv path) |
| `HIGHLIGHTS_SCRIPT` | no | `detect_birds.py` | Path to the bird detection script |
| `YTDLP_PATH` | no | `yt-dlp` | Path to yt-dlp binary |
| `LATITUDE` | stream-manager | | Location latitude for sunrise/sunset |
| `LONGITUDE` | stream-manager | | Location longitude for sunrise/sunset |
| `YOUTUBE_STREAM_KEY` | stream-manager | | YouTube stream key |
| `TIMEZONE` | no | `UTC` | Timezone for scheduling |

## Architecture

```
cmd/
  stream-manager/    daemon: scheduler -> manager event loop
  backfill/          one-off: date range -> download -> detect -> upload
  login/             OAuth login flow

internal/
  auth/              OAuth config, token loading/saving, auto-refresh
  config/            env var loading and validation
  scheduler/         sunrise/sunset event emitter
  manager/           event-driven orchestrator
  youtube/           YouTube Live Streaming API (broadcasts)
  downloader/        yt-dlp wrapper
  processor/         detect_birds.py runner + ffmpeg concat
  uploader/          YouTube Data API (video uploads)
  event/             event type definitions
```

The stream-manager uses an event-driven architecture. The scheduler emits `Sunrise`/`Sunset` events, and the manager reacts by creating broadcasts, downloading VODs, running highlights, and uploading — each step emitting the next event.
