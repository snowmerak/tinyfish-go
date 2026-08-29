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

func TestFetchListUsage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", request.Method)
		}
		if request.URL.Path != "/usage" {
			t.Errorf("path = %q, want /usage", request.URL.Path)
		}
		query := request.URL.Query()
		assertQueryValue(t, query.Get("start_after"), "2026-08-01T00:00:00Z")
		assertQueryValue(t, query.Get("end_before"), "2026-08-29T12:00:00Z")
		assertQueryValue(t, query.Get("status"), "completed")
		assertQueryValue(t, query.Get("limit"), "25")
		assertQueryValue(t, query.Get("page"), "2")

		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{
            "items":[{
                "id":"fetch-1",
                "url":"https://example.com",
                "final_url":"https://example.com",
                "title":"Example",
                "description":null,
                "language":"en",
                "author":null,
                "published_date":null,
                "format":"markdown",
                "status":"completed",
                "request_origin":"api",
                "request_id":"request-1",
                "text_length":123,
                "links_count":2,
                "image_links_count":1,
                "latency_ms":42.5,
                "created_at":"2026-08-20T10:00:00Z",
                "error":null
            }],
            "total":42,
            "limit":25,
            "page":2,
            "total_pages":2,
            "has_more":false
        }`)
	}))
	defer server.Close()

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	client := newTestClient(t, server.URL, server.URL)
	response, err := client.Fetch.ListUsage(context.Background(), FetchUsageRequest{
		StartAfter: &start,
		EndBefore:  &end,
		Status:     FetchUsageCompleted,
		Limit:      25,
		Page:       2,
	})
	if err != nil {
		t.Fatalf("ListUsage() error = %v", err)
	}
	if response.Total != 42 || len(response.Items) != 1 {
		t.Fatalf("unexpected response: %+v", response)
	}
	if response.Items[0].ID != "fetch-1" || response.Items[0].CreatedAt.IsZero() {
		t.Errorf("unexpected usage item: %+v", response.Items[0])
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
	if _, err := client.Fetch.ListUsage(context.Background(), FetchUsageRequest{Limit: 1001}); err == nil {
		t.Error("Fetch.ListUsage() accepted limit over 1000")
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
