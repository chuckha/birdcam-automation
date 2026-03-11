package uploader

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	yt "google.golang.org/api/youtube/v3"
)

type Uploader struct {
	service *yt.Service
}

func New(httpClient *http.Client) (*Uploader, error) {
	service, err := yt.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating youtube service: %w", err)
	}
	return &Uploader{service: service}, nil
}

func (u *Uploader) Upload(ctx context.Context, filePath, title string, publishAt time.Time) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("opening video file %s: %w", filePath, err)
	}
	defer f.Close()

	video := &yt.Video{
		Snippet: &yt.VideoSnippet{
			Title: title,
		},
		Status: &yt.VideoStatus{
			PrivacyStatus: "private",
			PublishAt:     publishAt.Format(time.RFC3339),
		},
	}

	resp, err := u.service.Videos.Insert([]string{"snippet", "status"}, video).Media(f).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("uploading video %q: %w", title, err)
	}
	return resp.Id, nil
}
