// Package resume provides intelligent session recovery.
//
// Claude Code's resume philosophy: recovery is not replay — it is rebuilding
// a workable state. The engine filters bad messages, repairs tool pairing,
// detects interrupt types, and injects continuation prompts so the agent can
// pick up where it left off.
package resume

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// InterruptType classifies how a session was interrupted.
type InterruptType string

const (
	InterruptNone       InterruptType = "none"        // not interrupted
	InterruptUserCancel InterruptType = "user_cancel" // user sent /stop
	InterruptTimeout    InterruptType = "timeout"     // context deadline exceeded
	InterruptNetworkErr InterruptType = "network"     // provider connection lost
	InterruptPanic      InterruptType = "panic"       // runtime panic recovered
	InterruptOverBudget InterruptType = "over_budget" // token budget exhausted
	InterruptUnknown    InterruptType = "unknown"
)

// ResumeResult contains the repaired session state ready for continuation.
type ResumeResult struct {
	Messages      []providers.Message
	InterruptKind InterruptType
	Repaired      bool     // true if any repairs were applied
	RepairLog     []string // human-readable repair actions taken
	Continuation  string   // injected continuation prompt, if any
	State         *RuntimeStateSnapshot
}

// Engine performs intelligent session recovery.
type Engine struct {
	maxOrphanedCalls int // max orphaned tool calls before truncating history
	worktreeRestorer WorktreeRestorer
}

// RuntimeStateSnapshot captures non-message runtime state reconstructed from a session log.
type RuntimeStateSnapshot struct {
	Mode             string            `json:"mode,omitempty"`
	PendingApprovals []string          `json:"pending_approvals,omitempty"`
	CompactionCount  int               `json:"compaction_count,omitempty"`
	WorktreePath     string            `json:"worktree_path,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// SessionLogEntry is a lightweight restore record that may contain a message,
// an error marker, or a runtime state snapshot.
type SessionLogEntry struct {
	Kind      string                `json:"kind"`
	Message   *providers.Message    `json:"message,omitempty"`
	Error     string                `json:"error,omitempty"`
	State     *RuntimeStateSnapshot `json:"state,omitempty"`
	Timestamp time.Time             `json:"timestamp,omitempty"`
}

// WorktreeRestorer restores a previously active worktree path.
type WorktreeRestorer interface {
	RestoreWorktree(ctx context.Context, path string) error
}

// NewEngine creates a resume engine with default settings.
func NewEngine() *Engine {
	return &Engine{
		maxOrphanedCalls: 5,
	}
}

// SetWorktreeRestorer registers an optional worktree restore implementation.
func (e *Engine) SetWorktreeRestorer(restorer WorktreeRestorer) {
	e.worktreeRestorer = restorer
}

// Resume processes a persisted message history and returns a repaired, resumable state.
func (e *Engine) Resume(ctx context.Context, msgs []providers.Message, lastErr error) (*ResumeResult, error) {
	result := &ResumeResult{
		InterruptKind: detectInterruptType(lastErr),
	}

	slog.Debug("resume engine starting",
		"messages", len(msgs), "interrupt", result.InterruptKind)

	// Step 1: Filter bad messages.
	filtered, filterLog := filterBadMessages(msgs)
	result.RepairLog = append(result.RepairLog, filterLog...)
	if len(filterLog) > 0 {
		result.Repaired = true
	}

	// Step 2: Repair tool pairing.
	trimmedForBudget, budgetLog := e.trimForOrphanBudget(filtered)
	result.RepairLog = append(result.RepairLog, budgetLog...)
	if len(budgetLog) > 0 {
		result.Repaired = true
	}

	repaired, repairLog := repairToolPairing(trimmedForBudget)
	result.RepairLog = append(result.RepairLog, repairLog...)
	if len(repairLog) > 0 {
		result.Repaired = true
	}

	// Step 3: Trim orphaned trailing content.
	trimmed, trimLog := trimOrphanedTrailing(repaired)
	result.RepairLog = append(result.RepairLog, trimLog...)
	if len(trimLog) > 0 {
		result.Repaired = true
	}

	// Step 4: Inject continuation message based on interrupt type.
	continued, contMsg := e.injectContinuation(trimmed, result.InterruptKind)
	result.Continuation = contMsg

	result.Messages = continued

	slog.Debug("resume engine complete",
		"output_messages", len(result.Messages),
		"repaired", result.Repaired,
		"repairs", len(result.RepairLog))

	return result, nil
}

// ResumeFromLog deserializes a session log and restores both messages and runtime state.
func (e *Engine) ResumeFromLog(ctx context.Context, entries []SessionLogEntry) (*ResumeResult, error) {
	msgs, lastErr, state := e.deserializeLog(entries)
	result, err := e.Resume(ctx, msgs, lastErr)
	if err != nil {
		return nil, err
	}
	if err := e.RestoreSessionState(ctx, state); err != nil {
		return nil, err
	}
	result.State = state
	return result, nil
}

// RestoreSessionState restores non-message runtime state such as worktree and metadata.
func (e *Engine) RestoreSessionState(ctx context.Context, snapshot *RuntimeStateSnapshot) error {
	if snapshot == nil {
		return nil
	}
	if snapshot.WorktreePath != "" && e.worktreeRestorer != nil {
		if err := e.worktreeRestorer.RestoreWorktree(ctx, snapshot.WorktreePath); err != nil {
			return fmt.Errorf("restore worktree: %w", err)
		}
	}
	return nil
}

// detectInterruptType classifies the error that ended the previous session.
func detectInterruptType(err error) InterruptType {
	if err == nil {
		return InterruptNone
	}

	msg := strings.ToLower(err.Error())
	switch {
	case contains(msg, "context canceled", "context deadline exceeded"):
		return InterruptTimeout
	case contains(msg, "user cancel", "stop", "abort"):
		return InterruptUserCancel
	case contains(msg, "connection", "EOF", "network", "dial"):
		return InterruptNetworkErr
	case contains(msg, "panic", "runtime error"):
		return InterruptPanic
	case contains(msg, "budget", "token limit", "over budget"):
		return InterruptOverBudget
	default:
		return InterruptUnknown
	}
}

func contains(s string, substrs ...string) bool {
	s = strings.ToLower(s)
	for _, sub := range substrs {
		sub = strings.ToLower(sub)
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

func (e *Engine) deserializeLog(entries []SessionLogEntry) ([]providers.Message, error, *RuntimeStateSnapshot) {
	var msgs []providers.Message
	var lastErr error
	var state *RuntimeStateSnapshot
	for _, entry := range entries {
		switch entry.Kind {
		case "message":
			if entry.Message != nil {
				msgs = append(msgs, *entry.Message)
			}
		case "error":
			if entry.Error != "" {
				lastErr = errors.New(entry.Error)
			}
		case "state":
			if entry.State != nil {
				state = entry.State
			}
		}
	}
	return msgs, lastErr, state
}

func (e *Engine) trimForOrphanBudget(msgs []providers.Message) ([]providers.Message, []string) {
	if e.maxOrphanedCalls <= 0 {
		return msgs, nil
	}
	resultIDs := make(map[string]bool)
	for _, msg := range msgs {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			resultIDs[msg.ToolCallID] = true
		}
	}
	var orphanAssistantIdx []int
	for i, msg := range msgs {
		if msg.Role != "assistant" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if !resultIDs[tc.ID] {
				orphanAssistantIdx = append(orphanAssistantIdx, i)
				break
			}
		}
	}
	if len(orphanAssistantIdx) <= e.maxOrphanedCalls {
		return msgs, nil
	}
	start := orphanAssistantIdx[len(orphanAssistantIdx)-e.maxOrphanedCalls]
	if start <= 0 {
		return msgs, nil
	}
	return msgs[start:], []string{
		fmt.Sprintf("trimmed history before message[%d] to enforce orphan budget %d", start, e.maxOrphanedCalls),
	}
}

// filterBadMessages removes messages with missing role or corrupt structure.
func filterBadMessages(msgs []providers.Message) ([]providers.Message, []string) {
	var out []providers.Message
	var log []string

	for i, m := range msgs {
		if m.Role == "" {
			log = append(log, fmt.Sprintf("removed message[%d]: empty role", i))
			continue
		}
		if m.Role == "tool" && m.ToolCallID == "" {
			log = append(log, fmt.Sprintf("removed message[%d]: tool message without call ID", i))
			continue
		}
		out = append(out, m)
	}
	return out, log
}

// repairToolPairing ensures every tool_use has a matching tool_result.
func repairToolPairing(msgs []providers.Message) ([]providers.Message, []string) {
	resultIDs := make(map[string]bool)
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID != "" {
			resultIDs[m.ToolCallID] = true
		}
	}

	var out []providers.Message
	var log []string

	for _, m := range msgs {
		out = append(out, m)

		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				if !resultIDs[tc.ID] {
					now := time.Now()
					out = append(out, providers.Message{
						Role:       "tool",
						Content:    "[session interrupted — tool result unavailable, please retry if needed]",
						ToolCallID: tc.ID,
						IsError:    true,
						CreatedAt:  &now,
					})
					resultIDs[tc.ID] = true
					log = append(log, fmt.Sprintf("repaired orphaned tool_use %s (%s)", tc.ID, tc.Name))
				}
			}
		}
	}
	return out, log
}

// trimOrphanedTrailing removes trailing assistant messages with no content.
func trimOrphanedTrailing(msgs []providers.Message) ([]providers.Message, []string) {
	var log []string
	for len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		if last.Role == "assistant" && last.Content == "" && len(last.ToolCalls) == 0 && last.Thinking == "" {
			log = append(log, "trimmed orphaned trailing assistant message")
			msgs = msgs[:len(msgs)-1]
			continue
		}
		break
	}
	return msgs, log
}

// injectContinuation appends a system message guiding the agent to resume work.
func (e *Engine) injectContinuation(msgs []providers.Message, interrupt InterruptType) ([]providers.Message, string) {
	if interrupt == InterruptNone {
		return msgs, ""
	}

	var prompt string
	switch interrupt {
	case InterruptUserCancel:
		prompt = "The previous session was cancelled by the user. Review what was accomplished and ask if the user wants to continue or start fresh."
	case InterruptTimeout:
		prompt = "The previous session timed out. Review the progress made and continue from where it stopped. Prioritise completing any in-progress tool operations."
	case InterruptNetworkErr:
		prompt = "The previous session was interrupted by a network error. Verify the current state of any files or operations that were in progress, then continue."
	case InterruptPanic:
		prompt = "The previous session ended unexpectedly due to an internal error. Check the workspace state carefully before continuing."
	case InterruptOverBudget:
		prompt = "The previous session exceeded its token budget. Summarise what was accomplished and continue with the remaining work efficiently."
	default:
		prompt = "The previous session was interrupted. Review the current state and continue from where it left off."
	}

	now := time.Now()
	msgs = append(msgs, providers.Message{
		Role:      "system",
		Content:   prompt,
		CreatedAt: &now,
	})

	return msgs, prompt
}
