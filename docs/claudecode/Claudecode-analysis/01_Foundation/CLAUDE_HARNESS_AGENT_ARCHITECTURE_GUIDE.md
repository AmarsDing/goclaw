# Claude Code Harness 工程与 Agent 架构解读

日期：2026-04-08

## 这份文档适合怎么读

如果你现在看这套代码有点晕，这是很正常的。

Claude Code 这类工程最容易让人困惑的地方在于：它不是“一个 prompt + 一个 API 调用”那么简单，而是一个完整的 agent runtime，也就是一层“harness”。  
这里的 `harness`，你可以先把它理解成：

> 一套把“用户输入、模型、工具、权限、队列、状态、子 agent、后台任务、会话恢复”全部拴在一起的运行外壳。

通俗一点：

- 模型像大脑
- 工具像手脚
- harness 像神经系统 + 调度系统 + 安全系统 + 记忆系统

这份文档会尽量回答这几个问题：

1. Claude Code 的主执行链路到底怎么走
2. `print.ts`、`QueryEngine.ts`、`query.ts` 各自负责什么
3. 工具调用是如何编排的
4. subagent 是怎么被启动和隔离的
5. 为什么代码里会同时出现队列、状态机、task、bridge、MCP
6. 难理解的地方，我会加“白话解释”

## 一句话总览

Claude Code 的 agent 架构可以粗略拆成 6 层：

1. 入口层：`main.tsx`
2. harness 调度层：`cli/print.ts`
3. 单轮会话引擎层：`QueryEngine.ts`
4. 模型-工具循环层：`query.ts`
5. 工具执行层：`Tool.ts` + `services/tools/*`
6. 子 agent / 后台 agent 层：`tools/AgentTool/*`

如果你只记一条主链路，先记这个：

```text
main.tsx
  -> print.ts
     -> ask() / QueryEngine
        -> query()
           -> 模型输出 assistant message / tool_use
           -> runTools()
           -> 写回 tool_result
           -> 继续下一轮
     -> 如有需要，再经由 AgentTool 启动 subagent
        -> runAgent()
           -> 其实又跑了一套 query() 循环
```

## 先说“harness”到底是什么

这份代码里没有一个唯一的 `Harness.ts` 文件，但从工程结构上看，harness 主要就是这些模块一起组成的：

- [`main.tsx`](../src/main.tsx)
- [`cli/print.ts`](../src/cli/print.ts)
- [`QueryEngine.ts`](../src/QueryEngine.ts)
- [`query.ts`](../src/query.ts)
- [`cli/structuredIO.ts`](../src/cli/structuredIO.ts)
- [`utils/messageQueueManager.ts`](../src/utils/messageQueueManager.ts)
- [`Tool.ts`](../src/Tool.ts)
- [`services/tools/toolOrchestration.ts`](../src/services/tools/toolOrchestration.ts)
- [`tools/AgentTool/*`](../src/tools/AgentTool/)
- [`state/AppStateStore.ts`](../src/state/AppStateStore.ts)

白话解释：

- `main.tsx` 负责“把机器启动起来”
- `print.ts` 负责“总调度”
- `QueryEngine.ts` 负责“一次问答/一次 turn 怎么跑”
- `query.ts` 负责“模型和工具之间怎么循环”
- `Tool.ts` 定义“工具到底长什么样”
- `AgentTool` 负责“让 agent 再去叫别的 agent 干活”

所以你看到的不是一个简单 CLI，而是一套“小型操作系统式”的 agent runtime。

## 模块关系图

先看整体结构：

```text
用户输入 / SDK 输入 / Bridge 输入
  -> main.tsx
  -> print.ts
     -> StructuredIO
     -> messageQueueManager
     -> QueryEngine.ask()
        -> processUserInput()
        -> fetchSystemPromptParts()
        -> query()
           -> API 请求
           -> assistant message streaming
           -> tool_use blocks
           -> runTools() / StreamingToolExecutor
           -> tool_result messages
           -> 继续 query loop
     -> AppState / tasks / sdk events / bridge forwarding
     -> AgentTool
        -> runAgent()
           -> createSubagentContext()
           -> query() again
```

## 第 1 层：入口层 `main.tsx`

`main.tsx` 不是 agent 核心，但它负责非常关键的“开机场”工作。

它做的事情大概是：

1. 解析 CLI 参数
2. 初始化环境、设置、插件、策略
3. 决定是进 REPL 还是 headless/SDK 流
4. 把执行权交给后面的 runtime

可以看：

- [main.tsx](../src/main.tsx)

尤其这里：

- `await run()` 说明 `main.tsx` 的主要职责是“准备好以后，把主循环交出去”

白话解释：

`main.tsx` 像机场值机柜台。  
它不负责飞行，但它决定：

- 你坐哪班机
- 带不带托运行李
- 身份是否通过检查
- 后面把你送到哪个登机口

## 第 2 层：真正的 harness 总调度器 `print.ts`

如果只选一个文件当“Claude Code harness 核心”，我会选：

- [print.ts](../src/cli/print.ts)

原因是它做了三件最核心的事情：

1. 接输入
2. 调度 turn
3. 协调外围系统

### `print.ts` 在做什么

从代码看，它负责：

- 建立 `StructuredIO`
- 初始化 sandbox
- 恢复初始消息
- 维护 `mutableMessages`
- 维护 `readFileState`
- 管理 MCP 客户端
- 管理 bridge
- 管理后台任务
- 管理 command queue
- 调用 `ask()`
- 决定何时继续下一轮、何时等待后台 agent

关键位置：

- `const structuredIO = getStructuredIO(...)`
- `const mutableMessages: Message[] = initialMessages`
- `const run = async () => { ... }`
- `await drainCommandQueue()`
- `for await (const message of ask(...))`

可以看：

- [print.ts](../src/cli/print.ts#L587)
- [print.ts](../src/cli/print.ts#L1142)
- [print.ts](../src/cli/print.ts#L1865)
- [print.ts](../src/cli/print.ts#L2147)

### 为什么这里叫 harness 很合适

因为它不是只管“打印输出”。

它像一个总控室，里面同时盯着：

- 用户有没有发新消息
- 模型这一轮有没有结束
- 工具有没有在跑
- 背景 agent 有没有完成
- MCP server 有没有变化
- bridge 那边要不要同步
- 队列里还有没有待处理命令

白话解释：

你可以把 `print.ts` 理解成“中央调度台”。  
模型、工具、任务、队列、桥接、输入输出，全都归它统一编排。

## 第 3 层：输入输出协议层 `StructuredIO`

`StructuredIO` 是另一个很关键但容易被忽略的模块：

- [structuredIO.ts](../src/cli/structuredIO.ts)

### 它解决什么问题

Claude Code 不是只服务人类终端。

它还要兼容：

- SDK
- Bridge / remote-control
- 结构化 JSON 输入输出
- 权限请求和控制消息

所以它需要一层协议适配器，把“stdin/stdout 字符流”变成“结构化消息流”。

### 它的核心职责

1. 读取输入流并解析成结构化消息
2. 维护 pending control requests
3. 发送 `can_use_tool` 之类的控制请求
4. 接收 `control_response`
5. 支持 sandbox / MCP elicitation / permission flow

关键点：

- `structuredInput` 是结构化输入生成器
- `outbound` 是统一输出流
- `pendingRequests` 管理等待中的控制请求

白话解释：

`StructuredIO` 像“同声传译器”。

用户世界、SDK 世界、远程控制世界，说的都不完全是一种语言。  
它负责统一翻译成 Claude Code 内部能处理的消息格式。

## 第 4 层：统一命令队列 `messageQueueManager`

再看一个非常关键的模块：

- [messageQueueManager.ts](../src/utils/messageQueueManager.ts)

### 为什么 agent 要有队列

因为 Claude Code 不是“用户发一句，模型回一句”这么线性的。

运行过程中可能同时出现：

- 用户新输入
- background agent 完成通知
- orphaned permission
- proactive tick
- system notification

如果没有统一队列，这些事件就会互相打架。

### 它的设计重点

1. 所有命令都走同一个队列
2. 有优先级：`now > next > later`
3. React UI 和 headless path 都能消费它
4. 队列是模块级，而不是只存在于 React state 里

关键注释已经讲得很清楚：

- user input、task notification、orphaned permissions 都走同一队列

白话解释：

这就像客服系统里的“工单池”。

不是谁喊得响谁先处理，而是：

- 紧急的先来
- 一般的排后面
- 系统通知不要饿死用户输入

### 为什么这很重要

因为 Claude Code 支持“长 turn 中途插话”。

比如：

- agent 正在跑工具
- 用户突然发了个新问题
- 或者 permission response 先回来了

这时队列和中断机制一起，才能保证系统不会乱。

## 第 5 层：`ask()` 和 `QueryEngine`，为什么要再包一层

很多人第一次看会问：

> 已经有 `query.ts` 了，为什么还要 `QueryEngine.ts` 和 `ask()`？

这是一个非常好的问题。

### `ask()` 是什么

`ask()` 在这里：

- [QueryEngine.ts](../src/QueryEngine.ts#L1186)

它做的不是“直接完成整个推理”，而是：

1. 构造一个 `QueryEngine`
2. 把当前 turn 的上下文装进去
3. 调用 `engine.submitMessage(...)`
4. 最后把 read file cache 同步回外层

### `QueryEngine` 又做什么

`QueryEngine` 是“单会话 / 单对话引擎”。

类定义在：

- [QueryEngine.ts](../src/QueryEngine.ts#L175)

它持有这些跨 turn 状态：

- `mutableMessages`
- `abortController`
- `permissionDenials`
- `readFileState`
- `totalUsage`

也就是说，`QueryEngine` 处理的是：

> 一段会话中的连续多轮消息，而不只是一次 API 请求。

白话解释：

- `print.ts` 是总调度台
- `QueryEngine` 是一次正式“对话处理器”
- `query.ts` 才是处理器内部真正驱动模型和工具循环的发动机

## 第 6 层：`QueryEngine.submitMessage()` 的工作流

这是理解 harness 的关键。

它的大概流程是：

1. 准备 system prompt、user context、system context
2. 处理用户输入
3. 执行 slash commands / hooks / attachments 预处理
4. 生成/更新消息列表
5. 构造 `ToolUseContext`
6. 调用 `query()`
7. 一边收流，一边记 transcript / usage / file state
8. 最终产出 result

关键代码看这里：

- [QueryEngine.ts](../src/QueryEngine.ts#L284)
- [QueryEngine.ts](../src/QueryEngine.ts#L335)
- [QueryEngine.ts](../src/QueryEngine.ts#L410)
- [QueryEngine.ts](../src/QueryEngine.ts#L675)

### 难点 1：为什么系统 prompt 不是在 `query.ts` 里拼

因为 `QueryEngine` 需要在进入模型循环前，把“用户输入处理”和“系统上下文拼装”都准备好。

它调用：

- `fetchSystemPromptParts()`

这个函数专门负责构造 API cache key 前缀相关部分：

- system prompt
- user context
- system context

看这里：

- [queryContext.ts](../src/utils/queryContext.ts#L30)

白话解释：

`QueryEngine` 先做“备菜”，`query.ts` 再开始“炒菜”。

## 第 7 层：真正的模型-工具循环 `query.ts`

如果说 `print.ts` 是总调度器，`QueryEngine` 是会话引擎，那么：

- [query.ts](../src/query.ts)

就是 Claude Code 的“agentic loop 核心”。

### `query()` 干什么

`query()` 是一个 async generator，它会：

1. 接收当前 messages、system prompt、tool context
2. 发起模型请求
3. 读取 streaming assistant 输出
4. 识别 `tool_use`
5. 执行工具
6. 把 `tool_result` 写回 message 历史
7. 决定是否继续下一轮
8. 最终返回 terminal result

入口在：

- [query.ts](../src/query.ts#L219)

### 为什么它是 loop

因为一轮 agent 并不一定只打一发 API。

很常见的情况是：

1. 模型先说“我要读文件”
2. harness 执行 `FileRead`
3. 返回 `tool_result`
4. 模型继续分析
5. 又决定运行 `Bash`
6. 再返回 `tool_result`
7. 最后才给出答案

这就是 agent loop。

白话解释：

不是：

> 用户问一句，模型答一句

而是：

> 用户问一句，模型想一下，去拿工具，回来继续想，再去拿工具，最后才答一句

## 第 8 层：`query.ts` 里真正重要的几个机制

### 8.1 上下文压缩与预算控制

`query.ts` 在真正请求模型前，会做很多上下文治理：

- tool result budget
- snip compact
- microcompact
- context collapse
- autocompact

可以看：

- [query.ts](../src/query.ts#L369)
- [query.ts](../src/query.ts#L412)
- [query.ts](../src/query.ts#L440)
- [query.ts](../src/query.ts#L453)

白话解释：

这一步像“出门前先整理背包”。  
上下文太长，模型就背不动，所以 harness 会先压缩、裁剪、重排，再送去模型。

### 8.2 Query chain tracking

`queryTracking` 会记录 chainId 和 depth。

它的用途是：

- 追踪一条查询链到底走了多少层
- 方便分析和调试复杂 tool loop

白话解释：

就像给这次 agent 行动贴了一个“流程编号”。

### 8.3 `ToolUseContext`

这是整套工具系统的核心上下文对象。

定义在：

- [Tool.ts](../src/Tool.ts#L158)

里面包含：

- 可用工具列表
- AppState getter/setter
- read file cache
- abort controller
- agent definitions
- messages
- permission context
- file history / attribution 更新器
- requestPrompt / elicitation / notification 等能力

白话解释：

`ToolUseContext` 就像工具调用时随身携带的“工具箱背包”。

工具不需要自己去全局乱找状态，harness 把它需要的东西都装在一个 context 里传进去。

## 第 9 层：工具系统是怎么设计的

`Tool.ts` 是所有工具的统一接口定义。

看：

- [Tool.ts](../src/Tool.ts)

### 它做了什么

它抽象了工具的共性：

- 输入 schema
- 输出 schema
- `isEnabled`
- `isConcurrencySafe`
- `isReadOnly`
- `call`
- UI 渲染
- tool result 映射

这意味着在 Claude Code 里：

> 工具不是随便一段函数，而是“带权限、带 schema、带 UI、带并发约束”的一等公民

白话解释：

别把工具理解成普通 helper function。  
在这里，工具更像“一个标准化插件单元”。

## 第 10 层：工具执行编排 `toolOrchestration.ts`

模型输出 `tool_use` 以后，Claude Code 不是简单一个个执行。

它会先判断：

- 哪些工具可并发
- 哪些必须串行

看这里：

- [toolOrchestration.ts](../src/services/tools/toolOrchestration.ts)

### 它的策略

1. 先把工具调用分批
2. 连续的并发安全工具可以并行跑
3. 非并发安全工具单独串行跑
4. 执行完成后再按顺序回放 context modifier

白话解释：

这像厨房排工序：

- 洗菜、切菜可以并行
- 真正下锅爆炒必须一个锅一个锅来

这也是为什么 Claude Code 比“玩具 agent”工程感强很多。  
它考虑的不是“能不能调工具”，而是“怎么安全、稳定、有顺序地调工具”。

## 第 11 层：流式工具执行 `StreamingToolExecutor`

Claude Code 还有一个更高级的机制：

- [StreamingToolExecutor.ts](../src/services/tools/StreamingToolExecutor.ts)

### 它解决什么问题

有些模型在 streaming 输出时，会边生成边吐出多个 tool call。  
如果你等整个 assistant message 完全结束再执行工具，会浪费时间。

所以它做的是：

> 工具一边流出来，一边开始执行。

### 它的关键能力

- 并发安全工具可并行
- 非并发工具独占
- 结果按接收顺序回放
- 支持 fallback / abort / sibling error 取消

白话解释：

这像工厂流水线：  
不是等整辆车设计完才开始生产轮胎，而是图纸一出来，轮胎线先开工。

## 第 12 层：subagent 架构，真的很关键

Claude Code 的 agent 不是只有一个主 agent。

它支持主 agent 再启动子 agent，这就是：

- [AgentTool.tsx](../src/tools/AgentTool/AgentTool.tsx)
- [runAgent.ts](../src/tools/AgentTool/runAgent.ts)

### `AgentTool` 是什么

`AgentTool` 是一个标准工具，但它的作用不是读文件或者跑命令，而是：

> 启动另一个 agent 来干活

也就是说：

- 主 agent 调用 `AgentTool`
- `AgentTool` 再去创建一个新的 agent runtime
- 子 agent 跑自己的 query loop

白话解释：

这不是“函数调用函数”，更像“经理再雇一个实习生去单独做一个小任务”。

## 第 13 层：`runAgent()` 到底做了什么

`runAgent()` 在：

- [runAgent.ts](../src/tools/AgentTool/runAgent.ts#L248)

它的核心流程大概是：

1. 选择 agent model
2. 分配 agentId
3. 组装 initial messages
4. 克隆或创建 file state
5. 准备 user/system context
6. 覆盖 permission mode
7. 选择这个 agent 能用的工具
8. 生成 agent 专属 system prompt
9. 建立 abort controller
10. 注册 hooks / preload skills / MCP
11. 创建 subagent context
12. 调用 `query()`
13. 记录 sidechain transcript
14. 清理资源

### 最重要的一句

子 agent 不是“主 agent 内部的一段子逻辑”。

它本质上是：

> 复用同一套 query/runtime 机制，再跑一次相对独立的 agent loop。

这点非常关键。

白话解释：

Claude Code 的 subagent 不是“if 分支里多做一点事”，而是“重新开了一个小型 Claude Code”。

## 第 14 层：subagent 为什么要有独立上下文

这部分最容易让人看漏。

`runAgent()` 会调用和 subagent 隔离有关的逻辑，例如：

- `createSubagentContext()`
- file state clone
- abort controller 处理
- allowedTools 覆盖

而 `forkedAgent.ts` 里也明确写了一个很重要的目标：

> 共享 cache-critical params，但隔离 mutable state

看：

- [forkedAgent.ts](../src/utils/forkedAgent.ts#L1)

### 白话解释

这里追求的是一个很微妙的平衡：

- 该共享的共享
  例如 prompt cache 前缀，尽量复用父 agent 的缓存

- 该隔离的隔离
  例如可变消息、文件读取状态、权限行为、后台任务生命周期

如果完全共享：

- 会互相污染状态

如果完全不共享：

- 会重复花很多 token

所以这个架构的高级之处在于：

> 它不是简单“复制一份上下文”，而是有选择地共享和隔离

## 第 15 层：为什么这里要有 AppState

Claude Code 不是纯函数式 pipeline。

它有很多需要跨组件、跨 turn、跨 agent 共享的运行状态，所以有：

- [AppStateStore.ts](../src/state/AppStateStore.ts)

这里的 `AppState` 很大，包含：

- toolPermissionContext
- tasks
- mcp clients/tools/resources
- plugins
- agent definitions
- notifications
- session hooks
- teammate 状态
- bridge 状态
- kairos/proactive 相关状态

### 为什么不能只靠 message history

因为很多东西不是“对话内容”，而是“运行时状态”。

比如：

- 哪个后台 agent 在跑
- 当前 permission mode 是什么
- 哪些 MCP tools 已连接
- 当前正在查看哪个 teammate

这些不能只靠 message list 表达。

白话解释：

`messages` 是聊天记录。  
`AppState` 是操作系统状态栏。

两者都需要，但不是一回事。

## 第 16 层：task 系统为什么重要

Claude Code 的 agent 不是只有前台同步执行。

它还有：

- background agent
- shell task
- workflow task
- remote agent task
- teammate task

因此整个 harness 需要把“长时间运行的工作单元”纳入 task 系统。

在 `print.ts` 里你会看到：

- command queue draining 完之后，还要检查 background tasks
- 如果有任务还在跑，就继续 wait / drain / re-check

关键位置：

- [print.ts](../src/cli/print.ts#L2368)

白话解释：

这不是传统聊天机器人“回一句就结束”的模型。  
它更像一个有前台线程和后台任务的 IDE 助手。

## 第 17 层：SDK 事件队列，为什么还要再来一层

另一个很工程化的设计是：

- [sdkEventQueue.ts](../src/utils/sdkEventQueue.ts)

### 它做什么

它把这些事件排队发给 SDK consumer：

- `task_started`
- `task_progress`
- `task_notification`
- `session_state_changed`

### 为什么不用普通 message 直接顶掉

因为这些不是用户会话语义里的“聊天消息”，而是外部宿主要消费的 runtime event。

白话解释：

用户消息是“内容层”。  
SDK event 是“系统层信号”。

就像一个 IDE 插件既要看聊天内容，也要看“这个后台任务开始了没有、结束了没有”。

## 第 18 层：为什么这套架构读起来容易乱

因为它不是按“分层 textbook”写的，而是按“真实工程需求”长出来的。

最容易让人乱的几个点：

### 18.1 `print.ts` 和 `QueryEngine.ts` 看起来都像主循环

其实不是冲突，而是分工：

- `print.ts` 负责“runtime 调度”
- `QueryEngine.ts` 负责“单对话 turn 执行”

### 18.2 `query.ts` 和 `runAgent.ts` 都在跑 query

这是刻意设计。

因为 subagent 本质上就是再跑一套同构 runtime，而不是写一个完全不同的逻辑栈。

### 18.3 `messages` 和 `AppState` 同时存在

因为：

- `messages` 负责 conversation history
- `AppState` 负责 runtime state

### 18.4 `StructuredIO`、queue、bridge、sdk events 都像“通信层”

它们确实都属于通信/协调层，但面向对象不同：

- `StructuredIO`：CLI/SDK 协议
- `messageQueue`：内部待处理命令
- `bridge`：远程会话同步
- `sdkEventQueue`：对宿主暴露系统事件

## 第 19 层：你可以这样理解 Claude Code 的“agent runtime 哲学”

我看完这套代码之后，会把它总结成下面这几条设计哲学。

### 1. agent 不是一次 API 调用，而是持续循环

这点体现在 `query.ts`。

### 2. 工具不是裸函数，而是标准化能力单元

这点体现在 `Tool.ts`。

### 3. 子 agent 不是特殊分支，而是同构子 runtime

这点体现在 `AgentTool + runAgent + query()`。

### 4. 会话内容和运行时状态必须分离

这点体现在 `messages` 与 `AppState` 的双轨设计。

### 5. 队列、中断、后台任务是一等公民

这点体现在 `messageQueueManager` 和 `print.ts`。

### 6. cache、context、permissions、streaming 都是 agent 工程的一部分

这也是为什么 Claude Code 读起来会比普通“agent demo”复杂很多，但也强很多。

## 第 20 层：最值得你重点学习的文件

如果你是为了学习，我建议按这个顺序读：

1. [QueryEngine.ts](../src/QueryEngine.ts)
   先理解一次 turn 是怎么被组织起来的。

2. [query.ts](../src/query.ts)
   再理解模型和工具是怎么循环的。

3. [Tool.ts](../src/Tool.ts)
   搞懂工具接口设计。

4. [toolOrchestration.ts](../src/services/tools/toolOrchestration.ts)
   看工具并发与串行调度。

5. [print.ts](../src/cli/print.ts)
   最后回来看整体调度，你会清楚很多。

6. [AgentTool.tsx](../src/tools/AgentTool/AgentTool.tsx)
   再看子 agent 启动入口。

7. [runAgent.ts](../src/tools/AgentTool/runAgent.ts)
   看 subagent 怎样复用主 runtime。

## 第 21 层：最通俗的最终总结

### 如果把 Claude Code 比作一个公司

- `main.tsx` 是前台和行政
- `print.ts` 是总调度中心
- `QueryEngine.ts` 是项目经理
- `query.ts` 是真正干活的工作流引擎
- `Tool.ts` 是标准化岗位说明书
- `toolOrchestration.ts` 是排班系统
- `AgentTool` 是招聘和外包系统
- `runAgent.ts` 是分出去的小组项目管理
- `AppState` 是公司运营看板
- `messageQueue` 是待办池
- `StructuredIO` 是对外沟通接口

### 如果把 Claude Code 比作一台电脑

- `main.tsx` 是 BIOS/启动器
- `print.ts` 是内核调度器
- `QueryEngine.ts` 是用户态会话管理器
- `query.ts` 是 CPU 执行循环
- `Tool.ts` 是系统调用接口
- `AgentTool` 是启动子进程
- `AppState` 是内存里的系统状态
- `messageQueue` 是事件队列

## 源码证据索引

- [main.tsx](../src/main.tsx)
- [print.ts](../src/cli/print.ts)
- [structuredIO.ts](../src/cli/structuredIO.ts)
- [messageQueueManager.ts](../src/utils/messageQueueManager.ts)
- [QueryEngine.ts](../src/QueryEngine.ts)
- [query.ts](../src/query.ts)
- [Tool.ts](../src/Tool.ts)
- [toolOrchestration.ts](../src/services/tools/toolOrchestration.ts)
- [StreamingToolExecutor.ts](../src/services/tools/StreamingToolExecutor.ts)
- [AgentTool.tsx](../src/tools/AgentTool/AgentTool.tsx)
- [runAgent.ts](../src/tools/AgentTool/runAgent.ts)
- [forkedAgent.ts](../src/utils/forkedAgent.ts)
- [AppStateStore.ts](../src/state/AppStateStore.ts)
- [sdkEventQueue.ts](../src/utils/sdkEventQueue.ts)
- [queryContext.ts](../src/utils/queryContext.ts)
- [processUserInput.ts](../src/utils/processUserInput/processUserInput.ts)

## 最后一句

如果你以后自己做 agent 框架，可以重点借鉴 Claude Code 这 4 个点：

1. `query loop` 和 `runtime harness` 分层
2. 工具标准化接口，而不是散装函数
3. subagent 复用同构 runtime，而不是写另一套逻辑
4. 队列、权限、缓存、后台任务都当成架构核心，而不是后补功能