# Claude Code Hooks / Event Bus / Lifecycle Architecture Guide

## 1. 这篇文档讲什么

这一篇专门讲 Claude Code 里一条很容易被忽略、但其实非常“产品级”的主线：

- Hook 是怎么接进系统的
- 生命周期事件是怎么被触发的
- 为什么它不是“工具执行时顺手跑个脚本”那么简单
- 它和主消息流、UI、SDK、权限、后台任务之间是怎么协作的

如果前面的 Harness / Agent 架构文档讲的是“Claude Code 怎么思考和调用工具”，那么这一篇讲的是：

**Claude Code 怎么在运行过程中给外部世界留出插口，并且保证整套系统还能稳定运转。**

这块非常值得学习，因为很多 agent 项目只做到：

- 有 prompt
- 有 tool
- 有 loop

但 Claude Code 多做了一层：

- 有 lifecycle
- 有 hook contract
- 有事件广播
- 有异步与中断治理
- 有 UI/SDK/remote 共用的通知出口

这就是“能跑”到“能产品化”的差别。

---

## 2. 先给一个总印象

你可以把 Claude Code 的 Hook 系统理解成：

**在 agent runtime 的关键时刻，系统会发出标准事件，并允许外部脚本/函数/HTTP 回调参与进来。**

它不是一个简单的“插件回调”。

它更像一套小型运行时协议，至少包含这几层：

1. 生命周期定义
2. Hook 配置与匹配
3. Hook 执行器
4. Hook 事件广播
5. Hook 输出回流
6. Hook 与 UI / SDK / 队列 / Session 的衔接

从源码角度看，关键文件包括：

- [utils/hooks.ts](../src/utils/hooks.ts)
- [utils/hooks/hookEvents.ts](../src/utils/hooks/hookEvents.ts)
- [Tool.ts](../src/Tool.ts)
- [cli/print.ts](../src/cli/print.ts)
- [setup.ts](../src/setup.ts)
- [state/onChangeAppState.ts](../src/state/onChangeAppState.ts)
- [utils/messageQueueManager.ts](../src/utils/messageQueueManager.ts)

---

## 3. 一句话理解 Hook

最通俗的说法：

**Hook 就是 Claude Code 在某个关键节点主动喊你一声：“我现在要开始这个阶段了，你要不要参与？”**

比如：

- 会话开始时
- tool 调用前
- tool 调用后
- compact 前后
- session 结束时
- 用户提交 prompt 时
- subagent 启停时

这时候系统会：

- 收集对应 hook 配置
- 匹配条件
- 执行 hook
- 读取结果
- 再决定是否影响主流程

所以 Hook 不是“外挂日志”。

它是：

**runtime 在关键生命周期里开放出来的可控扩展点。**

---

## 4. Hook 的大致模块调用链

下面这条链，是理解 Claude Code Hook 架构最重要的一张图。

```text
用户输入 / 会话启动 / tool执行 / compact / session结束
  -> 进入 Claude Code 主流程
  -> 命中某个 lifecycle 节点
  -> 构造 HookInput
  -> 从 settings / session / plugin / skill 中收集 hooks
  -> 按 event + matcher 过滤
  -> 调用 utils/hooks.ts 中的执行逻辑
  -> execAgentHook / execPromptHook / execHttpHook / shell hook
  -> 产生 started / progress / response 事件
  -> hookEvents.ts 广播事件
  -> UI / SDK / remote 侧按需消费
  -> Hook 的结果再反馈给主流程
  -> 主流程继续推进
```

如果换成更工程化的分层：

```text
Lifecycle Layer
  setup.ts / sessionStart.ts / toolExecution / compact / shutdown

Hook Resolution Layer
  settings hooks + plugin hooks + skill hooks + session hooks

Hook Execution Layer
  utils/hooks.ts
  execAgentHook.ts / execPromptHook.ts / execHttpHook.ts / shell spawn

Hook Event Layer
  utils/hooks/hookEvents.ts

Consumption Layer
  cli/print.ts
  SDK output
  message queue
  session notifications
```

---

## 5. Hook 不是一个点，而是一整套“生命周期语言”

看 [utils/hooks.ts](../src/utils/hooks.ts) 的 import，你会发现它引入了大量类型：

- `SessionStartHookInput`
- `SessionEndHookInput`
- `SetupHookInput`
- `PreToolUseHookInput`
- `PostToolUseHookInput`
- `PreCompactHookInput`
- `PostCompactHookInput`
- `UserPromptSubmitHookInput`
- `SubagentStartHookInput`
- `SubagentStopHookInput`
- `PermissionRequestHookInput`
- `ElicitationHookInput`
- `FileChangedHookInput`
- `ConfigChangeHookInput`

这意味着 Claude Code 不是用一个模糊的 `event: string` 混过去。

它做的是：

**先把“系统会发生什么事”建模成一套明确的生命周期语言。**

这一步很关键。

很多人做 agent 扩展时会直接写：

```ts
emit("beforeTool")
emit("afterTool")
```

这当然也能跑，但问题是：

- 输入结构不清楚
- 谁能改什么不清楚
- 哪些阶段可阻塞、哪些阶段只能旁路不清楚
- 后面 SDK / UI / remote 都难统一

Claude Code 的做法更像是：

**先把事件世界定义出来，再让实现去挂靠。**

这就是成熟 runtime 的味道。

---

## 6. setup 阶段：Hook 不是临时读配置，而是启动时先建好地基

在 [setup.ts](../src/setup.ts) 里，Claude Code 很早就处理 Hook 相关初始化：

- 捕获 hooks 配置快照
- 初始化文件变化 watcher
- 处理 worktree 和 hook 的关系
- session memory 初始化时注册相关机制

这说明 Hook 不是“要触发时临时读一下配置文件”。

而是：

**在 runtime 启动阶段，就把 Hook 视为基础设施的一部分。**

这里很值得借鉴。

通俗解释：

如果把 Claude Code 想成一辆车，Hook 不是后备箱里临时拿出来的工具箱，而是出厂时就埋进车里的电路。

这样做的好处：

- 配置一致性更好
- 中途不容易被偷偷改坏
- watcher / session / queue 这些东西能提前接好
- 后面生命周期里调用就更稳定

---

## 7. Hook 配置来源：不是只有 settings.json

这块特别容易被低估。

从 [utils/hooks.ts](../src/utils/hooks.ts) 和相关 import 可以看出来，Hook 的来源并不单一，至少包括：

- 用户/项目 settings 里的 hooks
- plugin hooks
- skill hooks
- session hooks
- function hooks / callback hooks

也就是说，Claude Code 允许多种“扩展提供方”把 Hook 接进来。

这是典型的平台化设计。

通俗解释：

不是只有“主程序自己能挂钩子”。

而是：

- 用户配置能挂
- 插件能挂
- skill 能挂
- 某次会话临时逻辑也能挂

这就像一个大型商场，不只商场自己有广播系统，入驻品牌、临时活动、导购服务也都能合法接入。

---

## 8. Hook 匹配：不是所有 Hook 都会跑

Hook 系统很重要的一层是匹配逻辑。

在 [Tool.ts](../src/Tool.ts) 里可以看到与 Hook matcher 相关的设计，例如：

- tool 级别匹配
- 参数模式匹配
- permission rule 风格匹配

注释里也明确提到：

- 类似 `"git *"` 这种 matcher
- 从 `"Bash(git *)"` 里推导匹配规则

这说明 Claude Code 的 Hook 不是“某个事件来了就把所有脚本都跑一遍”。

而是：

**先做事件级过滤，再做 matcher 级过滤。**

通俗解释：

不是“厨房一响，全楼都跑出来”。

而是先问两件事：

1. 这是不是你负责的事件？
2. 这是不是你关心的那种具体输入？

只有都对上，Hook 才会执行。

这样就避免了：

- 噪音太多
- 无谓开销
- 用户配置越来越慢
- 插件互相干扰

---

## 9. Hook 执行层：Claude Code 不把所有 Hook 当成同一种东西

看 [utils/hooks.ts](../src/utils/hooks.ts) 的 import，会看到几个执行器：

- `execPromptHook`
- `execAgentHook`
- `execHttpHook`
- shell command / spawn 逻辑

这表明 Claude Code 的 Hook 不是单一 transport。

它支持多种执行方式：

- 本地 shell hook
- agent-style hook
- prompt-style hook
- HTTP hook

这是个很强的信号：

**Claude Code 把 Hook 看成“扩展协议”，而不是“只能执行 bash 脚本”。**

通俗解释：

很多系统的 hook 就是：

```bash
before_tool.sh
after_tool.sh
```

Claude Code 更像是：

- 你可以让本地 shell 来做
- 你可以让另一个 agent 来做
- 你可以把请求发给 HTTP 服务来做

这就把 Hook 从“脚本机制”提升成了“扩展集成接口”。

---

## 10. Hook 执行时为什么这么复杂

如果你只看功能，会觉得：

“跑个 hook 而已，为什么 hooks.ts 这么大？”

因为真实世界里 Hook 很麻烦，它至少要处理：

- 超时
- 中断
- stdout / stderr 收集
- async 背景执行
- 会不会阻塞主流程
- 结果怎么写回 UI
- 是否要唤醒模型
- 错误怎么降级
- telemetry 怎么记

这些事情加起来，Hook 就不再是“跑一行命令”。

它已经接近一个小型任务运行时了。

在 [utils/hooks.ts](../src/utils/hooks.ts) 里就能看到：

- `TOOL_HOOK_EXECUTION_TIMEOUT_MS`
- session end 专门更短的 timeout
- background 执行逻辑
- `registerPendingAsyncHook`
- `enqueuePendingNotification`
- `emitHookResponse`
- `createCombinedAbortSignal`

这套设计说明 Claude Code 默认接受一个现实：

**Hook 是不可靠的外部参与者。**

既然不可靠，就必须围起来治理。

---

## 11. 一个特别值得学的点：异步 Hook 不只是“放后台”

在 `executeInBackground()` 这一段逻辑里，可以看到 Claude Code 对异步 Hook 的处理非常讲究。

它不是简单地：

```ts
spawn(cmd, { detached: true })
```

而是同时考虑：

- stdout/stderr 还能不能继续被收集
- 结束后如何发 response 事件
- 某些 exit code 是否意味着要唤醒模型
- 新 prompt 到来时这个 hook 是否该继续活着
- 强制 cancel 时是否该被杀掉

尤其是 `asyncRewake` 这条分支，很能说明产品意识。

如果异步 Hook 完成后带来重要结果，系统会：

- 通过 `enqueuePendingNotification`
- 以 `task-notification` 形式塞进统一命令队列
- 在 idle 或 busy 场景下都能被主流程感知

通俗解释：

不是“后台脚本跑完就算了”。

而是：

**后台脚本跑完以后，Claude Code 还会想办法把这个结果重新送回大脑。**

这就很像一个真正的助手系统，而不是一次性 CLI。

---

## 12. Hook Event Bus：为什么还要单独做一层事件广播

[utils/hooks/hookEvents.ts](../src/utils/hooks/hookEvents.ts) 是这套系统里非常漂亮的一个文件。

它的定位很清楚：

**Hook 执行事件，不直接塞进主消息流，而是走一条独立的事件通道。**

文件开头就写得很直白：

- 这是一个 generic event system
- 和 main message stream 分离
- handler 可以决定怎么消费

也就是说，Claude Code 把“hook 的运行状态”抽成了另一条观察总线。

核心事件有三种：

- `started`
- `progress`
- `response`

这三种已经足够组成一个稳定的生命周期。

---

## 13. 为什么 Hook 事件不直接塞 messages

这点非常值得学习。

因为如果 Hook 事件直接混到 messages 里，会马上遇到几个问题：

1. 主会话 transcript 会被大量过程噪音污染
2. UI 不同场景对 hook 的展示需求不同
3. SDK 可能想拿到更结构化的数据，而不是渲染后的文本
4. 一些 event 只是临时进度，不适合永久入档

所以 Claude Code 把它拆开成：

- 主消息流：对话和工具结果
- Hook event bus：hook 运行态

通俗解释：

主消息流像“正式聊天记录”。

Hook event bus 像“后台工单状态流”。

这两者有关联，但不是一回事。

这就是为什么 Claude Code 不会把所有内部过程都硬塞给 transcript。

---

## 14. hookEvents.ts 的设计细节为什么很成熟

这个文件虽然不大，但很成熟，主要体现在几个点：

### 14.1 有 pending buffer

`pendingEvents` 会在 handler 尚未注册时暂存事件。

这意味着：

即使事件先发生，消费者后挂上来，也不会立刻丢失全部信息。

这对启动阶段尤其重要。

### 14.2 有 always-emitted 与 gating

它不是所有 hook event 都无脑发。

而是分成：

- 永远发的低噪音生命周期事件
- 只有启用 includeHookEvents / remote 模式时才发的更完整事件

这代表 Claude Code 很在意“默认噪音控制”。

### 14.3 有 progress interval

`startHookProgressInterval()` 定期拉 stdout/stderr/output，并且只在输出变化时发 progress。

这说明它不是暴力刷屏，而是做了节流式进度汇报。

### 14.4 response 统一落 debug log

即使 UI 不展示，完整 hook 输出仍会进 debug log。

这对排障非常关键。

---

## 15. Hook 和 print.ts 的关系：事件总线最终要有人消费

在 [cli/print.ts](../src/cli/print.ts) 里可以看到：

- `registerHookEventHandler`
- SDK / remote / structured output 相关逻辑
- session metadata 同步

这说明 `print.ts` 不只是“打印到终端”。

它其实是：

**主运行时和展示层/外部接口层之间的总协调器。**

Hook event bus 把事件抛出来以后，`print.ts` 等消费方再决定：

- 转成 SDK message
- 写到 structured output
- 驱动 remote session
- 在某些模式里更新状态显示

通俗解释：

`hooks.ts` 负责“把事情做出来”。

`hookEvents.ts` 负责“把事件广播出去”。

`print.ts` 负责“谁该看到什么”。

这三层拆得非常干净。

---

## 16. Hook 和统一消息队列：为什么二者要接上

[messageQueueManager.ts](../src/utils/messageQueueManager.ts) 是另外一个特别关键的文件。

它实现的是统一命令队列：

- user input
- task notification
- orphaned permission
- 其他系统命令

Hook 系统会在一些异步场景里把结果塞回这个队列，比如：

- 异步 hook 执行完成
- 需要给模型一个 task-notification
- 某种后台结果要唤醒下一轮处理

这背后的设计思想非常好：

**不要为了 Hook 再发明一套平行的唤醒机制，而是把它接回统一调度入口。**

通俗解释：

系统里已经有一个“待处理收件箱”，那异步 Hook 完成以后，最稳的办法不是自己偷偷叫醒模型，而是把消息投递到这个统一收件箱。

这样整个系统的调度规则就仍然是一套。

这是很成熟的 runtime 观念。

---

## 17. Hook 和 AppState：为什么说 lifecycle 不只是后端逻辑

[state/onChangeAppState.ts](../src/state/onChangeAppState.ts) 展示了另一个很成熟的点：

**状态变化本身也被视为一种生命周期事件。**

例如：

- permission mode 改变
- expandedView 改变
- settings 改变
- 某些全局配置改变

这里虽然不完全等同于 Hook，但思路是一致的：

- 某个状态变化
- 触发集中式 side effect
- 同步到外部系统
- 避免每个调用点都自己记得广播

最典型的是 permission mode：

这个文件把模式变化放到单一 choke point 去通知 CCR / SDK。

这和 Hook 系统非常像。

可以把它理解成：

**Claude Code 不只给“tool 前后”定义 lifecycle，也给“状态变化”定义 lifecycle。**

这就是产品级 runtime 的统一思想。

---

## 18. Hook 与权限系统的关系

从 [Tool.ts](../src/Tool.ts) 和 hooks 相关注释可以看出，Hook 和 permission 是深度耦合的。

这里的关键点不是“permission 会不会触发 hook”这么简单，而是：

- Hook 可以参与 pre-tool 阶段
- Hook 可以影响是否继续
- 某些 hook auto-approve 与 `canUseTool` 有协调关系
- hooks / classifier / permission dialog 的时机需要排好顺序

这说明 Claude Code 不是把 Hook 当装饰品，而是把它放进了主决策链。

通俗解释：

很多系统的 hook 只能“旁听”。

Claude Code 的某些 hook 可以“进入会议”。

但进入会议不代表胡来，所以它又配了一整套：

- matcher
- timeout
- abort
- event bus
- permission 联动

这才敢让 Hook 靠近主流程。

---

## 19. SessionStart / Setup / SessionEnd 为什么要特别重视

从 [hookEvents.ts](../src/utils/hooks/hookEvents.ts) 的 `ALWAYS_EMITTED_HOOK_EVENTS` 能看到：

- `SessionStart`
- `Setup`

这些被视为低噪音且高价值的基础 lifecycle 事件。

而在 [utils/hooks.ts](../src/utils/hooks.ts) 中，`SessionEnd` 还有专门更短的超时配置。

这很能说明 Claude Code 的设计取向：

- 启动阶段很重要
- 结束阶段也很重要
- 但结束阶段不能拖死退出流程

通俗解释：

启动像开机自检，可以慢一点但要可靠。

结束像关机清理，也要做，但绝不能因为某个 hook 卡住导致整个程序退不掉。

这就是为什么 SessionEnd 单独限时。

这个细节特别值得借鉴。

---

## 20. 一个非常产品化的思想：Hook 输出不等于用户消息

这点很多人做 agent 时会混淆。

Claude Code 明显区分了几类东西：

- 真正的 user / assistant message
- tool progress / tool result
- hook progress / hook response
- task-notification
- debug log

也就是说，同样是“系统里产生的信息”，它不会统一都当对话消息。

这是非常重要的产品建模能力。

通俗解释：

不是只要有信息就塞聊天框。

而是先问：

- 这是给用户看的？
- 这是给开发者调试的？
- 这是给 SDK 机器消费的？
- 这是给下一轮模型推理看的？

不同答案，就走不同通道。

这也是 Claude Code 比很多 demo 更成熟的地方。

---

## 21. Hooks / Event Bus / Lifecycle 这三者到底怎么区分

这里很容易绕，我给你一个最通俗的版本。

### 21.1 Lifecycle

Lifecycle 是“系统什么时候发生了某类关键事情”。

比如：

- session 开始
- tool 即将执行
- compact 完成
- session 结束

它回答的是：

**什么时候。**

### 21.2 Hook

Hook 是“在这些关键时刻，可以插入什么外部行为”。

它回答的是：

**谁能插手。**

### 21.3 Event Bus

Event bus 是“这些 Hook 的执行状态，怎么广播给其他消费者”。

它回答的是：

**结果怎么传播。**

一句话背下来：

- lifecycle 定义时机
- hook 定义扩展点
- event bus 定义传播机制

这三个分开，系统就清楚。

---

## 22. 设计上最值得借鉴的地方

这一部分是最适合你拿去做自己系统设计参考的。

### 22.1 先建模生命周期，再做扩展

不要一上来就写一堆 callback。

先问自己：

- 你的 agent 系统有哪些关键阶段？
- 哪些阶段允许阻塞？
- 哪些阶段只允许观测？
- 每个阶段输入输出结构是什么？

Claude Code 最大的优点之一，就是它先把这件事说清楚。

### 22.2 外部扩展一定要当成不可靠参与者

Hook 不是你的核心代码。

所以必须有：

- timeout
- abort
- logging
- background handling
- 降级路径

不然用户配一个烂脚本就能拖垮整个系统。

### 22.3 事件广播要独立于主消息流

很多系统把所有东西都塞进 messages，最后 UI 和 SDK 全乱。

Claude Code 的启发是：

- 会话消息是一条线
- 系统事件可以是另一条线

这样你后面更容易：

- 加 structured output
- 加 remote viewer
- 加调试模式
- 加 event replay

### 22.4 异步结果尽量回归统一调度入口

不要让后台 hook 自己乱唤醒。

Claude Code 用统一队列的做法非常好。

这样系统里的：

- 用户输入
- 后台结果
- task 通知
- permission 残留

都走同一套优先级和调度模型。

### 22.5 side effect 要集中处理

`onChangeAppState.ts` 这种“单一 choke point”非常值得学。

不要让每个 setState 的地方都自己决定要不要同步外部系统。

这种散落式写法早晚会出 bug。

---

## 23. 哪些地方不容易抄，抄的时候要小心

Claude Code 这套设计很强，但不是所有项目都要原样照搬。

### 23.1 事件种类太多会把自己压垮

Claude Code 事件很多，是因为它已经是大产品。

如果你自己做小型 agent runtime，先从最核心几类开始：

- session_start
- pre_tool
- post_tool
- session_end

不要一开始就把 20 多种事件都做满。

### 23.2 Hook transport 不用一开始就全支持

Claude Code 支持 shell / agent / prompt / HTTP。

你自己做第一版时，可以只做：

- shell hook
- in-process callback

等系统稳定后再加 HTTP 或 MCP 风格外部扩展。

### 23.3 不要过早让 Hook 进入主决策链

Claude Code 有足够多治理手段，才敢让 Hook 接近权限和主流程。

如果你自己的系统还没有：

- timeout
- abort
- telemetry
- queue
- 降级机制

那就先让 Hook 只做 observer，不要先做 blocker。

---

## 24. 从学习顺序上，这篇应该怎么消化

我建议你按这个顺序读源码和文档：

1. 先看 [utils/hooks/hookEvents.ts](../src/utils/hooks/hookEvents.ts)
2. 再看 [utils/messageQueueManager.ts](../src/utils/messageQueueManager.ts)
3. 然后看 [utils/hooks.ts](../src/utils/hooks.ts)
4. 再回头看 [cli/print.ts](../src/cli/print.ts)
5. 最后配合 [state/onChangeAppState.ts](../src/state/onChangeAppState.ts) 理解“状态变化也是 lifecycle”

这样会比较顺。

如果一上来就啃 `hooks.ts`，很容易被细节淹没。

---

## 25. 一段最通俗的总结

如果你只想记住一句话，那我建议记这个：

**Claude Code 的 Hook 系统，本质上是在 agent runtime 外面再包了一层“可控的外部参与机制”。**

它厉害的地方不是“能跑脚本”，而是：

- 它知道什么时候该让外部参与
- 它知道怎么限制外部参与
- 它知道外部参与的状态怎么传播
- 它知道结果怎么回流到主系统

这就是为什么它看起来不是一个简单 CLI，而更像一个完整的 agent runtime 平台。

---

## 26. 最后给你的学习提醒

学这块时，最容易犯的错是把它只看成“Hook 功能”。

其实更应该学的是它背后的设计思想：

- 生命周期先行
- 扩展点标准化
- 事件通道解耦
- 异步治理
- 统一调度
- side effect 集中化

这些思想不仅能用在 Claude Code 上，也能直接迁移到：

- 你自己的 coding agent
- workflow engine
- long-running assistant
- multi-agent runtime

如果你把这套思想学会了，后面看 Claude Code 的 MCP、subagent、remote control、Kairos，都能更容易看懂。
