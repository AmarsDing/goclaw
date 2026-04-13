package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

// --- shared test helpers ---

func minimalInput() *RunInput {
	return &RunInput{
		SessionKey: "sess-1",
		RunID:      "run-1",
		UserID:     "user-1",
	}
}

func stateWithInput(input *RunInput) *RunState {
	ws := &workspace.WorkspaceContext{ActivePath: "/tmp"}
	return NewRunState(input, ws, "claude-3", nil)
}

func defaultState() *RunState {
	return stateWithInput(minimalInput())
}

// mockTokenCounter returns a fixed count for every call.
type mockTokenCounter struct {
	countPerMessage int
}

func (m *mockTokenCounter) Count(_ string, _ string) int { return m.countPerMessage }
func (m *mockTokenCounter) CountMessages(_ string, msgs []providers.Message) int {
	return len(msgs) * m.countPerMessage
}
func (m *mockTokenCounter) ModelContextWindow(_ string) int { return 200_000 }

// --- ThinkStage tests ---

func TestThinkStage_NoToolCalls_ReturnsBreakLoop(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				Content:      "final answer",
				FinishReason: "stop",
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v, want BreakLoop", stage.Result())
	}
	// Final answer skips AppendPending (FinalizeStage builds the definitive message).
	pending := state.Messages.Pending()
	if len(pending) != 0 {
		t.Errorf("pending = %v, want empty (FinalizeStage builds the definitive message)", pending)
	}
}

func TestThinkStage_WithToolCalls_ReturnsContinue(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				FinishReason: "tool_calls",
				ToolCalls: []providers.ToolCall{
					{ID: "tc1", Name: "read_file"},
				},
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue", stage.Result())
	}
	if state.Think.LastResponse == nil {
		t.Fatal("LastResponse is nil")
	}
	if len(state.Think.LastResponse.ToolCalls) != 1 {
		t.Errorf("ToolCalls len = %d, want 1", len(state.Think.LastResponse.ToolCalls))
	}
}

func TestThinkStage_Truncation_FirstRetry_AppendsContinueMessage(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			// Truncation only triggers when tool calls are present (args truncated).
			return &providers.ChatResponse{
				FinishReason: "length",
				ToolCalls:    []providers.ToolCall{{ID: "tc1", Name: "write_file"}},
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v after first truncation, want Continue", stage.Result())
	}
	if state.Think.TruncRetries != 1 {
		t.Errorf("TruncRetries = %d, want 1", state.Think.TruncRetries)
	}
	// retry messages appended: assistant (partial) + user (hint)
	pending := state.Messages.Pending()
	if len(pending) != 2 {
		t.Fatalf("pending len = %d, want 2", len(pending))
	}
	if pending[0].Role != "assistant" {
		t.Errorf("pending[0] role = %q, want assistant", pending[0].Role)
	}
	if pending[1].Role != "user" {
		t.Errorf("pending[1] role = %q, want user", pending[1].Role)
	}
}

func TestThinkStage_Truncation_ThirdRetry_ReturnsAbortRun(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				FinishReason: "length",
				ToolCalls:    []providers.ToolCall{{ID: "tc1", Name: "write_file"}},
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	// simulate 2 prior retries
	state.Think.TruncRetries = 2

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != AbortRun {
		t.Errorf("Result() = %v after 3rd truncation, want AbortRun", stage.Result())
	}
}

func TestThinkStage_TruncationReset_OnSuccess(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				Content:      "ok",
				FinishReason: "stop",
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	state.Think.TruncRetries = 2 // had retries before this success

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Think.TruncRetries != 0 {
		t.Errorf("TruncRetries = %d after success, want 0", state.Think.TruncRetries)
	}
}

func TestThinkStage_UsageAccumulation(t *testing.T) {
	t.Parallel()
	callCount := 0
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			callCount++
			return &providers.ChatResponse{
				Content:      "hello",
				FinishReason: "stop",
				Usage:        &providers.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	// call twice
	_ = stage.Execute(context.Background(), state)
	_ = stage.Execute(context.Background(), state)

	if state.Think.TotalUsage.PromptTokens != 20 {
		t.Errorf("PromptTokens = %d, want 20", state.Think.TotalUsage.PromptTokens)
	}
	if state.Think.TotalUsage.CompletionTokens != 10 {
		t.Errorf("CompletionTokens = %d, want 10", state.Think.TotalUsage.CompletionTokens)
	}
	if state.Think.TotalUsage.TotalTokens != 30 {
		t.Errorf("TotalTokens = %d, want 30", state.Think.TotalUsage.TotalTokens)
	}
}

func TestThinkStage_Nudge70_FiresOnce(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{FinishReason: "stop", Content: "ok"}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	// iteration 7 out of 10 = 70%
	state.Iteration = 7

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !state.Evolution.Nudge70Sent {
		t.Error("Nudge70Sent should be true at 70%")
	}
	if state.Evolution.Nudge90Sent {
		t.Error("Nudge90Sent should be false at 70%")
	}

	// second call at same iteration — nudge should NOT fire again
	pendingBefore := len(state.Messages.Pending())
	_ = stage.Execute(context.Background(), state)
	// pending grew by 1 (only the assistant message), not 2
	pendingAfter := len(state.Messages.Pending())
	if pendingAfter-pendingBefore > 1 {
		t.Errorf("nudge fired again: pending grew by %d, want ≤1", pendingAfter-pendingBefore)
	}
}

func TestThinkStage_Nudge90_FiresOnce(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{FinishReason: "stop", Content: "ok"}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	// iteration 9 out of 10 = 90%
	state.Iteration = 9

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !state.Evolution.Nudge90Sent {
		t.Error("Nudge90Sent should be true at 90%")
	}
}

func TestThinkStage_Nudge70_NotBeforeThreshold(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{FinishReason: "stop", Content: "ok"}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	state.Iteration = 5 // 50%, below threshold

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Evolution.Nudge70Sent {
		t.Error("Nudge70Sent should be false at 50%")
	}
}

func TestThinkStage_CallLLMNil_ReturnsError(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected error when CallLLM is nil")
	}
}

func TestThinkStage_LLMError_Propagates(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return nil, errors.New("rate limited")
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected error from LLM, got nil")
	}
}

func TestThinkStage_ReactiveCompact_BeforeLLM(t *testing.T) {
	t.Parallel()
	compactCalls := 0
	deps := &PipelineDeps{
		Config:       PipelineConfig{ContextWindow: 1000, MaxTokens: 100},
		TokenCounter: &mockTokenCounter{countPerMessage: 250},
		CompactMessages: func(_ context.Context, msgs []providers.Message, _ string) ([]providers.Message, error) {
			compactCalls++
			return msgs[:1], nil
		},
		CallLLM: func(_ context.Context, state *RunState, req providers.ChatRequest) (*providers.ChatResponse, error) {
			if got := len(req.Messages); got < 2 {
				t.Fatalf("expected compacted messages to still include system + history, got %d", got)
			}
			return &providers.ChatResponse{Content: "done", FinishReason: "stop"}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	state.Messages.SetHistory([]providers.Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"},
	})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if compactCalls != 1 {
		t.Fatalf("compactCalls = %d, want 1", compactCalls)
	}
	if got := len(state.Messages.History()); got != 1 {
		t.Fatalf("history len = %d, want 1 after reactive compact", got)
	}
	if state.Compact.CompactionCount != 1 {
		t.Fatalf("CompactionCount = %d, want 1", state.Compact.CompactionCount)
	}
}

// --- PruneStage tests ---

func TestPruneStage_UnderBudget_NoOp(t *testing.T) {
	t.Parallel()
	pruneCallCount := 0
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 10000,
			MaxTokens:     1000,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 1}, // tiny counts
		PruneMessages: func(msgs []providers.Message, _ int) []providers.Message {
			pruneCallCount++
			return msgs
		},
	}
	stage := NewPruneStage(deps, nil)
	state := defaultState()
	// add a small history
	state.Messages.SetHistory([]providers.Message{
		{Role: "user", Content: "hello"},
	})

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue", stage.Result())
	}
	if pruneCallCount != 0 {
		t.Errorf("PruneMessages called %d times, want 0", pruneCallCount)
	}
}

func TestPruneStage_Over70Percent_CallsPruneMessages(t *testing.T) {
	t.Parallel()
	pruneCallCount := 0
	// budget = 10000 - 0 (no overhead) - 1000 = 9000
	// softThreshold = 9000 * 70/100 = 6300
	// history = 100 msgs * 100 tokens each = 10000 > 6300
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 10000,
			MaxTokens:     1000,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 100},
		PruneMessages: func(msgs []providers.Message, budget int) []providers.Message {
			pruneCallCount++
			// return trimmed history that's under budget
			return msgs[:1]
		},
	}
	stage := NewPruneStage(deps, nil)
	state := defaultState()

	history := make([]providers.Message, 100)
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if pruneCallCount == 0 {
		t.Error("PruneMessages should have been called")
	}
}

func TestPruneStage_Over100Percent_CallsCompact(t *testing.T) {
	t.Parallel()
	compactCallCount := 0
	// budget = 1000 - 0 - 100 = 900
	// 50 msgs * 100 tokens = 5000 > 900 (100%)
	// after prune still > budget → compact
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 1000,
			MaxTokens:     100,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 100},
		PruneMessages: func(msgs []providers.Message, _ int) []providers.Message {
			// return same size — still over budget
			return msgs
		},
		CompactMessages: func(_ context.Context, msgs []providers.Message, _ string) ([]providers.Message, error) {
			compactCallCount++
			// return 1 message (under budget)
			return msgs[:1], nil
		},
	}
	stage := NewPruneStage(deps, NewMemoryFlushStage(deps))
	state := defaultState()

	history := make([]providers.Message, 50)
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if compactCallCount == 0 {
		t.Error("CompactMessages should have been called")
	}
	if !state.Prune.MidLoopCompacted {
		t.Error("MidLoopCompacted should be true")
	}
}

func TestPruneStage_StillOverAfterCompaction_ReturnsAbortRun(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 1000,
			MaxTokens:     100,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 100},
		PruneMessages: func(msgs []providers.Message, _ int) []providers.Message {
			return msgs // no reduction
		},
		CompactMessages: func(_ context.Context, msgs []providers.Message, _ string) ([]providers.Message, error) {
			// compaction still returns too many messages
			return msgs, nil
		},
	}
	stage := NewPruneStage(deps, NewMemoryFlushStage(deps))
	state := defaultState()

	history := make([]providers.Message, 50)
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != AbortRun {
		t.Errorf("Result() = %v, want AbortRun after compaction still over budget", stage.Result())
	}
}

func TestPruneStage_ZeroBudget_NoOp(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 100,
			MaxTokens:     200, // MaxTokens > ContextWindow → budget ≤ 0
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 50},
	}
	stage := NewPruneStage(deps, nil)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue for zero budget", stage.Result())
	}
}

// TestPruneStage_EffectiveContextWindow_OverridesConfig verifies that when
// ContextStage has resolved a per-model context window (e.g. gpt-4o=128k
// vs Config.ContextWindow=10k), PruneStage uses the resolved value.
//
// Without this, swapping models at session time would leave the pipeline
// pruning against the wrong budget. Phase 4 regression gate.
func TestPruneStage_EffectiveContextWindow_OverridesConfig(t *testing.T) {
	t.Parallel()
	pruneCallCount := 0
	// Config says 10k window, but model-specific resolution says 128k.
	// History is 60k tokens — would be over budget at 10k (fires prune),
	// under budget at 128k (no prune should happen).
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 10_000, // stale/default — should be ignored
			MaxTokens:     1_000,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 600},
		PruneMessages: func(msgs []providers.Message, _ int) []providers.Message {
			pruneCallCount++
			return msgs
		},
	}
	stage := NewPruneStage(deps, nil)
	state := defaultState()
	state.Context.EffectiveContextWindow = 128_000 // resolved by ContextStage

	history := make([]providers.Message, 100) // 100 * 600 = 60k < budget 127k
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if pruneCallCount != 0 {
		t.Errorf("PruneMessages should not fire with 128k window, called %d times", pruneCallCount)
	}
}

// --- RecentContextForRecall tests (Phase 9) ---

func TestBuildRecentContext_EmptyHistory(t *testing.T) {
	t.Parallel()
	if got := RecentContextForRecall(nil); got != "" {
		t.Errorf("empty history should return empty, got %q", got)
	}
}

func TestBuildRecentContext_SkipsNonUserMessages(t *testing.T) {
	t.Parallel()
	hist := []providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi back"},
		{Role: "tool", Content: "tool output"},
	}
	got := RecentContextForRecall(hist)
	if got != "hello" {
		t.Errorf("should only include user messages, got %q", got)
	}
}

func TestBuildRecentContext_CapsAtTwoTurns(t *testing.T) {
	t.Parallel()
	hist := []providers.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: "third"},
	}
	got := RecentContextForRecall(hist)
	// Should contain last two user turns ("second" and "third"), not "first"
	if !strings.Contains(got, "second") {
		t.Errorf("missing second turn, got %q", got)
	}
	if !strings.Contains(got, "third") {
		t.Errorf("missing third turn, got %q", got)
	}
	if strings.Contains(got, "first") {
		t.Errorf("should exclude old turns, got %q", got)
	}
}

func TestBuildRecentContext_PreservesTurnOrder(t *testing.T) {
	t.Parallel()
	hist := []providers.Message{
		{Role: "user", Content: "earlier"},
		{Role: "user", Content: "later"},
	}
	got := RecentContextForRecall(hist)
	// "earlier" should come before "later" in the output
	earlierIdx := strings.Index(got, "earlier")
	laterIdx := strings.Index(got, "later")
	if earlierIdx == -1 || laterIdx == -1 || earlierIdx >= laterIdx {
		t.Errorf("order broken: got %q (earlier=%d, later=%d)", got, earlierIdx, laterIdx)
	}
}

func TestBuildRecentContext_TruncatesLongMessages(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 500)
	hist := []providers.Message{
		{Role: "user", Content: long},
	}
	got := RecentContextForRecall(hist)
	if len([]rune(got)) > 300 {
		t.Errorf("result should be capped at 300 chars, got %d", len(got))
	}
}

// --- Prune Stage tests continued ---

// TestPruneStage_ReserveTokens_BuffersBudget verifies that ReserveTokens is
// subtracted from the usable budget so compaction fires earlier than the hard
// limit. Phase 5 regression gate for provider over-delivery protection.
func TestPruneStage_ReserveTokens_BuffersBudget(t *testing.T) {
	t.Parallel()
	pruneCallCount := 0
	// Without reserve: budget = 10000 - 0 - 1000 = 9000, softThreshold 6300
	// With reserve=2000: budget = 10000 - 0 - 1000 - 2000 = 7000, softThreshold 4900
	// 50 msgs * 100 = 5000 tokens — crosses the 4900 soft threshold only when reserve is set.
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 10_000,
			MaxTokens:     1_000,
			ReserveTokens: 2_000,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 100},
		PruneMessages: func(msgs []providers.Message, _ int) []providers.Message {
			pruneCallCount++
			return msgs[:1]
		},
	}
	stage := NewPruneStage(deps, nil)
	state := defaultState()
	history := make([]providers.Message, 50)
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if pruneCallCount != 1 {
		t.Errorf("ReserveTokens should have triggered early prune, called %d times", pruneCallCount)
	}
}

// TestPruneStage_EffectiveContextWindow_ZeroFallsBackToConfig verifies
// backward compatibility: when ContextStage hasn't resolved a model-specific
// window (unknown model, nil resolver, no registry), PruneStage falls back
// to Config.ContextWindow — matching pre-Phase-4 behavior.
func TestPruneStage_EffectiveContextWindow_ZeroFallsBackToConfig(t *testing.T) {
	t.Parallel()
	pruneCallCount := 0
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 10_000, // used because EffectiveContextWindow=0
			MaxTokens:     1_000,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 600},
		PruneMessages: func(msgs []providers.Message, _ int) []providers.Message {
			pruneCallCount++
			return msgs[:1]
		},
	}
	stage := NewPruneStage(deps, nil)
	state := defaultState()
	// state.Context.EffectiveContextWindow left as zero

	history := make([]providers.Message, 20) // 20 * 600 = 12k > 10k → should prune
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if pruneCallCount == 0 {
		t.Error("PruneMessages should fire with 10k fallback window")
	}
}

// --- ToolStage tests ---

func TestToolStage_NoToolCalls_NoOp(t *testing.T) {
	t.Parallel()
	execCalled := false
	deps := &PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, _ providers.ToolCall) ([]providers.Message, error) {
			execCalled = true
			return nil, nil
		},
	}
	stage := NewToolStage(deps)
	state := defaultState()
	// no LastResponse or empty ToolCalls
	state.Think.LastResponse = &providers.ChatResponse{FinishReason: "stop"}

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if execCalled {
		t.Error("ExecuteToolCall should not be called when no tool calls")
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue", stage.Result())
	}
}

func TestToolStage_SingleTool_ExecutesSequentially(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{
				{Role: "tool", Content: "result:" + tc.Name},
			}, nil
		},
	}
	stage := NewToolStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		ToolCalls: []providers.ToolCall{{ID: "1", Name: "read_file"}},
	}

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	pending := state.Messages.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending len = %d, want 1", len(pending))
	}
	if pending[0].Content != "result:read_file" {
		t.Errorf("pending[0].Content = %q", pending[0].Content)
	}
	if state.Tool.TotalToolCalls != 1 {
		t.Errorf("TotalToolCalls = %d, want 1", state.Tool.TotalToolCalls)
	}
}

func TestToolStage_MultipleTools_Sequential_MessagesInOrder(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{
				{Role: "tool", Content: "result:" + tc.Name},
			}, nil
		},
	}
	stage := NewToolStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		ToolCalls: []providers.ToolCall{
			{ID: "1", Name: "tool_a"},
			{ID: "2", Name: "tool_b"},
			{ID: "3", Name: "tool_c"},
		},
	}

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	pending := state.Messages.Pending()
	if len(pending) != 3 {
		t.Fatalf("pending len = %d, want 3", len(pending))
	}
	// messages must be in original tool call order
	if pending[0].Content != "result:tool_a" {
		t.Errorf("pending[0] = %q, want result:tool_a", pending[0].Content)
	}
	if pending[1].Content != "result:tool_b" {
		t.Errorf("pending[1] = %q, want result:tool_b", pending[1].Content)
	}
	if pending[2].Content != "result:tool_c" {
		t.Errorf("pending[2] = %q, want result:tool_c", pending[2].Content)
	}
	if state.Tool.TotalToolCalls != 3 {
		t.Errorf("TotalToolCalls = %d, want 3", state.Tool.TotalToolCalls)
	}
}

func TestToolStage_PartitionsParallelSafeTools(t *testing.T) {
	t.Parallel()
	var sequential []string
	var parallel []string
	var mu sync.Mutex
	deps := &PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			sequential = append(sequential, tc.Name)
			return []providers.Message{{Role: "tool", Content: "seq:" + tc.Name}}, nil
		},
		ExecuteToolRaw: func(_ context.Context, tc providers.ToolCall) (providers.Message, any, error) {
			mu.Lock()
			parallel = append(parallel, tc.Name)
			mu.Unlock()
			return providers.Message{Role: "tool", Content: "raw:" + tc.Name, ToolCallID: tc.ID}, nil, nil
		},
		ProcessToolResult: func(_ context.Context, _ *RunState, tc providers.ToolCall, rawMsg providers.Message, _ any) []providers.Message {
			return []providers.Message{{Role: "tool", Content: "par:" + tc.Name, ToolCallID: rawMsg.ToolCallID}}
		},
		ToolConcurrencySafe: func(toolName string, _ map[string]any) bool {
			return toolName == "read_a" || toolName == "read_b"
		},
	}
	stage := NewToolStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		ToolCalls: []providers.ToolCall{
			{ID: "1", Name: "read_a"},
			{ID: "2", Name: "read_b"},
			{ID: "3", Name: "write_c"},
		},
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(sequential) != 1 || sequential[0] != "write_c" {
		t.Fatalf("sequential = %v, want [write_c]", sequential)
	}
	if len(parallel) != 2 {
		t.Fatalf("parallel = %v, want 2 readonly tools", parallel)
	}
	pending := state.Messages.Pending()
	if len(pending) != 3 {
		t.Fatalf("pending len = %d, want 3", len(pending))
	}
	if pending[0].Content != "par:read_a" || pending[1].Content != "par:read_b" || pending[2].Content != "seq:write_c" {
		t.Fatalf("pending order/content = %#v", pending)
	}
}

func TestToolStage_LoopKilled_ReturnsBreakLoop(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		ExecuteToolCall: func(_ context.Context, _ *RunState, _ providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", Content: "ok"}}, nil
		},
	}
	stage := NewToolStage(deps)
	state := defaultState()
	state.Tool.LoopKilled = true
	state.Think.LastResponse = &providers.ChatResponse{
		ToolCalls: []providers.ToolCall{{ID: "1", Name: "tool_a"}},
	}

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v, want BreakLoop when LoopKilled", stage.Result())
	}
}

// TestToolStage_SoftInterrupt_CancelGroupCancelled_BlockGroupCompletes verifies
// that when InterruptCh fires during a parallel batch:
//   - Tools whose context is passed through (InterruptCancel) see cancellation.
//   - Tools whose context is context.WithoutCancel (InterruptBlock) run to completion.
func TestToolStage_SoftInterrupt_CancelGroupCancelled_BlockGroupCompletes(t *testing.T) {
	t.Parallel()

	// blockDone is set when the "block" tool's ExecuteToolRaw finishes.
	blockDone := make(chan struct{})
	// interruptCh is the soft-interrupt signal channel.
	interruptCh := make(chan struct{}, 1)

	// Tracks which contexts were cancelled at execution time.
	cancelCtxErr := make(chan error, 1)
	blockCtxErr := make(chan error, 1)

	deps := &PipelineDeps{
		InterruptCh: interruptCh,
		// ExecuteToolCall is required by the Execute guard even when the parallel path is taken.
		ExecuteToolCall: func(_ context.Context, _ *RunState, _ providers.ToolCall) ([]providers.Message, error) {
			return nil, nil
		},
		ExecuteToolRaw: func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error) {
			if tc.Name == "cancel_tool" {
				// Simulate: signal interrupt then wait for context cancellation.
				interruptCh <- struct{}{}
				<-ctx.Done() // batchCtx gets cancelled by the monitor goroutine
				cancelCtxErr <- ctx.Err()
				return providers.Message{Role: "tool", Content: "cancel_tool:cancelled", ToolCallID: tc.ID}, nil, nil
			}
			// "block_tool" uses context.WithoutCancel so it is unaffected.
			// Wait until cancel_tool has signalled, then verify ctx is still alive.
			<-blockDone // released by cancel_tool signalling (see below); use a sync point
			blockCtxErr <- ctx.Err() // should be nil for WithoutCancel ctx
			return providers.Message{Role: "tool", Content: "block_tool:done", ToolCallID: tc.ID}, nil, nil
		},
		ProcessToolResult: func(_ context.Context, _ *RunState, tc providers.ToolCall, rawMsg providers.Message, _ any) []providers.Message {
			return []providers.Message{{Role: "tool", Content: rawMsg.Content, ToolCallID: rawMsg.ToolCallID}}
		},
		ToolConcurrencySafe: func(_ string, _ map[string]any) bool { return true },
		// cancel_tool → InterruptCancel (inherits batchCtx); block_tool → InterruptBlock (WithoutCancel).
		ToolInterruptBehavior: func(toolName string, _ map[string]any) string {
			if toolName == "block_tool" {
				return "block"
			}
			return "cancel"
		},
	}

	// We need the pipeline's toolExecutionContext logic to respect ToolInterruptBehavior.
	// Since that logic lives in agent.Loop, we simulate it inside ExecuteToolRaw by
	// wrapping: block_tool → use WithoutCancel; cancel_tool → pass ctx through.
	// Override ExecuteToolRaw to apply the wrapping ourselves.
	rawFn := deps.ExecuteToolRaw
	deps.ExecuteToolRaw = func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error) {
		if tc.Name == "block_tool" {
			// Simulate InterruptBlock: use context.WithoutCancel.
			ctx = context.WithoutCancel(ctx)
		}
		return rawFn(ctx, tc)
	}

	// Sync point: block_tool waits until cancel_tool has signalled the interrupt.
	// We close blockDone after the interrupt is signalled so block_tool can proceed.
	go func() {
		// Wait for interrupt signal to be sent (cancel_tool sends it, monitor picks it up).
		// In practice, the monitor goroutine cancels batchCtx before cancel_tool unblocks.
		// We give block_tool a small window to proceed after the interrupt is fired.
		<-interruptCh // drain to let cancel_tool proceed (re-signal was already consumed by monitor)
		close(blockDone)
		// Re-signal so the monitor goroutine (still running) can consume it.
		// Actually, the monitor already consumed it. The test helper above drains it;
		// we close blockDone to unblock the block_tool goroutine.
	}()

	// Re-wire: the cancel_tool itself sends to interruptCh, but the monitor goroutine
	// inside executeParallel will consume it. We need block_tool to also proceed.
	// Simpler approach: use a separate sync channel.
	readyCh := make(chan struct{})
	interruptCh2 := make(chan struct{}, 1)
	deps.InterruptCh = interruptCh2
	deps.ExecuteToolRaw = func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error) {
		if tc.Name == "block_tool" {
			ctx = context.WithoutCancel(ctx)
			// Wait until cancel_tool has sent the interrupt signal.
			<-readyCh
			blockCtxErr <- ctx.Err()
			return providers.Message{Role: "tool", Content: "block_tool:done", ToolCallID: tc.ID}, nil, nil
		}
		// cancel_tool: signal interrupt, then wait for own context to be cancelled.
		interruptCh2 <- struct{}{}
		close(readyCh)     // let block_tool proceed
		<-ctx.Done()       // wait for batchCtx cancel propagated by monitor
		cancelCtxErr <- ctx.Err()
		return providers.Message{Role: "tool", Content: "cancel_tool:cancelled", ToolCallID: tc.ID}, nil, nil
	}

	stage := NewToolStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		ToolCalls: []providers.ToolCall{
			{ID: "1", Name: "cancel_tool"},
			{ID: "2", Name: "block_tool"},
		},
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// cancel_tool should have seen context.Canceled.
	select {
	case err := <-cancelCtxErr:
		if err == nil {
			t.Error("cancel_tool ctx should be cancelled after soft interrupt")
		}
	default:
		t.Error("cancel_tool never sent its ctx.Err()")
	}

	// block_tool should have seen nil ctx.Err() (WithoutCancel).
	select {
	case err := <-blockCtxErr:
		if err != nil {
			t.Errorf("block_tool ctx.Err() = %v, want nil (WithoutCancel)", err)
		}
	default:
		t.Error("block_tool never sent its ctx.Err()")
	}

	// Both tools should have produced results.
	pending := state.Messages.Pending()
	if len(pending) != 2 {
		t.Fatalf("pending = %d messages, want 2", len(pending))
	}
}

func TestToolStage_ToolBudgetExceeded_ReturnsBreakLoop(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxToolCalls: 5},
		ExecuteToolCall: func(_ context.Context, _ *RunState, _ providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", Content: "ok"}}, nil
		},
	}
	stage := NewToolStage(deps)
	state := defaultState()
	state.Tool.TotalToolCalls = 4 // one more puts it at 5
	state.Think.LastResponse = &providers.ChatResponse{
		ToolCalls: []providers.ToolCall{{ID: "1", Name: "tool_a"}},
	}

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v after budget exceeded, want BreakLoop", stage.Result())
	}
}

func TestToolStage_CheckReadOnly_ShouldBreak_ReturnsBreakLoop(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxToolCalls: 100},
		ExecuteToolCall: func(_ context.Context, _ *RunState, _ providers.ToolCall) ([]providers.Message, error) {
			return []providers.Message{{Role: "tool", Content: "ok"}}, nil
		},
		CheckReadOnly: func(_ *RunState) (*providers.Message, bool) {
			msg := &providers.Message{Role: "user", Content: "read-only warning"}
			return msg, true // shouldBreak = true
		},
	}
	stage := NewToolStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		ToolCalls: []providers.ToolCall{{ID: "1", Name: "read_file"}},
	}

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != BreakLoop {
		t.Errorf("Result() = %v, want BreakLoop when CheckReadOnly triggers", stage.Result())
	}
	// warning message appended
	pending := state.Messages.Pending()
	found := false
	for _, m := range pending {
		if m.Content == "read-only warning" {
			found = true
		}
	}
	if !found {
		t.Error("read-only warning message not found in pending")
	}
}

func TestToolStage_ExecuteToolCallNil_ReturnsError(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{}
	stage := NewToolStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		ToolCalls: []providers.ToolCall{{ID: "1", Name: "tool_a"}},
	}

	err := stage.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected error when ExecuteToolCall is nil")
	}
}

func TestToolStage_NilLastResponse_NoOp(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{}
	stage := NewToolStage(deps)
	state := defaultState()
	// LastResponse is nil

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue", stage.Result())
	}
}

// --- ObserveStage tests ---

func TestObserveStage_DrainInjectCh_AddsToPending(t *testing.T) {
	t.Parallel()
	injected := []providers.Message{
		{Role: "user", Content: "injected-1"},
		{Role: "user", Content: "injected-2"},
	}
	deps := &PipelineDeps{
		DrainInjectCh: func() []providers.Message { return injected },
	}
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{FinishReason: "stop"}

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	pending := state.Messages.Pending()
	if len(pending) != 2 {
		t.Fatalf("pending len = %d, want 2", len(pending))
	}
	if pending[0].Content != "injected-1" || pending[1].Content != "injected-2" {
		t.Errorf("pending = %v", pending)
	}
}

func TestObserveStage_FinalContent_SetWhenNoToolCalls(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{}
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		Content:      "final answer",
		Thinking:     "my reasoning",
		FinishReason: "stop",
	}

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != "final answer" {
		t.Errorf("FinalContent = %q, want final answer", state.Observe.FinalContent)
	}
	if state.Observe.FinalThinking != "my reasoning" {
		t.Errorf("FinalThinking = %q, want my reasoning", state.Observe.FinalThinking)
	}
}

func TestObserveStage_FinalContent_NotSetWhenToolCalls(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{}
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{
		Content: "intermediate",
		ToolCalls: []providers.ToolCall{
			{ID: "1", Name: "read_file"},
		},
		FinishReason: "tool_calls",
	}

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != "" {
		t.Errorf("FinalContent = %q, want empty (tool calls present)", state.Observe.FinalContent)
	}
}

func TestObserveStage_BlockReplies_IncrementedOnlyWithToolCalls(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{}
	stage := NewObserveStage(deps)
	state := defaultState()

	// first tool iteration response (has tool calls → should count)
	state.Think.LastResponse = &providers.ChatResponse{
		Content:   "reply 1",
		ToolCalls: []providers.ToolCall{{ID: "1", Name: "tool_a"}},
	}
	_ = stage.Execute(context.Background(), state)

	// second tool iteration response (has tool calls → should count)
	state.Think.LastResponse = &providers.ChatResponse{
		Content:   "reply 2",
		ToolCalls: []providers.ToolCall{{ID: "2", Name: "tool_b"}},
	}
	_ = stage.Execute(context.Background(), state)

	if state.Observe.BlockReplies != 2 {
		t.Errorf("BlockReplies = %d, want 2", state.Observe.BlockReplies)
	}
	if state.Observe.LastBlockReply != "reply 2" {
		t.Errorf("LastBlockReply = %q, want reply 2", state.Observe.LastBlockReply)
	}
}

// Regression test for #838: final answer (no tool calls) must NOT increment
// BlockReplies, otherwise gateway dedup falsely suppresses delivery.
func TestObserveStage_FinalAnswer_NoToolCalls_BlockRepliesNotIncremented(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{}
	stage := NewObserveStage(deps)
	state := defaultState()

	// Final answer: content but NO tool calls
	state.Think.LastResponse = &providers.ChatResponse{
		Content:      "Here is your answer.",
		FinishReason: "stop",
		ToolCalls:    nil,
	}
	_ = stage.Execute(context.Background(), state)

	if state.Observe.BlockReplies != 0 {
		t.Errorf("BlockReplies = %d, want 0 for final answer without tool calls", state.Observe.BlockReplies)
	}
	if state.Observe.LastBlockReply != "" {
		t.Errorf("LastBlockReply = %q, want empty for final answer", state.Observe.LastBlockReply)
	}
	// FinalContent should still be set
	if state.Observe.FinalContent != "Here is your answer." {
		t.Errorf("FinalContent = %q, want answer text", state.Observe.FinalContent)
	}
}

func TestObserveStage_NilResponse_NoOp(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{}
	stage := NewObserveStage(deps)
	state := defaultState()
	// LastResponse stays nil

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.BlockReplies != 0 {
		t.Errorf("BlockReplies = %d, want 0", state.Observe.BlockReplies)
	}
}

func TestObserveStage_EmptyContent_BlockRepliesNotIncremented(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{}
	stage := NewObserveStage(deps)
	state := defaultState()
	state.Think.LastResponse = &providers.ChatResponse{Content: "", FinishReason: "tool_calls",
		ToolCalls: []providers.ToolCall{{ID: "1", Name: "tool"}}}

	_ = stage.Execute(context.Background(), state)

	if state.Observe.BlockReplies != 0 {
		t.Errorf("BlockReplies = %d, want 0 when content empty", state.Observe.BlockReplies)
	}
}

// --- CheckpointStage tests ---

func TestCheckpointStage_SkipsIteration0(t *testing.T) {
	t.Parallel()
	flushCalled := false
	deps := &PipelineDeps{
		Config: PipelineConfig{CheckpointInterval: 5},
		FlushMessages: func(_ context.Context, _ string, _ []providers.Message) error {
			flushCalled = true
			return nil
		},
	}
	stage := NewCheckpointStage(deps)
	state := defaultState()
	state.Iteration = 0
	state.Messages.AppendPending(providers.Message{Role: "user", Content: "test"})

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if flushCalled {
		t.Error("FlushMessages should not be called at iteration 0")
	}
}

func TestCheckpointStage_FlushesAtInterval(t *testing.T) {
	t.Parallel()
	var flushedMsgs []providers.Message
	deps := &PipelineDeps{
		Config: PipelineConfig{CheckpointInterval: 5},
		FlushMessages: func(_ context.Context, _ string, msgs []providers.Message) error {
			flushedMsgs = msgs
			return nil
		},
	}
	stage := NewCheckpointStage(deps)
	state := defaultState()
	state.Iteration = 5 // 5 % 5 == 0 → flush
	state.Messages.AppendPending(providers.Message{Role: "user", Content: "checkpoint-msg"})

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(flushedMsgs) != 1 {
		t.Fatalf("flushed %d messages, want 1", len(flushedMsgs))
	}
	if flushedMsgs[0].Content != "checkpoint-msg" {
		t.Errorf("flushed[0].Content = %q", flushedMsgs[0].Content)
	}
	if state.Compact.CheckpointFlushedMsgs != 1 {
		t.Errorf("CheckpointFlushedMsgs = %d, want 1", state.Compact.CheckpointFlushedMsgs)
	}
}

func TestCheckpointStage_NonFatalOnFlushError(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{CheckpointInterval: 5},
		FlushMessages: func(_ context.Context, _ string, _ []providers.Message) error {
			return errors.New("db unavailable")
		},
	}
	stage := NewCheckpointStage(deps)
	state := defaultState()
	state.Iteration = 5
	state.Messages.AppendPending(providers.Message{Role: "user", Content: "msg"})

	err := stage.Execute(context.Background(), state)
	// error must be swallowed (non-fatal)
	if err != nil {
		t.Errorf("Execute() should swallow flush error, got: %v", err)
	}
}

func TestCheckpointStage_DefaultInterval5(t *testing.T) {
	t.Parallel()
	flushCalled := false
	deps := &PipelineDeps{
		// CheckpointInterval = 0 → defaults to 5 internally
		FlushMessages: func(_ context.Context, _ string, _ []providers.Message) error {
			flushCalled = true
			return nil
		},
	}
	stage := NewCheckpointStage(deps)
	state := defaultState()
	state.Iteration = 5
	state.Messages.AppendPending(providers.Message{Role: "user", Content: "msg"})

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !flushCalled {
		t.Error("FlushMessages should be called at iteration 5 with default interval")
	}
}

func TestCheckpointStage_SkipsNonIntervalIteration(t *testing.T) {
	t.Parallel()
	flushCalled := false
	deps := &PipelineDeps{
		Config: PipelineConfig{CheckpointInterval: 5},
		FlushMessages: func(_ context.Context, _ string, _ []providers.Message) error {
			flushCalled = true
			return nil
		},
	}
	stage := NewCheckpointStage(deps)
	state := defaultState()
	state.Iteration = 3 // not divisible by 5
	state.Messages.AppendPending(providers.Message{Role: "user", Content: "msg"})

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if flushCalled {
		t.Error("FlushMessages should not be called at non-interval iteration")
	}
}

// --- FinalizeStage tests ---

func TestFinalizeStage_SanitizesContent(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		SanitizeContent: func(content string) string {
			return "sanitized:" + content
		},
		FlushMessages: func(_ context.Context, _ string, _ []providers.Message) error { return nil },
	}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Observe.FinalContent = "raw content"

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != "sanitized:raw content" {
		t.Errorf("FinalContent = %q, want sanitized:raw content", state.Observe.FinalContent)
	}
}

func TestFinalizeStage_DeduplicatesMediaByPath(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Tool.MediaResults = []MediaResult{
		{Path: "/tmp/a.png", ContentType: "image/png"},
		{Path: "/tmp/b.png", ContentType: "image/png"},
		{Path: "/tmp/a.png", ContentType: "image/png"}, // duplicate
	}

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(state.Tool.MediaResults) != 2 {
		t.Errorf("MediaResults len = %d after dedup, want 2", len(state.Tool.MediaResults))
	}
}

func TestFinalizeStage_PopulatesFileSizes(t *testing.T) {
	t.Parallel()
	// create a real temp file
	f, err := os.CreateTemp("", "finalize-test-*.bin")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(f.Name())

	content := []byte("hello world")
	if _, err := f.Write(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()

	deps := &PipelineDeps{}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Tool.MediaResults = []MediaResult{
		{Path: f.Name(), ContentType: "application/octet-stream", Size: 0},
	}

	err = stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Tool.MediaResults[0].Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", state.Tool.MediaResults[0].Size, len(content))
	}
}

func TestFinalizeStage_PopulatesFileSizes_SkipsNonexistent(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Tool.MediaResults = []MediaResult{
		{Path: "/nonexistent/path/file.png", ContentType: "image/png", Size: 0},
	}

	// should not error out on missing file
	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Tool.MediaResults[0].Size != 0 {
		t.Errorf("Size = %d for nonexistent file, want 0", state.Tool.MediaResults[0].Size)
	}
}

// --- ContextStage tests ---

func TestContextStage_ResolveWorkspace(t *testing.T) {
	t.Parallel()
	wantWS := &workspace.WorkspaceContext{ActivePath: "/resolved"}
	deps := &PipelineDeps{
		ResolveWorkspace: func(_ context.Context, _ *RunInput) (*workspace.WorkspaceContext, error) {
			return wantWS, nil
		},
	}
	stage := NewContextStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Workspace != wantWS {
		t.Errorf("Workspace = %v, want %v", state.Workspace, wantWS)
	}
}

func TestContextStage_ResolveWorkspaceError(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		ResolveWorkspace: func(_ context.Context, _ *RunInput) (*workspace.WorkspaceContext, error) {
			return nil, errors.New("workspace not found")
		},
	}
	stage := NewContextStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected error from ResolveWorkspace, got nil")
	}
}

func TestContextStage_LoadContextFiles(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		LoadContextFiles: func(_ context.Context, _ string) ([]bootstrap.ContextFile, bool) {
			return []bootstrap.ContextFile{
				{Path: "SOUL.md", Content: "soul content"},
			}, true
		},
	}
	stage := NewContextStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(state.Context.ContextFiles) != 1 {
		t.Fatalf("ContextFiles len = %d, want 1", len(state.Context.ContextFiles))
	}
	if !state.Context.HadBootstrap {
		t.Error("HadBootstrap should be true")
	}
}

func TestContextStage_BuildMessages(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		BuildMessages: func(_ context.Context, _ *RunInput, _ []providers.Message, _ string) ([]providers.Message, error) {
			return []providers.Message{
				{Role: "system", Content: "system prompt"},
				{Role: "user", Content: "history msg"},
			}, nil
		},
	}
	stage := NewContextStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	sys := state.Messages.System()
	if sys.Content != "system prompt" {
		t.Errorf("System content = %q, want system prompt", sys.Content)
	}
	hist := state.Messages.History()
	if len(hist) != 1 || hist[0].Content != "history msg" {
		t.Errorf("History = %v, want 1 history msg", hist)
	}
}

func TestContextStage_BuildMessages_ErrorPropagates(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		BuildMessages: func(_ context.Context, _ *RunInput, _ []providers.Message, _ string) ([]providers.Message, error) {
			return nil, errors.New("build failed")
		},
	}
	stage := NewContextStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected error from BuildMessages, got nil")
	}
}

func TestContextStage_ComputeOverhead(t *testing.T) {
	t.Parallel()
	// TokenCounter returns 50 tokens per message; system msg = 1 msg → 50
	deps := &PipelineDeps{
		TokenCounter: &mockTokenCounter{countPerMessage: 50},
	}
	stage := NewContextStage(deps)
	state := defaultState()
	// put a system message so the counter has something to count
	state.Messages.SetSystem(providers.Message{Role: "system", Content: "sys"})

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Context.OverheadTokens != 50 {
		t.Errorf("OverheadTokens = %d, want 50", state.Context.OverheadTokens)
	}
}

func TestContextStage_EnrichMedia(t *testing.T) {
	t.Parallel()
	called := false
	deps := &PipelineDeps{
		EnrichMedia: func(_ context.Context, _ *RunState) error {
			called = true
			return nil
		},
	}
	stage := NewContextStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !called {
		t.Error("EnrichMedia callback was not called")
	}
}

func TestContextStage_EnrichMedia_ErrorPropagates(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		EnrichMedia: func(_ context.Context, _ *RunState) error {
			return errors.New("media enrichment failed")
		},
	}
	stage := NewContextStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected error from EnrichMedia, got nil")
	}
}

func TestContextStage_InjectReminders(t *testing.T) {
	t.Parallel()
	reminder := providers.Message{Role: "user", Content: "reminder msg"}
	deps := &PipelineDeps{
		InjectReminders: func(_ context.Context, _ *RunInput, msgs []providers.Message) []providers.Message {
			return append(msgs, reminder)
		},
	}
	stage := NewContextStage(deps)
	state := defaultState()
	state.Messages.SetHistory([]providers.Message{
		{Role: "user", Content: "existing"},
	})

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	hist := state.Messages.History()
	if len(hist) != 2 {
		t.Fatalf("History len = %d, want 2 (existing + reminder)", len(hist))
	}
	if hist[1].Content != "reminder msg" {
		t.Errorf("hist[1].Content = %q, want reminder msg", hist[1].Content)
	}
}

func TestContextStage_AllNilCallbacks(t *testing.T) {
	t.Parallel()
	// No callbacks set — should not panic
	deps := &PipelineDeps{}
	stage := NewContextStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
}

// --- MemoryFlushStage tests ---

func TestMemoryFlushStage_CallsCallback(t *testing.T) {
	t.Parallel()
	called := false
	deps := &PipelineDeps{
		RunMemoryFlush: func(_ context.Context, _ *RunState) error {
			called = true
			return nil
		},
	}
	stage := NewMemoryFlushStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !called {
		t.Error("RunMemoryFlush was not called")
	}
}

func TestMemoryFlushStage_NilCallback(t *testing.T) {
	t.Parallel()
	// RunMemoryFlush is nil — should not panic and return nil
	deps := &PipelineDeps{}
	stage := NewMemoryFlushStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
}

func TestMemoryFlushStage_ErrorNonFatal(t *testing.T) {
	t.Parallel()
	// Callback returns an error — MemoryFlushStage must swallow it (non-fatal).
	deps := &PipelineDeps{
		RunMemoryFlush: func(_ context.Context, _ *RunState) error {
			return errors.New("flush db unavailable")
		},
	}
	stage := NewMemoryFlushStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	// must NOT propagate the error
	if err != nil {
		t.Errorf("Execute() should swallow flush error, got: %v", err)
	}
}

func TestFinalizeStage_FlushesRemainingPending(t *testing.T) {
	t.Parallel()
	var flushedMsgs []providers.Message
	deps := &PipelineDeps{
		FlushMessages: func(_ context.Context, _ string, msgs []providers.Message) error {
			flushedMsgs = append(flushedMsgs, msgs...)
			return nil
		},
	}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Observe.FinalContent = "hello"
	state.Messages.AppendPending(providers.Message{Role: "assistant", Content: "tool result"})

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	// Expect 2 flushed: the pre-existing pending + the final assistant message built by FinalizeStage
	if len(flushedMsgs) != 2 {
		t.Fatalf("flushed %d messages, want 2", len(flushedMsgs))
	}
	if flushedMsgs[0].Content != "tool result" {
		t.Errorf("flushed[0].Content = %q, want tool result", flushedMsgs[0].Content)
	}
	if flushedMsgs[1].Role != "assistant" || flushedMsgs[1].Content != "hello" {
		t.Errorf("flushed[1] = %q/%q, want assistant/hello", flushedMsgs[1].Role, flushedMsgs[1].Content)
	}
}

func TestFinalizeStage_CallsBootstrapCleanup_WhenHadBootstrap(t *testing.T) {
	t.Parallel()
	cleanupCalled := false
	deps := &PipelineDeps{
		BootstrapCleanup: func(_ context.Context, _ *RunState) error {
			cleanupCalled = true
			return nil
		},
	}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Context.HadBootstrap = true

	err := stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !cleanupCalled {
		t.Error("BootstrapCleanup should be called when HadBootstrap=true")
	}
}

func TestFinalizeStage_SkipsBootstrapCleanup_WhenNoBootstrap(t *testing.T) {
	t.Parallel()
	cleanupCalled := false
	deps := &PipelineDeps{
		BootstrapCleanup: func(_ context.Context, _ *RunState) error {
			cleanupCalled = true
			return nil
		},
	}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Context.HadBootstrap = false

	_ = stage.Execute(context.Background(), state)
	if cleanupCalled {
		t.Error("BootstrapCleanup should NOT be called when HadBootstrap=false")
	}
}

func TestFinalizeStage_PopulatesFileSizes_SkipsAlreadySet(t *testing.T) {
	t.Parallel()
	// create a real temp file to make sure stat would work
	f, err := os.CreateTemp("", "finalize-size-set-*.bin")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	f.Write([]byte("data"))
	f.Close()

	deps := &PipelineDeps{}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	// Size already set — should not be overwritten
	state.Tool.MediaResults = []MediaResult{
		{Path: f.Name(), ContentType: "image/png", Size: 999},
	}

	err = stage.Execute(context.Background(), state)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	// Size=999 was already set, stat should not overwrite
	if state.Tool.MediaResults[0].Size != 999 {
		t.Errorf("Size = %d, want 999 (pre-set)", state.Tool.MediaResults[0].Size)
	}
}

func TestFinalizeStage_MaybeSummarize_Called(t *testing.T) {
	t.Parallel()
	summarizeCalled := false
	deps := &PipelineDeps{
		MaybeSummarize: func(_ context.Context, sessionKey string) {
			if sessionKey == "sess-1" {
				summarizeCalled = true
			}
		},
	}
	stage := NewFinalizeStage(deps)
	state := defaultState()

	_ = stage.Execute(context.Background(), state)
	if !summarizeCalled {
		t.Error("MaybeSummarize should be called in finalize")
	}
}

// --- integration-style: multiple stages chained ---

func TestStagesChained_ThinkThenObserve_FinalContentSet(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{
				Content:      "chain result",
				FinishReason: "stop",
			}, nil
		},
	}
	think := NewThinkStage(deps)
	observe := NewObserveStage(deps)
	state := defaultState()

	_ = think.Execute(context.Background(), state)
	_ = observe.Execute(context.Background(), state)

	if state.Observe.FinalContent != "chain result" {
		t.Errorf("FinalContent = %q, want chain result", state.Observe.FinalContent)
	}
}

func TestFinalizeStage_DeduplicatesMedia_WithRealFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "media.png")
	if err := os.WriteFile(path, []byte("imgdata"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	deps := &PipelineDeps{}
	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Tool.MediaResults = []MediaResult{
		{Path: path, ContentType: "image/png"},
		{Path: path, ContentType: "image/png"}, // dup
	}

	_ = stage.Execute(context.Background(), state)

	if len(state.Tool.MediaResults) != 1 {
		t.Errorf("MediaResults = %d after dedup, want 1", len(state.Tool.MediaResults))
	}
	if state.Tool.MediaResults[0].Size == 0 {
		t.Error("Size should be populated from real file")
	}
}
