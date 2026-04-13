package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SSEClientConfig configures the Server-Sent Events bridge client (GET /v1/bridge/events).
type SSEClientConfig struct {
	// BaseURL is the gateway HTTP base, e.g. "http://127.0.0.1:9090"
	BaseURL string
	// Token is GOCLAW_GATEWAY_TOKEN (Bearer).
	Token string
}

// SSEClient streams gateway EventFrame JSON over SSE.
type SSEClient struct {
	cfg SSEClientConfig
}

// NewSSEClient creates an SSE bridge client.
func NewSSEClient(cfg SSEClientConfig) *SSEClient {
	return &SSEClient{cfg: cfg}
}

// StreamEvents connects to /v1/bridge/events and invokes handler for each EventFrame until ctx done or stream error.
func (c *SSEClient) StreamEvents(ctx context.Context, handler func(raw json.RawMessage) error) error {
	if c.cfg.BaseURL == "" {
		return fmt.Errorf("base URL required")
	}
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/v1/bridge/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("sse: status %d: %s", resp.StatusCode, string(b))
	}

	br := bufio.NewReader(resp.Body)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" {
				continue
			}
			raw := json.RawMessage(payload)
			if err := handler(raw); err != nil {
				return err
			}
		}
	}
}

// StreamEventsWithReconnect wraps StreamEvents with simple exponential backoff (for long-lived remote IDE sessions).
func (c *SSEClient) StreamEventsWithReconnect(ctx context.Context, handler func(raw json.RawMessage) error) error {
	backoff := 500 * time.Millisecond
	for {
		err := c.StreamEvents(ctx, handler)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}
