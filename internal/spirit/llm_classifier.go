package spirit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// LLMIntentClassifier uses the configured LLM to classify user intent as JSON.
// On failure or invalid output, Classify returns an error so Router falls back to keyword classification.
type LLMIntentClassifier struct {
	provider providers.Provider
	model    string
}

// NewLLMIntentClassifier builds a classifier. provider must be non-nil.
func NewLLMIntentClassifier(p providers.Provider, model string) *LLMIntentClassifier {
	if p == nil {
		return nil
	}
	return &LLMIntentClassifier{provider: p, model: model}
}

// Classify implements IntentClassifier.
func (c *LLMIntentClassifier) Classify(ctx context.Context, message string, profile *Profile) (*Intent, error) {
	if c == nil || c.provider == nil {
		return nil, errors.New("spirit: no LLM provider for intent classification")
	}
	if strings.TrimSpace(message) == "" {
		return nil, errors.New("spirit: empty message")
	}

	sys := `You classify a single user message for an assistant runtime. Reply with ONLY a JSON object (no markdown), no other text.
Schema:
{
  "type": "<one of: code, research, design, write, manage, chat, unknown>",
  "confidence": <number 0.0-1.0>,
  "description": "<short English phrase>",
  "sub_tasks": [{"id":"t1","description":"...","intent":"<same set as type>","depends_on":[]}]
}
Rules:
- Use at most 3 sub_tasks; omit empty if the message is a single simple request.
- "intent" for each sub_task must be one of the same type names as top-level type.
- depends_on lists other subtask ids that must finish first (use [] if none).`

	user := message
	if profile != nil && profile.UserID != "" {
		user = fmt.Sprintf("(user_id=%s)\n\n%s", profile.UserID, message)
	}

	resp, err := c.provider.Chat(ctx, providers.ChatRequest{
		Model: c.model,
		Messages: []providers.Message{
			{Role: "system", Content: sys},
			{Role: "user", Content: user},
		},
		Options: map[string]any{
			providers.OptMaxTokens:   512,
			providers.OptTemperature: 0.1,
		},
	})
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(resp.Content)
	if raw == "" {
		return nil, errors.New("spirit: empty classifier response")
	}

	payload := extractJSONObject(raw)
	var wire struct {
		Type        string            `json:"type"`
		Confidence  float64           `json:"confidence"`
		Description string            `json:"description"`
		SubTasks    []jsonSubTaskWire `json:"sub_tasks"`
	}
	if err := json.Unmarshal([]byte(payload), &wire); err != nil {
		slog.Debug("spirit llm classifier: json parse failed", "err", err, "raw", truncateForLog(raw, 200))
		return nil, fmt.Errorf("parse classifier json: %w", err)
	}

	intent := &Intent{
		Type:        IntentType(strings.ToLower(strings.TrimSpace(wire.Type))),
		Confidence:  wire.Confidence,
		Description: strings.TrimSpace(wire.Description),
	}
	if intent.Confidence <= 0 || intent.Confidence > 1 {
		intent.Confidence = 0.7
	}
	if intent.Type == "" {
		intent.Type = IntentUnknown
	}
	for _, st := range wire.SubTasks {
		if strings.TrimSpace(st.ID) == "" || strings.TrimSpace(st.Description) == "" {
			continue
		}
		subIntent := IntentType(strings.ToLower(strings.TrimSpace(st.Intent)))
		if subIntent == "" {
			subIntent = intent.Type
		}
		intent.SubTasks = append(intent.SubTasks, SubTask{
			ID:          strings.TrimSpace(st.ID),
			Description: strings.TrimSpace(st.Description),
			Intent:      subIntent,
			Priority:    st.Priority,
			DependsOn:   st.DependsOn,
		})
	}

	slog.Debug("spirit llm classifier", "type", intent.Type, "confidence", intent.Confidence, "subtasks", len(intent.SubTasks))
	return intent, nil
}

type jsonSubTaskWire struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Intent      string   `json:"intent,omitempty"`
	Priority    int      `json:"priority"`
	DependsOn   []string `json:"depends_on"`
}

var jsonFence = regexp.MustCompile(`(?s)` + "```" + `(?:json)?\s*([\s\S]*?)` + "```")

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if m := jsonFence.FindStringSubmatch(s); len(m) > 1 {
		s = strings.TrimSpace(m[1])
	}
	// If extra text, try first { ... } span.
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
