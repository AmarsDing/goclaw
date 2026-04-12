package resume

import (
	"context"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestResumeRepairsOrphanedToolCalls(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{
			{ID: "tc-1", Name: "read_file", Arguments: map[string]any{"path": "/tmp/a"}},
		}},
		// Missing tool result for tc-1
	}

	e := NewEngine()
	result, err := e.Resume(context.Background(), msgs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Repaired {
		t.Error("expected repair flag to be set")
	}

	// Should have: user, assistant, synthetic tool result
	if len(result.Messages) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(result.Messages))
	}

	toolMsg := result.Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "tc-1" {
		t.Errorf("expected synthetic tool result, got %+v", toolMsg)
	}
	if !toolMsg.IsError {
		t.Error("synthetic tool result should be marked as error")
	}
}

func TestResumeFiltersBadMessages(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "hi"},
		{Role: "", Content: "corrupt"},
		{Role: "tool", Content: "no call id"},
		{Role: "assistant", Content: "ok"},
	}

	e := NewEngine()
	result, err := e.Resume(context.Background(), msgs, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages after filtering, got %d", len(result.Messages))
	}
}

func TestResumeDetectsInterruptType(t *testing.T) {
	tests := []struct {
		err    error
		expect InterruptType
	}{
		{nil, InterruptNone},
		{errors.New("context deadline exceeded"), InterruptTimeout},
		{errors.New("user cancel"), InterruptUserCancel},
		{errors.New("connection reset"), InterruptNetworkErr},
		{errors.New("runtime error: nil pointer"), InterruptPanic},
		{errors.New("over budget"), InterruptOverBudget},
		{errors.New("something else"), InterruptUnknown},
	}

	for _, tt := range tests {
		got := detectInterruptType(tt.err)
		if got != tt.expect {
			t.Errorf("detectInterruptType(%v) = %s, want %s", tt.err, got, tt.expect)
		}
	}
}

func TestResumeContinuationInjected(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "do something"},
		{Role: "assistant", Content: "working on it"},
	}

	e := NewEngine()
	result, err := e.Resume(context.Background(), msgs, errors.New("context deadline exceeded"))
	if err != nil {
		t.Fatal(err)
	}

	if result.Continuation == "" {
		t.Error("expected continuation prompt to be set")
	}
	if result.InterruptKind != InterruptTimeout {
		t.Errorf("expected timeout interrupt, got %s", result.InterruptKind)
	}
}
