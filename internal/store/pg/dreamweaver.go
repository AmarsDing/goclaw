package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/consolidation"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/spirit"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type PGHookConfigStore struct{ db *sql.DB }

func NewPGHookConfigStore(db *sql.DB) *PGHookConfigStore { return &PGHookConfigStore{db: db} }

func (s *PGHookConfigStore) ListHooks(ctx context.Context, tenantID, agentID uuid.UUID) ([]hooks.Hook, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT hook_id, event, mode, priority, matcher, handler, enabled
		FROM dreamweaver_hook_configs
		WHERE tenant_id = $1 AND agent_id = $2
		ORDER BY priority DESC, hook_id ASC
	`, tenantID, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []hooks.Hook
	for rows.Next() {
		var hook hooks.Hook
		var matcherJSON, handlerJSON []byte
		if err := rows.Scan(&hook.ID, &hook.Event, &hook.Mode, &hook.Priority, &matcherJSON, &handlerJSON, &hook.Enabled); err != nil {
			return nil, err
		}
		hook.AgentID = "*"
		if err := json.Unmarshal(matcherJSON, &hook.Matcher); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(handlerJSON, &hook.Handler); err != nil {
			return nil, err
		}
		out = append(out, hook)
	}
	return out, rows.Err()
}

func (s *PGHookConfigStore) UpsertHook(ctx context.Context, tenantID, agentID uuid.UUID, hook hooks.Hook) error {
	matcherJSON, _ := json.Marshal(hook.Matcher)
	handlerJSON, _ := json.Marshal(hook.Handler)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dreamweaver_hook_configs (tenant_id, agent_id, hook_id, event, mode, priority, enabled, matcher, handler, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
		ON CONFLICT (tenant_id, agent_id, hook_id) DO UPDATE
		SET event = EXCLUDED.event,
		    mode = EXCLUDED.mode,
		    priority = EXCLUDED.priority,
		    enabled = EXCLUDED.enabled,
		    matcher = EXCLUDED.matcher,
		    handler = EXCLUDED.handler,
		    updated_at = NOW()
	`, tenantID, agentID, hook.ID, hook.Event, hook.Mode, hook.Priority, hook.Enabled, matcherJSON, handlerJSON)
	return err
}

func (s *PGHookConfigStore) DeleteHook(ctx context.Context, tenantID, agentID uuid.UUID, hookID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM dreamweaver_hook_configs
		WHERE tenant_id = $1 AND agent_id = $2 AND hook_id = $3
	`, tenantID, agentID, hookID)
	return err
}

type PGTopicStore struct{ db *sql.DB }

func NewPGTopicStore(db *sql.DB) *PGTopicStore { return &PGTopicStore{db: db} }

func (s *PGTopicStore) UpsertTopic(ctx context.Context, mem consolidation.TopicMemory) error {
	tagsJSON, _ := json.Marshal(mem.Tags)
	sourcesJSON, _ := json.Marshal(mem.Sources)
	tenantID, err := uuid.Parse(mem.TenantID)
	if err != nil {
		return fmt.Errorf("parse tenant id: %w", err)
	}
	agentID, err := uuid.Parse(mem.AgentID)
	if err != nil {
		return fmt.Errorf("parse agent id: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO dreamweaver_topics (id, tenant_id, agent_id, user_id, topic, summary, tags, sources, score, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (tenant_id, agent_id, user_id, topic) DO UPDATE
		SET summary = EXCLUDED.summary,
		    tags = EXCLUDED.tags,
		    sources = EXCLUDED.sources,
		    score = EXCLUDED.score,
		    updated_at = EXCLUDED.updated_at
	`, mem.ID, tenantID, agentID, mem.UserID, mem.Topic, mem.Summary, tagsJSON, sourcesJSON, mem.Score, mem.CreatedAt, mem.UpdatedAt)
	return err
}

func (s *PGTopicStore) ListTopics(ctx context.Context, agentID, tenantID, userID string) ([]consolidation.TopicMemory, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id::text, tenant_id::text, user_id, topic, summary, tags, sources, score, created_at, updated_at
		FROM dreamweaver_topics
		WHERE tenant_id = $1::uuid AND agent_id = $2::uuid AND user_id = $3
		ORDER BY updated_at DESC
	`, tenantID, agentID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []consolidation.TopicMemory
	for rows.Next() {
		var mem consolidation.TopicMemory
		var tagsJSON, sourcesJSON []byte
		if err := rows.Scan(&mem.ID, &mem.AgentID, &mem.TenantID, &mem.UserID, &mem.Topic, &mem.Summary, &tagsJSON, &sourcesJSON, &mem.Score, &mem.CreatedAt, &mem.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(tagsJSON, &mem.Tags)
		_ = json.Unmarshal(sourcesJSON, &mem.Sources)
		out = append(out, mem)
	}
	return out, rows.Err()
}

func (s *PGTopicStore) FindByTopic(ctx context.Context, agentID, tenantID, topic string) (*consolidation.TopicMemory, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, agent_id::text, tenant_id::text, user_id, topic, summary, tags, sources, score, created_at, updated_at
		FROM dreamweaver_topics
		WHERE tenant_id = $1::uuid AND agent_id = $2::uuid AND topic = $3
		LIMIT 1
	`, tenantID, agentID, topic)
	var mem consolidation.TopicMemory
	var tagsJSON, sourcesJSON []byte
	if err := row.Scan(&mem.ID, &mem.AgentID, &mem.TenantID, &mem.UserID, &mem.Topic, &mem.Summary, &tagsJSON, &sourcesJSON, &mem.Score, &mem.CreatedAt, &mem.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(tagsJSON, &mem.Tags)
	_ = json.Unmarshal(sourcesJSON, &mem.Sources)
	return &mem, nil
}

func (s *PGTopicStore) DeleteTopic(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dreamweaver_topics WHERE id = $1`, id)
	return err
}

type PGDailyLogStore struct{ db *sql.DB }

func NewPGDailyLogStore(db *sql.DB) *PGDailyLogStore { return &PGDailyLogStore{db: db} }

func (s *PGDailyLogStore) UpsertDailyLog(ctx context.Context, log consolidation.DailyLog) error {
	sessionsJSON, _ := json.Marshal(log.Sessions)
	tenantID, err := uuid.Parse(log.TenantID)
	if err != nil {
		return fmt.Errorf("parse tenant id: %w", err)
	}
	agentID, err := uuid.Parse(log.AgentID)
	if err != nil {
		return fmt.Errorf("parse agent id: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO dreamweaver_daily_logs (id, tenant_id, agent_id, user_id, log_date, summary, sessions, tool_calls, tokens, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (tenant_id, agent_id, user_id, log_date) DO UPDATE
		SET summary = EXCLUDED.summary,
		    sessions = EXCLUDED.sessions,
		    tool_calls = EXCLUDED.tool_calls,
		    tokens = EXCLUDED.tokens,
		    updated_at = EXCLUDED.updated_at
	`, log.ID, tenantID, agentID, log.UserID, log.Date, log.Summary, sessionsJSON, log.ToolCalls, log.Tokens, log.CreatedAt, log.UpdatedAt)
	return err
}

func (s *PGDailyLogStore) GetDailyLog(ctx context.Context, agentID, tenantID, date string) (*consolidation.DailyLog, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, agent_id::text, tenant_id::text, user_id, log_date::text, summary, sessions, tool_calls, tokens, created_at, updated_at
		FROM dreamweaver_daily_logs
		WHERE tenant_id = $1::uuid AND agent_id = $2::uuid AND log_date = $3::date
		LIMIT 1
	`, tenantID, agentID, date)
	var log consolidation.DailyLog
	var sessionsJSON []byte
	if err := row.Scan(&log.ID, &log.AgentID, &log.TenantID, &log.UserID, &log.Date, &log.Summary, &sessionsJSON, &log.ToolCalls, &log.Tokens, &log.CreatedAt, &log.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(sessionsJSON, &log.Sessions)
	return &log, nil
}

func (s *PGDailyLogStore) ListDailyLogs(ctx context.Context, agentID, tenantID string, limit int) ([]consolidation.DailyLog, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id::text, tenant_id::text, user_id, log_date::text, summary, sessions, tool_calls, tokens, created_at, updated_at
		FROM dreamweaver_daily_logs
		WHERE tenant_id = $1::uuid AND agent_id = $2::uuid
		ORDER BY log_date DESC
		LIMIT $3
	`, tenantID, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []consolidation.DailyLog
	for rows.Next() {
		var log consolidation.DailyLog
		var sessionsJSON []byte
		if err := rows.Scan(&log.ID, &log.AgentID, &log.TenantID, &log.UserID, &log.Date, &log.Summary, &sessionsJSON, &log.ToolCalls, &log.Tokens, &log.CreatedAt, &log.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(sessionsJSON, &log.Sessions)
		out = append(out, log)
	}
	return out, rows.Err()
}

type PGSpiritProfileStore struct{ db *sql.DB }

func NewPGSpiritProfileStore(db *sql.DB) *PGSpiritProfileStore { return &PGSpiritProfileStore{db: db} }

func (s *PGSpiritProfileStore) GetProfile(ctx context.Context, userID, tenantID string) (*spirit.Profile, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id::text, user_id, tenant_id::text, name, language, style, preferences, agent_affinity, work_context, created_at, updated_at, interaction_count
		FROM dreamweaver_spirit_profiles
		WHERE tenant_id = $1::uuid AND user_id = $2
		LIMIT 1
	`, tenantID, userID)
	var profile spirit.Profile
	var styleJSON, prefsJSON, affinityJSON, workJSON []byte
	if err := row.Scan(&profile.ID, &profile.UserID, &profile.TenantID, &profile.Name, &profile.Language, &styleJSON, &prefsJSON, &affinityJSON, &workJSON, &profile.CreatedAt, &profile.UpdatedAt, &profile.InteractionCount); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(styleJSON, &profile.Style)
	_ = json.Unmarshal(prefsJSON, &profile.Preferences)
	_ = json.Unmarshal(affinityJSON, &profile.AgentAffinity)
	_ = json.Unmarshal(workJSON, &profile.WorkContext)
	return &profile, nil
}

func (s *PGSpiritProfileStore) SaveProfile(ctx context.Context, profile *spirit.Profile) error {
	if profile == nil {
		return nil
	}
	if profile.ID == "" {
		profile.ID = uuid.NewString()
	}
	styleJSON, _ := json.Marshal(profile.Style)
	prefsJSON, _ := json.Marshal(profile.Preferences)
	affinityJSON, _ := json.Marshal(profile.AgentAffinity)
	workJSON, _ := json.Marshal(profile.WorkContext)
	tenantID, err := uuid.Parse(profile.TenantID)
	if err != nil {
		return fmt.Errorf("parse tenant id: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO dreamweaver_spirit_profiles (id, tenant_id, user_id, name, language, style, preferences, agent_affinity, work_context, interaction_count, created_at, updated_at)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (tenant_id, user_id) DO UPDATE
		SET name = EXCLUDED.name,
		    language = EXCLUDED.language,
		    style = EXCLUDED.style,
		    preferences = EXCLUDED.preferences,
		    agent_affinity = EXCLUDED.agent_affinity,
		    work_context = EXCLUDED.work_context,
		    interaction_count = EXCLUDED.interaction_count,
		    updated_at = EXCLUDED.updated_at
	`, profile.ID, tenantID, profile.UserID, profile.Name, profile.Language, styleJSON, prefsJSON, affinityJSON, workJSON, profile.InteractionCount, profile.CreatedAt, profile.UpdatedAt)
	return err
}

func (s *PGSpiritProfileStore) DeleteProfile(ctx context.Context, userID, tenantID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dreamweaver_spirit_profiles WHERE tenant_id = $1::uuid AND user_id = $2`, tenantID, userID)
	return err
}

type PGPermissionAuditStore struct{ db *sql.DB }

func NewPGPermissionAuditStore(db *sql.DB) *PGPermissionAuditStore {
	return &PGPermissionAuditStore{db: db}
}

func (s *PGPermissionAuditStore) AppendAuditEntry(ctx context.Context, entry permissions.AuditEntry) error {
	classJSON, _ := json.Marshal(entry.Decision.Classification)
	requestJSON, _ := json.Marshal(entry.Request)
	decisionJSON, _ := json.Marshal(entry.Decision)
	agentID := nullableUUID(entry.Request.AgentID)
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return fmt.Errorf("tenant id missing from context")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dreamweaver_audit_log (id, tenant_id, agent_id, user_id, run_id, tool_name, action, allowed, reason, classification, request_payload, decision_payload, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`, entry.ID, tenantID, agentID, entry.Request.UserID, entry.Request.RunID, entry.Request.ToolName, entry.Decision.Action, entry.Decision.Allowed, entry.Decision.Reason, classJSON, requestJSON, decisionJSON, entry.Timestamp)
	return err
}

func (s *PGPermissionAuditStore) ListAuditEntries(ctx context.Context, limit int) ([]permissions.AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, created_at, request_payload, decision_payload
		FROM dreamweaver_audit_log
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []permissions.AuditEntry
	for rows.Next() {
		var entry permissions.AuditEntry
		var requestJSON, decisionJSON []byte
		if err := rows.Scan(&entry.ID, &entry.Timestamp, &requestJSON, &decisionJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(requestJSON, &entry.Request)
		_ = json.Unmarshal(decisionJSON, &entry.Decision)
		out = append(out, entry)
	}
	return out, rows.Err()
}

func nullableUUID(raw string) any {
	if raw == "" {
		return nil
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return parsed
}
