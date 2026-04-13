# 05 — Spirit（精灵）

对应 **用户长期伙伴**：画像、意图路由、多 Agent 编排、反馈学习。

---

## 需求细化

### 产品目标

- **一句话**：精灵是跨会话的「主入口人格」——理解用户习惯与工作上下文，把自然语言指令转成可执行的意图与子任务，并调度合适的 Agent（含本机主 Agent 与委托目标），在长期交互中通过反馈变得更默契。
- **与底层 Teams/Subagent 的分工**：精灵偏 **入口调度与画像**；底层多 Agent 执行语义仍以现有委托/管线为准（见 [11-agent-teams.md](../11-agent-teams.md)），精灵不重新定义「车道」规则。

### 用户场景（应覆盖）

1. **默契配合**：用户偏好（语言、详略、工具白名单/黑名单、默认模型等）在多次会话间保持一致；主 Agent 在系统提示中能看到与当前请求相关的精灵摘要。
2. **统一下达**：用户通过同一条对话链路发指令；精灵层负责 **意图识别 + Agent 建议**，产品层可进一步提供「仅与精灵对话」的独立入口（CLI/Web），但属增量能力。
3. **多步任务**：复杂请求被拆成带依赖的子任务，按依赖图 **就绪即并行** 执行，再合并结果（`Orchestrator` + 注入的 `AgentExecutor` / `ResultMerger`）。
4. **会学习**：显式反馈（成功/失败、评分、纠错文本）更新 **Agent 亲和度** 与 **工作上下文**（如 `RecentTopics`）；长期记忆与主题表可与 `dreamweaver_topics` / consolidation 层打通，而非仅进程内结构。

### 功能需求（按模块）

| ID | 模块 | 需求说明 | 当前代码锚点 |
|----|------|----------|--------------|
| FR-P1 | **Profile** | 每 `(tenant_id, user_id)` 至多一个精灵画像；含名称、语言、`CommunicationStyle`、`Preferences`、`AgentAffinity`、`WorkContext`（含目标、置顶备注、最近主题等）；支持创建默认画像、读写、`RecordInteraction` 更新亲和度与计数。 | `profile.go`，`ProfileManager`，`ProfileStore` |
| FR-P2 | **持久化** | 画像可落库；迁移表 `dreamweaver_spirit_profiles`（JSONB 字段与 `internal/store/pg` 实现一致）；读写需 **多租户隔离**（`tenant_id` 参与一切查询）。 | `PGSpiritProfileStore`（`internal/store/pg/dreamweaver.go`） |
| FR-R1 | **Router** | 将用户消息解析为 `Intent`（类型、置信度、子任务、建议 Agent）；优先使用注入的 `IntentClassifier`，失败则 **关键词回退**；`assignAgents` 结合 `AgentInfo` 列表与亲和度。 | `router.go` |
| FR-R2 | **Agent 列表来源** | 路由可见的 Agent 至少包含 **当前 Loop 主 Agent** 与 **`delegateTargets`** 中的委托目标，以便建议与编排和真实可调用能力一致。 | `loop_dreamweaver.go` → `buildDreamWeaverPromptSections` |
| FR-O1 | **Orchestrator** | 对 `Intent.SubTasks` 按 `DependsOn` 建图；无环依赖下 **并行执行就绪任务**；聚合为 `OrchestrationResult`；执行与合并通过接口注入，便于接真实 RPC/子 Agent。 | `orchestrator.go` |
| FR-L1 | **Learning** | `RecordFeedback` 更新亲和度、可选 `applyCorrection`（降权错误 Agent、把纠错文本并入 `RecentTopics`）；进程内 `history` 有界；**持久化反馈流**可与审计/主题记忆后续对接。 | `learning.go` |
| FR-I1 | **Loop 集成** | 在 `DreamWeaver` 总开关与 `spirit_enabled` 开启时，将精灵段落注入系统提示（意图、置信度、建议 Agent）。**画像存储**：`runtimeDB != nil` 时使用 `PGSpiritProfileStore`，否则 `noopProfileStore`（与 `SpiritEnabled` **无**耦合——`SpiritEnabled` 只控制提示段与分类器，不切换 Store 类型）。 | `DreamWeaverConfig.SpiritEnabled`，`loop_dreamweaver.go` |
| FR-I2 | **入口（产品）** | 独立「精灵对话」或仅精灵模式 **非必须**，但若提供：需约定会话归属同一 `user_id`/`tenant_id`，并复用上述 Profile/Router/Learning。 | 现状：无独立 CLI/Web 入口 |

### 非功能需求

| ID | 说明 |
|----|------|
| NFR-1 | **租户隔离**：所有画像与后续反馈记录以 `tenant_id` 为边界；禁止跨租户读写。 |
| NFR-2 | **可观测**：路由失败回退、学习失败应打日志（已有 `slog`），关键路径不静默吞错导致画像长期不更新。 |
| NFR-3 | **默认安全**：未启用 `SpiritEnabled` 时无精灵提示块；无 `runtimeDB` 时画像不持久化（noop）。有 DB 时画像可走 PG，与是否打开精灵提示 **独立**（若产品要求「仅 SpiritEnabled 才写画像」需另加门控）。 |
| NFR-4 | **扩展点**：`IntentClassifier`、`AgentExecutor`、`ResultMerger` 可替换，便于接入模型分类器或外部 Agent 运行时。 |

### 验收与里程碑（建议）

| 优先级 | 内容 |
|--------|------|
| **P0** | 存在 **`runtimeDB`** 时，Loop 使用 `PGSpiritProfileStore`，画像在多次 Run 间可复现；`Save` 与 `Get` 与迁移表一致（**不要求**同时 `SpiritEnabled` 才启用 PG Store）。 |
| **P1** | `RecordFeedback` 从真实 Run 结束路径或用户操作接入（非仅单测），并与持久化画像一致。 |
| **P2** | `Orchestrator` 与主链路结合：对多子任务请求真正走并行执行（需 `AgentExecutor` 实现与委托语义对齐）。 |
| **P3** | 独立精灵入口（CLI/Web）、多精灵/角色切换、与 `dreamweaver_topics` 或 consolidation 的显式同步。 |

### 已知缺口（与状态表一致）

- **画像门控（产品项）**：当前 **`runtimeDB != nil` 即用 PG**，未要求 `SpiritEnabled` 与 Store 绑定；若需「仅开启精灵时才持久化画像」需在 `initDreamWeaverServices` 增加条件。
- **学习闭环**：`POST /v1/feedback` 与 `LearningLoop.RecordFeedback` **已** 接入；仍可按产品扩展 Run 结束自动上报、审计维度等。
- **编排**：多 SubTask 时 **`Orchestrator` + `SpiritDelegateRunFn`** 已接主链路；复杂委托语义与失败重试仍可增强。
- **分类器**：`spirit_enabled` 且 provider 可用时使用 **`NewLLMIntentClassifier`**；否则关键词回退。

---

## 包结构 — `internal/spirit/`

| 文件 | 职责 |
|------|------|
| `profile.go` | `Profile`（语言、沟通风格、偏好、工作上下文、目标、`AgentAffinity`）；`ProfileManager` + `ProfileStore` 接口；`RecordInteraction` 更新亲和度 |
| `router.go` | `Router`：依赖 `IntentClassifier`，否则 `keywordClassify`；`assignAgents` 结合 `AgentInfo` 与亲和度 |
| `orchestrator.go` | `Orchestrator`：按子任务依赖并行执行；`AgentExecutor` / `ResultMerger` 注入；`OrchestrationResult` |
| `learning.go` | `LearningLoop`：`Feedback` 记录、成功率统计、`applyCorrection` 调整画像 |

## 概念关系

- **Profile**：跨 session 的用户与精灵共用配置（需持久化时实现 `ProfileStore`）。
- **Router**：将自然语言映射为 `Intent` 与子任务，并建议 Agent。
- **Orchestrator**：对子任务建依赖图，就绪任务并行跑，最后合并结果。
- **Learning**：根据成功/失败与纠错文本更新 `AgentAffinity` 与 `RecentTopics`。

与现有 **Teams / Subagent** 文档关系见 [11-agent-teams.md](../11-agent-teams.md)；精灵层偏「入口调度」，不改变底层调度车道语义。

---

## Ideas：需求 6（精灵）

> 原载于 [DreamWeaver README](./README.md)「Ideas 需求索引」，与本章「Spirit」对应。  
> **更细的功能/非功能条目、验收优先级** 见上文 [需求细化](#需求细化)。

### 6. 精灵功能

> 精灵功能：培养精灵，跟主人形成默契配合；所有指令可以从精灵下达；精灵调度需求所需的 agent，完成目标任务。

**状态：🟡 部分实现**

| 子项 | 状态 | 说明 |
|------|------|------|
| `internal/spirit/profile.go` | 🟢 已实现 | Profile 结构：风格、偏好、agent 亲和度、工作上下文 |
| `internal/spirit/router.go` | 🟢 已实现 | Router：根据 profile 路由到合适 agent |
| `internal/spirit/learning.go` | 🟢 已实现 | LearningLoop：从交互中提取偏好更新 profile |
| `internal/spirit/orchestrator.go` | 🟢 已实现 | Orchestrator：任务拆分 → 调度多个 agent |
| PG Profile 持久化 | 🟢 已实现 | `runtimeDB != nil` 时 Loop 使用 `PGSpiritProfileStore`（无 DB 时为 noop） |
| 精灵指令界面 | 🔴 未实现 | 无独立的精灵对话入口（CLI / Web） |
| 精灵成长/记忆 | 🟡 部分实现 | `LearningLoop` + PG 画像；纠错可 **`UpsertTopic`** 写入 `dreamweaver_topics`；与 consolidation 的显式产品化同步仍可增强 |
| 多精灵管理 | 🔴 未实现 | 目前 per-user 单精灵，无精灵切换/多角色 |
