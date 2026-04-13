package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/compression"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/consolidation"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/plugins"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type stubSessionStore struct {
	summary string
}

func (s *stubSessionStore) GetOrCreate(context.Context, string) *store.SessionData            { return nil }
func (s *stubSessionStore) Get(context.Context, string) *store.SessionData                    { return nil }
func (s *stubSessionStore) AddMessage(context.Context, string, providers.Message)             {}
func (s *stubSessionStore) GetHistory(context.Context, string) []providers.Message            { return nil }
func (s *stubSessionStore) GetSummary(context.Context, string) string                         { return s.summary }
func (s *stubSessionStore) SetSummary(context.Context, string, string)                        {}
func (s *stubSessionStore) GetLabel(context.Context, string) string                           { return "" }
func (s *stubSessionStore) SetLabel(context.Context, string, string)                          {}
func (s *stubSessionStore) SetAgentInfo(context.Context, string, uuid.UUID, string)           {}
func (s *stubSessionStore) TruncateHistory(context.Context, string, int)                      {}
func (s *stubSessionStore) SetHistory(context.Context, string, []providers.Message)           {}
func (s *stubSessionStore) Reset(context.Context, string)                                     {}
func (s *stubSessionStore) Delete(context.Context, string) error                              { return nil }
func (s *stubSessionStore) Save(context.Context, string) error                                { return nil }
func (s *stubSessionStore) UpdateMetadata(context.Context, string, string, string, string)    {}
func (s *stubSessionStore) AccumulateTokens(context.Context, string, int64, int64)            {}
func (s *stubSessionStore) IncrementCompaction(context.Context, string)                       {}
func (s *stubSessionStore) GetCompactionCount(context.Context, string) int                    { return 0 }
func (s *stubSessionStore) GetMemoryFlushCompactionCount(context.Context, string) int         { return 0 }
func (s *stubSessionStore) SetMemoryFlushDone(context.Context, string)                        {}
func (s *stubSessionStore) GetSessionMetadata(context.Context, string) map[string]string      { return nil }
func (s *stubSessionStore) SetSessionMetadata(context.Context, string, map[string]string)     {}
func (s *stubSessionStore) SetSpawnInfo(context.Context, string, string, int)                 {}
func (s *stubSessionStore) SetContextWindow(context.Context, string, int)                     {}
func (s *stubSessionStore) GetContextWindow(context.Context, string) int                      { return 0 }
func (s *stubSessionStore) SetLastPromptTokens(context.Context, string, int, int)             {}
func (s *stubSessionStore) GetLastPromptTokens(context.Context, string) (int, int)            { return 0, 0 }
func (s *stubSessionStore) List(context.Context, string) []store.SessionInfo                  { return nil }
func (s *stubSessionStore) ListPaged(context.Context, store.SessionListOpts) store.SessionListResult {
	return store.SessionListResult{}
}
func (s *stubSessionStore) ListPagedRich(context.Context, store.SessionListOpts) store.SessionListRichResult {
	return store.SessionListRichResult{}
}
func (s *stubSessionStore) LastUsedChannel(context.Context, string) (string, string) { return "", "" }

type captureTopicStore struct {
	memories []consolidation.TopicMemory
}

func (s *captureTopicStore) UpsertTopic(_ context.Context, mem consolidation.TopicMemory) error {
	s.memories = append(s.memories, mem)
	return nil
}
func (s *captureTopicStore) ListTopics(context.Context, string, string, string) ([]consolidation.TopicMemory, error) {
	return s.memories, nil
}
func (s *captureTopicStore) FindByTopic(_ context.Context, _, _, topic string) (*consolidation.TopicMemory, error) {
	for i := range s.memories {
		if s.memories[i].Topic == topic {
			return &s.memories[i], nil
		}
	}
	return nil, nil
}
func (s *captureTopicStore) DeleteTopic(context.Context, string) error { return nil }

type captureDailyLogStore struct {
	logs []consolidation.DailyLog
}

func (s *captureDailyLogStore) UpsertDailyLog(_ context.Context, log consolidation.DailyLog) error {
	s.logs = append(s.logs, log)
	return nil
}
func (s *captureDailyLogStore) GetDailyLog(context.Context, string, string, string) (*consolidation.DailyLog, error) {
	if len(s.logs) == 0 {
		return nil, nil
	}
	return &s.logs[len(s.logs)-1], nil
}
func (s *captureDailyLogStore) ListDailyLogs(context.Context, string, string, int) ([]consolidation.DailyLog, error) {
	return s.logs, nil
}

func TestPersistDreamWeaverContinuity_WritesTopicAndDailyLog(t *testing.T) {
	topicStore := &captureTopicStore{}
	dailyStore := &captureDailyLogStore{}
	loop := &Loop{
		agentUUID: uuid.New(),
		tenantID:  uuid.New(),
		sessions:  &stubSessionStore{summary: "Investigated plugin runtime and stored audit entries."},
		topicWorker: consolidation.NewTopicWorker(
			topicStore,
			staticTopicExtractor{},
			consolidation.TopicWorkerConfig{MinEpisodicCount: 1, MergeThreshold: 0.8, Interval: time.Hour},
		),
		dailyLogWorker: consolidation.NewDailyLogWorker(
			dailyStore,
			staticDailySummariser{},
			consolidation.DefaultDailyLogWorkerConfig(),
		),
	}

	loop.persistDreamWeaverContinuity(context.Background(), &RunRequest{
		UserID: "user-1",
	}, "session-1", 123, "", 1)

	if len(topicStore.memories) != 1 {
		t.Fatalf("expected 1 topic memory, got %d", len(topicStore.memories))
	}
	if topicStore.memories[0].Topic == "" {
		t.Fatal("expected inferred topic to be populated")
	}
	if len(dailyStore.logs) != 1 {
		t.Fatalf("expected 1 daily log, got %d", len(dailyStore.logs))
	}
	if got := dailyStore.logs[0].Tokens; got != 123 {
		t.Fatalf("daily log tokens = %d, want 123", got)
	}
}

func TestTranscriptJSONL_IsPersisted(t *testing.T) {
	dir := t.TempDir()
	loop := &Loop{dataDir: dir}

	loop.storeTranscript("run-1", "session-1", []providers.Message{{
		Role:    "user",
		Content: "hello",
	}})
	loop.appendTranscript("run-1", providers.Message{
		Role:    "assistant",
		Content: "world",
	})

	data, err := os.ReadFile(filepath.Join(dir, "transcripts", "run-1.jsonl"))
	if err != nil {
		t.Fatalf("read transcript jsonl: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"content":"hello"`) || !strings.Contains(text, `"content":"world"`) {
		t.Fatalf("transcript log missing expected content: %s", text)
	}
}

func TestApplyToolHookResults_ModifyAndBlock(t *testing.T) {
	tc := providers.ToolCall{
		ID:        "tc-1",
		Name:      "old_tool",
		Arguments: map[string]any{"path": "a.txt"},
	}
	updated, blocked, reason := applyToolHookResults(tc, []hooks.Result{{
		Action: hooks.ResultActionModify,
		Mutations: map[string]any{
			"tool_name": "new_tool",
			"arguments": map[string]any{"path": "b.txt"},
		},
	}})
	if blocked {
		t.Fatal("modify should not block")
	}
	if reason != "" {
		t.Fatalf("unexpected reason: %q", reason)
	}
	if updated.Name != "new_tool" {
		t.Fatalf("tool name = %q, want new_tool", updated.Name)
	}
	if updated.Arguments["path"] != "b.txt" {
		t.Fatalf("tool args not updated: %#v", updated.Arguments)
	}

	_, blocked, reason = applyToolHookResults(tc, []hooks.Result{{
		Action: hooks.ResultActionBlock,
		Output: "policy denied",
	}})
	if !blocked {
		t.Fatal("expected block result")
	}
	if reason != "policy denied" {
		t.Fatalf("block reason = %q, want policy denied", reason)
	}
}

func TestMakeCompactMessages_UsesCompressionEngine(t *testing.T) {
	loop := &Loop{
		dreamweaverCfg: &config.DreamWeaverConfig{
			Enabled:            true,
			CompressionEnabled: true,
		},
		compressionEng: compression.NewEngine(compression.Config{
			ContextWindow:       10,
			MaxToolResultTokens: 4,
			SnipRetainCount:     1,
			Thresholds:          compression.DefaultThresholds(),
		}, func(msgs []providers.Message) int {
			total := 0
			for _, msg := range msgs {
				total += len([]rune(msg.Content)) / 4
			}
			return total
		}, nil),
	}

	msgs := []providers.Message{{
		Role:    "tool",
		Content: strings.Repeat("x", 200),
	}}
	out, err := loop.makeCompactMessages(&RunRequest{})(context.Background(), msgs, "")
	if err != nil {
		t.Fatalf("compact messages returned error: %v", err)
	}
	if len(out) == 0 || out[0].Content == msgs[0].Content {
		t.Fatalf("expected compression engine to compact tool output, got %#v", out)
	}
}

func TestPluginPlaceholderTool_DelegatesToRegistry(t *testing.T) {
	tool := pluginPlaceholderTool{
		name:       "echo",
		pluginName: "missing-plugin",
		registry:   plugins.NewRegistry(t.TempDir()),
	}
	result := tool.Execute(context.Background(), map[string]any{"input": "hello"})
	if result == nil || !result.IsError {
		t.Fatalf("expected error result, got %#v", result)
	}
	if !strings.Contains(result.ForLLM, `plugin tool failed: plugin "missing-plugin" not found`) {
		t.Fatalf("unexpected error message: %q", result.ForLLM)
	}
}

func TestSyntheticToolError_escapesReason(t *testing.T) {
	s := syntheticToolError("permission_denied", "read_file", "x<y&z")
	if !strings.Contains(s, "&lt;") || !strings.Contains(s, "&amp;") {
		t.Fatalf("expected HTML-escaped reason, got: %s", s)
	}
	if !strings.Contains(s, "<tool_use_error>") {
		t.Fatal("expected structured wrapper")
	}
}
