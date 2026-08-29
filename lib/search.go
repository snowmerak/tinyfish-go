package tinyfish

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// SearchDomainType selects the kind of result returned by Search.
type SearchDomainType string

const (
	SearchDomainWeb           SearchDomainType = "web"
	SearchDomainNews          SearchDomainType = "news"
	SearchDomainResearchPaper SearchDomainType = "research_paper"
)

// SearchRequest describes a TinyFish Search request.
type SearchRequest struct {
	Query              string           `json:"query" jsonschema:"the web search query"`
	Purpose            string           `json:"purpose,omitempty" jsonschema:"why the results are needed, at most 2000 characters"`
	Location           string           `json:"location,omitempty" jsonschema:"country code for geo-targeted results, for example US"`
	Language           string           `json:"language,omitempty" jsonschema:"language code for results, for example en"`
	IncludeDomains     []string         `json:"include_domains,omitempty" jsonschema:"domains to include"`
	ExcludeDomains     []string         `json:"exclude_domains,omitempty" jsonschema:"domains to exclude"`
	RecencyMinutes     *int             `json:"recency_minutes,omitempty" jsonschema:"freshness window in minutes from 1 to 5256000"`
	AfterDate          string           `json:"after_date,omitempty" jsonschema:"lower date bound in YYYY-MM-DD format"`
	BeforeDate         string           `json:"before_date,omitempty" jsonschema:"upper date bound in YYYY-MM-DD format"`
	DomainType         SearchDomainType `json:"domain_type,omitempty" jsonschema:"result type: web, news, or research_paper"`
	PublicationYearMin *int             `json:"pub_year_min,omitempty" jsonschema:"minimum publication year for research papers"`
	PublicationYearMax *int             `json:"pub_year_max,omitempty" jsonschema:"maximum publication year for research papers"`
	Page               int              `json:"page,omitempty" jsonschema:"zero-based result page from 0 to 10"`
}

// SearchResponse is the structured response from TinyFish Search.
type SearchResponse struct {
	Query        string         `json:"query"`
	Results      []SearchResult `json:"results"`
	TotalResults int            `json:"total_results"`
	Page         int            `json:"page"`
}

// SearchResult is one ranked Search result.
type SearchResult struct {
	Position     int      `json:"position"`
	SiteName     string   `json:"site_name"`
	Title        string   `json:"title"`
	Snippet      string   `json:"snippet"`
	URL          string   `json:"url"`
	Date         string   `json:"date,omitempty"`
	Publisher    string   `json:"publisher,omitempty"`
	Authors      []string `json:"authors,omitempty"`
	Venue        string   `json:"venue,omitempty"`
	Year         *int     `json:"year,omitempty"`
	CitedByCount *int     `json:"cited_by_count,omitempty"`
	PDFURL       string   `json:"pdf_url,omitempty"`
}

// SearchService provides access to the TinyFish Search API.
type SearchService struct {
	client *Client
}

// Query searches the web and returns ranked, structured results.
func (service *SearchService) Query(ctx context.Context, request SearchRequest) (*SearchResponse, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}

	endpoint := *service.client.config.searchEndpoint
	query := endpoint.Query()
	request.addQuery(query)
	endpoint.RawQuery = query.Encode()

	var response SearchResponse
	if err := service.client.doJSON(
		ctx,
		http.MethodGet,
		endpoint.String(),
		nil,
		&response,
		service.client.config.searchTimeout,
		service.client.searchLimiter,
		1,
	); err != nil {
		return nil, err
	}
	return &response, nil
}

func (request SearchRequest) validate() error {
	if strings.TrimSpace(request.Query) == "" {
		return errors.New("tinyfish: search query must not be empty")
	}
	if utf8.RuneCountInString(request.Purpose) > 2000 {
		return errors.New("tinyfish: search purpose must be at most 2000 characters")
	}
	if request.Page < 0 || request.Page > 10 {
		return errors.New("tinyfish: search page must be between 0 and 10")
	}
	if request.RecencyMinutes != nil && (*request.RecencyMinutes < 1 || *request.RecencyMinutes > 5_256_000) {
		return errors.New("tinyfish: recency_minutes must be between 1 and 5256000")
	}
	if request.RecencyMinutes != nil && (request.AfterDate != "" || request.BeforeDate != "") {
		return errors.New("tinyfish: recency_minutes cannot be combined with date filters")
	}
	if err := validateDateRange(request.AfterDate, request.BeforeDate); err != nil {
		return err
	}
	if request.DomainType != "" && !slices.Contains([]SearchDomainType{
		SearchDomainWeb,
		SearchDomainNews,
		SearchDomainResearchPaper,
	}, request.DomainType) {
		return errors.New("tinyfish: domain_type must be web, news, or research_paper")
	}
	if request.DomainType == SearchDomainResearchPaper {
		if request.RecencyMinutes != nil || request.AfterDate != "" || request.BeforeDate != "" {
			return errors.New("tinyfish: research_paper searches use publication year filters instead of date filters")
		}
	} else if request.PublicationYearMin != nil || request.PublicationYearMax != nil {
		return errors.New("tinyfish: publication year filters require domain_type research_paper")
	}
	if err := validateYearRange(request.PublicationYearMin, request.PublicationYearMax); err != nil {
		return err
	}
	return nil
}

func (request SearchRequest) addQuery(query url.Values) {
	query.Set("query", request.Query)
	setIfNotEmpty(query, "purpose", request.Purpose)
	setIfNotEmpty(query, "location", request.Location)
	setIfNotEmpty(query, "language", request.Language)
	if len(request.IncludeDomains) > 0 {
		query.Set("include_domains", strings.Join(request.IncludeDomains, ","))
	}
	if len(request.ExcludeDomains) > 0 {
		query.Set("exclude_domains", strings.Join(request.ExcludeDomains, ","))
	}
	setIntPointer(query, "recency_minutes", request.RecencyMinutes)
	setIfNotEmpty(query, "after_date", request.AfterDate)
	setIfNotEmpty(query, "before_date", request.BeforeDate)
	setIfNotEmpty(query, "domain_type", string(request.DomainType))
	setIntPointer(query, "pub_year_min", request.PublicationYearMin)
	setIntPointer(query, "pub_year_max", request.PublicationYearMax)
	if request.Page != 0 {
		query.Set("page", strconv.Itoa(request.Page))
	}
}

func validateDateRange(after, before string) error {
	var afterTime, beforeTime time.Time
	var err error
	if after != "" {
		afterTime, err = time.Parse(time.DateOnly, after)
		if err != nil {
			return errors.New("tinyfish: after_date must use YYYY-MM-DD format")
		}
	}
	if before != "" {
		beforeTime, err = time.Parse(time.DateOnly, before)
		if err != nil {
			return errors.New("tinyfish: before_date must use YYYY-MM-DD format")
		}
	}
	if !afterTime.IsZero() && !beforeTime.IsZero() && afterTime.After(beforeTime) {
		return errors.New("tinyfish: after_date must not be after before_date")
	}
	return nil
}

func validateYearRange(minimum, maximum *int) error {
	if minimum != nil && (*minimum < 0 || *minimum > 9999) {
		return errors.New("tinyfish: pub_year_min must be between 0 and 9999")
	}
	if maximum != nil && (*maximum < 0 || *maximum > 9999) {
		return errors.New("tinyfish: pub_year_max must be between 0 and 9999")
	}
	if minimum != nil && maximum != nil && *minimum > *maximum {
		return errors.New("tinyfish: pub_year_min must not exceed pub_year_max")
	}
	return nil
}

func setIfNotEmpty(query url.Values, key, value string) {
	if value != "" {
		query.Set(key, value)
	}
}

func setIntPointer(query url.Values, key string, value *int) {
	if value != nil {
		query.Set(key, strconv.Itoa(*value))
	}
}
