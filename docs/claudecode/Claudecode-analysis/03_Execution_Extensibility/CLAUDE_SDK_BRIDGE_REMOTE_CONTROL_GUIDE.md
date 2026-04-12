# Claude Code SDK / Bridge / Remote Control 架构解读

## 1. 这篇文档在讲什么

Claude Code 不只是一个本地 CLI，它其实还被设计成一个“可嵌入、可远程、可多端接入”的 agent runtime。

这背后有三层关键能力：

1. SDK 协议层
2. Bridge / Remote Control 连接层
3. 会话远程持久化与恢复层

通俗一点说：

- SDK 层回答“Claude Code 怎么和别的程序说话”
- Bridge 层回答“Claude Code 怎么变成可远程控制的会话”
- Remote session 层回答“远端会话怎么持续活着、怎么恢复、怎么同步权限和状态”

如果你想学的不只是“一个终端 agent”，而是“一个能接入 IDE / Web / 手机 / 远端工作器的 agent 平台”，这一部分非常值得学。

---

## 2. 先给一个总图

### 2.1 本地 SDK 模式

最基本的模式是：

1. 外部程序通过 stdin/stdout 和 Claude Code 通信
2. `StructuredIO` 负责协议解析与输出
3. `print.ts` 作为 headless runtime 驱动主循环
4. `ask()` / `query.ts` 运行真正的 agent loop
5. 输出结果和中间事件再按 SDK 消息流发回去

关键文件：

- [structuredIO.ts](../src/cli/structuredIO.ts)
- [print.ts](../src/cli/print.ts)

### 2.2 Remote Control / Bridge 模式

Bridge 模式则更进一步：

1. Claude Code 在本机注册一个远程环境
2. 服务端给它派发 work / session
3. 本地进程持续 poll / heartbeat
4. 远端客户端通过会话协议接入
5. 会话事件、权限请求、消息流、结果都能双向同步

关键文件：

- [bridgeMain.ts](../src/bridge/bridgeMain.ts)
- [bridgeApi.ts](../src/bridge/bridgeApi.ts)
- [RemoteSessionManager.ts](../src/remote/RemoteSessionManager.ts)
- [sessionIngress.ts](../src/services/api/sessionIngress.ts)

---

## 3. 先理解一个关键思想：Claude Code 不是“UI 应用”，而是“runtime”

很多人第一次看 Claude Code 会把它当成：

- 一个终端交互程序

但从这套代码看，更准确的理解是：

- 它是一个 agent runtime
- 终端只是其中一个壳

为什么这么说？

因为它内部明确分了这些层：

1. 核心 agent loop
2. 结构化协议输入输出
3. 工具权限与交互回调
4. 本地会话 / 远端会话持久化
5. 多种 transport

通俗解释：

- Claude Code 不只是“界面”
- 它更像一个发动机
- 终端、bridge、远程 viewer、IDE 都是不同驾驶舱

---

## 4. `StructuredIO`：SDK 协议的核心门面

最基础也是最值得读的文件之一：

- [structuredIO.ts](../src/cli/structuredIO.ts)

### 4.1 它解决的是什么问题

如果要把 Claude Code 嵌入别的程序，不能靠“屏幕抓取终端文本”，必须有稳定协议。

`StructuredIO` 做的就是：

- 从输入流读取结构化消息
- 解析 JSON line
- 区分 user message、SDK message、control request/response
- 维护 pending request
- 输出结构化 stdout 事件

### 4.2 它为什么不是简单 read/write

因为 agent 系统不是单向聊天，它有很多“半同步”交互：

- 用户发消息
- agent 中途请求权限
- 外部系统回复 allow / deny
- agent 继续执行
- 中间还有 task progress、status change、suggestion 等事件

所以 `StructuredIO` 不是普通 stdin/stdout 包装器，而是一个小型协议 runtime。

### 4.3 它内部几个关键点

#### `pendingRequests`

这表示：

- 当前已经发出的 control request
- 还在等外部响应

最典型的就是工具权限请求。

白话解释：

- 就像前端发了一个弹窗审批请求
- 还没拿到用户点“允许/拒绝”
- 这时系统里必须有个地方挂着这个未完成请求

#### `outbound`

`StructuredIO` 没有让所有地方都直接写 stdout，而是统一走一个 `outbound` stream。

这很关键。

因为这样能保证：

- 消息顺序更稳定
- control request 不会乱插队
- 中间事件和最终 result 的相对顺序可控

白话解释：

- 不是谁想喊就抢着喊
- 而是大家都先进一个发言队列

#### `resolvedToolUseIds`

这个细节很工程化，用来防止重复 `control_response` 导致同一个 tool_use 被二次处理。

源码注释直接点出一个风险：

- 否则会造成重复 assistant message
- 最后 API 报 `"tool_use ids must be unique"`

这类设计体现出 Claude Code 不是 demo，而是在处理真实分布式/异步协议里的重复投递问题。

---

## 5. `print.ts`：headless runtime 的总调度器

如果说 `StructuredIO` 是协议门面，那么：

- [print.ts](../src/cli/print.ts)

就是 headless / SDK 模式下的总调度器。

### 5.1 它在整个架构里的位置

它接在中间：

- 上游接 `StructuredIO`
- 下游接 `ask()` 和工具执行

它的职责不是 UI 渲染，而是：

1. 收消息
2. 排队
3. 批处理
4. 调 agent loop
5. 发送中间事件和结果
6. 和 bridge / remote IO / permission 流打通

### 5.2 `run()` 才是 headless 模式的主驱动循环

在 `print.ts` 里，`run()` 干了这些事：

1. 标记 session 为 `running`
2. 更新 SDK MCP 等外部状态
3. drain command queue
4. 把一批 prompt 合并
5. 调 `ask()`
6. 实时转发消息
7. 处理 background tasks
8. 最后切回 `idle`

这个过程很像一个事件驱动 runtime，而不是一次函数调用。

### 5.3 为什么它要自己维护 command queue

因为输入不是只有“用户发一条 prompt”这么简单。

还可能有：

- 普通 prompt
- orphaned permission
- task notification
- proactive tick

这些都要进入统一队列处理。

白话解释：

- `run()` 像一个总控台
- 各种消息不是直接冲进模型
- 而是先进调度中心排队

### 5.4 批处理 prompt 的意义

`drainCommandQueue()` 会把连续兼容的 prompt 合并后再送给 `ask()`。

这件事很值得学。

因为在真实系统里，长 turn 执行时，用户或系统可能又进来了几条消息。

如果每条都单独再开一个 turn：

- 成本高
- 上下文碎
- 体验差

所以 Claude Code 会适度 coalesce。

这是一种典型的 runtime 优化。

---

## 6. `print.ts` 里最值得学的几处

### 6.1 `notifySessionStateChanged('running'/'idle')`

这说明 session 不只是消息流，还有显式的生命周期状态。

状态定义在：

- [sessionState.ts](../src/utils/sessionState.ts)

包括：

- `idle`
- `running`
- `requires_action`

这对前端、远端控制、移动端通知都非常重要。

白话解释：

- UI 不需要靠“最后一条消息是不是 result”去猜当前是不是还在工作
- runtime 会主动告诉你“我现在忙 / 我空闲 / 我卡在等审批”

### 6.2 `heldBackResult`

这是一个非常妙的细节。

当后台 agent 还在跑时，`print.ts` 会暂时 hold 住最终 result，不立刻发。

为什么？

因为如果先把结果发了，而后台任务还在继续：

- 对 SDK 消费端来说，turn 已经“结束”
- 但其实系统还在生成后续 task event

这样很容易让状态机乱掉。

所以它选择：

- 先继续发中间 task progress
- 真到合适时机，再把 result 放出来

这是一种“事件时序一致性”设计。

### 6.3 SDK events 不是等 turn 结束才发

`print.ts` 会频繁调用：

- `drainSdkEvents()`

这意味着：

- `task_started`
- `task_progress`
- `task_notification`
- `session_state_changed`

这些都可以中途实时发出去。

这非常关键，因为外部系统要的是：

- “正在发生什么”

而不是：

- “最后给我一大包结果”

### 6.4 proactive tick 是怎么接进 headless runtime 的

在 `print.ts` 里还能看到：

- `scheduleProactiveTick()`

如果开启 `PROACTIVE` 或 `KAIROS`，队列空了也会注入一个 meta tick，继续驱动模型自主循环。

这说明：

- 自主 agent 并不是另写一套 runtime
- 而是复用同一个 command queue + ask loop，只是多了一类系统注入命令

这点很优雅。

---

## 7. `SessionState`：为什么要显式建模会话状态

对应文件：

- [sessionState.ts](../src/utils/sessionState.ts)

### 7.1 这里做了什么

它不只是定义了 `idle/running/requires_action`，还定义了：

- `RequiresActionDetails`
- `SessionExternalMetadata`
- 各种 listener

也就是说，Claude Code 不把 session 状态当成 UI 层临时变量，而是当成正式协议的一部分。

### 7.2 `requires_action` 很关键

很多 agent 系统只知道两种状态：

- 正在跑
- 跑完了

Claude Code 明确加了一种：

- `requires_action`

这代表：

- agent 不是失败了
- 也不是完成了
- 它只是卡在等待外部动作，比如审批

这对远程会话太重要了。

白话解释：

- 就像外卖订单不是“送达/取消”两种状态
- 还会有“等待商家接单”“等待用户确认”

### 7.3 `pending_action` 镜像到 external_metadata

源码里还会把 pending action 镜像进 external metadata。

这很聪明，因为这样前端和远端系统就不用强依赖完整事件流扫描，也能直接知道：

- 当前卡在哪个 tool
- 当前要审批什么

这是“状态投影”的典型做法。

---

## 8. Bridge 模式到底是什么

Bridge 不是简单的“远程 shell”。

从 [bridgeMain.ts](../src/bridge/bridgeMain.ts) 看，它更像：

- 本地机器对远端控制平面注册为一个 environment
- 服务端向这个 environment 派发 session work
- 本地 Claude Code 拉取 work 并执行

这已经是“worker architecture”了。

### 8.1 `runBridgeLoop()` 的本质

`runBridgeLoop()` 是 bridge 模式的总循环。

它管理：

- active sessions
- work ids
- ingress tokens
- 定时器
- cleanup
- heartbeat
- token refresh

你可以把它理解成：

- 一个本地 worker daemon 的主循环

而不是：

- 一个“连上 websocket 就结束”的简单连接器

### 8.2 为什么它要有 heartbeat

因为服务端需要知道：

- 这个本地 worker 还活着吗
- 当前 work 还在被处理吗
- 这条租约是不是要续

源码里 `heartbeatActiveWorkItems()` 很明确地在做这件事。

而且它还区分结果：

- `ok`
- `auth_failed`
- `fatal`
- `failed`

这说明 heartbeat 不是装饰，而是调度协议的一部分。

白话解释：

- 这不是“报个平安”
- 而是“维持我对这份任务的处理权”

### 8.3 为什么还要 token refresh / reconnect

Bridge 运行时间可能很长，session token 会过期。

如果过期后什么都不做，就会出现一种很糟糕的状态：

- 本地以为自己还在跑
- 远端以为它已经死了或失联了

所以 bridgeMain 做了两件事：

1. 定时 token refresh
2. v2 场景下通过 `reconnectSession()` 触发 server re-dispatch

这个设计很成熟，因为它处理了“长时间运行 worker 的凭证续期”问题。

---

## 9. `RemoteSessionManager`：远端 viewer / controller 的门面

对应文件：

- [RemoteSessionManager.ts](../src/remote/RemoteSessionManager.ts)

### 9.1 它扮演什么角色

它负责协调：

- WebSocket 接收远端 session 消息
- HTTP POST 发送用户消息
- permission request/response 流

这其实是一个“远端会话客户端 SDK”。

### 9.2 为什么收发分成 WebSocket + HTTP

因为这两类流量性质不同：

- 用户发消息：一次性事件，HTTP 很自然
- 远端持续回消息、状态变更、权限请求：需要长连接订阅，WebSocket 更适合

这是一种很常见也很稳的分层。

### 9.3 它怎么处理远端权限请求

当收到 `control_request`，尤其是 `can_use_tool` 时：

1. 先记进 `pendingPermissionRequests`
2. 调回调给上层 UI / 客户端
3. 等用户或系统回复
4. 再发 `control_response`

这意味着远端控制不是“只能看结果”，而是能真正参与 agent 的交互闭环。

白话解释：

- 手机或网页端不只是看直播
- 还可以替你点“允许执行这个命令”

---

## 10. Bridge 和 SDK 权限流是怎么打通的

这块是整套架构里最有意思的地方之一。

### 10.1 本地 SDK 权限流

`StructuredIO` 自己就支持：

- 发 `control_request`
- 收 `control_response`

### 10.2 Bridge 权限流

当接入 bridge 后，`print.ts` 会：

1. 把本地产生的 permission request 转发给 bridge
2. bridge 远端批准后，再注入回本地 `StructuredIO`

源码里的关键逻辑在：

- [print.ts](../src/cli/print.ts)

其中你能看到：

- `structuredIO.setOnControlRequestSent(...)`
- `structuredIO.setOnControlRequestResolved(...)`
- `structuredIO.injectControlResponse(...)`

### 10.3 为什么这个设计很巧

因为它没有为 bridge 另写一套权限系统，而是：

- 复用已有 SDK control protocol
- bridge 只是把远端响应“桥接”回来

这就是非常标准的“协议复用优先”设计。

通俗解释：

- 不是造两套审批电路
- 而是在原审批电路中间接了一个远程开关

---

## 11. `sessionIngress`：远端会话日志的持久化协议

对应：

- [sessionIngress.ts](../src/services/api/sessionIngress.ts)

### 11.1 它在解决什么问题

如果会话运行在远端控制模式下，日志不能只写本地文件，否则：

- 远端切设备就看不到历史
- 会话崩了很难恢复

所以要把 transcript 也发到远端。

### 11.2 它不是简单 append，而是带并发控制

`appendSessionLogImpl()` 做了几件很成熟的事：

1. 用 `Last-Uuid` 做 optimistic concurrency control
2. 409 时尝试 adopt server last uuid
3. 网络 / 5xx / 429 做 retry
4. 401 直接视为不可重试
5. 同一 session 上 append 还要串行化

### 11.3 为什么同 session 还要 sequential

代码里有：

- `sequentialAppendBySession`

这说明作者很清楚：

- 即使逻辑上是一个 session
- 运行时也可能有并发写入

如果不串行化，lastUuid 链就可能被自己打乱。

白话解释：

- 同一本账本，不能两个人同时抢着写上一行

### 11.4 409 adopt server uuid 很值得学

当客户端发现自己 lastUuid 落后了，不是直接报错退出，而是：

- 先试着接上服务器当前头部
- 然后继续重试

这个设计很适合长连接 / 多进程 / 崩溃恢复场景。

它体现的思想是：

- 尽量自愈，不要把一致性问题直接甩给用户

---

## 12. `hydrateRemoteSession()` 和 `getTeleportEvents()`

这部分把远端会话恢复能力补齐了。

### 12.1 `hydrateRemoteSession()`

在 [sessionStorage.ts](../src/utils/sessionStorage.ts) 里，它会：

1. 根据 sessionId 切换本地 session
2. 从远端拉 logs
3. 写成本地 transcript 文件

也就是说：

- 远端会话最终还是回到本地统一 transcript 结构

### 12.2 `getTeleportEvents()`

在 [sessionIngress.ts](../src/services/api/sessionIngress.ts) 里，它还能通过分页拉取 session events。

这说明系统在往更现代的 session event API 迁移。

其背后思想是：

- transcript 不只是一个大文件
- 也可以被视为 event stream

这对跨端同步、断线恢复、分页加载都更友好。

---

## 13. Claude Code 的远程架构到底强在哪

不是因为它支持“远程连接”这四个字，而是因为它把远程模式当成正式运行形态，而不是附属功能。

### 13.1 它有正式的 worker loop

Bridge 不是临时脚本，而是有：

- poll
- ack
- heartbeat
- reconnect
- token refresh
- cleanup

### 13.2 它有正式的协议层

不是传终端文本，而是传：

- SDK message
- control_request/response
- task events
- session state

### 13.3 它有正式的状态模型

不是靠 UI 猜状态，而是显式建模：

- running
- idle
- requires_action

### 13.4 它有正式的日志一致性机制

不是“尽量写上去”，而是：

- per-session serial append
- optimistic concurrency
- recovery after 409

这些加在一起，才是一个像样的 remote agent architecture。

---

## 14. 这套架构和普通“agent API”差在哪

很多 agent 框架只做到：

1. 收一个请求
2. 调模型
3. 返回文本

Claude Code 这套明显更深一层：

1. 它支持长会话
2. 它支持中途审批
3. 它支持后台任务
4. 它支持远端 viewer/controller
5. 它支持 session 恢复
6. 它支持 worker lifecycle

通俗解释：

- 普通 agent API 像“发一条短信，收一条回复”
- Claude Code 更像“一个持续在线、可以中途互动、可跨设备接续的工作会话”

---

## 15. 最值得借鉴的设计点

### 15.1 把 CLI 当 runtime，不要当 UI

可借鉴点：

- 把核心 loop 与 UI 解耦
- 让终端、IDE、Web 都复用一套执行引擎

### 15.2 协议要支持控制流，不只是消息流

Claude Code 的 SDK 协议不只有 user/assistant message，还有：

- control_request
- control_response
- control_cancel_request
- task events
- session state events

可借鉴点：

- agent 协议必须原生支持“等待审批”“取消”“进度”“状态切换”

### 15.3 用统一协议桥接本地和远端

Claude Code 没为 remote 再造一套完全不同的 agent 核心协议，而是复用 SDK control 流。

可借鉴点：

- 优先桥接现有协议，不要并行维护两套语义系统

### 15.4 Worker 要有 heartbeat 和 lease 思维

只要你的 agent 是长时间运行、可远端控制的，就要考虑：

- 心跳
- 租约
- 令牌续期
- 掉线重连

可借鉴点：

- 不要把远程 agent 当成“加了 websocket 的本地程序”

### 15.5 状态要显式建模

最少也建议建模：

- idle
- running
- requires_action

可借鉴点：

- UI 和远端系统不要靠猜测推导状态

### 15.6 transcript append 要考虑并发一致性

`Last-Uuid + 409 adopt + sequential append` 很值得抄。

可借鉴点：

- 长会话日志不是随便 append 就行
- 一定要考虑崩溃恢复和多写者竞争

---

## 16. 难点白话解释

### 16.1 `StructuredIO` 到底像什么

白话版：

- 它像 Claude Code 的“协议总机”
- 外部程序打电话进来，它负责接线、分流、挂起未完成审批、按顺序把消息发出去

### 16.2 `print.ts` 为什么这么重要

白话版：

- 它不是拿来“打印”的
- 它是 headless 模式的运行时总控器
- 很多你以为 UI 层才做的事情，其实都是它在调度

### 16.3 Bridge 为什么不像 websocket 聊天室

白话版：

- 它不是“消息互传”而已
- 它还负责 worker 注册、任务租约、心跳、会话恢复、权限中转

所以更像一个远程 agent 执行基础设施。

### 16.4 `requires_action` 为什么不能省

白话版：

- agent 不是只有“忙”和“闲”
- 它还有一种状态是“卡在等人”

如果不显式建模，远端 UI 体验会非常混乱。

### 16.5 为什么日志还要带 `Last-Uuid`

白话版：

- 因为远端会话日志本质是事件链
- 不是谁都能随便往末尾插
- `Last-Uuid` 就像在说：“我是在你当前这条链后面接这一条”

---

## 17. 一句话总结

Claude Code 的 SDK / Bridge / Remote Control 架构，真正厉害的地方不在于“能远程连”，而在于它把 agent 当成一个长期运行的 runtime 来设计：

- 有正式协议
- 有控制流
- 有显式状态
- 有 worker 生命周期
- 有远端日志一致性
- 有恢复和续期机制

如果你以后想做自己的 agent 平台，而不只是一个本地 demo，这一层设计是特别值得拆开学习的。
