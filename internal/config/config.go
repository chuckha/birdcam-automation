package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Latitude              float64
	Longitude             float64
	YouTubeStreamKey      string
	OAuthClientSecretFile string
	OAuthTokenFile        string
	DataDir               string
	PythonPath            string
	HighlightsScript      string
	YtdlpPath             string
	TimeZone              *time.Location
}

func Load() (Config, error) {
	lat, err := requiredFloat("LATITUDE")
	if err != nil {
		return Config{}, err
	}
	lon, err := requiredFloat("LONGITUDE")
	if err != nil {
		return Config{}, err
	}
	streamKey, err := required("YOUTUBE_STREAM_KEY")
	if err != nil {
		return Config{}, err
	}
	clientSecretFile, err := required("OAUTH_CLIENT_SECRET_FILE")
	if err != nil {
		return Config{}, err
	}
	oauthFile, err := required("OAUTH_TOKEN_FILE")
	if err != nil {
		return Config{}, err
	}
	dataDir := withDefault("DATA_DIR", "/data")
	pythonPath := withDefault("PYTHON_PATH", "python3")
	highlightsScript := withDefault("HIGHLIGHTS_SCRIPT", "detect_birds.py")
	ytdlpPath := withDefault("YTDLP_PATH", "yt-dlp")
	tzName := withDefault("TIMEZONE", "UTC")

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return Config{}, fmt.Errorf("invalid TIMEZONE %q: %w", tzName, err)
	}

	return Config{
		Latitude:              lat,
		Longitude:             lon,
		YouTubeStreamKey:      streamKey,
		OAuthClientSecretFile: clientSecretFile,
		OAuthTokenFile:        oauthFile,
		DataDir:               dataDir,
		PythonPath:            pythonPath,
		HighlightsScript:      highlightsScript,
		YtdlpPath:        ytdlpPath,
		TimeZone:         loc,
	}, nil
}

func required(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("required environment variable %s is not set", key)
	}
	return v, nil
}

func requiredFloat(key string) (float64, error) {
	s, err := required(key)
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("environment variable %s must be a number: %w", key, err)
	}
	return f, nil
}

func withDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}
