package httpx

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"obscourse/internal/obs"
)

// Client is an HTTP client whose context propagation can be broken on purpose
// (Day 3 antidemo) and which can simulate a retry storm (Day 3 bottleneck demo).
type Client struct {
	chaos  *obs.Chaos
	traced *http.Client
	plain  *http.Client
}

func New(chaos *obs.Chaos) *Client {
	return &Client{
		chaos:  chaos,
		traced: &http.Client{Timeout: 10 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)},
		plain:  &http.Client{Timeout: 10 * time.Second}, // no otel transport -> no traceparent
	}
}

// Post sends a JSON body. With RetryStorm enabled it retries 5xx responses
// aggressively without backoff — every attempt shows up as its own client span.
func (c *Client) Post(r *http.Request, url string, body []byte, headers map[string]string) (*http.Response, error) {
	snap := c.chaos.Snapshot()
	cl := c.traced
	if snap.BreakPropagation {
		cl = c.plain
	}
	attempts := 1
	if snap.RetryStorm {
		attempts = 5
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := cl.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= 500 && i < attempts-1 {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			obs.L(r.Context()).Warn("upstream_retry", "url", url, "status", resp.StatusCode, "attempt", i+1)
			lastErr = fmt.Errorf("upstream status %d", resp.StatusCode)
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}
