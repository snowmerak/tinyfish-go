package tinyfish

import (
	"errors"
	"os"
	"strings"
	"time"
)

// Client is a TinyFish API client.
type Client struct {
	Search *SearchService
	Fetch  *FetchService

	config        config
	searchLimiter requestLimiter
	fetchLimiter  requestLimiter
}

// New constructs a TinyFish client. By default, the API key is read from
// TINYFISH_API_KEY.
func New(options ...Option) (*Client, error) {
	cfg := defaultConfig()
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&cfg); err != nil {
			return nil, err
		}
	}

	if cfg.apiKey == "" {
		cfg.apiKey = strings.TrimSpace(os.Getenv("TINYFISH_API_KEY"))
	}
	if cfg.apiKey == "" {
		return nil, errors.New("tinyfish: API key is required; set TINYFISH_API_KEY or use WithAPIKey")
	}

	client := &Client{config: cfg}
	if !cfg.limits.Disabled {
		client.searchLimiter = newSlidingWindowLimiter(cfg.limits.SearchRequestsPerMinute, time.Minute)
		client.fetchLimiter = newSlidingWindowLimiter(cfg.limits.FetchURLsPerMinute, time.Minute)
	}

	client.Search = &SearchService{client: client}
	client.Fetch = &FetchService{client: client}
	return client, nil
}
