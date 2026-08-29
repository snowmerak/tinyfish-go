package tinyfish

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultSearchEndpoint = "https://api.search.tinyfish.ai"
	defaultFetchEndpoint  = "https://api.fetch.tinyfish.ai"
	defaultUserAgent      = "tinyfish-go/dev"
)

// Limits configures the client-side rate limits. The TinyFish API remains the
// source of truth; these limits proactively smooth requests made by this client.
type Limits struct {
	SearchRequestsPerMinute int
	FetchURLsPerMinute      int
	Disabled                bool
}

// RetryPolicy controls retries for operations that are safe to repeat.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

type config struct {
	apiKey           string
	httpClient       *http.Client
	searchEndpoint   *url.URL
	fetchEndpoint    *url.URL
	userAgent        string
	limits           Limits
	retry            RetryPolicy
	searchTimeout    time.Duration
	fetchTimeout     time.Duration
	maxResponseBytes int64
}

func defaultConfig() config {
	searchEndpoint, _ := url.Parse(defaultSearchEndpoint)
	fetchEndpoint, _ := url.Parse(defaultFetchEndpoint)

	return config{
		httpClient:     &http.Client{},
		searchEndpoint: searchEndpoint,
		fetchEndpoint:  fetchEndpoint,
		userAgent:      defaultUserAgent,
		limits: Limits{
			SearchRequestsPerMinute: 30,
			FetchURLsPerMinute:      150,
		},
		retry: RetryPolicy{
			MaxAttempts: 3,
			BaseDelay:   250 * time.Millisecond,
			MaxDelay:    5 * time.Second,
		},
		searchTimeout:    10 * time.Second,
		fetchTimeout:     150 * time.Second,
		maxResponseBytes: 64 << 20,
	}
}

// Option configures a Client.
type Option func(*config) error

// WithAPIKey explicitly sets the TinyFish API key. If omitted, New reads
// TINYFISH_API_KEY once from the environment.
func WithAPIKey(apiKey string) Option {
	return func(cfg *config) error {
		cfg.apiKey = strings.TrimSpace(apiKey)
		return nil
	}
}

// WithHTTPClient replaces the HTTP client used for API calls.
func WithHTTPClient(client *http.Client) Option {
	return func(cfg *config) error {
		if client == nil {
			return errors.New("tinyfish: HTTP client must not be nil")
		}
		cfg.httpClient = client
		return nil
	}
}

// WithEndpoints overrides the Search and Fetch endpoints. It is primarily
// useful for testing and private gateways.
func WithEndpoints(searchEndpoint, fetchEndpoint string) Option {
	return func(cfg *config) error {
		searchURL, err := parseEndpoint("search", searchEndpoint)
		if err != nil {
			return err
		}
		fetchURL, err := parseEndpoint("fetch", fetchEndpoint)
		if err != nil {
			return err
		}
		cfg.searchEndpoint = searchURL
		cfg.fetchEndpoint = fetchURL
		return nil
	}
}

// WithUserAgent sets the User-Agent header sent by the client.
func WithUserAgent(userAgent string) Option {
	return func(cfg *config) error {
		if strings.TrimSpace(userAgent) == "" {
			return errors.New("tinyfish: user agent must not be empty")
		}
		cfg.userAgent = strings.TrimSpace(userAgent)
		return nil
	}
}

// WithLimits overrides the default client-side Search and Fetch limits.
func WithLimits(limits Limits) Option {
	return func(cfg *config) error {
		if !limits.Disabled && (limits.SearchRequestsPerMinute <= 0 || limits.FetchURLsPerMinute <= 0) {
			return errors.New("tinyfish: rate limits must be positive")
		}
		cfg.limits = limits
		return nil
	}
}

// WithoutRateLimiting disables proactive client-side rate limiting. Server-side
// limits and HTTP 429 responses still apply.
func WithoutRateLimiting() Option {
	return WithLimits(Limits{Disabled: true})
}

// WithRetryPolicy changes retry behavior for safe Search and Fetch requests.
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(cfg *config) error {
		if policy.MaxAttempts < 1 {
			return errors.New("tinyfish: retry MaxAttempts must be at least 1")
		}
		if policy.BaseDelay < 0 || policy.MaxDelay < 0 || policy.MaxDelay < policy.BaseDelay {
			return errors.New("tinyfish: invalid retry delays")
		}
		cfg.retry = policy
		return nil
	}
}

// WithTimeouts sets the fallback timeouts used when the caller's context has no
// deadline. A non-positive timeout disables the fallback for that API.
func WithTimeouts(searchTimeout, fetchTimeout time.Duration) Option {
	return func(cfg *config) error {
		cfg.searchTimeout = searchTimeout
		cfg.fetchTimeout = fetchTimeout
		return nil
	}
}

// WithMaxResponseBytes sets the maximum JSON response size accepted by the
// client. This protects callers from unexpectedly large responses.
func WithMaxResponseBytes(maxBytes int64) Option {
	return func(cfg *config) error {
		if maxBytes <= 0 {
			return errors.New("tinyfish: max response bytes must be positive")
		}
		cfg.maxResponseBytes = maxBytes
		return nil
	}
}

func parseEndpoint(name, rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("tinyfish: invalid " + name + " endpoint")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("tinyfish: " + name + " endpoint must use http or https")
	}
	return parsed, nil
}
