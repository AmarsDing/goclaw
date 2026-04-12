package spirit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/consolidation"
)

// Feedback captures user feedback on a spirit interaction.
type Feedback struct {
	RunID      string     `json:"run_id"`
	UserID     string     `json:"user_id"`
	TenantID   string     `json:"tenant_id"`
	Rating     int        `json:"rating"`               // 1–5, 0 = no rating
	Correction string     `json:"correction,omitempty"` // what should have happened
	AgentID    string     `json:"agent_id"`
	IntentType IntentType `json:"intent_type"`
	Success    bool       `json:"success"`
	Timestamp  time.Time  `json:"timestamp"`
}

// LearningLoop processes feedback to improve the spirit's future performance.
type LearningLoop struct {
	profileMgr *ProfileManager
	topicStore consolidation.TopicStore
	agentID    string     // agent UUID string for dreamweaver_topics rows
	history    []Feedback // bounded buffer of recent feedback
	maxHistory int
}

// NewLearningLoop creates a learning loop.
func NewLearningLoop(profileMgr *ProfileManager) *LearningLoop {
	return &LearningLoop{
		profileMgr: profileMgr,
		maxHistory: 1000,
	}
}

// BindTopicStore optionally persists correction-derived topics to consolidation TopicStore (e.g. PG).
// agentID must be the agent UUID string used elsewhere for this Loop.
func (l *LearningLoop) BindTopicStore(store consolidation.TopicStore, agentID string) {
	if l == nil {
		return
	}
	l.topicStore = store
	l.agentID = agentID
}

// RecordFeedback processes a piece of user feedback.
func (l *LearningLoop) RecordFeedback(ctx context.Context, fb Feedback) error {
	fb.Timestamp = time.Now()

	// Update agent affinity in profile.
	if err := l.profileMgr.RecordInteraction(ctx, fb.UserID, fb.TenantID, fb.AgentID, fb.Success); err != nil {
		slog.Warn("learning loop: record interaction failed", "err", err)
	}

	// Update profile preferences based on patterns.
	if fb.Correction != "" {
		if err := l.applyCorrection(ctx, fb); err != nil {
			slog.Warn("learning loop: apply correction failed", "err", err)
		}
		if err := l.syncTopicFromCorrection(ctx, fb); err != nil {
			slog.Warn("learning loop: topic sync failed", "err", err)
		}
	}

	// Append to history.
	l.history = append(l.history, fb)
	if len(l.history) > l.maxHistory {
		l.history = l.history[len(l.history)-l.maxHistory:]
	}

	slog.Debug("learning loop: feedback recorded",
		"user", fb.UserID, "agent", fb.AgentID,
		"success", fb.Success, "rating", fb.Rating)

	return nil
}

// applyCorrection adjusts the profile based on explicit user correction.
func (l *LearningLoop) applyCorrection(ctx context.Context, fb Feedback) error {
	profile, err := l.profileMgr.Get(ctx, fb.UserID, fb.TenantID)
	if err != nil {
		return err
	}

	// If the user corrected the agent choice, lower affinity for the wrong agent
	// and boost the correct one (if mentioned).
	if !fb.Success && fb.AgentID != "" {
		if profile.AgentAffinity == nil {
			profile.AgentAffinity = make(map[string]float64)
		}
		profile.AgentAffinity[fb.AgentID] *= 0.8
	}

	// Track recent topics from corrections.
	if fb.Correction != "" {
		profile.WorkContext.RecentTopics = appendUnique(
			profile.WorkContext.RecentTopics, fb.Correction, 10)
	}

	return l.profileMgr.Save(ctx, profile)
}

func (l *LearningLoop) syncTopicFromCorrection(ctx context.Context, fb Feedback) error {
	if l == nil || l.topicStore == nil || fb.TenantID == "" || fb.UserID == "" {
		return nil
	}
	agentID := l.agentID
	if fb.AgentID != "" {
		agentID = fb.AgentID
	}
	if agentID == "" {
		return nil
	}
	topicTitle := fb.Correction
	if len(topicTitle) > 200 {
		topicTitle = topicTitle[:200]
	}
	now := time.Now()
	mem := consolidation.TopicMemory{
		ID:        fmt.Sprintf("spirit-%d", now.UnixNano()),
		AgentID:   agentID,
		TenantID:  fb.TenantID,
		UserID:    fb.UserID,
		Topic:     topicTitle,
		Summary:   fb.Correction,
		Tags:      []string{"spirit", "correction"},
		Score:     0.5,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return l.topicStore.UpsertTopic(ctx, mem)
}

// SuccessRate returns the overall success rate for a user.
func (l *LearningLoop) SuccessRate(userID string) float64 {
	total := 0
	success := 0
	for _, fb := range l.history {
		if fb.UserID == userID {
			total++
			if fb.Success {
				success++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(success) / float64(total)
}

// AgentSuccessRate returns the success rate for a specific agent.
func (l *LearningLoop) AgentSuccessRate(agentID string) float64 {
	total := 0
	success := 0
	for _, fb := range l.history {
		if fb.AgentID == agentID {
			total++
			if fb.Success {
				success++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(success) / float64(total)
}

func appendUnique(slice []string, item string, maxLen int) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	slice = append(slice, item)
	if len(slice) > maxLen {
		slice = slice[len(slice)-maxLen:]
	}
	return slice
}
