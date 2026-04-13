package agent

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestCompressionCompactor_PreservesLeadingSystem(t *testing.T) {
	p := &stubProvider{response: "condensed story"}
	l := &Loop{
		provider: p,
		model:    "gpt-test",
		id:       "agent-1",
	}
	out, err := l.compressionCompactor(context.Background(), []providers.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 messages (system + summary), got %d", len(out))
	}
	if out[0].Role != "system" || out[0].Content != "You are helpful." {
		t.Errorf("leading system not preserved: %+v", out[0])
	}
	if out[1].CompactBoundary == nil {
		t.Error("expected CompactBoundary on summary message")
	}
	if out[1].Content == "" {
		t.Error("expected summary content")
	}
}

func TestCompressionCompactor_RestOnly(t *testing.T) {
	p := &stubProvider{response: "summary only"}
	l := &Loop{provider: p, model: "m", id: "a"}
	out, err := l.compressionCompactor(context.Background(), []providers.Message{
		{Role: "user", Content: "solo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Role != "system" {
		t.Fatalf("got %+v", out)
	}
}
