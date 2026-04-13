package pipeline

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestConversationTailForDrift_skipsSystem(t *testing.T) {
	mb := NewMessageBuffer(providers.Message{Role: "system", Content: "huge system prompt with secret keywords"})
	mb.SetHistory([]providers.Message{
		{Role: "user", Content: "hello world"},
		{Role: "assistant", Content: "hi there"},
	})
	st := &RunState{Messages: mb}
	s := ConversationTailForDrift(st, 500)
	low := strings.ToLower(s)
	if strings.Contains(low, "secret") || strings.Contains(low, "huge system") {
		t.Fatalf("system leaked into tail: %q", s)
	}
	if !strings.Contains(low, "hello") || !strings.Contains(low, "hi there") {
		t.Fatalf("expected user/assistant content: %q", s)
	}
}
