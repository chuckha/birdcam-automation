package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var scopes = []string{
	"https://www.googleapis.com/auth/youtube",
	"https://www.googleapis.com/auth/youtube.upload",
}

// LoadConfig reads a Google OAuth client_secret JSON file and returns
// the oauth2 config with YouTube scopes.
func LoadConfig(clientSecretFile string) (*oauth2.Config, error) {
	data, err := os.ReadFile(clientSecretFile)
	if err != nil {
		return nil, fmt.Errorf("reading client secret file: %w", err)
	}
	config, err := google.ConfigFromJSON(data, scopes...)
	if err != nil {
		return nil, fmt.Errorf("parsing client secret: %w", err)
	}
	return config, nil
}

// LoadToken reads a token JSON file. It supports both Go-style
// (access_token) and Python-style (token) field names.
func LoadToken(tokenFile string) (*oauth2.Token, error) {
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("reading token file: %w", err)
	}

	// Try Go format first.
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("parsing token: %w", err)
	}

	// If access_token was empty, try Python format.
	if tok.AccessToken == "" {
		var pyTok struct {
			Token        string    `json:"token"`
			RefreshToken string    `json:"refresh_token"`
			Expiry       time.Time `json:"expiry"`
		}
		if err := json.Unmarshal(data, &pyTok); err != nil {
			return nil, fmt.Errorf("parsing token (python format): %w", err)
		}
		tok.AccessToken = pyTok.Token
		tok.RefreshToken = pyTok.RefreshToken
		tok.Expiry = pyTok.Expiry
		tok.TokenType = "Bearer"
	}

	return &tok, nil
}

// SaveToken writes a token to the given file in Go's oauth2 format.
func SaveToken(tokenFile string, token *oauth2.Token) error {
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling token: %w", err)
	}
	if err := os.WriteFile(tokenFile, data, 0600); err != nil {
		return fmt.Errorf("writing token file: %w", err)
	}
	return nil
}

// NewClient returns an HTTP client that auto-refreshes the token using
// the provided config and saves refreshed tokens back to tokenFile.
func NewClient(ctx context.Context, config *oauth2.Config, tokenFile string) (*http.Client, error) {
	token, err := LoadToken(tokenFile)
	if err != nil {
		return nil, err
	}

	// TokenSource that auto-refreshes.
	ts := config.TokenSource(ctx, token)

	// Eagerly refresh if expired, and save the new token.
	newToken, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}
	if newToken.AccessToken != token.AccessToken {
		if err := SaveToken(tokenFile, newToken); err != nil {
			return nil, err
		}
	}

	return oauth2.NewClient(ctx, ts), nil
}
