package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"
)

// Executor handles the actual execution of hook handlers.
type Executor struct {
	httpClient *http.Client
}

// NewExecutor creates a hook executor.
func NewExecutor() *Executor {
	return &Executor{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ExecuteWebhook sends an HTTP POST with the payload to the hook's target URL.
func (e *Executor) ExecuteWebhook(ctx context.Context, hook *Hook, payload Payload) (*Result, error) {
	start := time.Now()

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.Handler.Target, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hook-ID", hook.ID)
	req.Header.Set("X-Hook-Event", string(hook.Event))

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return &Result{
			HookID:   hook.ID,
			Success:  false,
			Error:    err.Error(),
			Duration: time.Since(start),
		}, nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	return &Result{
		HookID:   hook.ID,
		Success:  resp.StatusCode >= 200 && resp.StatusCode < 300,
		Output:   string(respBody),
		Duration: time.Since(start),
	}, nil
}

// ExecuteCommand runs a shell command with the payload as JSON on stdin.
func (e *Executor) ExecuteCommand(ctx context.Context, hook *Hook, payload Payload) (*Result, error) {
	start := time.Now()

	cmd := exec.CommandContext(ctx, "sh", "-c", hook.Handler.Target)

	stdinData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	cmd.Stdin = bytes.NewReader(stdinData)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	success := err == nil

	output := stdout.String()
	if output == "" && stderr.Len() > 0 {
		output = stderr.String()
	}

	// Cap output length.
	if len(output) > 4096 {
		output = output[:4096] + "...[truncated]"
	}

	result := &Result{
		HookID:   hook.ID,
		Success:  success,
		Output:   output,
		Duration: time.Since(start),
	}
	if err != nil {
		result.Error = err.Error()
	}

	return result, nil
}
