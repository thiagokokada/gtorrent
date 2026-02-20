package httptransport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client executes XML-RPC requests through HTTP.
type Client struct {
	URL     string
	User    string
	Pass    string
	HTTP    *http.Client
	Timeout time.Duration
}

func (c *Client) Do(ctx context.Context, payload []byte) ([]byte, error) {
	client := c.HTTP
	if client == nil {
		t := c.Timeout
		if t == 0 {
			t = 15 * time.Second
		}
		client = &http.Client{Timeout: t}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml")
	if c.User != "" {
		req.SetBasicAuth(c.User, c.Pass)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(body) > 240 {
			body = body[:240]
		}
		return nil, fmt.Errorf("http xml-rpc status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}
