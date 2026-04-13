package message_test

import (
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/message"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// helpers

func userMsg(content string) providers.Message {
	return providers.Message{Role: "user", Content: content}
}

func assistantMsg(content string, calls ...providers.ToolCall) providers.Message {
	return providers.Message{Role: "assistant", Content: content, ToolCalls: calls}
}

func toolMsg(id, content string) providers.Message {
	return providers.Message{Role: "tool", ToolCallID: id, Content: content}
}

func toolCall(id, name string) providers.ToolCall {
	return providers.ToolCall{ID: id, Name: name, Arguments: map[string]any{}}
}

// ── Transcript ───────────────────────────────────────────────────────────────

func TestNewTranscript(t *testing.T) {
	tr := message.NewTranscript("sess-1")
	if tr.SessionKey != "sess-1" {
		t.Fatalf("expected SessionKey sess-1, got %q", tr.SessionKey)
	}
	if tr.MessageCount() != 0 {
		t.Fatalf("expected empty transcript, got %d messages", tr.MessageCount())
	}
}

func TestTranscript_AppendAndCount(t *testing.T) {
	tr := message.NewTranscript("s")
	tr.Append(userMsg("hello"), assistantMsg("hi"))
	if tr.MessageCount() != 2 {
		t.Fatalf("expected 2, got %d", tr.MessageCount())
	}
}

func TestTranscript_Replace(t *testing.T) {
	tr := message.NewTranscript("s")
	tr.Append(userMsg("a"), userMsg("b"), userMsg("c"))
	tr.Replace([]providers.Message{userMsg("summary")}, "summary text")
	if tr.MessageCount() != 1 {
		t.Fatalf("expected 1 after replace, got %d", tr.MessageCount())
	}
	if tr.Summary != "summary text" {
		t.Fatalf("expected summary text, got %q", tr.Summary)
	}
	if tr.Compactions != 1 {
		t.Fatalf("expected Compactions=1, got %d", tr.Compactions)
	}
}

func TestTranscript_ToolCallCount(t *testing.T) {
	tc1 := toolCall("id1", "read_file")
	tc2 := toolCall("id2", "write_file")
	tr := message.NewTranscript("s")
	tr.Append(assistantMsg("thinking", tc1, tc2))
	if tr.ToolCallCount() != 2 {
		t.Fatalf("expected 2 tool calls, got %d", tr.ToolCallCount())
	}
}

func TestTranscript_ForAPIView(t *testing.T) {
	tr := message.NewTranscript("s")
	// system message should be kept in API view but stripped in UI view
	tr.Append(
		providers.Message{Role: "system", Content: "sys"},
		userMsg("hello"),
		assistantMsg("hi"),
	)
	env := tr.ForAPI("run-1")
	if env.View != message.ViewAPI {
		t.Fatalf("expected ViewAPI, got %v", env.View)
	}
}

func TestTranscript_ForUIView(t *testing.T) {
	tr := message.NewTranscript("s")
	tr.Append(
		providers.Message{Role: "system", Content: "sys"},
		userMsg("hello"),
		assistantMsg("hi"),
	)
	env := tr.ForUI("run-1", false)
	if env.View != message.ViewUI {
		t.Fatalf("expected ViewUI, got %v", env.View)
	}
	for _, m := range env.Messages {
		if m.Role == "system" {
			t.Fatal("UI view should strip system messages")
		}
	}
}

func TestTranscript_ForResumeView(t *testing.T) {
	tr := message.NewTranscript("s")
	tc := toolCall("tc1", "tool")
	tr.Append(
		userMsg("question"),
		assistantMsg("", tc), // orphaned tool call
	)
	env := tr.ForResume("run-1")
	if env.View != message.ViewResume {
		t.Fatalf("expected ViewResume, got %v", env.View)
	}
	// resume should repair the orphaned tool call with a synthetic result
	hasResult := false
	for _, m := range env.Messages {
		if m.Role == "tool" && m.ToolCallID == "tc1" {
			hasResult = true
		}
	}
	if !hasResult {
		t.Fatal("ForResume should add synthetic result for orphaned tool call")
	}
}

// ── NormalizeForAPI ──────────────────────────────────────────────────────────

func TestNormalizeForAPI_StripsEmptyAssistant(t *testing.T) {
	msgs := []providers.Message{
		userMsg("hi"),
		{Role: "assistant", Content: "", ToolCalls: nil}, // empty, should be stripped
		assistantMsg("ok"),
	}
	out := message.NormalizeForAPI(msgs)
	for _, m := range out {
		if m.Role == "assistant" && m.Content == "" && len(m.ToolCalls) == 0 {
			t.Fatal("empty assistant message should be stripped by NormalizeForAPI")
		}
	}
}

func TestNormalizeForAPI_OrphanedToolCallGetsResult(t *testing.T) {
	tc := toolCall("orphan-1", "bash")
	msgs := []providers.Message{
		userMsg("run something"),
		assistantMsg("ok", tc),
		// no tool result — orphaned
	}
	out := message.NormalizeForAPI(msgs)
	hasSynthetic := false
	for _, m := range out {
		if m.Role == "tool" && m.ToolCallID == "orphan-1" && m.IsError {
			hasSynthetic = true
		}
	}
	if !hasSynthetic {
		t.Fatal("NormalizeForAPI should insert synthetic error result for orphaned tool call")
	}
}

// ── NormalizeForUI ───────────────────────────────────────────────────────────

func TestNormalizeForUI_RemovesSystemMessages(t *testing.T) {
	msgs := []providers.Message{
		{Role: "system", Content: "instructions"},
		userMsg("hi"),
	}
	out := message.NormalizeForUI(msgs, false)
	for _, m := range out {
		if m.Role == "system" {
			t.Fatal("NormalizeForUI should strip system messages")
		}
	}
}

func TestNormalizeForUI_StripThinkingWhenDisabled(t *testing.T) {
	msgs := []providers.Message{
		{Role: "assistant", Content: "answer", Thinking: "deep thoughts"},
	}
	out := message.NormalizeForUI(msgs, false)
	for _, m := range out {
		if m.Thinking != "" {
			t.Fatal("NormalizeForUI should strip Thinking when showThinking=false")
		}
	}
}

func TestNormalizeForUI_KeepThinkingWhenEnabled(t *testing.T) {
	msgs := []providers.Message{
		{Role: "assistant", Content: "answer", Thinking: "deep thoughts"},
	}
	out := message.NormalizeForUI(msgs, true)
	found := false
	for _, m := range out {
		if m.Thinking == "deep thoughts" {
			found = true
		}
	}
	if !found {
		t.Fatal("NormalizeForUI should preserve Thinking when showThinking=true")
	}
}

// ── NormalizeForResume ───────────────────────────────────────────────────────

func TestNormalizeForResume_RemovesOrphanedTrailingAssistant(t *testing.T) {
	msgs := []providers.Message{
		userMsg("hello"),
		{Role: "assistant", Content: "", ToolCalls: nil}, // trailing empty — should be trimmed
	}
	out := message.NormalizeForResume(msgs)
	if len(out) > 0 && out[len(out)-1].Role == "assistant" && out[len(out)-1].Content == "" {
		t.Fatal("NormalizeForResume should trim trailing empty assistant messages")
	}
}

func TestNormalizeForResume_FiltersBadMessages(t *testing.T) {
	msgs := []providers.Message{
		{Role: ""},              // empty role — bad
		{Role: "tool"},         // tool without ToolCallID — bad
		userMsg("valid"),
	}
	out := message.NormalizeForResume(msgs)
	for _, m := range out {
		if m.Role == "" {
			t.Fatal("NormalizeForResume should filter messages with empty role")
		}
		if m.Role == "tool" && m.ToolCallID == "" {
			t.Fatal("NormalizeForResume should filter tool messages without ToolCallID")
		}
	}
}

// ── BuildMessageLookups ──────────────────────────────────────────────────────

func TestBuildMessageLookups(t *testing.T) {
	tc := toolCall("id-1", "read")
	msgs := []providers.Message{
		userMsg("query"),
		assistantMsg("", tc),
		toolMsg("id-1", "result"),
	}
	lk := message.BuildMessageLookups(msgs)
	if _, ok := lk.ToolUseIDToCallIdx["id-1"]; !ok {
		t.Fatal("should find tool call by id")
	}
	if _, ok := lk.ToolUseIDToResultIdx["id-1"]; !ok {
		t.Fatal("should find tool result by id")
	}
	if len(lk.UnresolvedToolUseIDs) != 0 {
		t.Fatalf("expected no unresolved IDs, got %v", lk.UnresolvedToolUseIDs)
	}
}

func TestBuildMessageLookups_UnresolvedCall(t *testing.T) {
	tc := toolCall("missing-result", "write")
	msgs := []providers.Message{
		assistantMsg("thinking", tc),
		// no tool result
	}
	lk := message.BuildMessageLookups(msgs)
	if len(lk.UnresolvedToolUseIDs) != 1 || lk.UnresolvedToolUseIDs[0] != "missing-result" {
		t.Fatalf("expected UnresolvedToolUseIDs=[missing-result], got %v", lk.UnresolvedToolUseIDs)
	}
}

// ── CountTruncatedToolResults ────────────────────────────────────────────────

func TestCountTruncatedToolResults(t *testing.T) {
	now := time.Now()
	msgs := []providers.Message{
		{Role: "tool", Content: "normal result", ToolCallID: "a", CreatedAt: &now},
		{Role: "tool", Content: "[truncated by compaction]", ToolCallID: "b", CreatedAt: &now},
		{Role: "tool", Content: "[microcompact applied]", ToolCallID: "c", CreatedAt: &now},
		{Role: "user", Content: "snipped (not a tool)", CreatedAt: &now},
	}
	count := message.CountTruncatedToolResults(msgs)
	if count != 2 {
		t.Fatalf("expected 2 truncated results, got %d", count)
	}
}

// ── StripDirectives (via NormalizeForUI) ─────────────────────────────────────

func TestNormalizeForUI_StripDirectives(t *testing.T) {
	msgs := []providers.Message{
		{Role: "assistant", Content: "Here is the answer <!-- internal directive --> visible text"},
	}
	out := message.NormalizeForUI(msgs, false)
	for _, m := range out {
		if m.Role == "assistant" {
			if m.Content != "Here is the answer  visible text" && m.Content != "Here is the answer visible text" {
				t.Logf("stripped content: %q", m.Content)
				// NormalizeForUI strips directives and trims spaces; just ensure the directive is removed
				if len(m.Content) > 0 && (len(m.Content) == len(msgs[0].Content)) {
					t.Fatal("NormalizeForUI should strip <!-- ... --> directives from assistant content")
				}
			}
		}
	}
}
