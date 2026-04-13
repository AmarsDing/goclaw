package agent

import "strings"

// HandoffReview is a lightweight safety pass on subagent/delegate output before the parent
// model consumes it (Claude Code handoff classifier parity — heuristic only).
func HandoffReview(text string) (ok bool, reason string) {
	if strings.TrimSpace(text) == "" {
		return true, ""
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "-----begin") && strings.Contains(lower, "private key") {
		return false, "possible private key material in handoff"
	}
	if strings.Contains(lower, ".ssh/id_rsa") || strings.Contains(lower, ".ssh/id_ed25519") {
		return false, "sensitive path referenced in handoff"
	}
	return true, ""
}
