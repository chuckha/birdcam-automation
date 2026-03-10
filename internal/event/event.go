package event

import "time"

type Kind string

const (
	Sunrise          Kind = "Sunrise"
	Sunset           Kind = "Sunset"
	DownloadComplete Kind = "DownloadComplete"
	DayFilesReady    Kind = "DayFilesReady"
	HighlightsReady  Kind = "HighlightsReady"
	UploadComplete   Kind = "UploadComplete"
	DayComplete      Kind = "DayComplete"
)

type Event struct {
	Kind    Kind
	Time    time.Time
	Payload map[string]string
}
