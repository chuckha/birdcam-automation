package downloader

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type Downloader struct {
	ytdlpPath string
}

func New(ytdlpPath string) *Downloader {
	return &Downloader{ytdlpPath: ytdlpPath}
}

// Download fetches a YouTube video and returns the path to the downloaded file.
// dest is used as the output template base (without extension); yt-dlp picks the extension.
func (d *Downloader) Download(ctx context.Context, broadcastID string, dest string) (string, error) {
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", broadcastID)

	// Strip any extension from dest so yt-dlp can append the real one.
	base := strings.TrimSuffix(dest, filepath.Ext(dest))
	tmpl := base + ".%(ext)s"

	cmd := exec.CommandContext(ctx, d.ytdlpPath, "-o", tmpl, "--print", "after_move:filepath", url)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("yt-dlp failed for %s: %s: %w", broadcastID, string(output), err)
	}
	return strings.TrimSpace(string(output)), nil
}

// BuildArgs returns the command arguments that would be used for a download.
func (d *Downloader) BuildArgs(broadcastID string, dest string) []string {
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", broadcastID)
	return []string{d.ytdlpPath, "-o", dest, url}
}
