package spirit

import (
	"context"
	"log/slog"
	"strings"
)

// IntentType classifies user intent.
type IntentType string

const (
	IntentCode     IntentType = "code"     // coding, debugging, refactoring
	IntentResearch IntentType = "research" // information gathering, analysis
	IntentDesign   IntentType = "design"   // architecture, planning, design
	IntentWrite    IntentType = "write"    // documentation, content creation
	IntentManage   IntentType = "manage"   // project management, task tracking
	IntentChat     IntentType = "chat"     // general conversation
	IntentUnknown  IntentType = "unknown"
)

// Intent is the parsed understanding of a user's command.
type Intent struct {
	Type        IntentType `json:"type"`
	Confidence  float64    `json:"confidence"` // 0.0–1.0
	Description string     `json:"description"`
	SubTasks    []SubTask  `json:"sub_tasks,omitempty"`
	SuggestedAgents []string `json:"suggested_agents,omitempty"`
}

// SubTask is a decomposed piece of work.
type SubTask struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Intent      IntentType `json:"intent"`
	AgentID     string     `json:"agent_id,omitempty"` // resolved agent
	Priority    int        `json:"priority"`
	DependsOn   []string   `json:"depends_on,omitempty"` // other subtask IDs
}

// AgentInfo describes an available agent for routing.
type AgentInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Capabilities []string `json:"capabilities"`
	Available   bool     `json:"available"`
}

// IntentClassifier analyses user messages to determine intent.
type IntentClassifier interface {
	Classify(ctx context.Context, message string, profile *Profile) (*Intent, error)
}

// Router maps user intents to appropriate agents using the spirit's profile.
type Router struct {
	classifier IntentClassifier
	agents     []AgentInfo
}

// NewRouter creates a spirit router.
func NewRouter(classifier IntentClassifier) *Router {
	return &Router{
		classifier: classifier,
	}
}

// SetAgents updates the available agent list.
func (r *Router) SetAgents(agents []AgentInfo) {
	r.agents = agents
}

// Route analyses the user's message and returns an intent with agent assignments.
func (r *Router) Route(ctx context.Context, message string, profile *Profile) (*Intent, error) {
	intent, err := r.classifier.Classify(ctx, message, profile)
	if err != nil {
		// Fall back to keyword-based classification.
		intent = r.keywordClassify(message)
	}

	// Assign agents to sub-tasks based on intent and profile affinity.
	r.assignAgents(intent, profile)

	slog.Debug("spirit router",
		"intent", intent.Type, "confidence", intent.Confidence,
		"subtasks", len(intent.SubTasks),
		"agents", intent.SuggestedAgents)

	return intent, nil
}

func (r *Router) keywordClassify(message string) *Intent {
	lower := strings.ToLower(message)

	intent := &Intent{
		Confidence:  0.6,
		Description: message,
	}

	codeKeywords := []string{"code", "implement", "fix", "bug", "debug", "refactor", "function", "class", "error", "compile"}
	researchKeywords := []string{"find", "search", "research", "compare", "analyze", "what is", "how does"}
	designKeywords := []string{"design", "architect", "plan", "structure", "organize", "diagram"}
	writeKeywords := []string{"write", "document", "explain", "describe", "readme", "blog", "article"}
	manageKeywords := []string{"task", "project", "schedule", "assign", "track", "status", "progress"}

	intent.Type = IntentChat
	maxScore := 0

	for _, kw := range codeKeywords {
		if strings.Contains(lower, kw) {
			if score := len(kw); score > maxScore {
				maxScore = score
				intent.Type = IntentCode
			}
		}
	}
	for _, kw := range researchKeywords {
		if strings.Contains(lower, kw) {
			if score := len(kw); score > maxScore {
				maxScore = score
				intent.Type = IntentResearch
			}
		}
	}
	for _, kw := range designKeywords {
		if strings.Contains(lower, kw) {
			if score := len(kw); score > maxScore {
				maxScore = score
				intent.Type = IntentDesign
			}
		}
	}
	for _, kw := range writeKeywords {
		if strings.Contains(lower, kw) {
			if score := len(kw); score > maxScore {
				maxScore = score
				intent.Type = IntentWrite
			}
		}
	}
	for _, kw := range manageKeywords {
		if strings.Contains(lower, kw) {
			if score := len(kw); score > maxScore {
				maxScore = score
				intent.Type = IntentManage
			}
		}
	}

	intent.SubTasks = []SubTask{{
		ID:          "main",
		Description: message,
		Intent:      intent.Type,
		Priority:    1,
	}}

	return intent
}

func (r *Router) assignAgents(intent *Intent, profile *Profile) {
	if len(r.agents) == 0 {
		return
	}

	for i := range intent.SubTasks {
		bestAgent := r.findBestAgent(intent.SubTasks[i].Intent, profile)
		if bestAgent != "" {
			intent.SubTasks[i].AgentID = bestAgent
			if !contains(intent.SuggestedAgents, bestAgent) {
				intent.SuggestedAgents = append(intent.SuggestedAgents, bestAgent)
			}
		}
	}
}

func (r *Router) findBestAgent(intentType IntentType, profile *Profile) string {
	var bestID string
	bestScore := -1.0

	for _, agent := range r.agents {
		if !agent.Available {
			continue
		}

		score := 0.0

		// Check capability match.
		for _, cap := range agent.Capabilities {
			if strings.EqualFold(cap, string(intentType)) {
				score += 1.0
			}
		}

		// Factor in profile affinity.
		if affinity, ok := profile.AgentAffinity[agent.ID]; ok {
			score += affinity * 0.5
		}

		if score > bestScore {
			bestScore = score
			bestID = agent.ID
		}
	}

	// Fallback to first available agent.
	if bestID == "" && len(r.agents) > 0 {
		for _, agent := range r.agents {
			if agent.Available {
				return agent.ID
			}
		}
	}

	return bestID
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
