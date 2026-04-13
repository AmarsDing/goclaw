package compression

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func simpleCounter(msgs []providers.Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content) / 4
	}
	return total
}

func TestL0TruncatesLongToolResults(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ContextWindow = 1000
	cfg.MaxToolResultTokens = 10
	cfg.Thresholds.L0Trigger = 0.01

	longContent := strings.Repeat("x", 500)
	msgs := []providers.Message{
		{Role: "user", Content: "test"},
		{Role: "assistant", Content: "ok", ToolCalls: []providers.ToolCall{{ID: "1", Name: "read"}}},
		{Role: "tool", Content: longContent, ToolCallID: "1"},
	}

	e := NewEngine(cfg, simpleCounter, nil)
	result, compressed, err := e.Compress(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !compressed {
		t.Fatal("expected compression to occur")
	}

	toolMsg := result.Messages[2]
	if len(toolMsg.Content) >= len(longContent) {
		t.Error("expected tool result to be truncated")
	}
	if !strings.Contains(toolMsg.Content, "truncated") {
		t.Error("expected truncation marker")
	}
}

func TestNoCompressionBelowThreshold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ContextWindow = 100000

	msgs := []providers.Message{
		{Role: "user", Content: "hi"},
	}

	e := NewEngine(cfg, simpleCounter, nil)
	_, compressed, err := e.Compress(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if compressed {
		t.Error("should not compress below threshold")
	}
}

func TestL1SnipsOldToolResults(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ContextWindow = 100
	cfg.SnipRetainCount = 1
	cfg.Thresholds.L1Trigger = 0.01
	cfg.Thresholds.L0Trigger = 0.99 // disable L0 to test L1

	msgs := []providers.Message{
		{Role: "user", Content: "test"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "1", Name: "a"}}},
		{Role: "tool", Content: "old result that should be snipped", ToolCallID: "1"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "2", Name: "b"}}},
		{Role: "tool", Content: "recent result to keep", ToolCallID: "2"},
	}

	e := NewEngine(cfg, simpleCounter, nil)
	result, compressed, err := e.Compress(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !compressed {
		t.Fatal("expected compression")
	}

	if !strings.Contains(result.Messages[2].Content, "snipped") {
		t.Error("first tool result should be snipped")
	}
	if strings.Contains(result.Messages[4].Content, "snipped") {
		t.Error("last tool result should be kept intact")
	}
}

func TestSkipLeadingSystemRoles(t *testing.T) {
	msgs := []providers.Message{
		{Role: "system", Content: "a"},
		{Role: "system", Content: "b"},
		{Role: "user", Content: "u"},
	}
	rest := skipLeadingSystemRoles(msgs)
	if len(rest) != 1 || rest[0].Content != "u" {
		t.Fatalf("got %+v", rest)
	}
	if len(skipLeadingSystemRoles([]providers.Message{{Role: "system", Content: "x"}})) != 0 {
		t.Fatal("all-system slice should become empty")
	}
}
