# 01 — Agent Runtime Core（运行时核心）

本类能力对应 Claude Code 侧强调的：**Agent 作为 Runtime**、**多视图消息**、**权限治理链**、**恢复即重建可工作状态**。

## 1. 生命周期状态机 — `internal/lifecycle/`

| 文件 | 职责 |
|------|------|
| `state.go` | 状态枚举（Idle / Initializing / Thinking / Acting / Observing / Compacting / Resuming / Finalizing / Completed / Failed / Interrupted）、合法迁移图 `CanTransition` |
| `manager.go` | 按 `runID` 登记、状态迁移、历史 `Transition[]`、可选 `eventbus.DomainEventBus` 发布 `lifecycle.transition` |

**设计要点**：迁移非法组合会返回错误；终端态为 Completed / Failed / Interrupted。

## 2. 多视图消息模型 — `internal/message/`

| 文件 | 职责 |
|------|------|
| `views.go` | 视图枚举：Transcript / API / UI / Resume；`Envelope` 包装消息与元数据 |
| `normalize.go` | `NormalizeForAPI`（去 RawAssistant、补孤儿 tool 的 synthetic result）、`NormalizeForUI`、`NormalizeForResume`、`FindToolPairs` |
| `transcript.go` | `Transcript` 权威会话记录，提供 `ForAPI` / `ForUI` / `ForResume` |

**设计要点**：不修改 `providers.Message` 本体，在适配层做视图转换，便于与现有管线渐进集成。

## 3. 权限治理 — `internal/permissions/`（扩展）

| 文件 | 职责 |
|------|------|
| `policy.go`（既有） | 网关 RPC 角色与 scope（RBAC） |
| `governor.go` | `Governor`：规则 → 模式（safe/standard/autonomous）→ `Classifier` → Hook 前后 → 审批回调 → 审计 |
| `classifier.go` | 按工具名启发式划分 `SecurityClass`（read_only / write_fs / execute / network / dangerous），支持 `SetOverride` |

**设计要点**：与网关 `PolicyEngine` 分层：本包侧重 **工具调用级** 治理；RPC 级仍见 `policy.go`。

## 4. 会话恢复引擎 — `internal/resume/`

| 文件 | 职责 |
|------|------|
| `engine.go` | `Engine.Resume`：过滤坏消息、修复 tool 配对、修剪尾部空 assistant、按 `InterruptType` 注入 continuation system 消息 |

**中断类型**：`InterruptNone`、UserCancel、Timeout、NetworkErr、Panic、OverBudget、Unknown。

**测试**：`engine_test.go` 覆盖孤儿 tool 修复、过滤、continuation 注入。

---

## Ideas：融合需求对照（Claude Code → goclaw）

> 原载于 [DreamWeaver README](./README.md)「Ideas 需求索引」；**§1 的 A–C、J** 与本章「Agent Runtime Core」主题对应。

#### A. Agent Loop / Query Engine

| CC 机制 | CC 源码 | goclaw 对应 | 状态 | 差距与下一步 |
|---------|---------|-------------|------|-------------|
| **显式 turn 循环** `query()→queryLoop()` while-loop，每轮 yield StreamEvent | `query.ts`, `QueryEngine.ts` | `internal/pipeline/pipeline.go` 的 Setup→Iteration→Finalize 三阶段 | 🟢 已实现 | 结构对齐；goclaw 缺少 `stream_request_start` 级别的 yield 事件 |
| **TurnState** 共享上下文（messages, toolUseContext, autoCompactTracking, pendingToolUseSummary） | `query.ts` 内部 `State J` | `pipeline.RunState`（Observe/Think/Execute/Finalize 子状态） | 🟢 已实现 | goclaw 的 RunState 字段更多，无 `pendingToolUseSummary` 概念 |
| **流式 tool 执行** `StreamingToolExecutor`：并发安全工具并行，不安全串行 | `StreamingToolExecutor.ts` | 无 | 🔴 未实现 | 当前 tool 执行为串行；需新增 `StreamingToolExecutor` 或协程池 |
| **Token Budget / max_output 恢复** 超长输出自动恢复（最多 3 次） | `tokenBudget.ts`, `query.ts` | 无 | 🔴 未实现 | 需在 pipeline iteration 中加 max_output recovery 逻辑 |
| **Stop hooks** 模型完成后执行 stop hook 链 | `stopHooks.ts`, `handleStopHooks` | `hooks.Fire(EventRunCompleted)` 在 finalize | 🟡 部分实现 | goclaw fire 在 finalize 而非每轮结束；CC 的 stop hook 可注入额外 context |
| **Reactive compact** 上下文超限时触发反应式压缩 | `reactiveCompact.ts` | 无 | 🔴 未实现 | `internal/compression` 只有主动压缩策略 |

#### B. Tool System

| CC 机制 | CC 源码 | goclaw 对应 | 状态 | 差距与下一步 |
|---------|---------|-------------|------|-------------|
| **Tool 接口** schema + call + permission + concurrency hint + render | `Tool.ts`, `tools.ts` | `internal/tools/registry.go` Tool interface | 🟢 已实现 | goclaw 已有 schema/Execute/Approval；缺 `isConcurrencySafe` 标记 |
| **并发分区执行** readonly 工具并行，写工具串行 | `toolOrchestration.ts` `runTools()` | 无 | 🔴 未实现 | 需在 registry 层新增并发策略 |
| **Deferred tools + ToolSearch** 延迟加载 MCP 工具 schema，按需发现 | `ToolSearchTool`, deferred tools | 无 | 🔴 未实现 | 当前 MCP 工具全量加载 |
| **Pre/Post Tool Hooks** 工具执行前后 hook 链 | `toolHooks.ts`, `toolExecution.ts` | `hooks.Fire(EventToolStart/EventToolEnd)` | 🟢 已实现 | CC 的 hook 可 **block** 执行、注入 context；goclaw 当前 fire-and-forget |
| **contextModifier** 工具执行后修改后续上下文 | `Tool.ts` `contextModifier` | 无 | 🔴 未实现 | 需在 tool 接口加 PostExecuteContextModifier |
| **Tool result budget** 工具输出长度限制与截断 | compact 内 `toolResultBudget` | `compression.ToolResultBudget()` | 🟢 已实现 | 已对齐 |

#### C. Permission & Security

| CC 机制 | CC 源码 | goclaw 对应 | 状态 | 差距与下一步 |
|---------|---------|-------------|------|-------------|
| **Permission 模式** default/plan/acceptEdits/dontAsk/bypassPermissions/auto | `permissions.ts`, `PermissionContext.ts` | `internal/permissions/governor.go` allow/deny/ask | 🟡 部分实现 | goclaw 有三态决策，缺 mode 状态机（plan mode 等）|
| **规则管道** deny→always-allow→tool.checkPermissions→mode→ask | `D0z`→`YP` chain | `Governor.Evaluate()` 串行规则 | 🟡 部分实现 | 规则顺序相似但缺 per-session persistent allowlist |
| **异步审批多路复用** UI/bridge/channel/hooks/classifier 竞争响应 | `InteractiveHandler` races | `ResolveApproval` 阻塞等待 | 🟡 部分实现 | 单通道等待；需多路 fan-out |
| **Swarm 权限委托** worker→leader 权限请求转发 | mailbox + coordinator handler | 无 | 🔴 未实现 | 需在多 agent 场景加权限传递 |
| **Auto classifier** 自动分类工具安全等级决定是否免审批 | `auto` mode + classifier | `internal/permissions/classifier.go` | 🟢 已实现 | 已有 classify，但未与 auto mode 联动 |
| **Permission request hooks** shell hook 参与审批决策 | `executePermissionRequestHooks` | 无 | 🔴 未实现 | hooks 可 fire 但不参与 permission 决策 |

#### J. 多 Agent / Coordinator / Swarm

| CC 机制 | CC 源码 | goclaw 对应 | 状态 | 差距与下一步 |
|---------|---------|-------------|------|-------------|
| **AgentTool 子 agent 派生** in-process/background/worktree/remote | `runAgent.ts`, `AgentTool` | goclaw 已有 subagent 机制（spawn） | 🟡 部分实现 | 缺少 worktree isolation 和 remote agent |
| **Coordinator 模式** 受限工具 + 特殊 system prompt | `coordinatorMode.ts` | 无 | 🔴 未实现 | |
| **Team / Mailbox** 消息传递 + 审批 | `TeamCreateTool`, `SendMessageTool`, mailbox | 无 | 🔴 未实现 | goclaw 有 agent_teams 表但无运行时 team 通信 |
| **权限委托** worker ask → leader resolve | coordinator/swarm handlers | 无 | 🔴 未实现 | 见 C 节 |
