package youtube

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

type Client struct {
	service *yt.Service
}

func New(ctx context.Context, oauthTokenFile string) (*Client, error) {
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

	return &Client{service: service}, nil
}

func (c *Client) CreateBroadcast(ctx context.Context, title string, scheduledStart time.Time) (string, error) {
	broadcast := &yt.LiveBroadcast{
		Snippet: &yt.LiveBroadcastSnippet{
			Title:              title,
			ScheduledStartTime: scheduledStart.Format(time.RFC3339),
		},
		Status: &yt.LiveBroadcastStatus{
			PrivacyStatus: "public",
		},
	}

	resp, err := c.service.LiveBroadcasts.Insert([]string{"snippet", "status"}, broadcast).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("creating broadcast %q: %w", title, err)
	}
	return resp.Id, nil
}

func (c *Client) GoLive(ctx context.Context, broadcastID string) error {
	_, err := c.service.LiveBroadcasts.Transition("live", broadcastID, []string{"status"}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("transitioning broadcast %s to live: %w", broadcastID, err)
	}
	return nil
}

func (c *Client) EndBroadcast(ctx context.Context, broadcastID string) error {
	_, err := c.service.LiveBroadcasts.Transition("complete", broadcastID, []string{"status"}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("ending broadcast %s: %w", broadcastID, err)
	}
	return nil
}
