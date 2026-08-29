package tinyfish

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// APIError is an HTTP-level TinyFish API error.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Details    json.RawMessage
	RequestID  string
	RetryAfter time.Duration
}

func (err *APIError) Error() string {
	if err.Code == "" {
		return fmt.Sprintf("tinyfish: HTTP %d: %s", err.StatusCode, err.Message)
	}
	return fmt.Sprintf("tinyfish: %s (HTTP %d): %s", err.Code, err.StatusCode, err.Message)
}

// Temporary reports whether retrying the same safe operation may succeed.
func (err *APIError) Temporary() bool {
	switch err.StatusCode {
	case 429, 502, 503, 504:
		return true
	default:
		return false
	}
}

// IsErrorCode reports whether err is an APIError with the supplied TinyFish
// error code.
func IsErrorCode(err error, code string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == code
}
