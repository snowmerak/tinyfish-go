package tinyfish

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// FetchUsageStatus filters usage history by operation result.
type FetchUsageStatus string

const (
	FetchUsageCompleted FetchUsageStatus = "completed"
	FetchUsageFailed    FetchUsageStatus = "failed"
)

// FetchUsageRequest filters and paginates Fetch usage history.
type FetchUsageRequest struct {
	StartAfter *time.Time       `json:"start_after,omitempty" jsonschema:"return records created after this ISO 8601 timestamp"`
	EndBefore  *time.Time       `json:"end_before,omitempty" jsonschema:"return records created before this ISO 8601 timestamp"`
	Status     FetchUsageStatus `json:"status,omitempty" jsonschema:"result status: completed or failed"`
	Limit      int              `json:"limit,omitempty" jsonschema:"items per page from 1 to 1000; defaults to 100"`
	Page       int              `json:"page,omitempty" jsonschema:"one-based page number; defaults to 1"`
}

// FetchUsageResponse is a page of Fetch usage records.
type FetchUsageResponse struct {
	Items      []FetchUsageItem `json:"items"`
	Total      int              `json:"total"`
	Limit      int              `json:"limit"`
	Page       int              `json:"page"`
	TotalPages int              `json:"total_pages"`
	HasMore    bool             `json:"has_more"`
}

// FetchUsageItem describes one URL processed by the Fetch API. Usage responses
// contain metadata only and do not include the extracted page text.
type FetchUsageItem struct {
	ID                  string           `json:"id"`
	URL                 string           `json:"url"`
	FinalURL            string           `json:"final_url"`
	Title               *string          `json:"title"`
	Description         *string          `json:"description"`
	Language            *string          `json:"language"`
	Author              *string          `json:"author"`
	PublishedDate       *string          `json:"published_date"`
	Format              FetchFormat      `json:"format"`
	Status              FetchUsageStatus `json:"status"`
	RequestOrigin       string           `json:"request_origin"`
	RequestID           *string          `json:"request_id"`
	TextLength          *int             `json:"text_length"`
	LinksCount          int              `json:"links_count"`
	ImageLinksCount     int              `json:"image_links_count"`
	LatencyMilliseconds *float64         `json:"latency_ms"`
	NotModified         bool             `json:"not_modified,omitempty"`
	ETag                string           `json:"etag,omitempty"`
	LastModified        string           `json:"last_modified,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	Error               *string          `json:"error"`
}

// ListUsage retrieves a paginated history of Fetch operations. Reading usage
// metadata does not consume the Fetch URL rate limiter.
func (service *FetchService) ListUsage(ctx context.Context, request FetchUsageRequest) (*FetchUsageResponse, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}

	endpoint := *service.client.config.fetchEndpoint
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/usage"
	endpoint.RawPath = ""
	query := endpoint.Query()
	if request.StartAfter != nil {
		query.Set("start_after", request.StartAfter.Format(time.RFC3339Nano))
	}
	if request.EndBefore != nil {
		query.Set("end_before", request.EndBefore.Format(time.RFC3339Nano))
	}
	setIfNotEmpty(query, "status", string(request.Status))
	if request.Limit != 0 {
		query.Set("limit", strconv.Itoa(request.Limit))
	}
	if request.Page != 0 {
		query.Set("page", strconv.Itoa(request.Page))
	}
	endpoint.RawQuery = query.Encode()

	var response FetchUsageResponse
	if err := service.client.doJSON(
		ctx,
		http.MethodGet,
		endpoint.String(),
		nil,
		&response,
		service.client.config.searchTimeout,
		nil,
		0,
	); err != nil {
		return nil, err
	}
	return &response, nil
}

func (request FetchUsageRequest) validate() error {
	if request.StartAfter != nil && request.EndBefore != nil && request.StartAfter.After(*request.EndBefore) {
		return errors.New("tinyfish: start_after must not be after end_before")
	}
	if request.Status != "" && request.Status != FetchUsageCompleted && request.Status != FetchUsageFailed {
		return errors.New("tinyfish: fetch usage status must be completed or failed")
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return errors.New("tinyfish: fetch usage limit must be between 1 and 1000 when provided")
	}
	if request.Page < 0 {
		return errors.New("tinyfish: fetch usage page must be at least 1 when provided")
	}
	return nil
}
