package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/compression"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/consolidation"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/lifecycle"
	dwmessage "github.com/nextlevelbuilder/goclaw/internal/message"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/plugins"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/resume"
	"github.com/nextlevelbuilder/goclaw/internal/spirit"
	pgstore "github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/sdk"
)

func (l *Loop) initDreamWeaverServices() {
	if !l.dreamweaverEnabled() {
		return
	}

	l.lifecycleMgr = lifecycle.NewManager(l.domainBus)
	l.hookRegistry = hooks.NewRegistry()
	l.governor = permissions.NewGovernor()
	l.governor.SetSafetyCheck(permissions.DefaultSafetyChecker())
	l.governor.SetOnModeChange(func(agentID string, prev, next permissions.AgentMode, reason string) {
		if !l.sdkBridgeEnabled() || l.sdkBridge == nil {
			return
		}
		l.sdkBridge.Emit(sdk.Event{
			Type:       sdk.EventPermissionModeChange,
			AgentID:    l.id,
			Data: sdk.PermissionModeChangeData{
				AgentID: agentID,
				OldMode: string(prev),
				NewMode: string(next),
				Reason:  reason,
			},
		})
	})
	l.resumeEng = resume.NewEngine()
	l.sdkBridge = sdk.NewBridge()
	l.pluginRegistry = plugins.NewRegistry(filepath.Join(l.dataDir, "plugins"))
	profileStore := spirit.ProfileStore(noopProfileStore{})
	if l.runtimeDB != nil {
		profileStore = pgstore.NewPGSpiritProfileStore(l.runtimeDB)
		l.governor.SetAuditStore(pgstore.NewPGPermissionAuditStore(l.runtimeDB))
		l.topicWorker = consolidation.NewTopicWorker(
			pgstore.NewPGTopicStore(l.runtimeDB),
			staticTopicExtractor{},
			consolidation.TopicWorkerConfig{
				MinEpisodicCount: 1,
				MergeThreshold:   0.8,
				Interval:         time.Hour,
			},
		)
		l.dailyLogWorker = consolidation.NewDailyLogWorker(
			pgstore.NewPGDailyLogStore(l.runtimeDB),
			staticDailySummariser{},
			consolidation.DefaultDailyLogWorkerConfig(),
		)
	}
	l.profileMgr = spirit.NewProfileManager(profileStore)
	var classifier spirit.IntentClassifier
	if l.spiritEnabled() && l.provider != nil {
		classifier = spirit.NewLLMIntentClassifier(l.provider, l.model)
	}
	l.spiritRouter = spirit.NewRouter(classifier)
	if l.spiritDelegateRunFn != nil {
		exec := &delegateAgentExecutor{
			runFn:        l.spiritDelegateRunFn,
			fromAgentKey: l.id,
			loop:         l,
		}
		l.spiritOrchestrator = spirit.NewOrchestrator(exec, nil)
	}
	l.learningLoop = spirit.NewLearningLoop(l.profileMgr)
	if l.runtimeDB != nil {
		l.learningLoop.BindTopicStore(pgstore.NewPGTopicStore(l.runtimeDB), l.agentUUID.String())
	}
	var compactor compression.Compactor
	if l.provider != nil {
		compactor = l.compressionCompactor
	}
	l.compressionEng = compression.NewEngine(
		compression.DefaultConfig(),
		func(msgs []providers.Message) int {
			total := 0
			for _, msg := range msgs {
				total += len([]rune(msg.Content))/4 + len([]rune(msg.Thinking))/4 + len(msg.ToolCalls)*16
			}
			return total
		},
		compactor,
	)
	l.loadDreamWeaverHooks(context.Background())

	l.lifecycleMgr.OnTransition(func(t lifecycle.Transition) {
		if !l.sdkBridgeEnabled() || l.sdkBridge == nil {
			return
		}
		l.sdkBridge.Emit(sdk.Event{
			Type:  sdk.EventStateChange,
			RunID: t.RunID,
			Data: sdk.StateChangeData{
				From:   t.From.String(),
				To:     t.To.String(),
				Reason: t.Reason,
			},
		})
	})

	if l.pluginsEnabled() && l.pluginRegistry != nil {
		registry, ok := l.tools.(*tools.Registry)
		if ok && l.dataDir != "" {
		hookReg := l.hookRegistry
		l.pluginRegistry.OnDeactivate(func(inst *plugins.Instance) error {
			if l.mcpManager != nil {
				l.mcpManager.DisconnectPluginMCPServers(inst.Manifest.Name)
			}
			// Notify bridge and other infrastructure that the tool registry changed.
			if l.onToolRegistryChange != nil {
				l.onToolRegistryChange()
			}
			return nil
		})
		l.pluginRegistry.OnActivate(func(inst *plugins.Instance) error {
			if l.mcpManager != nil && len(inst.Manifest.Capabilities.MCPServers) > 0 {
				if err := l.mcpManager.ConnectPluginMCPServers(context.Background(), l.tenantID, inst.Manifest.Name, inst.Manifest.Capabilities.MCPServers); err != nil {
					return err
				}
			}
			// Register plugin-contributed tools.
			for _, decl := range inst.Manifest.Capabilities.Tools {
				registry.Register(pluginRuntimeTool{
					name:        decl.Name,
					description: decl.Description,
					parameters:  decl.Parameters,
					pluginName:  inst.Manifest.Name,
					registry:    l.pluginRegistry,
				})
			}
			// 4-6: Auto-scan plugin hooks/ directory and register declared hooks.
			if hookReg != nil {
				l.registerPluginHooks(hookReg, inst)
			}
			// Notify bridge and other infrastructure that the tool registry changed.
			if l.onToolRegistryChange != nil {
				l.onToolRegistryChange()
			}
			return nil
		})
			pluginDir := filepath.Join(l.dataDir, "plugins")
			if err := l.pluginRegistry.LoadFromDir(context.Background(), pluginDir); err == nil {
				for _, inst := range l.pluginRegistry.List() {
					_ = l.pluginRegistry.Activate(context.Background(), inst.Manifest.Name)
				}
			}
		}
	}
}

func (l *Loop) dreamweaverEnabled() bool {
	return l.dreamweaverCfg != nil && l.dreamweaverCfg.Enabled
}

func (l *Loop) lifecycleEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.LifecycleEnabled
}

func (l *Loop) hooksEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.HooksEnabled
}

func (l *Loop) governorEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.GovernorEnabled
}

func (l *Loop) compressionEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.CompressionEnabled
}

// effectiveDreamweaver merges global agent config with optional user-home and workspace overlays
// (`~/.goclaw/dreamweaver.json`, then `{workspace}/.goclaw/dreamweaver.json`).
func (l *Loop) effectiveDreamweaver(req *RunRequest) *config.DreamWeaverConfig {
	base := l.dreamweaverCfg
	if base == nil {
		return nil
	}
	layers := []*config.DreamWeaverConfig{base}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		p := filepath.Join(home, ".goclaw", "dreamweaver.json")
		if over, err := config.LoadDreamWeaverFile(p); err == nil && over != nil {
			layers = append(layers, over)
		}
	}
	if req != nil && req.UserID != "" {
		if val, ok := l.userSetups.Load(req.UserID); ok {
			ws := val.(*userSetup).workspace
			if ws != "" {
				path := filepath.Join(ws, ".goclaw", "dreamweaver.json")
				if over, err := config.LoadDreamWeaverFile(path); err == nil && over != nil {
					layers = append(layers, over)
				}
			}
		}
	}
	return config.MergeChain(layers...)
}

// compressionEnabledFor uses the merged DreamWeaver config for the run (project-level overrides).
func (l *Loop) compressionEnabledFor(req *RunRequest) bool {
	cfg := l.effectiveDreamweaver(req)
	return cfg != nil && cfg.Enabled && cfg.CompressionEnabled
}

func (l *Loop) resumeEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.ResumeEnabled
}

func (l *Loop) sdkBridgeEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.SDKBridgeEnabled
}

func (l *Loop) spiritEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.SpiritEnabled
}

func (l *Loop) pluginsEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.PluginsEnabled
}

func (l *Loop) promptSectionsEnabled() bool {
	return l.dreamweaverEnabled() && l.dreamweaverCfg.PromptSectionsEnabled
}

func (l *Loop) transitionLifecycle(ctx context.Context, runID string, to lifecycle.State, reason string) {
	if !l.lifecycleEnabled() || l.lifecycleMgr == nil || runID == "" {
		return
	}
	_ = l.lifecycleMgr.Transition(ctx, runID, to, reason)
}

func (l *Loop) registerLifecycleRun(runID, userID string) {
	if !l.lifecycleEnabled() || l.lifecycleMgr == nil || runID == "" {
		return
	}
	l.lifecycleMgr.Register(runID, l.agentUUID.String(), l.tenantID.String(), userID)
}

func (l *Loop) cleanupLifecycleRun(runID string) {
	if !l.lifecycleEnabled() || l.lifecycleMgr == nil || runID == "" {
		return
	}
	l.lifecycleMgr.Cleanup(runID)
}

func (l *Loop) storeTranscript(runID, sessionKey string, msgs []providers.Message) {
	if runID == "" {
		return
	}
	tr := dwmessage.NewTranscript(sessionKey)
	tr.Append(msgs...)
	l.transcripts.Store(runID, tr)
	l.writeTranscriptJSONL(runID, msgs, false)
}

func (l *Loop) appendTranscript(runID string, msgs ...providers.Message) {
	if runID == "" || len(msgs) == 0 {
		return
	}
	value, ok := l.transcripts.Load(runID)
	if !ok {
		return
	}
	if tr, ok := value.(*dwmessage.Transcript); ok {
		tr.Append(msgs...)
	}
	l.writeTranscriptJSONL(runID, msgs, true)
}

func (l *Loop) transcriptForRun(runID string) *dwmessage.Transcript {
	if runID == "" {
		return nil
	}
	value, ok := l.transcripts.Load(runID)
	if !ok {
		return nil
	}
	tr, _ := value.(*dwmessage.Transcript)
	return tr
}

func (l *Loop) deleteTranscript(runID string) {
	if runID == "" {
		return
	}
	l.transcripts.Delete(runID)
}

func (l *Loop) transcriptLogPath(runID string) string {
	if runID == "" || l.dataDir == "" {
		return ""
	}
	return filepath.Join(l.dataDir, "transcripts", runID+".jsonl")
}

// transcriptEvent is a single entry in the append-only JSONL event log.
// Kind values: "user", "assistant", "tool_call", "tool_result", "system", "compact".
type transcriptEvent struct {
	TS    string         `json:"ts"`
	RunID string         `json:"run_id"`
	Kind  string         `json:"kind"`
	Data  map[string]any `json:"data"`
}

// messageKind returns the JSONL event kind for a providers.Message.
func messageKind(msg providers.Message) string {
	switch msg.Role {
	case "user":
		return "user"
	case "assistant":
		if len(msg.ToolCalls) > 0 {
			return "tool_call"
		}
		return "assistant"
	case "tool":
		return "tool_result"
	case "system":
		if msg.CompactBoundary != nil {
			return "compact"
		}
		return "system"
	default:
		return msg.Role
	}
}

// messageData converts a providers.Message to the JSONL event data map.
func messageData(msg providers.Message) map[string]any {
	d := map[string]any{}
	if msg.Content != "" {
		d["content"] = msg.Content
	}
	if msg.ToolCallID != "" {
		d["tool_call_id"] = msg.ToolCallID
	}
	if msg.IsError {
		d["is_error"] = true
	}
	if len(msg.ToolCalls) > 0 {
		calls := make([]map[string]any, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			calls = append(calls, map[string]any{
				"id":        tc.ID,
				"name":      tc.Name,
				"arguments": tc.Arguments,
			})
		}
		d["tool_calls"] = calls
	}
	if msg.CompactBoundary != nil {
		d["compact_boundary"] = msg.CompactBoundary
	}
	return d
}

func (l *Loop) writeTranscriptJSONL(runID string, msgs []providers.Message, appendMode bool) {
	path := l.transcriptLogPath(runID)
	if path == "" || len(msgs) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	flag := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	for _, msg := range msgs {
		evt := transcriptEvent{
			TS:    ts,
			RunID: runID,
			Kind:  messageKind(msg),
			Data:  messageData(msg),
		}
		_ = enc.Encode(evt)
	}
}

type noopProfileStore struct{}

func (noopProfileStore) GetProfile(context.Context, string, string) (*spirit.Profile, error) {
	return nil, nil
}
func (noopProfileStore) SaveProfile(context.Context, *spirit.Profile) error  { return nil }
func (noopProfileStore) DeleteProfile(context.Context, string, string) error { return nil }

// memoryDriftTrackingEnabled is true when DreamWeaver is on and episodic auto-inject is available.
// Enables per-run DriftDetector (RecordInjection in ContextStage, Tick in ThinkStage iteration >= 1).
func (l *Loop) memoryDriftTrackingEnabled(req *RunRequest) bool {
	if req == nil || l.autoInjector == nil {
		return false
	}
	dw := l.effectiveDreamweaver(req)
	return dw != nil && dw.Enabled
}

func defaultDreamWeaverConfig(cfg *config.DreamWeaverConfig) *config.DreamWeaverConfig {
	if cfg != nil {
		return cfg
	}
	return &config.DreamWeaverConfig{
		Enabled:               false,
		LifecycleEnabled:      false,
		GovernorEnabled:       false,
		HooksEnabled:          false,
		CompressionEnabled:    false,
		ResumeEnabled:         false,
		SDKBridgeEnabled:      false,
		SpiritEnabled:         false,
		PluginsEnabled:        false,
		PromptSectionsEnabled: false,
	}
}

func makeHookPayload(event hooks.Event, req *RunRequest, data map[string]any) hooks.Payload {
	return hooks.Payload{
		Event:      event,
		RunID:      req.RunID,
		AgentID:    "",
		SessionKey: req.SessionKey,
		Timestamp:  time.Now(),
		Data:       data,
	}
}

type pluginPlaceholderTool struct {
	name        string
	description string
	parameters  map[string]any
	pluginName  string
	registry    *plugins.Registry
}

func (t pluginPlaceholderTool) Name() string               { return t.name }
func (t pluginPlaceholderTool) Description() string        { return t.description }
func (t pluginPlaceholderTool) Parameters() map[string]any { return t.parameters }
func (t pluginPlaceholderTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	if t.registry == nil {
		return tools.ErrorResult("plugin runtime unavailable: registry is nil")
	}
	result, err := t.registry.ExecuteTool(ctx, t.pluginName, t.name, args)
	if err != nil {
		return tools.ErrorResult("plugin tool failed: " + err.Error())
	}
	return result
}

type pluginRuntimeTool struct {
	name        string
	description string
	parameters  map[string]any
	pluginName  string
	registry    *plugins.Registry
}

func (t pluginRuntimeTool) Name() string               { return t.name }
func (t pluginRuntimeTool) Description() string        { return t.description }
func (t pluginRuntimeTool) Parameters() map[string]any { return t.parameters }
func (t pluginRuntimeTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	if t.registry == nil {
		return tools.ErrorResult("plugin runtime unavailable: registry is nil")
	}
	result, err := t.registry.ExecuteTool(ctx, t.pluginName, t.name, args)
	if err != nil {
		return tools.ErrorResult("plugin tool failed: " + err.Error())
	}
	return result
}

type staticTopicExtractor struct{}

func (staticTopicExtractor) ExtractTopics(_ context.Context, summaries []string) ([]consolidation.ExtractedTopic, error) {
	var out []consolidation.ExtractedTopic
	for idx, summary := range summaries {
		topic := inferTopicFromSummary(summary)
		if topic == "" {
			topic = fmt.Sprintf("session-topic-%d", idx+1)
		}
		out = append(out, consolidation.ExtractedTopic{
			Topic:   topic,
			Summary: truncateSummary(summary, 400),
			Tags:    topicTags(topic),
			Sources: []int{idx},
		})
	}
	return out, nil
}

type staticDailySummariser struct{}

func (staticDailySummariser) Summarise(_ context.Context, sessions []consolidation.SessionSummaryInput) (string, error) {
	parts := make([]string, 0, len(sessions))
	for _, session := range sessions {
		summary := strings.TrimSpace(session.Summary)
		if summary == "" {
			summary = "Session completed without an explicit summary."
		}
		parts = append(parts, summary)
	}
	return strings.Join(parts, "\n\n"), nil
}

func inferTopicFromSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	summary = strings.ReplaceAll(summary, "\n", " ")
	words := strings.Fields(summary)
	if len(words) > 8 {
		words = words[:8]
	}
	return strings.Join(words, " ")
}

func truncateSummary(summary string, limit int) string {
	if limit <= 0 {
		return summary
	}
	runes := []rune(summary)
	if len(runes) <= limit {
		return summary
	}
	return string(runes[:limit]) + "..."
}

func topicTags(topic string) []string {
	if topic == "" {
		return nil
	}
	seen := map[string]bool{}
	var tags []string
	for _, word := range strings.Fields(strings.ToLower(topic)) {
		word = strings.Trim(word, ".,:;!?()[]{}\"'")
		if word == "" || seen[word] {
			continue
		}
		seen[word] = true
		tags = append(tags, word)
	}
	return tags
}

func (l *Loop) loadDreamWeaverHooks(ctx context.Context) {
	if l.runtimeDB == nil || l.hookRegistry == nil {
		return
	}
	store := pgstore.NewPGHookConfigStore(l.runtimeDB)
	registered, err := store.ListHooks(ctx, l.tenantID, l.agentUUID)
	if err != nil {
		return
	}
	for _, hook := range registered {
		_ = l.hookRegistry.Register(hook)
	}
}

func (l *Loop) persistDreamWeaverContinuity(ctx context.Context, req *RunRequest, sessionKey string, tokensUsed int, finalContent string, compactionCount int) {
	if req == nil || sessionKey == "" {
		return
	}
	summary := strings.TrimSpace(l.sessions.GetSummary(ctx, sessionKey))
	if summary == "" {
		summary = strings.TrimSpace(finalContent)
	}
	if summary == "" {
		summary = "Session completed without a stored summary."
	}

	if l.topicWorker != nil {
		_ = l.topicWorker.Process(ctx, l.agentUUID.String(), l.tenantID.String(), req.UserID, []consolidation.EpisodicInput{{
			ID:      fmt.Sprintf("%s:%d", sessionKey, compactionCount),
			Summary: summary,
		}})
	}

	if l.dailyLogWorker != nil {
		date := time.Now().In(time.UTC).Format("2006-01-02")
		_ = l.dailyLogWorker.Process(ctx, l.agentUUID.String(), l.tenantID.String(), req.UserID, date, []consolidation.SessionSummaryInput{{
			SessionKey: sessionKey,
			Summary:    summary,
			ToolCalls:  0,
			Tokens:     tokensUsed,
			StartedAt:  time.Now().UTC(),
		}})
	}
}

func (l *Loop) buildDreamWeaverPromptSections(ctx context.Context, req *RunRequest) (spiritSect, memorySect, runtimeSect, rulesSect string) {

	if l.spiritEnabled() && l.profileMgr != nil && l.spiritRouter != nil && req != nil && req.Message != "" {
		profile, _ := l.profileMgr.Get(ctx, req.UserID, l.tenantID.String())
		agents := []spirit.AgentInfo{{
			ID:           l.id,
			Name:         l.displayName,
			Description:  l.agentType,
			Capabilities: []string{"chat", "code", "research", "design"},
			Available:    true,
		}}
		for _, target := range l.delegateTargets {
			agents = append(agents, spirit.AgentInfo{
				ID:           target.AgentKey,
				Name:         target.DisplayName,
				Description:  target.Description,
				Capabilities: []string{"delegated"},
				Available:    true,
			})
		}
		l.spiritRouter.SetAgents(agents)
		intent, err := l.spiritRouter.Route(ctx, req.Message, profile)
		if err == nil && intent != nil {
			spiritSect = fmt.Sprintf(
				"## DreamWeaver Spirit\n\nDetected intent: %s.\nConfidence: %.2f.\nSuggested agents: %v.\n",
				intent.Type, intent.Confidence, intent.SuggestedAgents,
			)
			// When the orchestrator is available and the intent has subtasks
			// with assigned agents, run them and embed results in the spirit
			// section so the LLM can synthesize a final answer from delegate outputs.
			if l.spiritOrchestrator != nil && intentHasAssignedSubTasks(intent) {
				orchResult, orchErr := l.spiritOrchestrator.Execute(ctx, intent)
				if orchErr == nil && orchResult != nil && orchResult.MergedResult != "" {
					spiritSect += fmt.Sprintf(
						"\n### Orchestration Results\n\nThe following subtasks were delegated and completed:\n\n%s\n",
						orchResult.MergedResult,
					)
				}
			}
		}
	}

	if l.hasMemory {
		memorySect = "## DreamWeaver Memory\n\nPrefer durable memory only for reusable facts, preferences, and ongoing goals. Avoid re-storing transient execution noise.\n"
	}

	if l.lifecycleEnabled() && l.lifecycleMgr != nil && req != nil {
		if current, ok := l.lifecycleMgr.Current(req.RunID); ok {
			runtimeSect = fmt.Sprintf(
				"## DreamWeaver Runtime\n\nCurrent lifecycle state: %s.\nUse compact tool calls and leave the session resumable if interrupted.\n",
				current.String(),
			)
		}
	}

	// 4-2: Discover project rule files (.claude/ dir + CLAUDE.md) and inject.
	if l.dreamweaverEnabled() {
		rulesSect = l.loadProjectRuleFiles()
	}

	return
}

// registerPluginHooks scans the plugin's hooks/ directory and registers a HandlerCommand
// hook for each event declared in the manifest. The script path is resolved as:
//
//	<pluginRuntimeDir>/hooks/<event>[.sh|.bat|""]
//
// On Windows, .bat files are preferred; on other platforms, .sh files are tried first.
func (l *Loop) registerPluginHooks(reg *hooks.Registry, inst *plugins.Instance) {
	hooksDir := filepath.Join(inst.RuntimeDir, "hooks")
	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		return
	}
	for i, decl := range inst.Manifest.Capabilities.Hooks {
		if decl.Event == "" {
			continue
		}
		// Find the script: try event name directly, then with .sh/.bat extensions.
		candidates := []string{
			filepath.Join(hooksDir, decl.Event),
			filepath.Join(hooksDir, decl.Event+".sh"),
			filepath.Join(hooksDir, decl.Event+".bat"),
		}
		var scriptPath string
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				scriptPath = c
				break
			}
		}
		if scriptPath == "" {
			continue
		}
		mode := hooks.ModeAsync
		if strings.EqualFold(decl.Mode, "sync") {
			mode = hooks.ModeSync
		}
		hookID := fmt.Sprintf("plugin:%s:%s:%d", inst.Manifest.Name, decl.Event, i)
		h := hooks.Hook{
			ID:      hookID,
			Event:   hooks.Event(decl.Event),
			Mode:    mode,
			Priority: decl.Priority,
			AgentID: "*",
			Enabled: true,
			Handler: hooks.HandlerSpec{
				Type:    hooks.HandlerCommand,
				Target:  scriptPath,
				Timeout: 30,
			},
		}
		if err := reg.Register(h); err != nil {
			slog.Warn("plugin hook registration failed",
				"plugin", inst.Manifest.Name, "event", decl.Event, "error", err)
		} else {
			slog.Info("plugin hook registered",
				"plugin", inst.Manifest.Name, "hook", hookID, "event", decl.Event)
		}
	}
}

// loadProjectRuleFiles scans the user home dir and workspace for CLAUDE.md / .claude/*.md
// rule files and returns their combined content as a DreamWeaver prompt section.
//
// Layer priority (highest last, later sections override earlier):
//   1. User-level:    ~/.goclaw/CLAUDE.md + ~/.goclaw/.claude/*.md
//   2. Project-level: <workspace>/CLAUDE.md + <workspace>/.claude/*.md
//
// Files larger than 32 KB per file are truncated to avoid blowing the context window.
func (l *Loop) loadProjectRuleFiles() string {
	const maxFileBytes = 32 * 1024
	type ruleFile struct {
		path    string
		content string
	}

	// loadFromDir reads CLAUDE.md and .claude/*.md from a base directory.
	loadFromDir := func(base string) []ruleFile {
		if base == "" {
			return nil
		}
		var files []ruleFile

		// Root CLAUDE.md
		if data, err := os.ReadFile(filepath.Join(base, "CLAUDE.md")); err == nil {
			if len(data) > maxFileBytes {
				data = data[:maxFileBytes]
			}
			files = append(files, ruleFile{path: "CLAUDE.md", content: string(data)})
		}

		// .claude/*.md sub-directory
		claudeDir := filepath.Join(base, ".claude")
		if entries, err := os.ReadDir(claudeDir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				fp := filepath.Join(claudeDir, e.Name())
				data, err := os.ReadFile(fp)
				if err != nil {
					continue
				}
				if len(data) > maxFileBytes {
					data = data[:maxFileBytes]
				}
				files = append(files, ruleFile{
					path:    filepath.Join(".claude", e.Name()),
					content: string(data),
				})
			}
		}
		return files
	}

	type layeredSection struct {
		label string
		files []ruleFile
	}

	// Layer 1: user-level rules (~/.goclaw/)
	homeDir, _ := os.UserHomeDir()
	userFiles := loadFromDir(filepath.Join(homeDir, ".goclaw"))

	// Layer 2: project-level rules (workspace root) — only when workspace is set.
	var projectFiles []ruleFile
	if l.workspace != "" {
		projectFiles = loadFromDir(l.workspace)
	}

	layers := []layeredSection{
		{"user", userFiles},
		{"project", projectFiles},
	}

	var anyFound bool
	for _, layer := range layers {
		if len(layer.files) > 0 {
			anyFound = true
			break
		}
	}
	if !anyFound {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Project Rules\n\n")
	sb.WriteString("The following rules apply to this session (project rules take precedence over user rules):\n\n")
	for _, layer := range layers {
		if len(layer.files) == 0 {
			continue
		}
		for _, rf := range layer.files {
			label := fmt.Sprintf("%s/%s", layer.label, rf.path)
			sb.WriteString(fmt.Sprintf("### %s\n\n%s\n\n", label, strings.TrimSpace(rf.content)))
		}
	}
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

// intentHasAssignedSubTasks returns true when at least one SubTask has a non-empty AgentID.
func intentHasAssignedSubTasks(intent *spirit.Intent) bool {
	for _, st := range intent.SubTasks {
		if st.AgentID != "" {
			return true
		}
	}
	return false
}

// delegateAgentExecutor bridges spirit.AgentExecutor to the tools.DelegateRunFunc so
// the spirit orchestrator can dispatch sub-tasks via the existing delegation infrastructure.
type delegateAgentExecutor struct {
	runFn        tools.DelegateRunFunc
	fromAgentKey string
	loop         *Loop
}

func (e *delegateAgentExecutor) Execute(ctx context.Context, agentKey string, task spirit.SubTask) (string, error) {
	req := tools.DelegateRequest{
		ToAgentKey:   agentKey,
		Task:         task.Description,
		DelegationID: fmt.Sprintf("spirit-%s", task.ID),
		FromAgentKey: e.fromAgentKey,
	}
	parentRunID := tools.ToolRunIDFromCtx(ctx)
	if e.loop != nil && e.loop.hooksEnabled() && e.loop.hookRegistry != nil {
		_, _ = e.loop.hookRegistry.Fire(ctx, hooks.NewPayload(
			hooks.EventSubagentStart, parentRunID, e.loop.id, "",
			hooks.SubagentPayload{SubagentID: req.DelegationID, SubagentKind: "delegation", ParentRunID: parentRunID},
		))
	}
	result, err := e.runFn(ctx, req)
	if e.loop != nil && e.loop.hooksEnabled() && e.loop.hookRegistry != nil {
		_, _ = e.loop.hookRegistry.Fire(ctx, hooks.NewPayload(
			hooks.EventSubagentStop, parentRunID, e.loop.id, "",
			hooks.SubagentPayload{SubagentID: req.DelegationID, SubagentKind: "delegation", ParentRunID: parentRunID},
		))
	}
	if err != nil {
		return "", err
	}
	return result.Content, nil
}
