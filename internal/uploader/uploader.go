package uploader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	yt "google.golang.org/api/youtube/v3"
)

type Uploader struct {
	service *yt.Service
}

func New(ctx context.Context, oauthTokenFile string) (*Uploader, error) {
	tokenData, err := os.ReadFile(oauthTokenFile)
	if err != nil {
		return nil, fmt.Errorf("reading oauth token file: %w", err)
	}

	var token oauth2.Token
	if err := json.Unmarshal(tokenData, &token); err != nil {
		return nil, fmt.Errorf("parsing oauth token: %w", err)
	}

	config := &oauth2.Config{
		Endpoint: google.Endpoint,
	}
	client := config.Client(ctx, &token)

	service, err := yt.New(client)
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
