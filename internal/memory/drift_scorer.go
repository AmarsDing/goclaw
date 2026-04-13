package memory

import (
	"context"
	"strings"
	"unicode"
)

// LexicalRelevanceScorer implements drift scoring as word-set overlap (Jaccard-like)
// between a single memory line and the current conversation tail. Returns 0–1.
// Suitable as the scorer passed to DriftDetector.CheckDrift when no embedding model is wired.
func LexicalRelevanceScorer(_ context.Context, memoryLine, conversation string) (float64, error) {
	memoryLine = strings.TrimSpace(memoryLine)
	conversation = strings.TrimSpace(conversation)
	if memoryLine == "" || conversation == "" {
		return 0, nil
	}
	mw := wordSet(memoryLine)
	cw := wordSet(conversation)
	if len(mw) == 0 || len(cw) == 0 {
		return 0, nil
	}
	inter := 0
	for w := range mw {
		if _, ok := cw[w]; ok {
			inter++
		}
	}
	union := len(mw) + len(cw) - inter
	if union == 0 {
		return 0, nil
	}
	return float64(inter) / float64(union), nil
}

func wordSet(s string) map[string]struct{} {
	fields := strings.Fields(strings.ToLower(s))
	out := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		f = trimPunct(f)
		if len(f) < 2 {
			continue
		}
		out[f] = struct{}{}
	}
	return out
}

func trimPunct(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
}
