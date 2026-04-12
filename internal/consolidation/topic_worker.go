package consolidation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// TopicMemory represents a long-term memory entry organised by topic.
type TopicMemory struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"user_id"`
	Topic     string    `json:"topic"`
	Summary   string    `json:"summary"`
	Tags      []string  `json:"tags"`
	Sources   []string  `json:"sources"` // episodic memory IDs that contributed
	Score     float64   `json:"score"`   // importance score
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TopicStore persists topic memories.
type TopicStore interface {
	UpsertTopic(ctx context.Context, mem TopicMemory) error
	ListTopics(ctx context.Context, agentID, tenantID, userID string) ([]TopicMemory, error)
	FindByTopic(ctx context.Context, agentID, tenantID, topic string) (*TopicMemory, error)
	DeleteTopic(ctx context.Context, id string) error
}

// TopicExtractor clusters episodic memories into topics using an LLM.
type TopicExtractor interface {
	ExtractTopics(ctx context.Context, summaries []string) ([]ExtractedTopic, error)
}

// ExtractedTopic is a topic identified from episodic summaries.
type ExtractedTopic struct {
	Topic   string   `json:"topic"`
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
	Sources []int    `json:"sources"` // indices into the input summaries
}

// TopicWorkerConfig configures the topic consolidation worker.
type TopicWorkerConfig struct {
	MinEpisodicCount int           // minimum episodic entries before clustering (default 5)
	MergeThreshold   float64       // similarity threshold for merging topics (default 0.8)
	Interval         time.Duration // how often to run (default 1h)
}

// DefaultTopicWorkerConfig returns sensible defaults.
func DefaultTopicWorkerConfig() TopicWorkerConfig {
	return TopicWorkerConfig{
		MinEpisodicCount: 5,
		MergeThreshold:   0.8,
		Interval:         time.Hour,
	}
}

// TopicWorker consolidates episodic memories into topic-organised long-term memories.
type TopicWorker struct {
	store     TopicStore
	extractor TopicExtractor
	config    TopicWorkerConfig
}

// NewTopicWorker creates a topic consolidation worker.
func NewTopicWorker(store TopicStore, extractor TopicExtractor, cfg TopicWorkerConfig) *TopicWorker {
	return &TopicWorker{
		store:     store,
		extractor: extractor,
		config:    cfg,
	}
}

// Process takes a batch of episodic summaries and consolidates them into topics.
func (w *TopicWorker) Process(ctx context.Context, agentID, tenantID, userID string, episodics []EpisodicInput) error {
	if len(episodics) < w.config.MinEpisodicCount {
		slog.Debug("topic worker: not enough episodic entries",
			"count", len(episodics), "min", w.config.MinEpisodicCount)
		return nil
	}

	summaries := make([]string, len(episodics))
	for i, e := range episodics {
		summaries[i] = e.Summary
	}

	topics, err := w.extractor.ExtractTopics(ctx, summaries)
	if err != nil {
		return fmt.Errorf("topic extraction: %w", err)
	}

	for _, t := range topics {
		sources := make([]string, 0, len(t.Sources))
		for _, idx := range t.Sources {
			if idx < len(episodics) {
				sources = append(sources, episodics[idx].ID)
			}
		}

		existing, _ := w.store.FindByTopic(ctx, agentID, tenantID, t.Topic)
		if existing != nil {
			existing.Summary = mergeSummaries(existing.Summary, t.Summary)
			existing.Tags = mergeStringSlices(existing.Tags, t.Tags)
			existing.Sources = mergeStringSlices(existing.Sources, sources)
			existing.UpdatedAt = time.Now()
			if err := w.store.UpsertTopic(ctx, *existing); err != nil {
				slog.Warn("topic worker: upsert failed", "topic", t.Topic, "err", err)
			}
		} else {
			now := time.Now()
			mem := TopicMemory{
				ID:        fmt.Sprintf("topic-%d", now.UnixNano()),
				AgentID:   agentID,
				TenantID:  tenantID,
				UserID:    userID,
				Topic:     t.Topic,
				Summary:   t.Summary,
				Tags:      t.Tags,
				Sources:   sources,
				Score:     1.0,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := w.store.UpsertTopic(ctx, mem); err != nil {
				slog.Warn("topic worker: insert failed", "topic", t.Topic, "err", err)
			}
		}
	}

	slog.Info("topic worker completed",
		"episodics", len(episodics), "topics_found", len(topics))
	return nil
}

// EpisodicInput is a minimal view of an episodic memory for topic extraction.
type EpisodicInput struct {
	ID      string
	Summary string
}

func mergeSummaries(old, new string) string {
	if old == "" {
		return new
	}
	return old + "\n" + new
}

func mergeStringSlices(a, b []string) []string {
	seen := make(map[string]bool, len(a))
	for _, s := range a {
		seen[s] = true
	}
	out := make([]string, len(a))
	copy(out, a)
	for _, s := range b {
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}

// GenerateWikilinks produces wikilink references from topic memories.
func GenerateWikilinks(topics []TopicMemory) string {
	if len(topics) == 0 {
		return ""
	}
	var b strings.Builder
	for _, t := range topics {
		b.WriteString("[[")
		b.WriteString(t.Topic)
		b.WriteString("]]")
		if t.Summary != "" {
			b.WriteString(" — ")
			summary := t.Summary
			if len(summary) > 100 {
				summary = summary[:100] + "..."
			}
			b.WriteString(summary)
		}
		b.WriteString("\n")
	}
	return b.String()
}
