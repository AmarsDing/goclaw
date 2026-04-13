# DreamWeaver — 合并待办清单 & 实现指南

本文件从 [01](./01-agent-runtime-core.md)–[07](./07-deep-fusion-audit.md) 等文档中的 **未实现 / 部分实现** 条目汇总而来，用于排期与验收。  
**细粒度对照仍以各章 `Ideas` 表与 [README](./README.md) 为准。**

**维护约定**：完成一项后在本文件勾选，并在对应设计文档更新状态（避免 README 与分册长期漂移）。

> **最近一次审计**：2026-04-13，本文件「实现进度速查」与仓库代码对齐；分册 Ideas 表仍见 [文档债](#文档债)。

> **Batch 1 已落地（代码）**：P0-1 `ExecTool.IsConcurrencySafeWithArgs`（`internal/tools/shell.go` + `exec_concurrency_test.go`）；P0-2 `permissions.DefaultSafetyChecker` + `initDreamWeaverServices` 中 `SetSafetyCheck`（`internal/permissions/safety.go`、`safety_test.go`）；P0-4 `syntheticToolError` 用于权限拒绝与 hook 阻断（`internal/agent/loop_pipeline_adapter.go`）。

> **Batch 2–7 已落地（代码，2026-04-13）**：  
> - **权限**：`PermissionDecision.Source`、`Governor.ResolveApproval` 无 `ApprovalFunc` 时 headless deny；`Evaluate` 与 classifier 并行预热；`SwitchMode` + `SetOnModeChange` → SDK `EventPermissionModeChange`。  
> - **SDK**：`PermissionResultData.Source`、`pkg/sdk/frame.go`（`ParseGatewayEventFrame` / `ToSDKEvent`）。  
> - **Pipeline**：prune 路径 `PreCompact`/`PostCompact`；权限请求 SDK 在 `Classification==nil` 时安全。  
> - **压缩**：`Result.FoldLog` + `FoldEntry`；L3 占位 system 消息带 `CompactMeta`；`providers.Message.CompactBoundary`。  
> - **工具**：`InterruptBehaviorTool`、`Registry.InterruptBehaviorFor`、`PipelineDeps.ToolInterruptBehavior`、`ExecTool` 实现 cancel。  
> - **子代理**：`delegateAgentExecutor` 触发 `EventSubagentStart`/`Stop`（Spirit 委派）。  
> - **配置**：`config.MergeChain`；`effectiveDreamweaver` 合并 `~/.goclaw/dreamweaver.json` + 工作区覆盖。  
> - **其它**：`agent.HandoffReview` 启发式；`memory.Layer` 枚举。

> **Batch 8 已落地（代码，2026-04-13）**：  
> - **压缩**：`loop_compact.go` 中 `compressionCompactor`；`initDreamWeaverServices` 在 `provider != nil` 时注入 `compression.NewEngine(..., compactor)`；L4 对 `applyL4` 使用 `skipLeadingSystemRoles` 避免 system 重复。  
> - **子代理 / delegate**：`hooks.WithHookFire` + `injectContext` 注入；`delegate` 工具 `fireDelegateSubagentHooks`（sync/async）；`tools.WithToolRunID`；与 Spirit `delegateAgentExecutor` 对齐 `ParentRunID`。  
> - **中断**：`toolExecutionContext` — `InterruptBlock` → `context.WithoutCancel(ctx)`；**并行软中断**：`ActiveRun.InterruptCh`、`RunRequest.InterruptCh` / `PipelineDeps.InterruptCh`，`InjectMessage` 发信号；`tool_stage.executeParallel` 用 `batchCtx` + 监听 `InterruptCh` 只取消 cancel 组，与 `WithoutCancel` 的 block 工具配合。  
> - **插件 MCP**：`internal/mcp/plugin_servers.go` — `ConnectPluginMCPServers` / `DisconnectPluginMCPServers`（有 `MCPPool` 时走 `connectViaPool` = `Pool.Acquire`）；`resolver` 将 `MCPManager` 写入 `Loop`；**仅有 `MCPPool`、无 `MCPStore` 时也会建 pool-only `MCPManager`**；`loop_dreamweaver` OnActivate/OnDeactivate 接线；**MCP Bridge** `BridgeServer.RefreshTools()` + `OnToolRegistryChange` → `Server.RefreshMCPBridgeTools()`。

> **仍为产品/大颗粒缺口**（非上述批次）：市场/炼化/织梦坊运营、everything-cli 模块化、企业 Trust/MDM。

---

## 实现进度速查（2026-04-13）

| 条目 | 状态 | 说明 |
|------|------|------|
| P0-1 Exec 并发 args | ✅ | `IsConcurrencySafeWithArgs` + 测试 |
| P0-2 Safety 默认 | ✅ | `DefaultSafetyChecker` + `SetSafetyCheck` |
| P0-3 InterruptBehavior | ✅ | 接口 + registry + `toolExecutionContext`（WithoutCancel）+ `read_file` InterruptBlock；**`Router.InterruptCh`（容量 1）**：`InjectMessage` 发信号；`executeParallel` 创 `batchCtx`，监听 `InterruptCh` → 只取消 cancel 组；block 组用 `WithoutCancel` 完成 |
| P0-4 Synthetic error | ✅ | `syntheticToolError` |
| P0-5 Compactor | ✅ | `compressionCompactor` 已注入；L3/L4 可走 LLM 摘要（无 provider 走占位 fallback，已知限制） |
| P0-6 Typed payload | ✅ | `Payload.TypedData any` + `AsTyped[T]()` + `NewPayload()` helper；所有 Fire 调用点已迁移 |
| P0-7 Transcript 视图 | ✅ | `RunState.Transcript *message.Transcript`；`FinalizeStage` 在 run 结束时从 `Messages.All()` 构建（Option B，侵入最小）；`ForUI`/`ForAPI`/`ForResume` 可按需派生 |
| P1-1 Prune compact hooks | ✅ | `pruneMessages` 闭包 Pre/PostCompact |
| P1-2 JSONL 增量 | ✅ | `transcriptEvent{ts,run_id,kind,data}` typed format；`kind` 按 role 推导（user/assistant/tool_call/tool_result/compact/system）；append-only via `writeTranscriptJSONL` |
| P1-3 Compact boundary | ✅ | `CompactMeta` / `CompactBoundary` |
| P1-4 Subagent hooks | ✅ | 委派 + `delegate` 工具 Fire `subagent.start/stop`；`hooks.EventSessionFork` + `SessionForkPayload` 新增；`SessionForkHandler.hookFire` 可选注入后在 fork 成功时 fire |
| P1-5 MCP instructions 独立段 | ✅ | `connectAndDiscover` 捕获 `InitializeResult.Instructions`；`Manager.ServerInstructions()`；`buildMCPServerInstructionsSection`；`SystemPromptConfig.MCPServerInstructions` → 4.5c 段 |
| P1-6 项目规则分层 | ✅ | `loadProjectRuleFiles` 双层：**user** (`~/.goclaw/CLAUDE.md` + `.claude/*.md`) → **project** (workspace)；project 优先；section 标注 `user/` 或 `project/` 前缀 |
| P2-1 Plugin dispatch | ✅ | `ExecuteTool` 按 `manifest.Transport` 分派：`executeViaStdio`（默认）、`executeViaHTTP`（POST JSON）、`executeViaMCP`（JSON-RPC 2.0 `tools/call`）|
| P2-2 Plugin MCP 注册 | ✅ | **`ConnectPluginMCPServers`**；resolver 在 **仅有 `MCPPool`、无 `MCPStore`** 时也建 pool-only **`MCPManager`** |
| P2-3 goclaw 作 MCP Server | ✅ | `BridgeServer` 包装结构 + `RefreshTools()` 原子换新 server；`Loop.OnToolRegistryChange` 在插件 activate/deactivate 时触发；`Server.RefreshMCPBridgeTools()` 对外暴露 |
| P2-4 SDK EventFrame | ✅ | `pkg/sdk/frame.go` 等 |
| P3-1 Mode SDK 事件 | ✅ | `EventPermissionModeChange` |
| P3-2 Headless perm | ✅ | `ResolveApproval` 无审批时 deny |
| P3-3 Source 标签 | ✅ | `DecisionSource` + SDK |
| P3-4 Speculative classifier | ✅ | goroutine 并行预热 |
| CC-1 Fold log | ✅ | `FoldLog` / `FoldEntry`（见压缩批次） |

**图例**：✅ 已闭环 · 🟡 部分/有条件 · ❌ 未做

---

## 已实现 / 已核对（本仓库）

<details><summary>展开查看已完成项（基线 19 + 扩展 9 = 28 项）</summary>

- [x] **Ideas 第 2 条（基线）**：`goclaw changelog`、子系统映射、启发式 risk tags、`--json`、可选 CI artifact。
- [x] **`pkg/sdk` Bridge `Emit` sequence**：按 `run_id` 分桶递增。
- [x] **`internal/hooks` `Enabled`**：`matchingHooks` 跳过 `Enabled==false`。
- [x] **`internal/resume` `maxOrphanedCalls`**：`trimForOrphanBudget` 已使用。
- [x] **Reactive compact**：`think_stage.go` `maybeReactiveCompact()` 在 80% context window 时触发。
- [x] **hooks typed payload**：`Payload.TypedData` + `AsTyped[T]()` + `NewPayload()`；Fire 调用点已迁移。
- [x] **同步 Hook 可阻断/改参**：`applyToolHookResults` 处理 `Block`/`Modify`。
- [x] **`permissions` RuleMatcher args 匹配**：`governor.go` `ruleMatches` 通过 `ArgKey`/`ArgValue` 做 regex+glob。
- [x] **`hooks` Matcher.RunKind 全链路**：`matchingHooks` 检查 `RunKind`。
- [x] **流式/分区工具执行**：`tool_stage.go` `executePartitioned` + `executeParallel`。
- [x] **PreToolUse / PostToolUse hook 接入主链路**。
- [x] **PreThink / PostThink hooks**。
- [x] **SessionStart / SessionEnd hooks**。
- [x] **PreCompact / PostCompact / OnCompact hooks**（`compactMessages` + **`pruneMessages`** 闭包）。
- [x] **UserPromptSubmit hook**（`runViaPipeline` 入口触发，支持 block/modify）。
- [x] **ToolConcurrencySafe 输入感知**：签名 `func(string, map[string]any) bool`。
- [x] **Loop 使用 `PGSpiritProfileStore`**。
- [x] **`RecordFeedback`**：`POST /v1/feedback` → `LearningLoop`。
- [x] **`Orchestrator` + `SpiritDelegateRunFn`** 主链路。
- [x] **LLM `IntentClassifier`**。
- [x] **权限 Batch**：`DecisionSource`、`ResolveApproval` headless deny、speculative classify、`EventPermissionModeChange`。
- [x] **SDK**：`PermissionResultData.Source`、`pkg/sdk/frame.go`（Gateway 帧对齐）。
- [x] **压缩**：`FoldLog`/`FoldEntry`、`CompactBoundary`/`CompactMeta`、**`compressionCompactor`**（有 provider 时）。
- [x] **Interrupt 类型 + 执行上下文**：`InterruptBehaviorTool`、`toolExecutionContext`（`WithoutCancel` for block）、`read_file` → `InterruptBlock`；**并行软中断**：`Router`/`RunRequest`/`PipelineDeps` 的 `InterruptCh`，`executeParallel` 的 `batchCtx`。
- [x] **子代理 hook**：`delegateAgentExecutor` + **`delegate` 工具** + `hooks.WithHookFire` + `ToolRunID`。
- [x] **配置链**：`config.MergeChain`、`effectiveDreamWeaver` 用户/工作区层。
- [x] **HandoffReview / memory.Layer**（辅助项）。
- [x] **插件 MCP**：`ConnectPluginMCPServers` + `resolver` 注入 `MCPManager`（含 **pool-only** 无 `MCPStore` 场景）；**MCP Bridge** 工具列表热刷新；**Plugin transport** `executeViaStdio`/`executeViaHTTP`/`executeViaMCP` 分派。
- [x] **Transcript 多视图**：`RunState.Transcript` + `FinalizeStage` 在 run 结束时构建（`ForUI`/`ForAPI`/`ForResume`）；JSONL 改为 typed `{ts,run_id,kind,data}` 增量追加。
- [x] **Session fork hook**：`hooks.EventSessionFork` + `SessionForkPayload`；`SessionForkHandler.hookFire` 可选注入。
- [x] **项目规则分层**：`loadProjectRuleFiles` user/project 双层（`~/.goclaw/` + workspace）。

</details>

---

# 未实现条目 — 确认状态 & 实现指南

> **优先看上文「实现进度速查（2026-04-13）」表**：下列各节保留历史「确认」与参考实现片段，部分条目**已完成或部分完成**，以速查表为准，勿仅读本节首段「确认」。

> 每个条目包含：**确认结论**（对照代码的真实状态）、**涉及文件**、**实现方案**（可直接编码的步骤）、**验收标准**。

---

## P0 — 运行时核心与安全

### P0-1. ExecTool 实现 `ConcurrencySafeWithArgsTool` 接口

**确认**：`ConcurrencySafeWithArgsTool` 接口已定义于 `internal/tools/types.go:32-34`，`Registry.IsConcurrencySafeWithArgs` 已在 `registry.go` 中实现，pipeline 已通过 `deps.ToolConcurrencySafe(toolName, args)` 调用。但仓库中**无任何工具实现此接口**。`ExecTool`（`shell.go`）当前的元数据推断为 `CapMutating`，因此所有 exec 调用均串行执行。

**涉及文件**：
- `internal/tools/shell.go` — 主要修改
- `internal/tools/shell_deny_groups.go` — 参考 deny pattern

**实现方案**：

```go
// internal/tools/shell.go — 在 ExecTool 上添加方法

// readOnlyCommandPrefixes lists command prefixes that only read state.
var readOnlyCommandPrefixes = []string{
    "ls", "dir", "cat", "head", "tail", "wc", "find", "grep", "rg",
    "which", "where", "type", "file", "stat", "du", "df",
    "echo", "printf", "date", "whoami", "hostname", "uname",
    "pwd", "env", "printenv", "git status", "git log", "git diff",
    "git show", "git branch", "go version", "go list", "node -v",
    "npm list", "python --version", "pip list",
}

// IsConcurrencySafeWithArgs implements tools.ConcurrencySafeWithArgsTool.
// Returns true when the command is read-only (safe for parallel execution).
func (t *ExecTool) IsConcurrencySafeWithArgs(args map[string]any) bool {
    command, _ := args["command"].(string)
    if command == "" {
        return false
    }
    cmd := strings.TrimSpace(command)
    for _, prefix := range readOnlyCommandPrefixes {
        if cmd == prefix || strings.HasPrefix(cmd, prefix+" ") {
            return true
        }
    }
    // Pipe chains: only safe if every segment is read-only
    if strings.Contains(cmd, "|") {
        parts := strings.Split(cmd, "|")
        for _, part := range parts {
            sub := &ExecTool{}
            if !sub.IsConcurrencySafeWithArgs(map[string]any{"command": strings.TrimSpace(part)}) {
                return false
            }
        }
        return true
    }
    return false
}
```

**验收标准**：
- `go test ./internal/tools/ -run TestExecConcurrencySafe` 通过
- 编写测试用例覆盖：`ls -la`→true, `rm -rf /`→false, `cat foo | grep bar`→true, `cat foo | rm bar`→false, `""`→false
- pipeline 集成测试：两个并行 `exec ls` 调用确实并行执行

---

### P0-2. Safety Layer 默认规则集（bypass-immune 红线）

**确认**：`Governor` 的 `SafetyChecker` 接口已定义（`governor.go:141-148`），`Evaluate` 中第 4 层调用了 `safetyCheck`（`governor.go:237-248`）。但 `SetSafetyCheck` **从未被调用**（仅在 `governor.go:304-308` 定义），`loop_dreamweaver.go` 中 `NewGovernor()` 后只调了 `SetAuditStore`。

**涉及文件**：
- `internal/permissions/safety.go`（新文件）— 默认安全检查器
- `internal/agent/loop_dreamweaver.go` — 注册安全检查器

**实现方案**：

```go
// internal/permissions/safety.go

package permissions

import (
    "context"
    "regexp"
    "strings"
)

// dangerousPathPatterns are bypass-immune: even autonomous mode cannot override.
var dangerousPathPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)(^|/)\.env(\.|$)`),
    regexp.MustCompile(`(?i)(^|/)\.ssh/`),
    regexp.MustCompile(`(?i)(^|/)\.gnupg/`),
    regexp.MustCompile(`(?i)(^|/)\.aws/(credentials|config)`),
    regexp.MustCompile(`(?i)(^|/)(id_rsa|id_ed25519|.*\.pem|.*\.key)$`),
    regexp.MustCompile(`(?i)/etc/(passwd|shadow|sudoers)`),
    regexp.MustCompile(`(?i)(^|/)\.kube/config`),
}

// dangerousCommands are bypass-immune: exec tool cannot run these.
var dangerousExecPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)\bcurl\b.*\|\s*(ba)?sh`),   // pipe curl to shell
    regexp.MustCompile(`(?i)\bwget\b.*\|\s*(ba)?sh`),
    regexp.MustCompile(`(?i)\bchmod\b.*\+s\b`),          // setuid
    regexp.MustCompile(`(?i)\bsudo\b`),
    regexp.MustCompile(`(?i)\bdd\b.*\bof=/dev/`),
    regexp.MustCompile(`(?i)\bmkfs\b`),
    regexp.MustCompile(`(?i)\b(rm|del)\b.*(-rf|/s|/q).*(/|\\)(etc|usr|windows|system32)`),
}

// DefaultSafetyChecker returns a SafetyChecker that enforces bypass-immune
// rules on file paths and shell commands.
func DefaultSafetyChecker() SafetyChecker {
    return func(ctx context.Context, req PermissionRequest, class *SecurityClassification) (*SafetyVerdict, error) {
        // Check file path arguments
        for _, key := range []string{"path", "file_path", "file", "target"} {
            if val, ok := req.Arguments[key].(string); ok {
                for _, pat := range dangerousPathPatterns {
                    if pat.MatchString(val) {
                        return &SafetyVerdict{
                            Action: ActionDeny,
                            Reason: "bypass-immune: access to sensitive path " + val,
                        }, nil
                    }
                }
            }
        }
        // Check exec commands
        if req.ToolName == "exec" || req.ToolName == "shell" || req.ToolName == "bash" {
            cmd, _ := req.Arguments["command"].(string)
            for _, pat := range dangerousExecPatterns {
                if pat.MatchString(cmd) {
                    return &SafetyVerdict{
                        Action: ActionDeny,
                        Reason: "bypass-immune: dangerous command pattern blocked",
                    }, nil
                }
            }
        }
        return nil, nil // pass — no safety concern
    }
}
```

**注册（`loop_dreamweaver.go`）**：

```go
// 在 NewGovernor() 之后添加：
l.governor.SetSafetyCheck(permissions.DefaultSafetyChecker())
```

**验收标准**：
- `go test ./internal/permissions/ -run TestDefaultSafety` — 覆盖 `.env` 路径 deny、`sudo rm` deny、普通 `ls` pass
- autonomous 模式下执行 `exec cat ~/.ssh/id_rsa` 仍被 deny（bypass-immune 验证）
- 不影响正常工具执行的回归测试

---

### P0-3. 工具 interruptBehavior（中断策略接口）

**状态（2026-04）**：✅ **全部完成**：`InterruptBehaviorTool`、`Registry.InterruptBehaviorFor`、`PipelineDeps.ToolInterruptBehavior`、`toolExecutionContext`（WithoutCancel）、`read_file` 已标 `InterruptBlock`；**新增**：`ActiveRun.InterruptCh`（容量 1），`Router.InjectMessage` 注入消息时非阻塞发信号；`PipelineDeps.InterruptCh` + `RunRequest.InterruptCh`；`executeParallel` 创 `batchCtx`（`WithCancel`），goroutine 监听 `InterruptCh` → `cancelBatch()`，只中断 cancel 组，block 组以 `WithoutCancel` 跑完；pipeline 测试覆盖软中断场景。

**确认（历史）**：曾无中断策略；现已可通过 registry + 执行上下文区分 cancel/block。

**涉及文件**：
- `internal/tools/types.go` — 新增接口
- `internal/pipeline/tool_stage.go` — 消费接口
- `internal/agent/loop_pipeline_adapter.go` — 传递中断信号

**实现方案**：

```go
// internal/tools/types.go — 新增接口

// InterruptBehavior defines what happens when a user sends a new message
// while this tool is executing.
type InterruptBehavior string

const (
    InterruptCancel InterruptBehavior = "cancel" // cancel execution immediately
    InterruptBlock  InterruptBehavior = "block"  // block new input until tool completes
)

// InterruptBehaviorTool declares how the tool handles user interrupts.
// Tools that don't implement this default to InterruptCancel.
type InterruptBehaviorTool interface {
    GetInterruptBehavior() InterruptBehavior
}
```

```go
// internal/pipeline/deps.go — 新增回调

type PipelineDeps struct {
    // ... existing fields ...
    // ToolInterruptBehavior returns the interrupt strategy for a tool.
    // Returns InterruptCancel by default when nil or tool not found.
    ToolInterruptBehavior func(toolName string) string
}
```

```go
// internal/pipeline/tool_stage.go — executePartitioned 中处理

// When a user interrupt signal arrives during parallel execution:
// 1. Tools with InterruptCancel: cancel their context immediately
// 2. Tools with InterruptBlock: let them finish, queue the new message
// Implementation: wrap each parallel tool's context with a derived cancelFunc,
// only cancel the "cancel" group when interrupt arrives.
```

**验收标准**：
- `ExecTool` 默认 `InterruptCancel`
- `read_file` 等只读工具实现 `InterruptBlock`（文件读取几乎瞬间完成，不应被取消）
- 单元测试：模拟中断信号时，cancel 组被取消而 block 组运行完毕

---

### P0-4. 工具取消 Synthetic Error Tool Result

**确认**：工具被 hook block 或权限 deny 时，返回的 `providers.Message` 是 `Content: "[hook blocked] " + reason`（`loop_pipeline_adapter.go:339-344`）或 `"[permission denied] " + reason`（`loop_pipeline_adapter.go:314-318`），均为普通字符串。LLM 无法从结构上区分「工具出错」和「工具被外部阻止」。

**涉及文件**：
- `internal/agent/loop_pipeline_adapter.go` — 修改阻止/拒绝消息格式

**实现方案**：

```go
// internal/agent/loop_pipeline_adapter.go

// syntheticToolError generates a structured error message that helps the LLM
// understand why a tool was not executed and adjust its reasoning.
func syntheticToolError(category, toolName, reason string) string {
    return fmt.Sprintf(
        "<tool_use_error>\n<category>%s</category>\n<tool>%s</tool>\n<reason>%s</reason>\n"+
        "<guidance>This tool call was blocked by the system. Do not retry the same "+
        "call. Consider an alternative approach or inform the user.</guidance>\n</tool_use_error>",
        category, toolName, reason,
    )
}
```

修改两处返回点：

```go
// Permission deny (line ~314):
return []providers.Message{{
    Role:       "tool",
    Content:    syntheticToolError("permission_denied", tc.Name, decision.Reason),
    ToolCallID: tc.ID,
    IsError:    true,
}}, nil

// Hook block (line ~339):
return []providers.Message{{
    Role:       "tool",
    Content:    syntheticToolError("hook_blocked", tc.Name, reason),
    ToolCallID: tc.ID,
    IsError:    true,
}}, nil
```

**验收标准**：
- LLM 收到 `<tool_use_error>` 后不再重试同一调用（可通过集成测试验证）
- `TestApplyToolHookResults_ModifyAndBlock` 更新后通过
- 向后兼容：`IsError: true` 仍然设置

---

### P0-5. 压缩多级策略串行施压（已实现，需纠正 TODO 描述）

**状态（2026-04）**：🟡 **多级施压 + Compactor 已接线**。`CompressToBudget` 链不变；`initDreamWeaverServices` 在 `l.provider != nil` 时传入 `compressionCompactor`（`internal/agent/loop_compact.go`，与 `compactionSummaryPrompt` 一致）。`provider == nil` 时仍为 `nil` compactor，L3/L4 走占位 fallback。

**确认**：**此条目实际已实现**。`internal/compression/levels.go` 的 `CompressToBudget` 已实现 L0→L1→L2→L3→L4 五级串行降载链。原 TODO 误写「单次压缩」。

**剩余差距**：摘要质量依赖模型与 prompt；无 LLM 时仍为占位 system 消息。

**涉及文件（已实现）**：
- `internal/agent/loop_dreamweaver.go` — `compactor := l.compressionCompactor`（`provider != nil`）
- `internal/agent/loop_compact.go` — `compressionCompactor`

**验收标准**（保留作回归）：
- L3/L4 在 **有 provider** 时走 LLM 摘要路径
- 摘要保留关键信息（文件路径、决策、待办）
- token 消耗在 budget 内

---

### P0-6. `hooks` typed payload 全面替换 `map[string]any`

**状态（2026-04）**：✅ **已落地**：`Payload.TypedData any` + `AsTyped[T]()` + `NewPayload()`；`Data` 与 `TypedData` 双字段保持兼容；所有 `Fire` 调用点已迁移。

**确认（历史）**：原先仅 `Data map[string]any` 时，下游需从 map 断言字段；现已可同时使用 `TypedData`。

**涉及文件**：
- `internal/hooks/events.go` — `Payload.Data` 类型变更
- `internal/hooks/registry.go` — `Fire` 方法适配
- `internal/agent/loop_pipeline_adapter.go` — 所有 `Fire` 调用点

**实现方案**：

方案采用**渐进式双字段策略**，避免一次性破坏所有消费方：

```go
// internal/hooks/events.go

type Payload struct {
    Event      Event          `json:"event"`
    RunID      string         `json:"run_id"`
    AgentID    string         `json:"agent_id"`
    SessionKey string         `json:"session_key"`
    Timestamp  time.Time      `json:"timestamp"`
    Data       map[string]any `json:"data,omitempty"`       // legacy, deprecated
    TypedData  any            `json:"typed_data,omitempty"` // strongly-typed payload
}

// Tool() returns the typed ToolPayload if this is a tool event.
func (p Payload) Tool() *ToolPayload {
    if tp, ok := p.TypedData.(*ToolPayload); ok { return tp }
    return nil
}

// Compact() returns the typed CompactPayload if this is a compact event.
func (p Payload) Compact() *CompactPayload {
    if cp, ok := p.TypedData.(*CompactPayload); ok { return cp }
    return nil
}
// ... accessor for each typed payload
```

Fire 调用点改为同时设置两个字段：

```go
tp := hooks.ToolPayload{ToolName: tc.Name, Arguments: tc.Arguments, ...}
l.hookRegistry.Fire(ctx, hooks.Payload{
    Event:     hooks.EventPreToolUse,
    TypedData: &tp,
    Data:      tp.AsMap(), // backward compat
    ...
})
```

**验收标准**：
- 新 hook handler 可通过 `payload.Tool()` 获取类型安全的 `*ToolPayload`
- 旧 handler 通过 `payload.Data["tool_name"]` 仍可正常工作
- JSON 序列化包含 `typed_data` 字段

---

### P0-7. Message 多视图 Transcript 替换 MessageBuffer 默认路径

**确认**：`message.Transcript` 类型已实现（`transcript.go`），支持 `ForUI`/`ForAPI`/`ForResume` 多视图派生。`NormalizeForUI`（`normalize.go:67-122`）和 `NormalizeForAPI`（`normalize.go:17-59`）已完整实现。但 pipeline 主路径使用 `pipeline.MessageBuffer`（`message_buffer.go`），`Transcript` 未集成到 pipeline 的 `RunState` 中。

**涉及文件**：
- `internal/pipeline/run_state.go` — 添加 Transcript 引用
- `internal/agent/loop_pipeline_adapter.go` — 在 run 结束时生成 Transcript 视图
- `internal/pipeline/finalize_stage.go` — 使用 Transcript 构建最终输出

**实现方案**：

```go
// internal/pipeline/run_state.go — 添加 Transcript 字段

type RunState struct {
    // ... existing fields ...
    // Transcript is the authoritative session record, updated alongside Messages.
    // When set, FinalizeStage derives views from it instead of Messages directly.
    Transcript *message.Transcript
}
```

在 `buildPipelineDeps` 中初始化：

```go
// loop_pipeline_adapter.go — runViaPipeline
state.Transcript = message.NewTranscript(req.SessionKey)
state.Transcript.Append(state.Messages.All()...)
```

在每次 `AppendPending` 和 `ReplaceHistory` 后同步更新 Transcript：

```go
// Option A: MessageBuffer 添加 observer 回调
type MessageBuffer struct {
    // ...
    onChange func(msgs []providers.Message)
}
// Option B: FinalizeStage 在结束时一次性从 Messages 构建 Transcript
```

推荐 **Option B**（侵入最小）：

```go
// internal/pipeline/finalize_stage.go
func (s *FinalizeStage) Execute(ctx context.Context, state *RunState) error {
    // ... existing logic ...
    if state.Transcript != nil {
        state.Transcript.Replace(state.Messages.All(), "")
    }
    return nil
}
```

**验收标准**：
- `state.Transcript.ForUI(runID, true)` 输出与 `NormalizeForUI(state.Messages.All(), true)` 一致
- API 端点可选择返回 `ViewTranscript` 或 `ViewUI` 视图
- 现有测试不回归

---

## P1 — 连续性与会话

### P1-1. Pre/Post Compact Hooks 在 Reactive Compact 路径触发

**状态（2026-04）**：✅ **`pruneMessages` 闭包已补** Pre/PostCompact（`Level: "prune"`），与 `compactMessages` 的 `full` 路径一致。

**确认（历史）**：曾仅有 `compactMessages` 带 hook；`pruneMessages` 走压缩引擎时不触发 — 已修复。

**涉及文件**：
- `internal/agent/loop_pipeline_adapter.go` — `pruneMessages` 闭包

**实现方案**：

```go
// loop_pipeline_adapter.go — 在 pruneMessages 闭包中也添加 hook 触发

pruneMessages := cb.pruneMessages
if l.compressionEnabledFor(req) && l.compressionEng != nil {
    pruneMessages = func(msgs []providers.Message, budget int) []providers.Message {
        if l.hooksEnabled() && l.hookRegistry != nil {
            _, _ = l.hookRegistry.Fire(ctx, hooks.Payload{
                Event: hooks.EventPreCompact,
                RunID: req.RunID, AgentID: l.id, SessionKey: req.SessionKey,
                Timestamp: time.Now(),
                Data: hooks.CompactPayload{Level: "prune", TokensBefore: len(msgs)}.AsMap(),
            })
        }
        result, compressed, err := l.compressionEng.CompressToBudget(
            context.Background(), msgs, budget)
        var out []providers.Message
        if err != nil || !compressed || result == nil {
            out = cb.pruneMessages(msgs, budget)
        } else {
            out = result.Messages
        }
        if l.hooksEnabled() && l.hookRegistry != nil {
            _, _ = l.hookRegistry.Fire(ctx, hooks.Payload{
                Event: hooks.EventPostCompact,
                RunID: req.RunID, AgentID: l.id, SessionKey: req.SessionKey,
                Timestamp: time.Now(),
                Data: hooks.CompactPayload{Level: "prune", TokensBefore: len(msgs), TokensAfter: len(out)}.AsMap(),
            })
        }
        return out
    }
}
```

**验收标准**：
- 注册 `EventPreCompact` hook，触发 prune 路径时 hook 被调用
- `Level` 字段区分 `"prune"` vs `"full"` vs `"reactive"`

---

### P1-2. JSONL Transcript 持久化

**确认**：**实际已部分实现**。`loop_dreamweaver.go` 的 `writeTranscriptJSONL`（line 278-300）将 transcript 写入 `{dataDir}/transcripts/{runID}.jsonl`。但这是按 runID 写的完整 transcript dump，不是 CC 的 append-only 事件流（sidechain transcript）。

缺口：每次工具调用/LLM 回复不是增量追加事件，而是重写整个 transcript。Subagent 没有独立的 sidechain transcript。

**涉及文件**：
- `internal/agent/loop_dreamweaver.go` — 改为增量追加
- `internal/agent/loop_pipeline_adapter.go` — subagent 独立 transcript

**实现方案**：

```go
// internal/agent/transcript_writer.go (新文件)

package agent

import (
    "encoding/json"
    "os"
    "sync"
    "time"
)

// TranscriptEvent is a single entry in the append-only JSONL event log.
type TranscriptEvent struct {
    Timestamp time.Time      `json:"ts"`
    RunID     string         `json:"run_id"`
    Kind      string         `json:"kind"` // "user", "assistant", "tool_call", "tool_result", "compact", "error"
    Data      map[string]any `json:"data"`
}

// TranscriptWriter appends events to a JSONL file incrementally.
type TranscriptWriter struct {
    mu   sync.Mutex
    path string
    file *os.File
    enc  *json.Encoder
}

func NewTranscriptWriter(path string) (*TranscriptWriter, error) {
    f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        return nil, err
    }
    return &TranscriptWriter{path: path, file: f, enc: json.NewEncoder(f)}, nil
}

func (w *TranscriptWriter) Append(evt TranscriptEvent) error {
    w.mu.Lock()
    defer w.mu.Unlock()
    return w.enc.Encode(evt)
}

func (w *TranscriptWriter) Close() error {
    w.mu.Lock()
    defer w.mu.Unlock()
    return w.file.Close()
}
```

集成点：在 `executeToolCall` 闭包中每次工具调用前后 `Append`，在 `callLLM` 回调中 `Append`。

**验收标准**：
- 每个 run 生成一个 JSONL 文件，每行是独立事件
- `cat transcript.jsonl | jq .kind` 输出 `user`, `assistant`, `tool_call`, `tool_result` 等
- subagent 有独立的 `{runID}-sub-{subagentID}.jsonl`

---

### P1-3. Compact Boundary 消息类型

**确认**：compact 后消息链中无边界标记。`MessageBuffer.ReplaceHistory` 直接替换 history 数组，`compression/levels.go` 的 L3 插入的 `[context collapse]` 是普通 system message，没有特殊类型标记。

**涉及文件**：
- `internal/providers/types.go` — Message 添加 compact metadata
- `internal/compression/levels.go` — L3/L4 插入 boundary marker
- `internal/pipeline/message_buffer.go` — ReplaceHistory 保留 marker

**实现方案**：

```go
// internal/providers/types.go — 添加字段

type Message struct {
    // ... existing fields ...
    // CompactBoundary marks this message as the start of a compacted region.
    // When set, resume logic can use it to locate the boundary between
    // pre-compact summary and post-compact live messages.
    CompactBoundary *CompactMeta `json:"compact_boundary,omitempty"`
}

type CompactMeta struct {
    Level           string    `json:"level"`           // "L3", "L4", "full"
    OriginalCount   int       `json:"original_count"`  // messages before compact
    CompactedAt     time.Time `json:"compacted_at"`
    SummaryTokens   int       `json:"summary_tokens"`
}
```

```go
// internal/compression/levels.go — applyL3 中设置 boundary

// 在插入 summary/collapse message 时：
boundaryMsg := providers.Message{
    Role:    "system",
    Content: summary,
    CompactBoundary: &providers.CompactMeta{
        Level:         "L3",
        OriginalCount: len(before),
        CompactedAt:   time.Now(),
    },
}
```

**验收标准**：
- compact 后的消息链包含 `CompactBoundary != nil` 的消息
- resume 引擎可通过 `CompactBoundary` 找到边界位置
- JSON 序列化/反序列化往返正确

---

### P1-4. Session Fork + SubagentStart/Stop Hook 触发

**状态（2026-04）**：🟡 **委派路径已覆盖**：`delegateAgentExecutor`、`tools/delegate_tool.go` 经 `hooks.WithHookFire` 发 `subagent.start/stop`（含 `ParentRunID`）。**未覆盖**：纯 `session_fork` HTTP 流程若需独立 hook，仍要另接。

**确认（历史）**：曾无 `Fire`；现已有多处。

**涉及文件**：
- `internal/tools/subagent_tracing.go` — 在 span start/stop 时 fire hook
- `internal/agent/loop_pipeline_adapter.go` — delegation 路径触发 hook

**实现方案**：

在 subagent 执行起点/终点添加 hook fire：

```go
// 在 subagent 创建/启动处（如 orchestrator.go 或 delegation 回调中）：

if l.hooksEnabled() && l.hookRegistry != nil {
    _, _ = l.hookRegistry.Fire(ctx, hooks.Payload{
        Event:      hooks.EventSubagentStart,
        RunID:      parentRunID,
        AgentID:    l.id,
        SessionKey: req.SessionKey,
        Timestamp:  time.Now(),
        Data: hooks.SubagentPayload{
            SubagentID:   subRunID,
            SubagentKind: "delegation", // or "team", "fork"
            ParentRunID:  parentRunID,
        }.AsMap(),
    })
}
// ... subagent runs ...
if l.hooksEnabled() && l.hookRegistry != nil {
    _, _ = l.hookRegistry.Fire(ctx, hooks.Payload{
        Event:      hooks.EventSubagentStop,
        RunID:      parentRunID,
        AgentID:    l.id,
        SessionKey: req.SessionKey,
        Timestamp:  time.Now(),
        Data: hooks.SubagentPayload{
            SubagentID:   subRunID,
            SubagentKind: "delegation",
            ParentRunID:  parentRunID,
        }.AsMap(),
    })
}
```

**验收标准**：
- 注册 `EventSubagentStart` hook，delegation 执行时 hook 被调用
- hook payload 包含 `subagent_id`, `subagent_kind`, `parent_run_id`

---

### P1-5. MCP Instructions 独立 System Section + Cache Break

**确认**：`systemprompt.go` 在 `CacheBoundaryMarker` 之后添加 `buildMCPToolsInlineSection`（line ~431-435）。MCP 工具描述被内联到 system prompt 中。但没有独立的 MCP `instructions` 资源字段（MCP 协议中每个 server 可声明 `instructions` 文本），也没有 cache boundary 在 MCP section 前后。

**涉及文件**：
- `internal/agent/systemprompt.go` — 拆分 MCP section
- `internal/agent/systemprompt_sections.go` — 构建独立 instructions block
- `internal/mcp/bridge_tool.go` — 提取 server instructions

**实现方案**：

```go
// internal/agent/systemprompt.go

// 在 buildSystemPrompt 中：
// 1. 静态 system prompt（cacheable）
// 2. CacheBoundaryMarker
// 3. MCP instructions section（独立 section，标注为动态）
// 4. MCP tool descriptions

// 新增 MCP instructions section:
func buildMCPInstructionsSection(servers []MCPServerInfo) string {
    var sb strings.Builder
    for _, s := range servers {
        if s.Instructions == "" {
            continue
        }
        sb.WriteString(fmt.Sprintf("\n## MCP Server: %s\n\n%s\n", s.Name, s.Instructions))
    }
    if sb.Len() == 0 {
        return ""
    }
    return "\n<mcp_instructions>\n" + sb.String() + "</mcp_instructions>\n"
}
```

**验收标准**：
- MCP server 带 `instructions` 字段时，独立于工具描述注入
- 缓存边界在 instructions 前，使得工具描述变更不 invalidate 静态 prompt 缓存

---

### P1-6. 项目规则自动发现与分层

**确认**：`loadProjectRuleFiles`（`loop_dreamweaver.go` ~635-688）已实现 `CLAUDE.md` + `.claude/*.md` 自动发现。但仅支持单层（workspace root），不支持多级目录（如 parent dir、home dir）或与 plugin/user config 合并。

**涉及文件**：
- `internal/agent/loop_dreamweaver.go` — 扩展 `loadProjectRuleFiles`

**实现方案**：

```go
// 扩展搜索路径：
func (l *Loop) loadProjectRuleFiles() string {
    searchDirs := []struct {
        dir   string
        label string
    }{
        {filepath.Join(os.Getenv("HOME"), ".goclaw"), "user"},
        {l.workspace, "project"},
        // future: plugin rules via registry
    }
    var sections []string
    for _, sd := range searchDirs {
        rules := l.loadRulesFromDir(sd.dir)
        if rules != "" {
            sections = append(sections, fmt.Sprintf("### %s rules\n%s", sd.label, rules))
        }
    }
    if len(sections) == 0 {
        return ""
    }
    return "## Project Rules\n\n" + strings.Join(sections, "\n\n")
}
```

**验收标准**：
- `~/.goclaw/CLAUDE.md` 规则被注入为 "user rules"
- workspace `CLAUDE.md` 规则被注入为 "project rules"
- project rules 优先级高于 user rules

---

## P2 — 扩展与 MCP

### P2-1. Plugin 真实 Tool Dispatch

**确认**：**已实现**。`Registry.ExecuteTool`（`plugins/registry.go`）通过 `exec.CommandContext` 启动插件 entrypoint，发送 JSON 请求，解析 stdout 为 `tools.Result`。`pluginRuntimeTool` 在 `loop_dreamweaver.go` 中调用 `registry.ExecuteTool`。

**状态**：~~此条目应标记为已实现~~。需验证非 stdio transport（http/mcp）是否也已实现。

**真实缺口**：仅 `stdio` transport 已实现；`http` 和 `mcp` transport 的 `ExecuteTool` 走相同 stdio 路径。

**涉及文件**：
- `internal/plugins/registry.go` — 添加 HTTP/MCP transport support

**实现方案**：

```go
// internal/plugins/registry.go — ExecuteTool 中按 transport 分派

func (r *Registry) ExecuteTool(ctx context.Context, pluginName, toolName string, args map[string]any) (*tools.Result, error) {
    inst, err := r.get(pluginName)
    if err != nil { return nil, err }

    switch inst.Manifest.Transport {
    case "stdio":
        return r.executeViaStdio(ctx, inst, toolName, args)
    case "http":
        return r.executeViaHTTP(ctx, inst, toolName, args)
    case "mcp":
        return r.executeViaMCP(ctx, inst, toolName, args)
    default:
        return nil, fmt.Errorf("unsupported transport: %s", inst.Manifest.Transport)
    }
}

func (r *Registry) executeViaHTTP(ctx context.Context, inst *Instance, toolName string, args map[string]any) (*tools.Result, error) {
    // POST to inst.Manifest.Entrypoint (URL) with JSON body
    // Parse response as tools.Result
}

func (r *Registry) executeViaMCP(ctx context.Context, inst *Instance, toolName string, args map[string]any) (*tools.Result, error) {
    // Use MCP client protocol to invoke tool on the plugin's MCP server
    // Reuse internal/mcp/pool.go for connection management
}
```

**验收标准**：
- HTTP transport plugin：POST `{"tool":"name","arguments":{...}}` 到 entrypoint URL
- MCP transport plugin：通过 MCP protocol invoke tool
- stdio transport 行为不变

---

### P2-2. Plugin Manifest MCP Servers 自动注册

**状态（2026-04）**：🟡 **已实现（Manager + Acquire 契约）**：`internal/mcp/plugin_servers.go` 中 `ConnectPluginMCPServers` / `DisconnectPluginMCPServers`；逻辑 server 名 `plugin:<plugin>:<decl>`；有 `MCPPool` 时 `connectViaPool` → `Pool.Acquire`；`resolver` 注入 `Loop.MCPManager`；`loop_dreamweaver` OnActivate/OnDeactivate 调用。**条件**：agent 须走带 `MCPStore` 的解析路径以创建 `MCPManager`；否则 `mcpManager == nil`，插件 MCP 不连接。

**确认（历史）**：曾仅 JSON 声明；现已运行时注册。

**验收标准**（对齐实现）：
- activate 时 `ConnectPluginMCPServers` 成功则工具进 registry
- 已连接同名逻辑 server 时跳过（见 `m.servers` 检查）
- deactivate 时 `DisconnectPluginMCPServers` 卸载工具并 `pool.Release`

---

### P2-3. goclaw 作为 MCP Server 对外暴露

**确认**：`internal/gateway/server.go` 已挂载 `/mcp/bridge`（MCP streamable HTTP via mcp-go 库）。`bridge_sse.go` 是 SSE EventFrame 推送（`/v1/bridge/events`），不是 MCP 协议。MCP bridge 已对外暴露 tool listing（由 mcp-go 库自动处理），但工具列表的注册来源需确认。

**现状**：基本已实现。缺口是**动态工具注册同步**——当工具在运行时增减时，MCP bridge 的 tool listing 需要更新。

**涉及文件**：
- `internal/mcp/bridge_server.go` — 工具同步机制

**实现方案**：

```go
// internal/mcp/bridge_server.go — 添加 RefreshTools 方法

func (s *BridgeServer) RefreshTools(tools []mcp.Tool) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.tools = tools
    // Notify connected clients about tool list change (MCP protocol notification)
    s.server.NotifyToolsChanged()
}
```

在 plugin activate/deactivate 时调用 `RefreshTools`。

---

### P2-4. SDK 与 Gateway EventFrame 对齐

**确认**：`pkg/sdk/events.go` 定义 `Event{Type, RunID, Data, ...}`；`pkg/protocol/frames.go` 定义 `EventFrame{Type, Event, Payload, Seq, ...}`。SSE client（`sse_client.go`）接收 `json.RawMessage` 而不是 `sdk.Event`。两个模型**字段名和结构不同**。

**涉及文件**：
- `pkg/sdk/sse_client.go` — 添加 `EventFrame → sdk.Event` 转换
- `pkg/protocol/frames.go` — 添加转换方法

**实现方案**：

```go
// pkg/sdk/sse_client.go — 添加 EventFrame 解析

// ParseEventFrame converts a gateway EventFrame into an sdk.Event.
func ParseEventFrame(raw json.RawMessage) (*Event, error) {
    var frame struct {
        Type    string          `json:"type"`
        Event   string          `json:"event"`
        Payload json.RawMessage `json:"payload"`
        Seq     int64           `json:"seq"`
        RunID   string          `json:"run_id"`
    }
    if err := json.Unmarshal(raw, &frame); err != nil {
        return nil, err
    }
    return &Event{
        Type:     EventType(frame.Event),
        RunID:    frame.RunID,
        Sequence: frame.Seq,
        Data:     frame.Payload, // caller can further unmarshal
    }, nil
}
```

**验收标准**：
- `SSEClient.StreamEvents` 返回 `sdk.Event` 而非 `json.RawMessage`
- 字段映射：`frame.event → event.Type`, `frame.seq → event.Sequence`
- round-trip 测试通过

---

## P3 — 权限与多 Agent

### P3-1. Permission 模式状态机 SDK 事件广播

**确认**：`Governor.SwitchMode`（`governor.go:460-473`）更新 `g.modes` map 并写日志，**不广播事件**。`pkg/sdk/events.go` 中没有 `EventPermissionModeChange`。

**涉及文件**：
- `pkg/sdk/events.go` — 添加事件类型
- `internal/permissions/governor.go` — SwitchMode 添加回调
- `internal/agent/loop_pipeline_adapter.go` — 连接回调到 SDK bridge

**实现方案**：

```go
// pkg/sdk/events.go
const EventPermissionModeChange EventType = "permission.mode_change"

type PermissionModeChangeData struct {
    AgentID  string `json:"agent_id"`
    OldMode  string `json:"old_mode"`
    NewMode  string `json:"new_mode"`
}
```

```go
// internal/permissions/governor.go — 添加 OnModeChange 回调

type Governor struct {
    // ... existing fields ...
    onModeChange func(agentID string, oldMode, newMode AgentMode)
}

func (g *Governor) OnModeChange(fn func(agentID string, oldMode, newMode AgentMode)) {
    g.mu.Lock()
    defer g.mu.Unlock()
    g.onModeChange = fn
}

func (g *Governor) SwitchMode(agentID string, mode AgentMode) {
    g.mu.Lock()
    old := g.modes[agentID]
    g.modes[agentID] = mode
    cb := g.onModeChange
    g.mu.Unlock()
    slog.Info("permission mode switched", "agent", agentID, "from", old, "to", mode)
    if cb != nil {
        cb(agentID, old, mode)
    }
}
```

```go
// loop_pipeline_adapter.go — 注册回调
l.governor.OnModeChange(func(agentID string, oldMode, newMode permissions.AgentMode) {
    if l.sdkBridgeEnabled() && l.sdkBridge != nil {
        l.sdkBridge.Emit(sdk.Event{
            Type: sdk.EventPermissionModeChange,
            Data: sdk.PermissionModeChangeData{
                AgentID: agentID,
                OldMode: string(oldMode),
                NewMode: string(newMode),
            },
        })
    }
})
```

**验收标准**：
- `SwitchMode("agent1", "autonomous")` 触发 SDK 事件
- SSE 客户端收到 `permission.mode_change` 事件

---

### P3-2. Headless Permission Path（Hook 决策替代人工审批）

**确认**：`ResolveApproval`（`governor.go:270-295`）当 `ApprovalFunc == nil` 时直接返回原 decision（`ActionAsk` 保持不变）。Pipeline 中对 `ActionAsk` 只处理了 SDK 广播和等待审批，如果审批后 decision 仍是 Ask 则**继续执行**（无阻止），这是一个安全隐患。

**涉及文件**：
- `internal/permissions/governor.go` — 添加 hook-based headless fallback
- `internal/agent/loop_pipeline_adapter.go` — Ask 未解决时 deny

**实现方案**：

```go
// internal/permissions/governor.go

// ResolveApproval converts an ask decision into allow/deny.
// In headless mode (ApprovalFunc == nil), fires hooks to let automation decide.
func (g *Governor) ResolveApproval(ctx context.Context, req PermissionRequest, dec *PermissionDecision) (*PermissionDecision, error) {
    if dec == nil || dec.Action != ActionAsk {
        return dec, nil
    }
    // Try interactive approval first
    if g.ApprovalFunc != nil {
        approved, err := g.ApprovalFunc(ctx, req, dec.Classification)
        if err != nil { return nil, err }
        if approved {
            dec.Action = ActionAllow
            dec.Reason = "user approved"
        } else {
            dec.Action = ActionDeny
            dec.Reason = "user denied"
        }
        return dec, nil
    }
    // Headless fallback: fire hooks for automated decision
    if g.headlessHook != nil {
        verdict, err := g.headlessHook(ctx, req, dec)
        if err != nil { return nil, err }
        if verdict != nil {
            dec.Action = verdict.Action
            dec.Reason = "headless hook: " + verdict.Reason
            return dec, nil
        }
    }
    // No approval available: deny by default (secure default)
    dec.Action = ActionDeny
    dec.Reason = "no approval mechanism available (headless mode)"
    return dec, nil
}
```

**验收标准**：
- headless 模式下 `ActionAsk` 不再保持 Ask 不变（deny by default）
- headless hook 可配置允许特定工具（如 read_file）
- 有 ApprovalFunc 时行为不变

---

### P3-3. Permission 决策来源标签

**确认**：`PermissionDecision` 无 `Source` 字段（`governor.go:79-89`），来源仅通过 `Reason` 字符串隐含。

**涉及文件**：
- `internal/permissions/governor.go` — 添加 Source 字段

**实现方案**：

```go
// internal/permissions/governor.go

type DecisionSource string

const (
    SourceRule           DecisionSource = "rule"
    SourceToolChecker    DecisionSource = "tool_checker"
    SourceSafety         DecisionSource = "safety"
    SourceMode           DecisionSource = "mode"
    SourceHook           DecisionSource = "hook"
    SourceClassifier     DecisionSource = "classifier"
    SourceUserTemporary  DecisionSource = "user_temporary"
    SourceUserPermanent  DecisionSource = "user_permanent"
    SourceHeadlessHook   DecisionSource = "headless_hook"
)

type PermissionDecision struct {
    // ... existing fields ...
    Source DecisionSource `json:"source"` // 新增
}
```

在 `Evaluate` 的每个 return 点设置 `Source`：

```go
// Layer 1a: deny rules
dec := g.newDecision(req, mode, class, ActionDeny, reason, rule)
dec.Source = SourceRule  // 新增
return dec, nil

// Layer 4: safety
dec.Source = SourceSafety

// Layer 5: mode
dec.Source = SourceMode
```

**验收标准**：
- 每个 `PermissionDecision` 都有非空 `Source`
- audit log 可按 Source 分类统计
- SDK PermissionResult 事件包含 Source

---

### P3-4. Speculative Classifier（并行预热）

**确认**：`Governor.Evaluate` 完全串行（`governor.go:165-267`），classifier 在 Layer 3 同步执行。

**涉及文件**：
- `internal/permissions/governor.go` — 并行预热 classifier

**实现方案**：

```go
// 在 Evaluate 开头启动 classifier goroutine：
func (g *Governor) Evaluate(ctx context.Context, req PermissionRequest) (*PermissionDecision, error) {
    // Speculative classification: start in background while checking rules
    type classResult struct {
        class *SecurityClassification
    }
    classCh := make(chan classResult, 1)
    go func() {
        classCh <- classResult{class: g.classifier.Classify(req.ToolName, req.Arguments)}
    }()

    // Layer 4a: Pre-hooks (can run while classifier is computing)
    for _, h := range hooks { ... }

    // Layer 1a: deny rules (can short-circuit before classifier completes)
    if dec := g.evaluateRules(req, nil, ActionDeny, false); dec != nil {
        return dec, nil // classifier result not needed
    }

    // Now wait for classifier result
    cr := <-classCh
    class := cr.class
    // ... rest of evaluation using class ...
}
```

注意：当前 `Classify` 是纯同步 heuristic（`classifier.go:27-42`），开销极小。此优化主要为将来 LLM-based classifier 铺路。

**验收标准**：
- deny rule 短路时不等待 classifier 完成
- 整体 Evaluate 延迟不增加
- 为将来 LLM classifier 预留并行窗口

---

## P4 — 生态

### P4-1. Marketplace 制品校验/签名

**实现方案**（概述）：
1. 发布时对 tar.gz 计算 SHA256 摘要，用 ed25519 私钥签名
2. 上传时存储 `{digest, signature, pubkey_fingerprint}` 到 `marketplace_catalog` 表
3. 安装时下载制品后验证 digest + signature
4. `internal/marketplace/verify.go` 新文件实现 `VerifyArtifact(path string, sig ArtifactSignature) error`

### P4-2. Alchemy HTTP API

**实现方案**（概述）：
1. `internal/http/alchemy.go` 新建 `POST /v1/alchemy/generate` 端点
2. 接收 `{template, variables, options}` JSON body
3. 调用 `internal/alchemy/generator.go` 的 `Generate` 方法
4. 沙箱执行：生成的代码在 Docker container 中测试后再返回

### P4-3. Workshop 强 Schema 校验

**实现方案**（概述）：
1. `internal/workshop/schema.go` 已存在 — 添加 JSON Schema validation（使用 `santhosh-tekuri/jsonschema`）
2. `publisher.go` 在 `Publish` 前调用 `ValidateManifest(manifest)`
3. 校验失败返回结构化错误列表

---

## P5 — 精灵

### P5-1. 独立精灵对话入口 + 多精灵

**实现方案**（概述）：
1. `internal/http/spirit.go` 新建 `POST /v1/spirit/chat` 端点（独立于主 agent 对话）
2. 使用独立 session key `spirit:{spiritID}:{userID}`
3. 多精灵支持：`PGSpiritProfileStore` 已按 agent_id 分区，扩展为支持多个 spirit profile

---

## P6 — 配置与企业

### P6-1. 分层 Config 合并

**确认**：`MergeDreamWeaverConfig`（`merge_dreamweaver.go`）实现了两层 deep merge（global + workspace），但缺少 plugin 层、user home 层。

**涉及文件**：
- `internal/config/merge_dreamweaver.go` — 扩展合并链
- `internal/agent/loop_dreamweaver.go` — `effectiveDreamweaver` 调用链

**实现方案**：

```go
// internal/config/merge_dreamweaver.go

// MergeChain applies configs in priority order (later wins):
// defaults → user home → workspace → plugin overrides → runtime
func MergeChain(configs ...*DreamWeaverConfig) *DreamWeaverConfig {
    var result *DreamWeaverConfig
    for _, c := range configs {
        result = MergeDreamWeaverConfig(result, c)
    }
    return result
}
```

```go
// loop_dreamweaver.go — effectiveDreamweaver

func (l *Loop) effectiveDreamweaver() *config.DreamWeaverConfig {
    userHome := loadUserHomeDreamweaver()    // ~/.goclaw/dreamweaver.json
    workspace := loadWorkspaceDreamweaver()  // {workspace}/.goclaw/dreamweaver.json
    pluginOverrides := l.collectPluginConfigs()
    return config.MergeChain(
        l.dreamweaverCfg,  // defaults
        userHome,
        workspace,
        pluginOverrides,
    )
}
```

**验收标准**：
- 4 层 config 按优先级合并
- plugin 可覆盖 workspace 设置
- `MergeChain(nil, nil, nil)` 不 panic

---

### P6-2. Settings Watcher 热更新

**确认**：`internal/config/hotreload.go` 已有 `Watcher`（fsnotify），用于主 config 文件和 skills。但 DreamWeaver config 不参与 hot-reload。

**涉及文件**：
- `internal/agent/loop_dreamweaver.go` — 注册 DreamWeaver config watcher

**实现方案**：

```go
// loop_dreamweaver.go — 在 init/start 中注册 watcher

func (l *Loop) watchDreamWeaverConfig() {
    paths := []string{
        filepath.Join(l.workspace, ".goclaw", "dreamweaver.json"),
        filepath.Join(os.Getenv("HOME"), ".goclaw", "dreamweaver.json"),
    }
    for _, p := range paths {
        l.configWatcher.Watch(p, func() {
            newCfg := l.effectiveDreamweaver()
            l.mu.Lock()
            l.activeDreamweaver = newCfg
            l.mu.Unlock()
            slog.Info("dreamweaver config hot-reloaded", "path", p)
        })
    }
}
```

**验收标准**：
- 修改 `.goclaw/dreamweaver.json` 后，下一次 run 使用新配置
- 无需重启 agent

---

## 从 CC 源码分析新增的遗漏条目

### CC-1. Context Collapse 可追踪折叠提交

**确认**：`compression/levels.go` 的 `Result` 记录了 `LevelsApplied`, `TokensBefore/After`, `TruncatedTools`, `CompactedPrefix`——但**无法重建被折叠的消息**。一旦 `applyL3` 替换消息，原始内容丢失。

**实现方案**：

```go
// internal/compression/levels.go — Result 添加 fold log

type FoldEntry struct {
    Level        Level     `json:"level"`
    FoldedCount  int       `json:"folded_count"`
    FoldedDigest string    `json:"folded_digest"` // SHA256 of folded content
    Summary      string    `json:"summary"`
    Timestamp    time.Time `json:"timestamp"`
}

type Result struct {
    // ... existing fields ...
    FoldLog []FoldEntry `json:"fold_log,omitempty"`
}
```

在 `applyL3`/`applyL4` 中记录 fold entry：

```go
// applyL3
foldEntry := FoldEntry{
    Level:       L3ContextCollapse,
    FoldedCount: len(before),
    FoldedDigest: sha256Hex(marshalMessages(before)),
    Summary:     summaryText,
    Timestamp:   time.Now(),
}
// Attach to Result.FoldLog
```

结合 P1-2 的 JSONL transcript，完整消息链可通过 JSONL 文件重建，fold digest 用于校验。

---

### CC-2. Memory 三层区分

**确认**：goclaw 有 L0/L1/L2 分层注释（`auto_injector.go:3`）、episodic store、topic/daily PG workers。但 session transcript（可恢复运行时状态）和 memory（长期知识）在代码中共用 `providers.Message` 类型，没有类型级别区分。

**实现方案**（概述）：
1. 定义 `MemoryLayer` enum: `LayerContext`, `LayerTranscript`, `LayerCrossSession`
2. `message.Transcript` 标注为 `LayerTranscript`
3. `memory.L0Summary` 标注为 `LayerCrossSession`
4. pipeline `RunState.Messages`（MessageBuffer）标注为 `LayerContext`
5. 每层有独立的 eviction/persist 策略

---

### CC-3. Fork Subagent 缓存前缀优化

**实现方案**（概述）：
1. 所有 fork children 共享 parent 的 system prompt + 历史前缀（作为 `cache_control.ephemeral` 标记）
2. 在 `session_fork.go` 中，fork 时保留 parent 的 prompt cache key
3. LLM 请求中标记共享前缀，最大化 Anthropic/OpenAI 的 prompt cache 命中

---

### CC-4. Handoff Classifier（子代理交接审查）

**实现方案**（概述）：
1. `internal/agent/handoff.go` 新文件
2. subagent 返回结果时，parent agent 用 classifier 审查输出安全性
3. 检查：结果中是否包含被 deny 的内容（如密钥泄露）、是否越权修改了文件
4. 审查失败时丢弃结果并在 parent context 中标记 warning

---

## 文档债

- [ ] 更新 [07](./07-deep-fusion-audit.md) 开篇结论：主链路已部分接线时的表述
- [ ] 同步 [01](./01-agent-runtime-core.md)–[07](./07-deep-fusion-audit.md) Ideas 表中的已实现状态

---

## 实施优先级建议

| 批次 | 条目 | 预估工期 | 收益 |
|------|------|----------|------|
| **Batch 1** | P0-1 ExecTool 并发, P0-2 Safety 默认规则, P0-4 Synthetic Error | 2-3 天 | 安全 + 性能基础 |
| **Batch 2** | P1-1 Prune hooks, P1-4 Subagent hooks, P3-2 Headless perm | 2 天 | 完整 hook 覆盖 |
| **Batch 3** | P0-5 Compactor 注入, P1-3 Compact boundary, CC-1 Fold log | 2-3 天 | 压缩系统完整 |
| **Batch 4** | P3-1 Mode SDK event, P3-3 Source label, P2-4 SDK alignment | 1-2 天 | SDK 完整性 |
| **Batch 5** | ~~P0-3 / P0-6 / P0-7~~ 已闭环 | — | 架构提升（全完成）|
| **Batch 6** | ~~P2-1 / P2-2~~ 已闭环；P6-1 Config chain | 按需 | 插件生态 |
| **Batch 7** | ~~P1-2 / P1-5 / P1-6~~ 已闭环；P6-2 Hot-reload | 按需 | 配置体验 |
| **Batch 8** | P4-* 市场/炼化/织梦坊, P5-* 精灵 | 按需 | 生态功能 |
