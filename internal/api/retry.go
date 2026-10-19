package api

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"
)

// maxRetryWait is the longest Retry-After the client waits out itself; a longer
// one comes back to the caller as a retry-later error instead of a hang.
const maxRetryWait = 30 * time.Second

// wait pauses between retries, ending early when the command's deadline does.
func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		c.Sleep(d)
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// transportFailure decides what a request that got no response means. A
// write may already have happened, so it is never retried.
func transportFailure(ctx context.Context, err error, idempotent, lastAttempt bool) (bool, error) {
	switch {
	case !idempotent:
		return false, agenterrors.Wrap(err, agenterrors.FixableByAgent).WithHint(outcomeUnknownHint)
	case ctx.Err() != nil:
		return false, timedOut(ctx.Err())
	case lastAttempt:
		return false, agenterrors.Wrap(err, agenterrors.FixableByRetry).WithHint("Network request failed after retries")
	default:
		return true, nil
	}
}

// shouldRetry: reads retry on 429 and gateway errors; writes only on 429,
// which PostHog sends before doing anything.
func shouldRetry(status int, idempotent bool) bool {
	return retryable(status) && (idempotent || status == http.StatusTooManyRequests)
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func retryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		return time.Until(when)
	}
	return 0
}

func backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	ms := int(math.Pow(2, float64(attempt))) * 250
	return time.Duration(ms) * time.Millisecond
}
