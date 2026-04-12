package message

import (
	"sort"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// NormalizeForAPI converts a transcript-view message slice into API-view,
// suitable for sending to an LLM provider. It:
//   - strips empty messages
//   - removes internal metadata from tool results
//   - enforces tool call / tool result pairing (orphaned calls get synthetic error results)
//   - removes RawAssistantContent to avoid double-sending
func NormalizeForAPI(msgs []providers.Message) []providers.Message {
	out := make([]providers.Message, 0, len(msgs))
	pendingCalls := make(map[string]bool)

	for _, m := range msgs {
		if m.Role == "" {
			continue
		}
		if m.Role == "assistant" && m.Content == "" && len(m.ToolCalls) == 0 && m.Thinking == "" {
			continue
		}

		cp := m
		cp.RawAssistantContent = nil

		if cp.Role == "assistant" && len(cp.ToolCalls) > 0 {
			for _, tc := range cp.ToolCalls {
				pendingCalls[tc.ID] = true
			}
		}
		if cp.Role == "tool" && cp.ToolCallID != "" {
			delete(pendingCalls, cp.ToolCallID)
		}

		out = append(out, cp)
	}

	// Patch orphaned tool calls with synthetic error results.
	if len(pendingCalls) > 0 {
		for id := range pendingCalls {
			now := time.Now()
			out = append(out, providers.Message{
				Role:       "tool",
				Content:    "[interrupted — no result received]",
				ToolCallID: id,
				IsError:    true,
				CreatedAt:  &now,
			})
		}
	}

	return out
}

// NormalizeForUI converts a transcript-view message slice into UI-view,
// suitable for frontend rendering. It:
//   - strips system messages (internal only)
//   - sanitises directive markers from assistant content
//   - preserves ordering but may reorder tool results to follow their calls
//   - strips thinking blocks for non-thinking-enabled UIs
func NormalizeForUI(msgs []providers.Message, showThinking bool) []providers.Message {
	out := make([]providers.Message, 0, len(msgs))
	lookups := BuildMessageLookups(msgs)
	usedToolResults := make(map[int]bool)

	for i, m := range msgs {
		if m.Role == "system" {
			continue
		}
		if m.Role == "tool" {
			if _, ok := lookups.ToolUseIDToCallIdx[m.ToolCallID]; ok {
				if idx, found := lookups.ToolUseIDToResultIdx[m.ToolCallID]; found && idx == i {
					continue
				}
			}
		}

		cp := m
		cp.RawAssistantContent = nil

		if !showThinking {
			cp.Thinking = ""
		}

		if cp.Role == "assistant" {
			cp.Content = stripDirectives(cp.Content)
		}

		out = append(out, cp)
		if cp.Role == "assistant" && len(cp.ToolCalls) > 0 {
			for _, tc := range cp.ToolCalls {
				resultIdx, ok := lookups.ToolUseIDToResultIdx[tc.ID]
				if !ok || usedToolResults[resultIdx] {
					continue
				}
				toolMsg := msgs[resultIdx]
				toolMsg.RawAssistantContent = nil
				if !showThinking {
					toolMsg.Thinking = ""
				}
				out = append(out, toolMsg)
				usedToolResults[resultIdx] = true
			}
		}
	}

	for i, msg := range msgs {
		if msg.Role != "tool" || usedToolResults[i] || msg.ToolCallID == "" {
			continue
		}
		cp := msg
		cp.RawAssistantContent = nil
		out = append(out, cp)
	}
	return out
}

// NormalizeForResume converts a transcript-view message slice into resume-view,
// suitable for continuing an interrupted session. It:
//   - filters corrupt/incomplete messages
//   - repairs orphaned tool_use with synthetic results
//   - removes trailing assistant messages with no content (orphaned thinking)
//   - ensures the last message can be validly continued
func NormalizeForResume(msgs []providers.Message) []providers.Message {
	filtered := filterBadMessages(msgs)
	repaired := repairToolPairing(filtered)
	trimmed := trimOrphanedTrailing(repaired)
	return trimmed
}

// MessageLookups precomputes tool-call pairing information for a transcript.
type MessageLookups struct {
	ToolUseIDToResultIdx map[string]int
	ToolUseIDToCallIdx   map[string]int
	UnresolvedToolUseIDs []string
}

// BuildMessageLookups builds O(1) lookup maps for tool calls and tool results.
func BuildMessageLookups(msgs []providers.Message) MessageLookups {
	lookups := MessageLookups{
		ToolUseIDToResultIdx: make(map[string]int),
		ToolUseIDToCallIdx:   make(map[string]int),
	}
	for i, msg := range msgs {
		if msg.Role == "assistant" {
			for _, tc := range msg.ToolCalls {
				lookups.ToolUseIDToCallIdx[tc.ID] = i
			}
			continue
		}
		if msg.Role == "tool" && msg.ToolCallID != "" {
			lookups.ToolUseIDToResultIdx[msg.ToolCallID] = i
		}
	}
	for toolUseID := range lookups.ToolUseIDToCallIdx {
		if _, ok := lookups.ToolUseIDToResultIdx[toolUseID]; !ok {
			lookups.UnresolvedToolUseIDs = append(lookups.UnresolvedToolUseIDs, toolUseID)
		}
	}
	sort.Strings(lookups.UnresolvedToolUseIDs)
	return lookups
}

// FindToolPairs extracts all tool call/result pairs from a message slice.
func FindToolPairs(msgs []providers.Message) []ToolPair {
	lookups := BuildMessageLookups(msgs)

	var pairs []ToolPair
	for i, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			var result *providers.Message
			if idx, ok := lookups.ToolUseIDToResultIdx[tc.ID]; ok {
				cp := msgs[idx]
				result = &cp
			}
			pairs = append(pairs, ToolPair{
				Call:   tc,
				Result: result,
				Index:  i,
			})
		}
	}
	return pairs
}

// filterBadMessages removes messages with missing role or corrupt structure.
func filterBadMessages(msgs []providers.Message) []providers.Message {
	out := make([]providers.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "" {
			continue
		}
		if m.Role == "tool" && m.ToolCallID == "" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// repairToolPairing ensures every tool_use has a matching tool_result.
func repairToolPairing(msgs []providers.Message) []providers.Message {
	resultIDs := make(map[string]bool)
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID != "" {
			resultIDs[m.ToolCallID] = true
		}
	}

	out := make([]providers.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m)

		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				if !resultIDs[tc.ID] {
					now := time.Now()
					out = append(out, providers.Message{
						Role:       "tool",
						Content:    "[session interrupted — result unavailable]",
						ToolCallID: tc.ID,
						IsError:    true,
						CreatedAt:  &now,
					})
					resultIDs[tc.ID] = true
				}
			}
		}
	}
	return out
}

// trimOrphanedTrailing removes trailing assistant messages with no content.
func trimOrphanedTrailing(msgs []providers.Message) []providers.Message {
	for len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		if last.Role == "assistant" && last.Content == "" && len(last.ToolCalls) == 0 {
			msgs = msgs[:len(msgs)-1]
			continue
		}
		break
	}
	return msgs
}

// stripDirectives removes internal directive markers (e.g. <!-- ... -->) from content.
func stripDirectives(content string) string {
	for {
		start := strings.Index(content, "<!--")
		if start == -1 {
			break
		}
		end := strings.Index(content[start:], "-->")
		if end == -1 {
			break
		}
		content = content[:start] + content[start+end+3:]
	}
	return strings.TrimSpace(content)
}

// CountTruncatedToolResults counts tool messages that were truncated or compacted.
func CountTruncatedToolResults(msgs []providers.Message) int {
	count := 0
	for _, msg := range msgs {
		if msg.Role != "tool" {
			continue
		}
		content := strings.ToLower(msg.Content)
		if strings.Contains(content, "truncated") || strings.Contains(content, "snipped") || strings.Contains(content, "microcompact") {
			count++
		}
	}
	return count
}
