package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// compactionSummaryPrompt is the structured summarization instruction used by both
// mid-loop compaction and background summarization. Matching OpenClaw TS compaction.ts
// MERGE_SUMMARIES_INSTRUCTIONS + IDENTIFIER_PRESERVATION_INSTRUCTIONS.
const compactionSummaryPrompt = `Summarize this conversation concisely for the AI agent to resume work.

MUST PRESERVE:
- Active tasks and their current status (in-progress, blocked, pending)
- Pending subagent tasks (IDs, labels, statuses) — agent needs to know what is still running
- Pending team task results awaiting delivery (task IDs, assignees, statuses)
- Any "waiting for..." state — do NOT drop expectations of future results
- Batch operation progress (e.g., "5/17 items completed")
- The last thing the user requested and what was being done about it
- Decisions made and their rationale
- TODOs, open questions, and constraints
- Any commitments or follow-ups promised

IDENTIFIER PRESERVATION:
Preserve all opaque identifiers exactly as written (no shortening or reconstruction),
including UUIDs, hashes, IDs, tokens, API keys, hostnames, IPs, ports, URLs, and file names.

PRIORITIZE recent context over older history. The agent needs to know
what it was doing, not just what was discussed.

Conversation to summarize:

`

// compactMessagesInPlace summarizes the first ~70% of messages into a condensed
// summary, keeping the last ~30% intact. Operates purely on the local messages
// slice — no session state touched, no locks needed.
// Returns nil on failure (caller keeps original messages).
func (l *Loop) compactMessagesInPlace(ctx context.Context, messages []providers.Message) []providers.Message {
	if len(messages) < 6 {
		return nil
	}

	// Resolve keepCount from compaction config (same defaults as maybeSummarize).
	keepCount := 4
	if l.compactionCfg != nil && l.compactionCfg.KeepLastMessages > 0 {
		keepCount = l.compactionCfg.KeepLastMessages
	}
	// Ensure we keep at least 30% of messages.
	if minKeep := len(messages) * 3 / 10; minKeep > keepCount {
		keepCount = minKeep
	}

	splitIdx := len(messages) - keepCount

	// Walk backward from splitIdx to find a clean boundary —
	// avoid splitting tool_use → tool_result pairs.
	for splitIdx > 0 {
		m := messages[splitIdx]
		if m.Role == "tool" || (m.Role == "assistant" && len(m.ToolCalls) > 0) {
			splitIdx--
			continue
		}
		break
	}
	if splitIdx <= 1 {
		return nil
	}

	// Build summary input (same pattern as maybeSummarize in loop_history.go).
	toSummarize := messages[:splitIdx]
	var sb strings.Builder
	for _, m := range toSummarize {
		switch m.Role {
		case "user":
			fmt.Fprintf(&sb, "user: %s\n", m.Content)
		case "assistant":
			fmt.Fprintf(&sb, "assistant: %s\n", SanitizeAssistantContent(m.Content))
		}
	}

	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := l.provider.Chat(sctx, providers.ChatRequest{
		Messages: []providers.Message{{
			Role:    "user",
			Content: compactionSummaryPrompt + sb.String(),
		}},
		Model:   l.model,
		Options: map[string]any{"max_tokens": 1024, "temperature": 0.3},
	})
	if err != nil {
		slog.Warn("mid_loop_compaction_failed", "agent", l.id, "error", err)
		return nil
	}

	summary := providers.Message{
		Role:    "user",
		Content: "[Summary of earlier conversation]\n" + SanitizeAssistantContent(resp.Content),
	}
	result := make([]providers.Message, 0, 1+keepCount)
	result = append(result, summary)
	result = append(result, messages[splitIdx:]...)

	slog.Info("mid_loop_compacted",
		"agent", l.id,
		"original_msgs", len(messages),
		"summarized", splitIdx,
		"kept", len(result))

	return result
}

// compressionCompactor implements compression.Compactor for L3/L4 when an LLM provider is available.
// Leading system messages are kept verbatim; the rest is summarized with compactionSummaryPrompt.
func (l *Loop) compressionCompactor(ctx context.Context, msgs []providers.Message) ([]providers.Message, error) {
	if l.provider == nil {
		return nil, fmt.Errorf("compression compactor: no provider")
	}
	if len(msgs) == 0 {
		return nil, nil
	}

	sysEnd := 0
	for sysEnd < len(msgs) && msgs[sysEnd].Role == "system" {
		sysEnd++
	}
	leading := msgs[:sysEnd]
	rest := msgs[sysEnd:]
	if len(rest) == 0 {
		out := make([]providers.Message, len(leading))
		copy(out, leading)
		return out, nil
	}

	var sb strings.Builder
	for _, m := range leading {
		content := m.Content
		if len(content) > 2000 {
			content = content[:2000] + "\n...(system truncated)"
		}
		fmt.Fprintf(&sb, "system: %s\n\n", content)
	}
	appendCompactionTranscript(&sb, rest)

	sctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	resp, err := l.provider.Chat(sctx, providers.ChatRequest{
		Messages: []providers.Message{{
			Role:    "user",
			Content: compactionSummaryPrompt + sb.String(),
		}},
		Model:   l.model,
		Options: map[string]any{"max_tokens": 1024, "temperature": 0.3},
	})
	if err != nil {
		slog.Warn("compression_compactor_failed", "agent", l.id, "error", err)
		return nil, err
	}

	summaryText := SanitizeAssistantContent(resp.Content)
	now := time.Now()
	summary := providers.Message{
		Role:    "system",
		Content: "[Summary of earlier conversation]\n" + summaryText,
		CompactBoundary: &providers.CompactMeta{
			Level:         "LLM",
			OriginalCount: len(msgs),
			CompactedAt:   now,
		},
	}

	if len(leading) > 0 {
		out := make([]providers.Message, 0, len(leading)+1)
		out = append(out, leading...)
		out = append(out, summary)
		return out, nil
	}
	return []providers.Message{summary}, nil
}

func appendCompactionTranscript(sb *strings.Builder, msgs []providers.Message) {
	for _, m := range msgs {
		switch m.Role {
		case "user":
			fmt.Fprintf(sb, "user: %s\n", m.Content)
		case "assistant":
			fmt.Fprintf(sb, "assistant: %s\n", SanitizeAssistantContent(m.Content))
			if len(m.ToolCalls) > 0 {
				fmt.Fprintf(sb, "  [tool_calls: %d]\n", len(m.ToolCalls))
			}
		case "tool":
			content := m.Content
			if len(content) > 2000 {
				content = content[:2000] + "\n...(truncated)"
			}
			fmt.Fprintf(sb, "tool result: %s\n", content)
		case "system":
			content := m.Content
			if len(content) > 1500 {
				content = content[:1500] + "\n...(truncated)"
			}
			fmt.Fprintf(sb, "system: %s\n", content)
		default:
			fmt.Fprintf(sb, "%s: %s\n", m.Role, m.Content)
		}
	}
}
