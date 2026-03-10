package downloader

import (
	"context"
	"fmt"
	"os/exec"
)

type Downloader struct {
	ytdlpPath string
}

func New(ytdlpPath string) *Downloader {
	return &Downloader{ytdlpPath: ytdlpPath}
}

func (d *Downloader) Download(ctx context.Context, broadcastID string, dest string) error {
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", broadcastID)
	cmd := exec.CommandContext(ctx, d.ytdlpPath, "-o", dest, url)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("yt-dlp failed for %s: %s: %w", broadcastID, string(output), err)
	}
	return nil
}

// BuildArgs returns the command arguments that would be used for a download.
// Useful for testing command construction without executing.
func (d *Downloader) BuildArgs(broadcastID string, dest string) []string {
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", broadcastID)
	return []string{d.ytdlpPath, "-o", dest, url}
}
