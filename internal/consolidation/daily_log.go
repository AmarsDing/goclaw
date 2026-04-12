package consolidation

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// DailyLog is a cross-session work summary for a single day.
type DailyLog struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"user_id"`
	Date      string    `json:"date"` // YYYY-MM-DD
	Summary   string    `json:"summary"`
	Sessions  []string  `json:"sessions"`  // session keys included
	ToolCalls int       `json:"tool_calls"`
	Tokens    int       `json:"tokens"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DailyLogStore persists daily log entries.
type DailyLogStore interface {
	UpsertDailyLog(ctx context.Context, log DailyLog) error
	GetDailyLog(ctx context.Context, agentID, tenantID, date string) (*DailyLog, error)
	ListDailyLogs(ctx context.Context, agentID, tenantID string, limit int) ([]DailyLog, error)
}

// DailySummariser generates a natural-language summary from session data.
type DailySummariser interface {
	Summarise(ctx context.Context, sessions []SessionSummaryInput) (string, error)
}

// SessionSummaryInput captures a session's key data for daily summarisation.
type SessionSummaryInput struct {
	SessionKey string
	Summary    string
	ToolCalls  int
	Tokens     int
	StartedAt  time.Time
}

// DailyLogWorkerConfig configures the daily log worker.
type DailyLogWorkerConfig struct {
	Timezone     string        // IANA timezone for day boundary (default "UTC")
	RunAt        string        // time of day to run (default "23:55")
	MaxSessions  int           // max sessions to include (default 50)
}

// DefaultDailyLogWorkerConfig returns sensible defaults.
func DefaultDailyLogWorkerConfig() DailyLogWorkerConfig {
	return DailyLogWorkerConfig{
		Timezone:    "UTC",
		RunAt:       "23:55",
		MaxSessions: 50,
	}
}

// DailyLogWorker aggregates the day's sessions into a daily summary.
type DailyLogWorker struct {
	store      DailyLogStore
	summariser DailySummariser
	config     DailyLogWorkerConfig
}

// NewDailyLogWorker creates a daily log worker.
func NewDailyLogWorker(store DailyLogStore, summariser DailySummariser, cfg DailyLogWorkerConfig) *DailyLogWorker {
	return &DailyLogWorker{
		store:      store,
		summariser: summariser,
		config:     cfg,
	}
}

// Process aggregates sessions for a date into a daily log entry.
func (w *DailyLogWorker) Process(ctx context.Context, agentID, tenantID, userID, date string, sessions []SessionSummaryInput) error {
	if len(sessions) == 0 {
		return nil
	}

	if len(sessions) > w.config.MaxSessions {
		sessions = sessions[:w.config.MaxSessions]
	}

	summary, err := w.summariser.Summarise(ctx, sessions)
	if err != nil {
		return fmt.Errorf("daily summarise: %w", err)
	}

	totalTools := 0
	totalTokens := 0
	sessionKeys := make([]string, len(sessions))
	for i, s := range sessions {
		sessionKeys[i] = s.SessionKey
		totalTools += s.ToolCalls
		totalTokens += s.Tokens
	}

	now := time.Now()
	log := DailyLog{
		ID:        fmt.Sprintf("daily-%s-%s-%d", agentID, date, now.UnixNano()),
		AgentID:   agentID,
		TenantID:  tenantID,
		UserID:    userID,
		Date:      date,
		Summary:   summary,
		Sessions:  sessionKeys,
		ToolCalls: totalTools,
		Tokens:    totalTokens,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := w.store.UpsertDailyLog(ctx, log); err != nil {
		return fmt.Errorf("upsert daily log: %w", err)
	}

	slog.Info("daily log completed",
		"date", date, "sessions", len(sessions),
		"tools", totalTools, "tokens", totalTokens)
	return nil
}
