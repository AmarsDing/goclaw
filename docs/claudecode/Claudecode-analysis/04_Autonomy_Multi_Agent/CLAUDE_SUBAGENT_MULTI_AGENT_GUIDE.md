# Claude Code Subagent / Multi-Agent 协作架构解读

## 1. 这篇文档在讲什么

Claude Code 的 `AgentTool` 不是一个“再开一次模型调用”的小工具，而是整套多代理协作架构的入口。

它至少支持这几种形态：

1. 同步 subagent
2. 后台 async subagent
3. fork worker
4. teammate / team 协作
5. worktree 隔离子代理
6. remote agent

也就是说，Claude Code 里的“自动助手”不是单线程干到底，而是会在需要时把任务拆出去，让别的 agent 并行或隔离完成。

通俗一点说：

- 普通聊天模型像一个人闷头干活
- Claude Code 更像一个会分工的项目经理 + 执行系统

---

## 2. 先给一个总图

### 2.1 主链路

一次典型的子代理调用，主链路大致是：

1. 主代理在 `query()` 中决定调用 `AgentTool`
2. `AgentTool.call()` 解析参数、选 agent、决定同步还是异步
3. `runAgent()` 为子代理构造一套独立上下文
4. 子代理内部再次进入 `query()`
5. 子代理不断产出消息和进度
6. 父代理或任务系统接收这些进度
7. 子代理结束后，结果被汇总、分类、通知给父代理

关键文件：

- [AgentTool.tsx](../src/tools/AgentTool/AgentTool.tsx)
- [runAgent.ts](../src/tools/AgentTool/runAgent.ts)
- [agentToolUtils.ts](../src/tools/AgentTool/agentToolUtils.ts)
- [forkSubagent.ts](../src/tools/AgentTool/forkSubagent.ts)
- [print.ts](../src/cli/print.ts)

### 2.2 一句话理解

Claude Code 的 subagent，本质上是：

- 在父 agent 里面再起一个“缩小版 Claude Code runtime”

这句话非常重要。

因为它解释了为什么代码会这么复杂：

- 它不是简单 RPC
- 它是同构 runtime 嵌套

---

## 3. `AgentTool` 到底负责什么

先看入口：

- [AgentTool.tsx](../src/tools/AgentTool/AgentTool.tsx)

### 3.1 它不是只管“启动”

`AgentTool.call()` 实际负责很多事：

1. 解析输入 schema
2. 处理 `subagent_type`
3. 处理 `team_name` / `name`
4. 判断是不是 teammate spawn
5. 判断是不是 fork path
6. 判断是否 worktree / remote 隔离
7. 选择 agent 定义
8. 构造 prompt messages
9. 选择同步还是后台
10. 注册 task、进度、通知和收尾逻辑

所以 `AgentTool` 更像：

- 多代理执行入口控制器

而不是：

- 一个单纯的“spawn 函数”

### 3.2 它的输入为什么这么丰富

从 schema 可以看出，它支持：

- `description`
- `prompt`
- `subagent_type`
- `model`
- `run_in_background`
- `name`
- `team_name`
- `mode`
- `isolation`
- `cwd`

这说明 Claude Code 对子代理的定位不是“隐藏内部能力”，而是“正式的可配置执行单元”。

通俗解释：

- 它不是偷偷帮你开个线程
- 而是明确让模型学会“派什么人、去哪里、怎么跑、是否后台、是否隔离”

---

## 4. 子代理的几种模式到底有什么区别

这块是最容易混的，我拆开讲。

### 4.1 普通同步 subagent

特点：

- 父代理发起
- 父代理等待它完成
- 结果直接回到当前 turn

适合：

- 小任务
- 需要马上拿结果继续推理

### 4.2 后台 async subagent

特点：

- 父代理先放它出去跑
- 自己不一定一直等
- 通过 task progress / task notification 回来

适合：

- 耗时任务
- 可并行任务

### 4.3 fork worker

特点：

- 不选具体 `subagent_type`
- 直接继承父对话上下文和 system prompt
- 更像“父代理切分出一个并行 worker”

适合：

- 要求高度继承父上下文
- 希望 prompt cache 命中最大化

### 4.4 teammate / team

特点：

- 有名字
- 有 team context
- 可被 `SendMessage({to: name})` 路由
- 更像长期活着的协作成员

适合：

- 多角色协同
- 团队编排

### 4.5 worktree 隔离 subagent

特点：

- 运行在隔离工作树
- 同仓库、不同工作副本

适合：

- 涉及文件修改但不想和父代理互相踩工作区

### 4.6 remote agent

特点：

- 不在本地进程执行
- 通过远程 session 跑

适合：

- 需要远端运行环境
- 本地不方便执行的任务

---

## 5. `AgentTool.call()` 的分流逻辑

这是最值得顺着读源码的一段。

### 5.1 先处理 team / teammate

如果传了 `team_name + name`，`AgentTool` 不走普通 subagent，而是转到：

- `spawnTeammate(...)`

这说明 Claude Code 把：

- “给团队里再加一个成员”

和：

- “临时拉个子代理做一次性任务”

视为两种不同语义。

这点很重要。

很多系统会把两者混成同一个 spawn，但 Claude Code 区分了：

- 临时 worker
- 团队成员

### 5.2 然后处理 fork path

如果没指定 `subagent_type`，而 fork 功能开着，就走：

- `FORK_AGENT`

这不是“默认 general-purpose agent”，而是专门的 fork 逻辑。

源码里甚至有递归 fork 防护：

- fork child 里不能再 fork

这是因为 fork worker 的定位是：

- 立刻执行，不再做编排

### 5.3 再处理 isolation

如果指定了：

- `isolation: 'worktree'`
- 或 ant-only 的 `remote`

就走对应的隔离路径。

这意味着“是否隔离执行”并不是 agent 类型的一部分，而是执行环境的一部分。

这个分层很合理。

### 5.4 最后判断 `shouldRunAsync`

决定是否后台运行的因素不止一个：

- `run_in_background === true`
- agent 定义里 `background === true`
- coordinator mode
- fork subagent 打开
- Kairos / proactive 模式

这说明“同步 / 异步”不是一个简单用户参数，而是：

- 运行时策略决策

通俗解释：

- 有时候不是你说后台就后台
- 而是系统觉得“这个场景必须后台，不然后面的主循环会被卡住”

---

## 6. `runAgent()` 为什么是关键中的关键

如果 `AgentTool.call()` 是总调度入口，那么：

- [runAgent.ts](../src/tools/AgentTool/runAgent.ts)

就是子代理真正被“生出来”的地方。

### 6.1 它做的不是单次调用，而是重建一个 mini runtime

`runAgent()` 会做这些事：

1. 构建初始消息
2. 准备 user/system context
3. 组装 agent tool pool
4. 建立 agent-specific permission mode
5. 注入 hooks
6. 预加载 skills
7. 初始化 agent MCP servers
8. 构造独立的 `ToolUseContext`
9. 记录 sidechain transcript 和 metadata
10. 再次调用 `query()`

你会发现，这几乎是“再跑一次 Claude Code”。

只是它跑的是：

- 子会话
- 子上下文
- 子工具池

这就是为什么我前面说：

- subagent 本质上是“套娃 runtime”

### 6.2 为什么不是简单继承父上下文所有东西

`runAgent()` 不是无脑继承。

它会做选择性继承和瘦身，比如：

- Explore / Plan 这类 read-only agent 会省掉一些 `claudeMd`
- 也会省掉没必要的 `gitStatus`

这说明作者很清楚：

- 子代理不是越像父代理越好
- 而是要按角色裁剪上下文

这是很值得借鉴的设计。

---

## 7. 子代理的工具池是怎么控制的

这部分主要在：

- [agentToolUtils.ts](../src/tools/AgentTool/agentToolUtils.ts)

### 7.1 `filterToolsForAgent()`

这个函数很关键。

它说明 Claude Code 不是“子代理默认继承所有工具”，而是按规则过滤：

- 全部 agent 禁用的工具
- custom agent 额外禁用的工具
- async agent 只允许一部分工具
- 某些特殊 teammate 会额外放开少量工具

这很重要，因为：

- 子代理越多，权限爆炸风险越大

通俗解释：

- 公司里不是每个实习生都给 root 权限
- 子代理也一样

### 7.2 `resolveAgentTools()`

这个函数做的是：

1. 展开 wildcard
2. 处理 disallowed tools
3. 校验 tool spec
4. 特殊处理 `Agent(...)` 这种带 allowedAgentTypes 的嵌套授权

特别值得注意的是：

- `Agent(worker,researcher)` 这种 spec 不只是允许用 AgentTool
- 还在约束“能再派哪些类型的 agent”

这说明 Claude Code 的 agent 权限是递归可控的。

不是：

- 给了 AgentTool 就完全放飞

而是：

- 给了 AgentTool，但只能派指定工种

---

## 8. fork worker 为什么这么特别

这部分在：

- [forkSubagent.ts](../src/tools/AgentTool/forkSubagent.ts)

### 8.1 fork 的核心目标不是“更智能”，而是“更共享缓存”

这块最精彩的地方在于：

- 它极度追求 prompt cache 命中

`buildForkedMessages()` 会想办法让 fork children 的 API request prefix 尽量字节级一致。

做法包括：

1. 保留父 assistant message 的完整 tool_use / thinking / text
2. 对所有 tool_result 用统一 placeholder
3. 只让最后的 directive 文本不同

这说明 fork worker 的设计目标之一是：

- 并行子任务还能尽量共享前缀缓存

这是非常“系统级优化”的思路。

### 8.2 fork child 为什么有一大段硬规则

`buildChildMessage()` 给 fork child 注入了一段非常强的执行规则，比如：

- 不要再 fork
- 不要聊天
- 直接用工具
- 最后再汇报一次
- 范围严格受限

这段东西本质上是：

- 把 fork child 从“通用 agent”压成“执行 worker”

通俗解释：

- 父代理像经理
- fork child 像派出去的外包工程师
- 只负责一块，干完报结果，不要自己再当经理

### 8.3 worktree notice 很值得学

如果 fork child 跑在 worktree 里，还会附加一段 notice：

- 你继承的是父上下文
- 但你现在工作路径变了
- 旧路径要翻译
- 文件可能已经变了，改前要重读

这段提示非常产品化。

因为隔离执行最容易出的问题就是：

- 上下文讲的是 A 路径
- 实际干活在 B 路径

Claude Code 用 prompt 显式修正这个错位。

---

## 9. 同步子代理和异步子代理的差别

`AgentTool.tsx` 里这部分也非常值得学。

### 9.1 同步路径

同步路径会：

1. 立即运行 `runAgent()`
2. 流式拿消息
3. 直接把结果继续交给当前父 turn

特点：

- 父代理当前 turn 被占用
- 但逻辑最简单

### 9.2 异步路径

异步路径会：

1. 先 `registerAsyncAgent`
2. 在后台 `void runAsyncAgentLifecycle(...)`
3. 通过任务系统更新状态
4. 结束后 enqueue notification 给父代理

特点：

- 父 turn 可以先往前走
- 子代理像后台任务一样管理

### 9.3 为什么同步路径还能“中途转后台”

这一点特别有意思。

Claude Code 允许一个原本前台的同步 agent：

- 先开始跑
- 如果太久或被后台化
- 再切到 background task 模式

这说明它的 agent lifecycle 不是死板的。

通俗解释：

- 像你本来盯着一个脚本跑
- 后来发现太慢，就把它丢到后台继续跑

这对交互体验非常实用。

---

## 10. `runAsyncAgentLifecycle()`：后台 agent 的完整生命周期

在：

- [agentToolUtils.ts](../src/tools/AgentTool/agentToolUtils.ts)

这里是后台 agent 非常核心的生命周期管理器。

### 10.1 它干了什么

大致顺序是：

1. 建 tracker
2. 持续消费 `makeStream()`
3. 收集 agent messages
4. 更新 progress
5. 发 task progress
6. 完成后 `finalizeAgentTool()`
7. 标记 task completed
8. 可选做 handoff classifier
9. 发完成通知

失败或被 kill 时：

1. 记录错误或 partial result
2. 更新 task 状态
3. 发 failed / killed 通知
4. 清理 skills / dump state

### 10.2 为什么这不是简单 Promise 包一层

因为后台 agent 不只是“等结果”。

后台 agent 运行时还有很多中间语义：

- 当前在干什么
- 最近用了什么工具
- token 到哪了
- 是否被用户终止
- 是否需要 handoff 风险提示

所以它必须是一个正式 lifecycle，而不是：

- `await subagent()`

### 10.3 `finalizeAgentTool()` 很值得读

它会从子代理消息里提取：

- 最终文本结果
- 工具使用次数
- token
- 总时长
- usage

同时还会打一个 cache eviction hint。

这说明子代理结束不是“返回字符串”，而是“返回结构化任务产物”。

---

## 11. handoff classifier：父代理为什么不盲信子代理

这是一个很妙的设计。

在 `classifyHandoffIfNeeded()` 里，Claude Code 会在某些条件下对交接结果再做一次安全审查。

它关注的是：

- 子代理是不是做了可能违规或危险的动作
- 父代理接手时要不要附加 warning

这非常成熟。

因为多代理系统里一个大问题是：

- 父代理可能会天然信任“自己派出去的人”

Claude Code 在这里显式加入：

- handoff review

通俗解释：

- 经理不能因为是自己带的下属就完全不复核

这是多 agent 安全设计里非常值得借鉴的一点。

---

## 12. 子代理为什么要单独写 sidechain transcript

这一点在 `runAgent.ts` 里非常关键：

- `recordSidechainTranscript(initialMessages, agentId)`
- `writeAgentMetadata(...)`

### 12.1 为什么不能和主 transcript 混写

因为子代理有自己的：

- 会话轨迹
- metadata
- worktree path
- description

如果全塞进主 transcript，会有几个问题：

1. 主链太乱
2. 恢复子代理很难
3. 多个子代理并发时更难管理

所以 Claude Code 的做法是：

- 主对话链一条
- 每个子代理一条 sidechain

这特别像分布式 tracing 里的：

- 主 span
- 子 span

---

## 13. `runAgent()` 为什么像“再跑一个 Claude Code”

这里值得再强调一次。

从 `runAgent()` 看，它会重新准备：

- system prompt
- user/system context
- tool pool
- permission mode
- hooks
- skills
- MCP clients
- transcript
- metadata
- query loop

这说明 Claude Code 的架构不是：

- 主代理调用一个小函数拿子结果

而是：

- 主代理启动一个同构但缩小配置的子 runtime

这就是为什么它的扩展性很强。

如果以后要加：

- 新 agent type
- 新隔离模式
- 新工具权限规则

大部分都能复用已有 runtime。

---

## 14. team / teammate 和普通 subagent 到底差在哪

### 14.1 普通 subagent

更像：

- 一次性外包任务

特点：

- 为一个任务而生
- 结束就回收

### 14.2 teammate

更像：

- 团队里的命名成员

特点：

- 有名字
- 在 roster 里可见
- 可以路由消息
- 有 team lead / teammate 关系

### 14.3 为什么这很重要

因为“并行执行”有两种完全不同需求：

1. 临时加算力
2. 持续多人协作

Claude Code 同时支持这两种，而不是只做第一种。

---

## 15. 最值得借鉴的设计点

### 15.1 把 subagent 做成同构 runtime，而不是特判函数

可借鉴点：

- 子代理最好复用主代理的大部分运行机制
- 不要为了“快做出来”单独写一套简化逻辑

### 15.2 spawn 不是一种，要区分执行语义

Claude Code 区分了：

- sync
- async
- fork
- teammate
- remote
- worktree

可借鉴点：

- “新起一个 agent”不等于一种行为
- 要先定义清楚语义类型

### 15.3 子代理工具权限必须单独收窄

可借鉴点：

- 父代理有权限，不代表子代理也该有
- 尤其 async / fork / remote 子代理，更要限制工具面

### 15.4 用 sidechain transcript 管理子代理

可借鉴点：

- 子代理最好有独立事件链
- 方便恢复、调试、并发管理和 UI 展示

### 15.5 前台转后台是高价值体验设计

可借鉴点：

- 不要把任务同步/异步做成不可逆
- 长任务应允许平滑后台化

### 15.6 handoff 要做安全复核

可借鉴点：

- 多 agent 系统里，代理间交接结果不应默认可信

---

## 16. 难点白话解释

### 16.1 为什么说 subagent 是“套娃 runtime”

白话版：

- 因为它不是拿父代理的一点上下文去调一个小函数
- 而是又重新准备 prompt、tools、permissions、hooks、MCP、transcript，再跑一遍 query loop

### 16.2 fork 为什么不是普通 agent

白话版：

- fork 更像“从当前大脑里切出一个并行 worker”
- 它最关心的是继承父上下文和缓存前缀，而不是切换到另一个专业角色

### 16.3 teammate 和 subagent 有什么最直观区别

白话版：

- subagent 像一次性临时工
- teammate 像团队成员，有名字、有位置、可持续沟通

### 16.4 为什么要 sidechain transcript

白话版：

- 子代理的过程不能都糊进主聊天记录
- 不然恢复和调试都会非常痛苦

### 16.5 为什么同步 agent 还能转后台

白话版：

- 因为真实工作里很多任务一开始不知道会不会很久
- 系统允许你先盯着，发现慢了再挂后台，这样体验更自然

---

## 17. 一句话总结

Claude Code 的 subagent / multi-agent 设计，并不是“主模型偶尔调用另一个模型”，而是把代理协作当成一套正式运行时能力：

- 有多种 spawn 语义
- 有独立工具权限
- 有 sidechain transcript
- 有前后台生命周期
- 有 fork 缓存优化
- 有 handoff 审查

如果你想做一个真正可扩展的 agent 系统，这套设计特别值得学。
