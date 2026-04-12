# Claude Code / Claude 继续学习路线

日期：2026-04-08

## 先说结论

如果你已经看过：

- 项目整体分析
- Kairos / heartbeat 对比
- harness / agent 架构解读

那你下一阶段最值得学的，不是再泛泛地看“Claude 很强”，而是继续往下面这 8 个方向深挖。

## 1. Prompt 体系设计

建议重点学：

- system prompt 是怎么分层拼装的
- user context / system context 为什么要分开
- append prompt 和 custom prompt 有什么区别
- 不同模式下 prompt 怎样变化，比如普通模式、proactive、Kairos、agent mode

为什么值得学：

因为 Claude Code 不是一段固定 system prompt，而是“动态拼装 prompt runtime”。这类设计非常值得借鉴到自己的 agent 系统里。

你可以继续读：

- [`constants/prompts.ts`](../src/constants/prompts.ts)
- [`utils/queryContext.ts`](../src/utils/queryContext.ts)
- [`main.tsx`](../src/main.tsx)

通俗理解：

这部分是在学“怎么给 agent 立规矩”。

## 2. 工具协议与 Tool Schema 设计

建议重点学：

- 一个工具为什么必须有 input/output schema
- tool result 为什么要标准化
- `isReadOnly`、`isConcurrencySafe`、`interruptBehavior` 这些字段为什么重要
- 为什么权限系统要嵌进工具层，而不是写在外面

为什么值得学：

很多 agent demo 只有“能调工具”，但 Claude Code 研究的是“怎么稳定、安全、可组合地调工具”。

建议继续读：

- [`Tool.ts`](../src/Tool.ts)
- [`services/tools/toolExecution.ts`](../src/services/tools/toolExecution.ts)
- [`services/tools/toolOrchestration.ts`](../src/services/tools/toolOrchestration.ts)

通俗理解：

这部分是在学“怎么给 agent 做一套靠谱的手脚”。

## 3. 权限系统设计

建议重点学：

- tool permission context 怎么工作
- allow / deny / ask 三套规则怎么组合
- 为什么要有 `shouldAvoidPermissionPrompts`
- 后台 agent 和前台 agent 的权限差异怎么处理

为什么值得学：

一旦 agent 真能改文件、跑命令、联网，权限系统就是核心，不是附属品。

建议继续读：

- [`utils/permissions/permissions.ts`](../src/utils/permissions/permissions.ts)
- [`utils/permissions/filesystem.ts`](../src/utils/permissions/filesystem.ts)
- [`cli/structuredIO.ts`](../src/cli/structuredIO.ts)
- [`tools/BashTool/*`](../src/tools/BashTool/)

通俗理解：

这部分是在学“怎么让 agent 有能力，但不失控”。

## 4. Context Window 管理与压缩策略

建议重点学：

- auto compact
- microcompact
- snip compact
- context collapse
- tool result budget

为什么值得学：

这几乎就是 Claude Code 最有工程价值的部分之一。  
大多数 agent 死在“上下文越来越大，越来越贵，越来越慢”，而 Claude Code 是认真在做长期运行治理。

建议继续读：

- [`query.ts`](../src/query.ts)
- [`services/compact/*`](../src/services/compact/)
- [`utils/toolResultStorage.ts`](../src/utils/toolResultStorage.ts)

通俗理解：

这部分是在学“怎么让 agent 不健忘，也不撑爆脑子”。

## 5. Subagent / 多 agent 架构

建议重点学：

- `AgentTool` 为什么设计成工具
- subagent 和 forked agent 的区别
- context 共享和隔离的平衡
- 背景 agent、前台 agent、teammate 的差别

为什么值得学：

这是你以后做 agent 平台时最容易真正用上的能力。

建议继续读：

- [`tools/AgentTool/*`](../src/tools/AgentTool/)
- [`utils/forkedAgent.ts`](../src/utils/forkedAgent.ts)
- [`tasks/*`](../src/tasks/)
- [`utils/swarm/*`](../src/utils/swarm/)

通俗理解：

这部分是在学“怎么让一个 agent 变成一个小团队”。

## 6. 长会话与记忆系统

建议重点学：

- session transcript 怎么持久化
- file history 怎么做快照
- memory prompt 怎么注入
- auto memory / dream / daily log 的设计

为什么值得学：

“长期助手”是否成立，关键不只在模型强不强，而在记忆和恢复能力。

建议继续读：

- [`utils/sessionStorage.ts`](../src/utils/sessionStorage.ts)
- [`utils/sessionRestore.ts`](../src/utils/sessionRestore.ts)
- [`memdir/*`](../src/memdir/)
- [`services/autoDream/*`](../src/services/autoDream/)

通俗理解：

这部分是在学“怎么让 Claude 不只是这一次聪明，而是长期有连续性”。

## 7. SDK / Bridge / Remote Control 设计

建议重点学：

- 为什么会有 `StructuredIO`
- SDK message 和普通 REPL message 的差别
- bridge 为什么单独成体系
- session ingress / CCR transport 是什么

为什么值得学：

如果你以后想做 IDE 插件、远程 agent、云端控制台，这部分非常重要。

建议继续读：

- [`cli/structuredIO.ts`](../src/cli/structuredIO.ts)
- [`cli/transports/*`](../src/cli/transports/)
- [`bridge/*`](../src/bridge/)
- [`remote/*`](../src/remote/)

通俗理解：

这部分是在学“怎么把 Claude 从一个本地 CLI，变成一个可嵌入、可远程控制的平台能力”。

## 8. 产品化设计而不是只有算法设计

建议重点学：

- prompt suggestion
- background task UX
- task notification
- teammate/session UI 状态
- plugin / MCP 热更新

为什么值得学：

Claude Code 强的地方不只是 agent loop，还在于“怎么把 agent 做成用户能长期用的产品”。

建议继续读：

- [`cli/print.ts`](../src/cli/print.ts)
- [`utils/sdkEventQueue.ts`](../src/utils/sdkEventQueue.ts)
- [`state/*`](../src/state/)
- [`services/mcp/*`](../src/services/mcp/)
- [`utils/plugins/*`](../src/utils/plugins/)

通俗理解：

这部分是在学“怎么把一个技术 demo 变成产品”。

## 我最建议你的学习顺序

如果你想效率最高，我建议按这个顺序继续：

1. Prompt 体系
2. Tool 体系
3. 权限系统
4. Context 压缩
5. Subagent / 多 agent
6. Session / memory
7. SDK / bridge
8. 产品化细节

原因是：

- 先学 Claude 怎么“想”
- 再学 Claude 怎么“动手”
- 再学 Claude 怎么“安全地长期工作”

## 哪些是最值得模仿到自己项目里的

如果你以后做自己的 agent 平台，我会优先借鉴这些：

### 1. Query loop 和 runtime harness 分离

这让系统更容易演进。

### 2. Tool schema + permission 深度融合

这让工具调用更可控。

### 3. 子 agent 复用同构 runtime

这让多 agent 不是额外补丁，而是自然扩展。

### 4. Context 治理是主架构，不是补救措施

这点非常少见，但非常值钱。

### 5. session / task / event / bridge 统一建模

这让 Claude Code 能同时服务 CLI、SDK、remote control。

## 如果你偏“找工作/面试”方向

你可以把学习重点放到这几个关键词上：

- Agent runtime
- Tool orchestration
- Permission-aware agent systems
- Context compression / long-context management
- Multi-agent architecture
- Session persistence and recovery
- Structured I/O protocol
- Human-in-the-loop workflows

这些词比单纯说“我研究过 Claude”更有含金量。

## 如果你偏“自己做产品”方向

你可以重点想这几个问题：

1. 你的 agent 是单轮工具调用，还是长期 runtime？
2. 你的工具权限边界在哪里？
3. 你的上下文怎样持续压缩？
4. 你的子 agent 是真的独立，还是只是函数分支？
5. 你的 session 能不能恢复？
6. 你的系统怎样向 IDE / Web / mobile 输出统一事件？

## 最后给你的一个建议

学习 Claude Code，最有价值的不是背“它有哪些功能”，而是学它背后的工程观：

- 把 agent 当成 runtime 来设计
- 把工具当成协议来设计
- 把权限当成一等公民
- 把长会话治理当成核心问题
- 把产品体验和系统架构放在一起看

如果你愿意，我下一步可以继续帮你补两份很适合深入学习的文档：

1. `Claude Prompt System Design Guide`
2. `Claude Tool & Permission Architecture Guide`