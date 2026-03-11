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
	service    *yt.Service
	playlistID string
}

func New(httpClient *http.Client, playlistID string) (*Uploader, error) {
	service, err := yt.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating youtube service: %w", err)
	}
	return &Uploader{service: service, playlistID: playlistID}, nil
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

	if u.playlistID != "" {
		if err := u.addToPlaylist(ctx, resp.Id); err != nil {
			return resp.Id, fmt.Errorf("adding video %s to playlist %s: %w", resp.Id, u.playlistID, err)
		}
	}

	return resp.Id, nil
}

func (u *Uploader) addToPlaylist(ctx context.Context, videoID string) error {
	item := &yt.PlaylistItem{
		Snippet: &yt.PlaylistItemSnippet{
			PlaylistId: u.playlistID,
			ResourceId: &yt.ResourceId{
				Kind:    "youtube#video",
				VideoId: videoID,
			},
		},
	}
	_, err := u.service.PlaylistItems.Insert([]string{"snippet"}, item).Context(ctx).Do()
	return err
}
