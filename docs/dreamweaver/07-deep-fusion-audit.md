# 07 — 深度融合审计（Deep Fusion Audit）

本文件不是“功能介绍”，而是一次针对 **DreamWeaver 当前代码** 与 **Claude Code 设计机制** 的代码级审计。目标是回答三个问题：

1. 已经实现的 DreamWeaver 包，哪些只是“库形态存在”，哪些已经具备可接入主链路的基础。
2. 与 Claude Code 的关键机制相比，当前实现缺了什么、浅了什么、错了什么。
3. 在 `goclaw` 现有主链路中，应该把这些能力接到哪里，才能形成真正的 Runtime，而不是文档级模块拼装。

说明：本审计文件记录的是“重新审视时的基线判断”。在这份审计之后，代码已完成第一轮 feature-gated 接线，具体见 [06-integration-status.md](./06-integration-status.md) 与 [README.md](./README.md) 的最新状态。

---

## 一、总体判定

当前 DreamWeaver 的 13 个核心包，整体处于 **“库已成型，Runtime 未接线”** 的状态。

### 结论一：方向对，但尚未构成真实 Runtime

- `internal/lifecycle`
- `internal/message`
- `internal/permissions`
- `internal/resume`
- `internal/compression`
- `internal/hooks`
- `pkg/sdk`
- `internal/plugins`
- `internal/consolidation`
- `internal/spirit`

这些包都已经具备清晰的命名、数据结构和基本 API，但主链路 `Loop -> buildPipelineDeps -> pipeline stages` 仍然主要沿用原有 goclaw 实现。换句话说：

- DreamWeaver **可以被 import**
- DreamWeaver **还没有真正参与 agent run**

### 结论二：现阶段最关键的不是“再加新模块”，而是“让现有模块进主链路”

真正的融合中心不在 `internal/dreamweaver/*` 这种新目录，而在：

- `internal/agent/loop_types.go`
- `internal/agent/loop_pipeline_adapter.go`
- `internal/agent/loop_pipeline_callbacks.go`
- `internal/pipeline/context_stage.go`
- `internal/pipeline/think_stage.go`
- `internal/pipeline/tool_stage.go`
- `internal/pipeline/prune_stage.go`
- `internal/pipeline/finalize_stage.go`

`goclaw` 当前已经有一条明确的 v3 运行路径，DreamWeaver 要深度融合，必须成为这条路径的一部分。

---

## 二、与 Claude Code 的对照判定

### 2.1 原则层面对齐情况

对照 `docs/claudecode/Claudecode-analysis/06_Methodology/CLAUDE_CODE_10_KEY_DESIGN_PRINCIPLES.md`，当前 DreamWeaver 的落地情况如下：

| Claude Code 原则 | 当前判定 |
|------------------|----------|
| Agent 是 Runtime，不只是 Prompt | **部分做到**。包结构已经按 Runtime 思路拆分，但尚未进入主运行路径。 |
| 先定义系统状态，再定义模型输出 | **部分做到**。`lifecycle`、`message`、`permissions` 已有雏形，但状态仍未统一到 session/runtime 级。 |
| 工具调用是主链路 | **部分做到**。`permissions`、`hooks` 指向工具主链路，但 `ToolStage` 还未接 DreamWeaver 治理。 |
| 内部表示 / UI / API 表示分离 | **方向正确但实现偏浅**。`message` 已分视图，但 UI reorder / lookup / metadata 不足。 |
| 权限是治理系统 | **有骨架，未达到 Claude Code 层级**。规则、模式、分类已有，但缺 `tool.checkPermissions`、safety layer、content-level matching。 |
| 长上下文需要分层治理 | **只有分层名词，没有完整分层流水线**。`compression` 仍是单层选择，不是串行治理链。 |
| 恢复是重建可工作状态 | **部分做到**。`resume` 有修复与 continuation，但没有 session log state restore / worktree restore。 |
| 扩展必须标准化 | **方向正确**。`hooks`、`sdk`、`plugins` 已存在，但接线与协议细节还薄。 |
| UI 是 Runtime 的一部分 | **尚未形成**。`sdk` 有事件模型，但还没把状态、权限、工具流转完整暴露出去。 |
| 异常状态也要建模 | **明显不足**。当前更多是 happy-path 库设计，异常流仍然零散。 |

---

## 三、分包审计

### 3.1 `internal/lifecycle`

#### 已有优点

- 状态机明确：`idle -> initializing -> thinking -> acting -> observing -> ...`
- 有 `CanTransition` 和历史记录，适合作为 audit/event 基础。
- `manager.go` 具备 callback + eventbus 发布能力。

#### 关键缺口

- 缺少 Claude Code 式 **session-level blocked state**，即 `requires_action`。
- 当前状态更偏 **pipeline 微阶段**，而不是 **运行时编排阶段**。
- 尚未接入 `pipeline.Run()` 或 `Loop`。

#### 审计结论

`lifecycle` 作为库是成立的，但还不是实际 Runtime state source of truth。

### 3.2 `internal/message`

#### 已有优点

- 明确区分 `Transcript / API / UI / Resume` 四种视图。
- `NormalizeForAPI`、`NormalizeForResume` 具备基本修复逻辑。
- `Transcript` 作为 authoritative record 的方向正确。

#### 关键缺口

- `NormalizeForUI()` 的注释声称“可能重排 tool result”，但代码并没有做 reorder。
- 缺少 Claude Code 式 `buildMessageLookups()` 一类的索引结构。
- `EnvelopeMetadata.TruncatedTools` 等字段存在，但没有真实生产逻辑。
- 还没进入真实 API / UI / persistence 主路径。

#### 审计结论

`message` 已经有视图分层意识，但目前仍是“轻 normalize 工具集”，不是完整 message runtime。

### 3.3 `internal/permissions`

#### 已有优点

- 已有 `rules + mode + classifier + approval + audit` 的基本骨架。
- 审计结构清晰，便于后续扩展。

#### 关键缺口

- `Evaluate()` 顺序与 Claude Code 的 inner permission pipeline 不一致。
- 没有 `tool.checkPermissions(...)` 这一层。
- 没有 non-bypassable `safetyCheck`。
- `RuleMatcher` 未真正作用到 arguments。
- `classifier` 接收了 args，但并未实质使用。
- 还没有接到 `ExecuteToolCall` / `ExecuteToolRaw` 上。

#### 审计结论

当前 `Governor` 更像“规则裁决器”，还不是完整工具治理链。

### 3.4 `internal/resume`

#### 已有优点

- 有 interrupt 分类。
- 有坏消息过滤、tool pairing repair、continuation 注入。

#### 关键缺口

- 只处理 `[]providers.Message`，没有真正的 session log deserialization。
- 没有 `restoreSessionStateFromLog` 类能力。
- 没有 worktree / metadata / approval / compaction state 恢复。
- `maxOrphanedCalls` 配置没有被用到。

#### 审计结论

当前更像“恢复前清洗器”，而不是真正的 session recovery engine。

### 3.5 `internal/compression`

#### 已有优点

- 已有 L0-L4 分层命名。
- `TokenCounter`、`Compactor` 可注入。
- 能做基础 tool result 截断和 compact。

#### 关键缺口

- 当前实现是 **选一个 level 执行**，不是 Claude Code 那种 **按顺序逐层施压**。
- 没有 `applyToolResultBudget()`。
- 没有 `COMPACTABLE_TOOLS` 白名单策略。
- 没有 cache-aware edit/prefix preservation。
- `L3/L4` 语义仍偏简化。

#### 审计结论

`compression` 有分层外观，但尚未达到 Claude Code 式上下文治理流水线。

### 3.6 `internal/hooks`

#### 已有优点

- 有 `sync/async`、`webhook/command/internal` 三类 handler。
- 事件点覆盖 session / think / tool / permission / compact / resume / error。

#### 关键缺口

- `registry.go` 存在实质 bug：`Enabled=false` 会被强制改成 `true`。
- `Matcher.RunKind` 未被使用。
- 没有 hook event buffer / delivery stream / pending events。
- 没有 async rewake 机制把异步 hook 结果送回主循环。
- `Payload.Data map[string]any` 太松，缺 typed payload。

#### 审计结论

`hooks` 的协议方向正确，但距离“运行时扩展总线”还有明显差距。

### 3.7 `pkg/sdk`

#### 已有优点

- 有 `Command` / `Event` / `Bridge` 基础协议。
- 事件类型基本覆盖 run/tool/permission/session。

#### 关键缺口

- `Bridge.Emit()` 的 `Sequence` 目前是 **全局递增**，不是注释声明的 per-run。
- `Subscribe()` 当前一 run 只支持一个 sink。
- 没有事件队列、慢消费者策略、缓冲/backpressure。
- 还没和现有 agent event 主路径对齐。

#### 审计结论

现在的 `sdk` 更像 embed-friendly 的轻桥接层，离 Claude Code StructuredIO 级别还有距离。

### 3.8 `internal/plugins`

#### 已有优点

- Manifest、生命周期、目录扫描都已具备。

#### 关键缺口

- 还没接到 gateway 启动或 `Loop` tool registry。
- 还没把 manifest 里的 tool/hook/mcp/skill 能力真正挂到运行时。

#### 审计结论

当前是 plugin registry，不是 plugin runtime。

### 3.9 `internal/consolidation` / `internal/spirit`

#### 已有优点

- Topic / daily / profile / router / orchestrator / learning 的结构都比较完整。
- 接口抽象清晰，适合后续落 DB 与 worker。

#### 关键缺口

- 没有 store 实现。
- 没有定时/事件驱动调度。
- 没有进入 `Loop` 的决策入口。

#### 审计结论

这部分已经具备产品骨架，但尚未进入真实运行面。

---

## 四、当前最关键的真实问题

### 4.1 不是“模块不够多”，而是“主链路没接”

当前最大的差距不是缺少更多 Phase 4/5 的新功能，而是：

- `Loop` 还不知道 DreamWeaver services 的存在
- `buildPipelineDeps()` 没有 DreamWeaver wrapper
- `pipeline` stages 没有把 DreamWeaver 当成运行时组件来调用

### 4.2 代码中已存在几个明确问题

以下不是设计差距，而是当前代码层面的直接问题：

- `internal/hooks/registry.go`：`Enabled` 被默认强制设为 `true`
- `pkg/sdk/bridge.go`：`Sequence` 与注释不符，应为 per-run 但实际是全局
- `internal/message/normalize.go`：UI reorder 注释与实现不一致
- `internal/permissions/governor.go`：`RuleMatcher` / args 没有实质参与决策
- `internal/resume/engine.go`：`maxOrphanedCalls` 是死配置

---

## 五、主链路接线判定

DreamWeaver 深度融合的核心接线点如下：

| 位置 | 作用 | DreamWeaver 应承担的职责 |
|------|------|--------------------------|
| `internal/agent/loop_types.go` | 持有 Loop runtime 依赖 | 增加 lifecycle / hooks / governor / compression / resume / sdk / spirit / plugins 等 service |
| `internal/agent/loop_pipeline_adapter.go` | 构造 `PipelineDeps` | 用 wrapper 把 DreamWeaver 逻辑挂到 callbacks，不改坏 pipeline stage |
| `internal/agent/loop_pipeline_callbacks.go` | 现有主回调集合 | 作为 DreamWeaver 接线的低风险插入层 |
| `internal/pipeline/context_stage.go` | 加载历史、构造 prompt、注入 memory | 接 resume / transcript / drift / prompt section |
| `internal/pipeline/think_stage.go` | LLM 调用入口 | 接 pre/post think hooks、lifecycle、sdk state |
| `internal/pipeline/tool_stage.go` | tool 执行入口 | 接 governor、pre/post tool hooks、requires_action |
| `internal/pipeline/prune_stage.go` | 历史治理入口 | 接 compression staged pipeline |
| `internal/pipeline/finalize_stage.go` | run 收尾与 session completed | 接 lifecycle final state、sdk、learning、daily/topic worker trigger |

---

## 六、深度融合的实施顺序判定

### 第一优先级：修正现有 DreamWeaver 库中的硬问题

- 修 hooks bug
- 修 bridge sequence
- 深化 governor
- 深化 compression
- 深化 message
- 深化 resume

### 第二优先级：让 DreamWeaver 真正进入 `Loop -> pipeline`

- `Loop` 增加 service fields
- `buildPipelineDeps()` 增加 wrappers
- `ToolStage` 前后真正接 governor / hooks / lifecycle
- `ContextStage` 真正接 resume / transcript / prompt section
- `PruneStage` 真正切到 DreamWeaver compression
- `FinalizeStage` 真正发 runtime/session state

### 第三优先级：持久化与产品化

- migrations
- pg stores
- plugin loading
- marketplace API
- spirit entry

---

## 七、当前完成度的重新判定

从“深度融合”标准来看，当前完成度不应再表述为“Phase 1-5 已实现”，而应拆成两层：

| 维度 | 判定 |
|------|------|
| **库层实现** | **高**：多数包已有可用数据结构与 API |
| **Claude Code 机制对齐度** | **中低**：机制名称接近，但很多实现仍偏浅 |
| **goclaw 主链路融合度** | **中**：核心能力已通过 `Loop` → `buildPipelineDeps()` → pipeline callbacks 接线（`DreamWeaverConfig` 控制；默认关闭） |
| **产品化完成度** | **低**：DB/API/UI 仍大多缺失 |

更准确地说：

- DreamWeaver **不是没做**
- DreamWeaver **也绝不能算已经深度融合完成**

它目前处于 **“库形态已落地；Runtime 接线已存在且 Phase 1（核心循环强化）任务在代码侧已对齐 README 实施计划；生态/市场（Phase 2）仍以库 + 本地 catalog/ref/清单/import CLI 为主，云与产品面未齐”** 的阶段。细节以 [README 实施计划](./README.md#实施计划) 中的 **Phase 1/2 状态** 表为准。

---

## 八、与后续文档的关系

- [README.md](./README.md)：总体状态与需求内化
- [06-integration-status.md](./06-integration-status.md)：具体接线位置与接入顺序

本文件只负责一件事：给出 **代码级真判断**，避免后续排期建立在“已经融合完成”的误判之上。

---

## Ideas：融合需求对照（Claude Code → goclaw）— §1 L

> 原载于 [DreamWeaver README](./README.md)「Ideas 需求索引」。

#### L. 配置系统

| CC 机制 | CC 源码 | goclaw 对应 | 状态 | 差距与下一步 |
|---------|---------|-------------|------|-------------|
| **分层合并** plugin<user<project<local<flags<policy | `configManager.ts` | `internal/config/config.go` 平铺加载 | 🟡 部分实现 | 缺少层级优先级合并 |
| **Trust gate** 信任后才加载项目级 env/settings | trust dialog | 无 | 🔴 未实现 | |
| **Remote / MDM policy** 企业远程策略覆盖 | remote managed, MDM read | 无 | 🔴 未实现 | |
| **Settings watcher** 配置文件变更热更新 | fsnotify watcher | `fsnotify` 依赖已有，未用于 config | 🟡 部分实现 | |

---

## 融合优先级建议

### P0（核心循环强化）
1. **流式 tool 执行** — 并发分区执行 readonly vs write 工具
2. **同步 hook 阻断** — PreToolUse hook 可 block/modify
3. **Reactive compact** — 超限时自动触发压缩
4. **JSONL transcript 持久化** — 从内存迁移到追加式文件

### P1（用户体验 + 安全）
5. **Permission 模式状态机** — plan/auto/acceptEdits 等模式切换
6. **CLAUDE.md 规则发现** — 自动扫描项目/用户级 instruction 文件
7. **MCP instructions section** — MCP server 指令注入系统提示词
8. **Plugin 真实执行** — 替换 placeholder tool 为实际 dispatch

### P2（多 agent + 生态）
9. **Session fork** — 分支会话
10. **Coordinator 模式** — 受限工具集 + leader/worker 权限委托
11. **Compact hooks + boundary** — Pre/PostCompact 事件 + 边界消息
12. **goclaw 作为 MCP server** — 暴露自身工具目录

### P3（远程 + 企业）
13. **Remote bridge** — WS/SSE 远程控制
14. **Layered config merge** — 配置分层优先级
15. **Trust gate** — 项目级环境信任门控
16. **Background sessions** — CLI 后台会话管理

