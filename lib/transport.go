package tinyfish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (client *Client) doJSON(
	ctx context.Context,
	method string,
	endpoint string,
	requestBody any,
	responseBody any,
	timeout time.Duration,
	limiter requestLimiter,
	weight int,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := withDefaultTimeout(ctx, timeout)
	defer cancel()

	var encodedBody []byte
	var err error
	if requestBody != nil {
		encodedBody, err = json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("tinyfish: encode request: %w", err)
		}
	}

	var lastErr error
	for attempt := 1; attempt <= client.config.retry.MaxAttempts; attempt++ {
		if limiter != nil {
			if err := limiter.WaitN(ctx, weight); err != nil {
				return fmt.Errorf("tinyfish: wait for rate limit: %w", err)
			}
		}

		request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(encodedBody))
		if err != nil {
			return fmt.Errorf("tinyfish: create request: %w", err)
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("X-API-Key", client.config.apiKey)
		request.Header.Set("User-Agent", client.config.userAgent)
		if requestBody != nil {
			request.Header.Set("Content-Type", "application/json")
		}

		response, err := client.config.httpClient.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = fmt.Errorf("tinyfish: send request: %w", err)
			if attempt == client.config.retry.MaxAttempts {
				return lastErr
			}
			if err := waitForRetry(ctx, retryDelay(client.config.retry, attempt, 0)); err != nil {
				return err
			}
			continue
		}

		body, readErr := readLimited(response.Body, client.config.maxResponseBytes)
		response.Body.Close()
		if readErr != nil {
			return readErr
		}

		if response.StatusCode >= 200 && response.StatusCode < 300 {
			if responseBody == nil || len(bytes.TrimSpace(body)) == 0 {
				return nil
			}
			if err := json.Unmarshal(body, responseBody); err != nil {
				return fmt.Errorf("tinyfish: decode response: %w", err)
			}
			return nil
		}

		apiErr := decodeAPIError(response, body)
		lastErr = apiErr
		if !apiErr.Temporary() || attempt == client.config.retry.MaxAttempts {
			return apiErr
		}
		if err := waitForRetry(ctx, retryDelay(client.config.retry, attempt, apiErr.RetryAfter)); err != nil {
			return err
		}
	}

	return lastErr
}

func withDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("tinyfish: read response: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("tinyfish: response exceeds %d bytes", limit)
	}
	return body, nil
}

func decodeAPIError(response *http.Response, body []byte) *APIError {
	envelope := struct {
		Error struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}{}
	_ = json.Unmarshal(body, &envelope)

	message := envelope.Error.Message
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = response.Status
	}
	if len(message) > 1024 {
		message = message[:1024] + "..."
	}

	return &APIError{
		StatusCode: response.StatusCode,
		Code:       envelope.Error.Code,
		Message:    message,
		Details:    envelope.Error.Details,
		RequestID:  response.Header.Get("X-Request-ID"),
		RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()),
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return retryAt.Sub(now)
	}
	return 0
}

func retryDelay(policy RetryPolicy, attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	delay := policy.BaseDelay
	for range attempt - 1 {
		if delay >= policy.MaxDelay/2 {
			delay = policy.MaxDelay
			break
		}
		delay *= 2
	}
	if delay > policy.MaxDelay {
		delay = policy.MaxDelay
	}
	if delay <= 0 {
		return 0
	}
	// Full jitter prevents multiple client instances from retrying in lockstep.
	return time.Duration(rand.Int64N(int64(delay) + 1))
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
