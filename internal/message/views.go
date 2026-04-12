// Package message provides multi-view message transformations.
//
// Claude Code's key insight: internal transcript, LLM API payload, UI rendering,
// and session resume each need optimised representations. Forcing a single struct
// through all layers causes cross-contamination. This package defines four views
// and stable conversion functions between them.
package message

import (
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// View identifies which representation a message slice has been prepared for.
type View int

const (
	ViewTranscript View = iota // full persistent record, all tool pairs kept
	ViewAPI                     // pruned/compacted for LLM API call
	ViewUI                      // sanitised for frontend rendering
	ViewResume                  // repaired for session continuation
)

func (v View) String() string {
	switch v {
	case ViewTranscript:
		return "transcript"
	case ViewAPI:
		return "api"
	case ViewUI:
		return "ui"
	case ViewResume:
		return "resume"
	default:
		return "unknown"
	}
}

// Envelope wraps a message slice with its current view and metadata.
type Envelope struct {
	Messages []providers.Message
	View     View
	RunID    string
	Metadata EnvelopeMetadata
}

// EnvelopeMetadata carries cross-view bookkeeping.
type EnvelopeMetadata struct {
	CompactionCount int       // how many compactions have been applied
	LastCompactedAt time.Time // timestamp of most recent compaction
	ResumeRepaired  bool      // true if resume repair was applied
	TruncatedTools  int       // number of tool results that were truncated
}

// ToolPair groups a tool_use call with its corresponding tool_result.
type ToolPair struct {
	Call   providers.ToolCall
	Result *providers.Message // nil if orphaned (no result received)
	Index  int                // position in the original message slice
}
