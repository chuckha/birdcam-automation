package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"golang.org/x/oauth2"

	"github.com/chuckha/birdcam-automation/internal/auth"
)

func main() {
	clientSecretFile := os.Getenv("OAUTH_CLIENT_SECRET_FILE")
	if clientSecretFile == "" {
		log.Fatal("OAUTH_CLIENT_SECRET_FILE is not set")
	}
	tokenFile := os.Getenv("OAUTH_TOKEN_FILE")
	if tokenFile == "" {
		log.Fatal("OAUTH_TOKEN_FILE is not set")
	}

	config, err := auth.LoadConfig(clientSecretFile)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	// Use OOB-style redirect for CLI.
	config.RedirectURL = "http://localhost:8085"

	url := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Open this URL in your browser:\n\n%s\n\n", url)
	fmt.Print("After authorizing, you'll be redirected. Paste the full redirect URL here: ")

	var redirectURL string
	fmt.Scanln(&redirectURL)

	// Parse the code from the redirect URL.
	code, err := extractCode(redirectURL)
	if err != nil {
		log.Fatalf("extracting code: %v", err)
	}

	token, err := config.Exchange(context.Background(), code)
	if err != nil {
		log.Fatalf("exchanging code for token: %v", err)
	}

	if err := auth.SaveToken(tokenFile, token); err != nil {
		log.Fatalf("saving token: %v", err)
	}

	fmt.Printf("Token saved to %s\n", tokenFile)
}

func extractCode(rawURL string) (string, error) {
	// Handle both a full URL and a bare code.
	if len(rawURL) == 0 {
		return "", fmt.Errorf("empty input")
	}

	// If it looks like a URL, parse the code param.
	if len(rawURL) > 4 && rawURL[:4] == "http" {
		// Quick parse: find "code=" and extract value.
		const key = "code="
		idx := -1
		for i := 0; i < len(rawURL)-len(key); i++ {
			if rawURL[i:i+len(key)] == key {
				idx = i + len(key)
				break
			}
		}
		if idx == -1 {
			return "", fmt.Errorf("no code parameter found in URL")
		}
		end := len(rawURL)
		for i := idx; i < len(rawURL); i++ {
			if rawURL[i] == '&' {
				end = i
				break
			}
		}
		return rawURL[idx:end], nil
	}

	return rawURL, nil
}
