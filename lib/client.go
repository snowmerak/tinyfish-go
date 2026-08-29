package tinyfish

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/time/rate"
)

// Client is a TinyFish API client.
type Client struct {
	Search *SearchService
	Fetch  *FetchService

	config        config
	searchLimiter *rate.Limiter
	fetchLimiter  *rate.Limiter
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
		client.searchLimiter = rate.NewLimiter(
			rate.Limit(float64(cfg.limits.SearchRequestsPerMinute)/60),
			1,
		)
		client.fetchLimiter = rate.NewLimiter(
			rate.Limit(float64(cfg.limits.FetchURLsPerMinute)/60),
			10,
		)
	}

	client.Search = &SearchService{client: client}
	client.Fetch = &FetchService{client: client}
	return client, nil
}
