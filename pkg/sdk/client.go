package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client connects to a goclaw gateway and provides a high-level API
// for controlling agent runs and receiving structured events.
type Client struct {
	mu       sync.Mutex
	conn     *websocket.Conn
	url      string
	token    string
	handlers map[EventType][]EventHandler
	done     chan struct{}
}

// EventHandler processes an incoming event.
type EventHandler func(event Event)

// ClientConfig configures the SDK client.
type ClientConfig struct {
	URL   string // WebSocket URL (e.g. "ws://localhost:9090/ws")
	Token string // gateway authentication token
}

// NewClient creates an SDK client connected to the gateway.
func NewClient(cfg ClientConfig) (*Client, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	header := make(map[string][]string)
	if cfg.Token != "" {
		header["Authorization"] = []string{"Bearer " + cfg.Token}
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	c := &Client{
		conn:     conn,
		url:      cfg.URL,
		token:    cfg.Token,
		handlers: make(map[EventType][]EventHandler),
		done:     make(chan struct{}),
	}

	go c.readLoop()
	return c, nil
}

// On registers a handler for a specific event type.
func (c *Client) On(eventType EventType, handler EventHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[eventType] = append(c.handlers[eventType], handler)
}

// StartRun sends a start_run command and returns the run ID.
func (c *Client) StartRun(ctx context.Context, payload StartRunPayload) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	cmd := Command{
		Type:    CmdStartRun,
		Payload: data,
	}

	if err := c.send(cmd); err != nil {
		return "", err
	}

	// Wait for run.started event or timeout.
	ch := make(chan string, 1)
	c.On(EventRunStarted, func(e Event) {
		ch <- e.RunID
	})

	select {
	case runID := <-ch:
		return runID, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(30 * time.Second):
		return "", fmt.Errorf("timeout waiting for run.started")
	}
}

// AbortRun sends an abort command for the given run.
func (c *Client) AbortRun(runID string) error {
	return c.send(Command{
		Type:  CmdAbortRun,
		RunID: runID,
	})
}

// Approve approves a pending permission request.
func (c *Client) Approve(requestID string, remember bool) error {
	data, _ := json.Marshal(ApprovePayload{
		RequestID: requestID,
		Remember:  remember,
	})
	return c.send(Command{
		Type:    CmdApprove,
		Payload: data,
	})
}

// Deny denies a pending permission request.
func (c *Client) Deny(requestID string) error {
	data, _ := json.Marshal(ApprovePayload{
		RequestID: requestID,
	})
	return c.send(Command{
		Type:    CmdDeny,
		Payload: data,
	})
}

// Close disconnects the client.
func (c *Client) Close() error {
	close(c.done)
	return c.conn.Close()
}

func (c *Client) send(cmd Command) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func (c *Client) readLoop() {
	for {
		select {
		case <-c.done:
			return
		default:
		}

		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}

		var event Event
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}

		c.mu.Lock()
		handlers := c.handlers[event.Type]
		c.mu.Unlock()

		for _, h := range handlers {
			h(event)
		}
	}
}
