package downloader

import (
	"testing"
)

func TestDownloader_BuildArgs(t *testing.T) {
	d := New("/usr/bin/yt-dlp")
	args := d.BuildArgs("abc123", "/data/output.mp4")

	expected := []string{"/usr/bin/yt-dlp", "-o", "/data/output.mp4", "https://www.youtube.com/watch?v=abc123"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d", len(expected), len(args))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d]: expected %q, got %q", i, expected[i], a)
		}
	}
}
