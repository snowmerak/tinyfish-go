package tinyfish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

// FetchFormat selects the representation of extracted page content.
type FetchFormat string

const (
	FetchFormatMarkdown FetchFormat = "markdown"
	FetchFormatHTML     FetchFormat = "html"
	FetchFormatJSON     FetchFormat = "json"
)

// FetchRequest describes a TinyFish Fetch request.
type FetchRequest struct {
	URLs                       []string    `json:"urls" jsonschema:"one to ten public HTTP or HTTPS URLs to fetch"`
	Purpose                    string      `json:"purpose,omitempty" jsonschema:"why the content is needed, at most 2000 characters"`
	Format                     FetchFormat `json:"format,omitempty" jsonschema:"output format: markdown, html, or json"`
	Links                      bool        `json:"links,omitempty" jsonschema:"include links found in the page"`
	ImageLinks                 bool        `json:"image_links,omitempty" jsonschema:"include image URLs found in the page"`
	TTL                        *int        `json:"ttl,omitempty" jsonschema:"cache freshness in seconds; zero forces a live fetch"`
	PerURLTimeoutMilliseconds  *int        `json:"per_url_timeout_ms,omitempty" jsonschema:"per-URL timeout from 1 to 110000 milliseconds"`
	IfNoneMatch                string      `json:"if_none_match,omitempty" jsonschema:"ETag from an earlier single-URL fetch"`
	IfModifiedSince            string      `json:"if_modified_since,omitempty" jsonschema:"Last-Modified value from an earlier single-URL fetch"`
	IncludeETagAndLastModified bool        `json:"include_etag_and_last_modified,omitempty" jsonschema:"include cache validators in results"`
	IncludeSelectors           []string    `json:"include_selectors,omitempty" jsonschema:"CSS selectors whose matching content should be included"`
	ExcludeSelectors           []string    `json:"exclude_selectors,omitempty" jsonschema:"CSS selectors whose matching content should be removed"`
}

// FetchResponse contains successful results and per-URL failures. TinyFish may
// return both in a successful HTTP response.
type FetchResponse struct {
	Results []FetchResult `json:"results"`
	Errors  []FetchError  `json:"errors"`
}

// FetchResult is extracted content for one URL.
type FetchResult struct {
	URL                 string          `json:"url"`
	FinalURL            string          `json:"final_url"`
	Title               *string         `json:"title"`
	Description         *string         `json:"description"`
	Language            *string         `json:"language"`
	Author              *string         `json:"author"`
	PublishedDate       *string         `json:"published_date"`
	Text                json.RawMessage `json:"text"`
	Links               []string        `json:"links,omitempty"`
	ImageLinks          []string        `json:"image_links,omitempty"`
	NotModified         bool            `json:"not_modified,omitempty"`
	ETag                string          `json:"etag,omitempty"`
	LastModified        string          `json:"last_modified,omitempty"`
	UnmatchedSelectors  []string        `json:"unmatched_selectors,omitempty"`
	LatencyMilliseconds *float64        `json:"latency_ms"`
	Format              FetchFormat     `json:"format"`
}

// DecodeText unmarshals the result text into destination. For markdown and
// HTML, destination should point to a string. For JSON format, it may point to
// any compatible Go value.
func (result FetchResult) DecodeText(destination any) error {
	if len(result.Text) == 0 {
		return errors.New("tinyfish: fetch result has no text")
	}
	if err := json.Unmarshal(result.Text, destination); err != nil {
		return fmt.Errorf("tinyfish: decode fetch text: %w", err)
	}
	return nil
}

// TextString returns markdown or HTML text. It returns an error when the result
// contains structured JSON content instead.
func (result FetchResult) TextString() (string, error) {
	var text string
	if err := result.DecodeText(&text); err != nil {
		return "", err
	}
	return text, nil
}

// FetchError describes a failure for one URL in an otherwise successful batch.
type FetchError struct {
	URL                string   `json:"url"`
	Error              string   `json:"error"`
	Status             *int     `json:"status,omitempty"`
	UnmatchedSelectors []string `json:"unmatched_selectors,omitempty"`
	CandidateSelectors []string `json:"candidate_selectors,omitempty"`
}

// FetchService provides access to the TinyFish Fetch API.
type FetchService struct {
	client *Client
}

// GetContents fetches and extracts content from one to ten URLs.
func (service *FetchService) GetContents(ctx context.Context, request FetchRequest) (*FetchResponse, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}

	var response FetchResponse
	if err := service.client.doJSON(
		ctx,
		http.MethodPost,
		service.client.config.fetchEndpoint.String(),
		request,
		&response,
		service.client.config.fetchTimeout,
		service.client.fetchLimiter,
		len(request.URLs),
	); err != nil {
		return nil, err
	}
	return &response, nil
}

func (request FetchRequest) validate() error {
	if len(request.URLs) == 0 || len(request.URLs) > 10 {
		return errors.New("tinyfish: fetch requires between 1 and 10 URLs")
	}
	for _, rawURL := range request.URLs {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("tinyfish: invalid fetch URL %q", rawURL)
		}
	}
	if request.Purpose != "" {
		if strings.TrimSpace(request.Purpose) == "" {
			return errors.New("tinyfish: fetch purpose must not be whitespace-only")
		}
		if utf8.RuneCountInString(request.Purpose) > 2000 {
			return errors.New("tinyfish: fetch purpose must be at most 2000 characters")
		}
	}
	if request.Format != "" && !slices.Contains([]FetchFormat{
		FetchFormatMarkdown,
		FetchFormatHTML,
		FetchFormatJSON,
	}, request.Format) {
		return errors.New("tinyfish: fetch format must be markdown, html, or json")
	}
	if request.TTL != nil && *request.TTL < 0 {
		return errors.New("tinyfish: fetch ttl must not be negative")
	}
	if request.PerURLTimeoutMilliseconds != nil && (*request.PerURLTimeoutMilliseconds < 1 || *request.PerURLTimeoutMilliseconds > 110_000) {
		return errors.New("tinyfish: per_url_timeout_ms must be between 1 and 110000")
	}
	if len(request.URLs) != 1 && (request.IfNoneMatch != "" || request.IfModifiedSince != "") {
		return errors.New("tinyfish: conditional fetch fields require exactly one URL")
	}
	if err := validateSelectors("include_selectors", request.IncludeSelectors); err != nil {
		return err
	}
	return validateSelectors("exclude_selectors", request.ExcludeSelectors)
}

func validateSelectors(name string, selectors []string) error {
	if len(selectors) > 20 {
		return fmt.Errorf("tinyfish: %s accepts at most 20 selectors", name)
	}
	for _, selector := range selectors {
		length := utf8.RuneCountInString(selector)
		if length < 1 || length > 1000 {
			return fmt.Errorf("tinyfish: each %s entry must be between 1 and 1000 characters", name)
		}
	}
	return nil
}
