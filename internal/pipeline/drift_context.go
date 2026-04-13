package pipeline

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// DefaultDriftTailMaxRunes caps text passed to DriftDetector.CheckDrift.
const DefaultDriftTailMaxRunes = 1200

// ConversationTailForDrift builds a recent conversation string for drift scoring.
// It uses All() but skips the system message so injected memory does not dominate the score.
func ConversationTailForDrift(state *RunState, maxRunes int) string {
	if state == nil || state.Messages == nil {
		return ""
	}
	if maxRunes <= 0 {
		maxRunes = DefaultDriftTailMaxRunes
	}
	var b strings.Builder
	for _, m := range state.Messages.All() {
		if m.Role == "system" {
			continue
		}
		line := strings.TrimSpace(m.Content)
		if line == "" {
			continue
		}
		switch m.Role {
		case "user", "assistant", "tool":
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(line)
		}
	}
	s := b.String()
	runes := []rune(s)
	if len(runes) > maxRunes {
		s = string(runes[len(runes)-maxRunes:])
	}
	return s
}

// RecentContextForRecall concatenates the trailing user turns from history into a short
// snippet for episodic recall (same rules as ContextStage auto-inject).
func RecentContextForRecall(history []providers.Message) string {
	const maxTurns = 2
	const maxRunes = 300
	if len(history) == 0 {
		return ""
	}
	turns := make([]string, 0, maxTurns)
	for i := len(history) - 1; i >= 0 && len(turns) < maxTurns; i-- {
		m := history[i]
		if m.Role != "user" || m.Content == "" {
			continue
		}
		turns = append([]string{m.Content}, turns...)
	}
	if len(turns) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, t := range turns {
		if i > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString(t)
	}
	joined := sb.String()
	runes := []rune(joined)
	if len(runes) > maxRunes {
		joined = string(runes[len(runes)-maxRunes:])
	}
	return joined
}
