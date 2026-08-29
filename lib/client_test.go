package tinyfish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSearchQuery(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", request.Method)
		}
		if got := request.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want test-key", got)
		}
		if got := request.Header.Get("User-Agent"); got != "test-agent" {
			t.Errorf("User-Agent = %q, want test-agent", got)
		}
		query := request.URL.Query()
		assertQueryValue(t, query.Get("query"), "go mcp")
		assertQueryValue(t, query.Get("include_domains"), "go.dev,github.com")
		assertQueryValue(t, query.Get("domain_type"), "news")
		assertQueryValue(t, query.Get("page"), "2")

		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"query":"go mcp","results":[{"position":1,"site_name":"go.dev","title":"Go","snippet":"A result","url":"https://go.dev"}],"total_results":1,"page":2}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, server.URL,
		WithUserAgent("test-agent"),
	)
	response, err := client.Search.Query(context.Background(), SearchRequest{
		Query:          "go mcp",
		IncludeDomains: []string{"go.dev", "github.com"},
		DomainType:     SearchDomainNews,
		Page:           2,
	})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if response.TotalResults != 1 || len(response.Results) != 1 {
		t.Fatalf("unexpected response: %+v", response)
	}
	if response.Results[0].URL != "https://go.dev" {
		t.Errorf("result URL = %q", response.Results[0].URL)
	}
}

func TestFetchGetContents(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", request.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if ttl, ok := body["ttl"]; !ok || ttl != float64(0) {
			t.Errorf("ttl = %#v, want explicit zero", ttl)
		}
		if got := body["format"]; got != "markdown" {
			t.Errorf("format = %#v, want markdown", got)
		}

		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{
            "results":[{
                "url":"https://example.com",
                "final_url":"https://www.example.com",
                "title":"Example",
                "description":null,
                "language":"en",
                "author":null,
                "published_date":null,
                "text":"# Example",
                "latency_ms":12.5,
                "format":"markdown"
            }],
            "errors":[{"url":"https://bad.example","error":"target_unreachable"}]
        }`)
	}))
	defer server.Close()

	ttl := 0
	client := newTestClient(t, server.URL, server.URL)
	response, err := client.Fetch.GetContents(context.Background(), FetchRequest{
		URLs:   []string{"https://example.com"},
		Format: FetchFormatMarkdown,
		TTL:    &ttl,
	})
	if err != nil {
		t.Fatalf("GetContents() error = %v", err)
	}
	if len(response.Results) != 1 || len(response.Errors) != 1 {
		t.Fatalf("unexpected response: %+v", response)
	}
	text, err := response.Results[0].TextString()
	if err != nil {
		t.Fatalf("TextString() error = %v", err)
	}
	if text != "# Example" {
		t.Errorf("text = %q", text)
	}
	if response.Errors[0].Error != "target_unreachable" {
		t.Errorf("per-URL error = %q", response.Errors[0].Error)
	}
}

func TestAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-Request-ID", "req-123")
		writer.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(writer, `{"error":{"code":"INVALID_API_KEY","message":"bad key","details":{"reason":"revoked"}}}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, server.URL)
	_, err := client.Search.Query(context.Background(), SearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("Query() error = nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized || apiErr.RequestID != "req-123" {
		t.Errorf("unexpected API error: %+v", apiErr)
	}
	if !IsErrorCode(fmt.Errorf("wrapped: %w", err), "INVALID_API_KEY") {
		t.Error("IsErrorCode() did not match wrapped API error")
	}
}

func TestRetriesSafeRequest(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(writer, `{"error":{"code":"SERVICE_BUSY","message":"try later"}}`)
			return
		}
		fmt.Fprint(writer, `{"query":"test","results":[],"total_results":0,"page":0}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, server.URL, WithRetryPolicy(RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   0,
		MaxDelay:    0,
	}))
	if _, err := client.Search.Query(context.Background(), SearchRequest{Query: "test"}); err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestSearchRateLimitHonorsContext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"query":"test","results":[],"total_results":0,"page":0}`)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-key"),
		WithEndpoints(server.URL, server.URL),
		WithLimits(Limits{SearchRequestsPerMinute: 1, FetchURLsPerMinute: 1}),
		WithRetryPolicy(RetryPolicy{MaxAttempts: 1}),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := client.Search.Query(context.Background(), SearchRequest{Query: "first"}); err != nil {
		t.Fatalf("first Query() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := client.Search.Query(ctx, SearchRequest{Query: "second"}); err == nil {
		t.Fatal("second Query() error = nil, want rate-limit wait error")
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	recency := 60
	if _, err := client.Search.Query(context.Background(), SearchRequest{
		Query:          "test",
		RecencyMinutes: &recency,
		AfterDate:      "2026-01-01",
	}); err == nil {
		t.Error("Search.Query() accepted recency with date filter")
	}
	if _, err := client.Fetch.GetContents(context.Background(), FetchRequest{
		URLs:        []string{"https://one.example", "https://two.example"},
		IfNoneMatch: `"etag"`,
	}); err == nil {
		t.Error("Fetch.GetContents() accepted conditional batch")
	}
}

func newTestClient(t *testing.T, searchEndpoint, fetchEndpoint string, options ...Option) *Client {
	t.Helper()
	options = append([]Option{
		WithAPIKey("test-key"),
		WithEndpoints(searchEndpoint, fetchEndpoint),
		WithoutRateLimiting(),
		WithRetryPolicy(RetryPolicy{MaxAttempts: 1}),
	}, options...)
	client, err := New(options...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func assertQueryValue(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("query value = %q, want %q", got, want)
	}
}
