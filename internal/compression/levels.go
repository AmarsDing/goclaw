// Package compression provides layered context compression for long-running sessions.
//
// Claude Code's insight: "context too long" is not a single problem but a systemic
// one requiring multiple strategies at different granularities. This package defines
// five compression levels (L0–L4) that activate progressively as token usage grows.
package compression

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Level identifies a compression tier.
type Level int

const (
	L0Microcompact    Level = iota // apply tool-result budget to oversized outputs
	L1SnipCompact                  // remove old tool_result bodies, keep summaries
	L2FullCompact                  // microcompact compactable tool results
	L3ContextCollapse              // collapse older context into a minimal boundary
	L4CacheReset                   // full compact / cache reset rebuild
)

func (l Level) String() string {
	switch l {
	case L0Microcompact:
		return "L0_tool_result_budget"
	case L1SnipCompact:
		return "L1_snip_compact"
	case L2FullCompact:
		return "L2_micro_compact"
	case L3ContextCollapse:
		return "L3_context_collapse"
	case L4CacheReset:
		return "L4_full_compact"
	default:
		return fmt.Sprintf("L%d_unknown", int(l))
	}
}

// Thresholds configures when each compression level activates.
// Values are expressed as a fraction of the context window (0.0–1.0).
type Thresholds struct {
	L0Trigger float64 // default 0.50 — start microcompacting tool results
	L1Trigger float64 // default 0.65 — snip old tool result bodies
	L2Trigger float64 // default 0.80 — full conversation compaction
	L3Trigger float64 // default 0.90 — collapse existing summaries
	L4Trigger float64 // default 0.95 — cache reset, rebuild minimal context
}

// DefaultThresholds returns conservative defaults.
func DefaultThresholds() Thresholds {
	return Thresholds{
		L0Trigger: 0.50,
		L1Trigger: 0.65,
		L2Trigger: 0.80,
		L3Trigger: 0.90,
		L4Trigger: 0.95,
	}
}

// Config configures the compression engine.
type Config struct {
	ContextWindow       int // total context window in tokens
	MaxToolResultTokens int // L0: max tokens per tool result before truncation
	SnipRetainCount     int // L1: keep last N tool results intact
	Thresholds          Thresholds
}

// DefaultConfig returns defaults for a 200k context window.
func DefaultConfig() Config {
	return Config{
		ContextWindow:       200000,
		MaxToolResultTokens: 4000,
		SnipRetainCount:     5,
		Thresholds:          DefaultThresholds(),
	}
}

// TokenCounter estimates the token count of a message slice.
type TokenCounter func(msgs []providers.Message) int

// Compactor summarises a message slice into fewer tokens.
type Compactor func(ctx context.Context, msgs []providers.Message) ([]providers.Message, error)

// Engine applies layered compression to a message history.
type Engine struct {
	config      Config
	countTokens TokenCounter
	compact     Compactor
}

// NewEngine creates a compression engine.
func NewEngine(cfg Config, counter TokenCounter, compactor Compactor) *Engine {
	return &Engine{
		config:      cfg,
		countTokens: counter,
		compact:     compactor,
	}
}

// Result describes what the engine did.
type Result struct {
	Messages        []providers.Message
	LevelApplied    Level
	LevelsApplied   []Level
	TokensBefore    int
	TokensAfter     int
	Duration        time.Duration
	TruncatedTools  int
	CompactedPrefix bool
}

// CompactableTools is the default whitelist for micro- and snip-compaction.
var CompactableTools = map[string]bool{
	"read":       true,
	"read_file":  true,
	"grep":       true,
	"glob":       true,
	"web_search": true,
	"web_fetch":  true,
	"edit":       true,
	"write_file": true,
	"exec":       true,
	"bash":       true,
	"shell":      true,
}

// Compress applies staged compression against the engine's configured budget.
func (e *Engine) Compress(ctx context.Context, msgs []providers.Message) (*Result, bool, error) {
	return e.CompressToBudget(ctx, msgs, e.config.ContextWindow)
}

// CompressToBudget applies staged compression until the message slice fits
// under the provided budget or no further stage makes progress.
func (e *Engine) CompressToBudget(ctx context.Context, msgs []providers.Message, budget int) (*Result, bool, error) {
	start := time.Now()
	tokens := e.countTokens(msgs)
	if budget <= 0 {
		budget = e.config.ContextWindow
	}
	initialLevel := e.requiredLevel(tokens, budget)
	if initialLevel < 0 {
		return nil, false, nil
	}

	slog.Debug("compression triggered",
		"tokens", tokens, "window", budget,
		"ratio", float64(tokens)/float64(maxInt(budget, 1)),
		"level", initialLevel)

	current := cloneMessages(msgs)
	applied := make([]Level, 0, 5)
	totalTruncated := 0
	compactedPrefix := false

	for _, level := range []Level{L0Microcompact, L1SnipCompact, L2FullCompact, L3ContextCollapse, L4CacheReset} {
		currentTokens := e.countTokens(current)
		if e.requiredLevel(currentTokens, budget) < level {
			continue
		}

		var next []providers.Message
		var truncated int
		var collapsed bool
		var err error

		switch level {
		case L0Microcompact:
			next, truncated = e.applyL0(current)
		case L1SnipCompact:
			next, truncated = e.applyL1(current)
		case L2FullCompact:
			next, truncated = e.applyL2(current)
		case L3ContextCollapse:
			next, collapsed, err = e.applyL3(ctx, current)
		case L4CacheReset:
			next, collapsed, err = e.applyL4(ctx, current)
		}
		if err != nil {
			return nil, false, fmt.Errorf("compression %s: %w", level, err)
		}
		if !messagesChanged(current, next) {
			continue
		}

		current = next
		applied = append(applied, level)
		totalTruncated += truncated
		compactedPrefix = compactedPrefix || collapsed

		if e.countTokens(current) <= budget {
			break
		}
	}

	if len(applied) == 0 {
		return nil, false, nil
	}

	after := e.countTokens(current)
	return &Result{
		Messages:        current,
		LevelApplied:    applied[len(applied)-1],
		LevelsApplied:   applied,
		TokensBefore:    tokens,
		TokensAfter:     after,
		Duration:        time.Since(start),
		TruncatedTools:  totalTruncated,
		CompactedPrefix: compactedPrefix,
	}, true, nil
}

func (e *Engine) requiredLevel(tokens int, budget int) Level {
	ratio := float64(tokens) / float64(maxInt(budget, 1))
	th := e.config.Thresholds

	switch {
	case ratio >= th.L4Trigger:
		return L4CacheReset
	case ratio >= th.L3Trigger:
		return L3ContextCollapse
	case ratio >= th.L2Trigger:
		return L2FullCompact
	case ratio >= th.L1Trigger:
		return L1SnipCompact
	case ratio >= th.L0Trigger:
		return L0Microcompact
	default:
		return -1
	}
}

// applyL0 truncates individual tool results that exceed MaxToolResultTokens.
func (e *Engine) applyL0(msgs []providers.Message) ([]providers.Message, int) {
	out := cloneMessages(msgs)
	truncatedCount := 0

	for i := range out {
		if out[i].Role != "tool" {
			continue
		}
		estimate := e.estimateMessageTokens(out[i])
		if estimate > e.config.MaxToolResultTokens {
			maxChars := maxInt(256, e.config.MaxToolResultTokens*4)
			truncated := out[i].Content
			if len(truncated) > maxChars {
				truncated = truncated[:maxChars]
			}
			lastNewline := strings.LastIndex(truncated, "\n")
			if lastNewline > len(truncated)/2 {
				truncated = truncated[:lastNewline]
			}
			out[i].Content = truncated + "\n\n[... output truncated — " +
				fmt.Sprintf("%d", len(out[i].Content)-len(truncated)) + " chars omitted ...]"
			truncatedCount++
		}
	}
	return out, truncatedCount
}

// applyL1 replaces old tool result bodies with short summaries.
func (e *Engine) applyL1(msgs []providers.Message) ([]providers.Message, int) {
	toolNames := buildToolNameMap(msgs)
	toolCount := 0
	for _, m := range msgs {
		if m.Role == "tool" && e.isCompactableTool(toolNames[m.ToolCallID]) {
			toolCount++
		}
	}

	out := cloneMessages(msgs)
	truncatedCount := 0

	snipBefore := toolCount - e.config.SnipRetainCount
	if snipBefore <= 0 {
		return e.applyL0(out)
	}

	seen := 0
	for i := range out {
		if out[i].Role != "tool" || !e.isCompactableTool(toolNames[out[i].ToolCallID]) {
			continue
		}
		seen++
		if seen <= snipBefore {
			preview := out[i].Content
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			out[i].Content = "[snipped tool result: " + preview + "]"
			truncatedCount++
		}
	}
	return out, truncatedCount
}

// applyL2 performs microcompact on compactable tool outputs.
func (e *Engine) applyL2(msgs []providers.Message) ([]providers.Message, int) {
	out := cloneMessages(msgs)
	toolNames := buildToolNameMap(msgs)
	truncatedCount := 0
	for i := range out {
		if out[i].Role != "tool" || !e.isCompactableTool(toolNames[out[i].ToolCallID]) {
			continue
		}
		if len(out[i].Content) <= 600 {
			continue
		}
		head := minInt(240, len(out[i].Content))
		tail := minInt(160, len(out[i].Content)-head)
		if tail <= 0 {
			continue
		}
		out[i].Content = out[i].Content[:head] + "\n...\n[microcompact]\n...\n" + out[i].Content[len(out[i].Content)-tail:]
		truncatedCount++
	}
	return out, truncatedCount
}

// applyL3 collapses older context before the latest user turn.
func (e *Engine) applyL3(ctx context.Context, msgs []providers.Message) ([]providers.Message, bool, error) {
	lastUser := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			lastUser = i
			break
		}
	}
	if lastUser <= 0 {
		return cloneMessages(msgs), false, nil
	}

	if e.compact == nil {
		out := make([]providers.Message, 0, len(msgs))
		out = append(out, msgs[:1]...)
		out = append(out, providers.Message{
			Role:    "system",
			Content: "[context collapse] Earlier conversation was collapsed to preserve budget.",
		})
		out = append(out, msgs[lastUser:]...)
		return out, true, nil
	}

	prefix, err := e.compact(ctx, cloneMessages(msgs[:lastUser]))
	if err != nil {
		return nil, false, err
	}
	out := make([]providers.Message, 0, len(prefix)+len(msgs[lastUser:]))
	out = append(out, prefix...)
	out = append(out, msgs[lastUser:]...)
	return out, true, nil
}

// applyL4 performs full compaction / cache reset: keep system messages and a
// compact summary of earlier conversation plus the latest exchange.
func (e *Engine) applyL4(ctx context.Context, msgs []providers.Message) ([]providers.Message, bool, error) {
	var out []providers.Message

	// Keep system messages.
	for _, m := range msgs {
		if m.Role == "system" {
			out = append(out, m)
		}
	}

	// Keep the last user message and any following assistant/tool messages.
	lastUserIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx >= 0 {
		// Inject a summary of what came before.
		if e.compact != nil {
			before := msgs[:lastUserIdx]
			if len(before) > 0 {
				compacted, err := e.compact(ctx, before)
				if err == nil {
					out = append(out, compacted...)
				}
			}
		}
		out = append(out, msgs[lastUserIdx:]...)
	}

	if len(out) == 0 {
		return cloneMessages(msgs), false, nil
	}
	return out, true, nil
}

func (e *Engine) estimateMessageTokens(msg providers.Message) int {
	if e.countTokens != nil {
		if count := e.countTokens([]providers.Message{msg}); count > 0 {
			return count
		}
	}
	contentRunes := len([]rune(msg.Content))
	thinkingRunes := len([]rune(msg.Thinking))
	toolWeight := len(msg.ToolCalls) * 32
	return int(math.Ceil(float64(contentRunes+thinkingRunes)/4.0)) + toolWeight
}

func (e *Engine) isCompactableTool(toolName string) bool {
	if toolName == "" {
		return true
	}
	allowed, ok := CompactableTools[strings.ToLower(toolName)]
	if !ok {
		return true
	}
	return allowed
}

func buildToolNameMap(msgs []providers.Message) map[string]string {
	toolNames := make(map[string]string)
	for _, msg := range msgs {
		if msg.Role != "assistant" {
			continue
		}
		for _, call := range msg.ToolCalls {
			toolNames[call.ID] = call.Name
		}
	}
	return toolNames
}

func cloneMessages(msgs []providers.Message) []providers.Message {
	out := make([]providers.Message, len(msgs))
	copy(out, msgs)
	return out
}

func messagesChanged(before, after []providers.Message) bool {
	if len(before) != len(after) {
		return true
	}
	for i := range before {
		if before[i].Content != after[i].Content || before[i].Role != after[i].Role || before[i].ToolCallID != after[i].ToolCallID {
			return true
		}
	}
	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
