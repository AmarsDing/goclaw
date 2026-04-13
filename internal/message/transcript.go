package message

import (
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Transcript is the authoritative record of a session's message history.
// It holds the full, unmodified message sequence and supports deriving
// any view on demand.
type Transcript struct {
	SessionKey  string
	Messages    []providers.Message
	Summary     string // compaction summary, if any
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Compactions int // number of compaction cycles applied
}

// NewTranscript creates a transcript for a session.
func NewTranscript(sessionKey string) *Transcript {
	now := time.Now()
	return &Transcript{
		SessionKey: sessionKey,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// Append adds messages to the transcript.
func (t *Transcript) Append(msgs ...providers.Message) {
	t.Messages = append(t.Messages, msgs...)
	t.UpdatedAt = time.Now()
}

// Replace replaces the entire message list (e.g. after compaction).
func (t *Transcript) Replace(msgs []providers.Message, summary string) {
	t.Messages = msgs
	t.Summary = summary
	t.Compactions++
	t.UpdatedAt = time.Now()
}

// ToEnvelope wraps the transcript messages in a ViewTranscript envelope.
//
// Deprecated: prefer ForAPI, ForUI, or ForResume which apply the appropriate
// normalization for each view. ToEnvelope returns raw messages without
// normalization and has no internal callers; it is retained for API
// compatibility and may be removed in a future release.
func (t *Transcript) ToEnvelope(runID string) Envelope {
	return Envelope{
		Messages: t.Messages,
		View:     ViewTranscript,
		RunID:    runID,
		Metadata: EnvelopeMetadata{
			CompactionCount: t.Compactions,
			TruncatedTools:  CountTruncatedToolResults(t.Messages),
		},
	}
}

// ForAPI derives an API-view envelope from this transcript.
func (t *Transcript) ForAPI(runID string) Envelope {
	return Envelope{
		Messages: NormalizeForAPI(t.Messages),
		View:     ViewAPI,
		RunID:    runID,
		Metadata: EnvelopeMetadata{
			CompactionCount: t.Compactions,
			TruncatedTools:  CountTruncatedToolResults(t.Messages),
		},
	}
}

// ForUI derives a UI-view envelope from this transcript.
func (t *Transcript) ForUI(runID string, showThinking bool) Envelope {
	return Envelope{
		Messages: NormalizeForUI(t.Messages, showThinking),
		View:     ViewUI,
		RunID:    runID,
		Metadata: EnvelopeMetadata{
			CompactionCount: t.Compactions,
			TruncatedTools:  CountTruncatedToolResults(t.Messages),
		},
	}
}

// ForResume derives a resume-view envelope from this transcript.
func (t *Transcript) ForResume(runID string) Envelope {
	msgs := NormalizeForResume(t.Messages)
	return Envelope{
		Messages: msgs,
		View:     ViewResume,
		RunID:    runID,
		Metadata: EnvelopeMetadata{
			ResumeRepaired:  true,
			CompactionCount: t.Compactions,
			TruncatedTools:  CountTruncatedToolResults(msgs),
		},
	}
}

// MessageCount returns the number of messages in the transcript.
func (t *Transcript) MessageCount() int {
	return len(t.Messages)
}

// ToolCallCount returns the total number of tool calls across all messages.
func (t *Transcript) ToolCallCount() int {
	count := 0
	for _, m := range t.Messages {
		count += len(m.ToolCalls)
	}
	return count
}
