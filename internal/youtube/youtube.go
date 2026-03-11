package youtube

import (
	"context"
	"fmt"
	"net/http"
	"time"

	yt "google.golang.org/api/youtube/v3"
)

type Client struct {
	service *yt.Service
}

func New(httpClient *http.Client) (*Client, error) {
	service, err := yt.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("creating youtube service: %w", err)
	}
	return &Client{service: service}, nil
}

type Broadcast struct {
	ID    string
	Title string
	Start time.Time
}

func (c *Client) ListCompletedBroadcasts(ctx context.Context) ([]Broadcast, error) {
	var broadcasts []Broadcast
	pageToken := ""
	for {
		call := c.service.LiveBroadcasts.List([]string{"snippet"}).
			BroadcastStatus("completed").
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("listing completed broadcasts: %w", err)
		}

		for _, b := range resp.Items {
			startStr := b.Snippet.ActualStartTime
			if startStr == "" {
				startStr = b.Snippet.ScheduledStartTime
			}
			if startStr == "" {
				continue
			}
			start, err := time.Parse(time.RFC3339, startStr)
			if err != nil {
				return nil, fmt.Errorf("parsing start time for broadcast %s: %w", b.Id, err)
			}
			broadcasts = append(broadcasts, Broadcast{
				ID:    b.Id,
				Title: b.Snippet.Title,
				Start: start,
			})
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return broadcasts, nil
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
