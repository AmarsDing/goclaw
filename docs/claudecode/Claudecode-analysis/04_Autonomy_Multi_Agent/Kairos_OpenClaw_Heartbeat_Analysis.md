# Claude Code Kairos vs OpenClaw Heartbeat 深度分析

日期：2026-04-08

## 结论先行

如果只问一句“`Kairos` 和 `OpenClaw heartbeat` 是不是一回事”，答案是否定的。

- `OpenClaw heartbeat` 更像“定时触发一次主会话 agent turn 的调度机制”。
- `Kairos` 更像“长期在线自动助手模式”，它把门控、系统提示、主动消息、持续会话、长期记忆、后台任务、调度能力拼成了一整套 runtime。
- Claude Code 里和 OpenClaw heartbeat 最接近的，其实不是 `Kairos` 整体，而是 `Proactive`/`Kairos` 共用的 `tick` 机制，加上一部分调度能力。

再往下拆：

- `OpenClaw heartbeat` 解决的是“多久自动醒一次、醒了往哪发、没事时怎么安静地 ack”。
- `Kairos` 解决的是“被唤醒以后，作为一个长期助手，应该如何持续工作、记忆、协调、通知、恢复上下文”。

所以两者不是同层概念：

- `heartbeat` 是调度触发层。
- `Kairos` 是自治助手运行模式层。

## 分析边界与可信度

这份仓库里的 `Kairos` 不是完整源码。构建脚本把相关 feature 全部关掉了：

- `KAIROS: false`
- `KAIROS_BRIEF: false`
- `KAIROS_CHANNELS: false`
- `KAIROS_DREAM: false`
- `KAIROS_GITHUB_WEBHOOKS: false`
- `KAIROS_PUSH_NOTIFICATION: false`

证据见：

- `claude-code-source/build.ts`

这意味着真正最核心的 `assistant/index.js`、`assistant/gate.js`、`commands/assistant/*` 在这份外部构建里已经被 dead-code elimination 干掉了。  
因此，下面关于 `Kairos` 的分析是“基于可见调用点、注释、外围模块行为”的高可信架构重建，而不是完整源码逐行复原。

## OpenClaw Heartbeat 是什么

根据 OpenClaw 官方文档，heartbeat 的定义非常明确：

- 它会“周期性地在主会话里运行 agent turn”
- 默认间隔通常是 `30m`
- 它可以选择是否把结果投递到 `last`、指定 channel，或根本不外发
- 它有明确的 `HEARTBEAT_OK` 应答约定，用来在“没事发生”时避免打扰用户
- 它支持 `lightContext`、`isolatedSession`、`activeHours`、`includeReasoning`

这说明 OpenClaw heartbeat 的设计重点是：

1. 定时唤醒
2. 控制上下文成本
3. 控制对外投递
4. 控制“无事发生”时的安静确认

官方文档原意可以概括成一句话：

> heartbeat 是“scheduled main-session turn”，不是 detached background task 记录系统。

也就是说，Heartbeat 不是后台任务引擎本体，而是主线程上的周期性检查点。

## Claude Code 里真正与“heartbeat”同层的东西

Claude Code 里至少有三种容易混淆的“心跳/唤醒”概念。

### 1. Proactive/Kairos 的 `<tick>`

这是自治行为层的“唤醒提示”。

在 `print.ts` 里，`scheduleProactiveTick()` 会用 `setTimeout(0)` 注入：

```xml
<tick>当前本地时间</tick>
```

然后把这个 meta prompt 重新塞回消息队列，让模型继续“醒着”工作。

证据：

- [`cli/print.ts:1831`](../src/cli/print.ts#L1831)
- [`constants/prompts.ts:860`](../src/constants/prompts.ts#L860)

这个机制非常像 heartbeat 的“周期性唤醒”，但它更偏向模型工作循环内部，而不是 OpenClaw 那种带外投递、ACK 契约、目标路由都很明确的产品化调度层。

### 2. CCR/Bridge 的 transport heartbeat

Claude Code 还存在真正意义上的基础设施心跳。

`ccrClient.ts` 里写得很直接：

- 默认 heartbeat 间隔 `20s`
- 用于 worker liveness detection

证据：

- [`cli/transports/ccrClient.ts:32`](../src/cli/transports/ccrClient.ts#L32)

这个 heartbeat 才是“进程/worker 存活上报”那类传统 heartbeat，和 Kairos 的自治行为不是一个概念。

### 3. Kairos 模式下的长期自治

Kairos 自己不是 heartbeat，而是“被激活之后的一整套长期助手运行模型”。

## Kairos 是如何被激活的

Kairos 的激活是多层门控，不是简单开一个 flag。

从 `main.tsx` 可见，至少有这些条件：

1. 编译时 `feature('KAIROS')` 为真
2. `.claude/settings.json` 或 CLI 进入 assistant mode
3. 当前目录必须 trusted
4. 再通过 `kairosGate.isKairosEnabled()` 之类的运行时 gate
5. 激活后强制开启 `brief`
6. 设置 `setKairosActive(true)`
7. 初始化 assistant team

证据：

- [`main.tsx:1033`](../src/main.tsx#L1033)
- [`bootstrap/state.ts:72`](../src/bootstrap/state.ts#L72)
- [`bootstrap/state.ts:1086`](../src/bootstrap/state.ts#L1086)

这说明 Kairos 的目标不是“让普通会话偶尔自动跑一下”，而是“切换进入一类特殊的长期助手工作模式”。

## Kairos 的模块调用链

下面这张调用链图是根据当前仓库里可见调用点重建出来的。

```text
用户/配置
  -> .claude/settings.json assistant:true 或 --assistant
  -> main.tsx
     -> trust gate: checkHasTrustDialogAccepted()
     -> runtime gate: kairosGate.isKairosEnabled()
     -> setKairosActive(true)
     -> force brief on
     -> assistantModule.initializeAssistantTeam()
     -> append assistant system prompt addendum
     -> 进入 REPL / run loop
        -> Proactive/Kairos tick loop
           -> scheduleProactiveTick()
           -> enqueue <tick>...</tick>
           -> model 决定继续工作 / Sleep / 发 Brief
        -> BriefTool
           -> SendUserMessage 给用户
        -> BashTool
           -> assistant mode 下阻塞命令超预算自动后台化
        -> ScheduleCronTool
           -> 未来触发/周期任务
        -> Memory layer
           -> append to daily log
           -> nightly dream consolidation
        -> Bridge layer
           -> assistant worker_type = claude_code_assistant
           -> perpetual session continuity
        -> Session history
           -> /v1/sessions/{id}/events 分页恢复上下文
```

## Kairos 用了哪些技术方法

### 1. 编译期开关 + Dead Code Elimination

`Kairos` 整条模块图是被 `feature('KAIROS')` 包起来的，构建时可整块消除。

这不是普通运行时 if 判断，而是“产品功能切片”的发布方式。

证据：

- `claude-code-source/build.ts:53`
- [`commands.ts:62`](../src/commands.ts#L62)

### 2. 运行时门控

从代码注释能看出它有 GrowthBook gate 和 entitlement 检查。

例如：

- Brief 用 `tengu_kairos_brief`
- cron 用 `tengu_kairos_cron`

证据：

- [`tools/BriefTool/BriefTool.ts:67`](../src/tools/BriefTool/BriefTool.ts#L67)
- [`tools/ScheduleCronTool/prompt.ts:12`](../src/tools/ScheduleCronTool/prompt.ts#L12)

这意味着 Kairos 不是“硬编码给所有人开”的，而是“编译时可裁掉、运行时还可灰度”的双层门控。

### 3. System Prompt 编排

`main.tsx` 会在 proactive 模式下追加一段统一的自治说明，并且在 Kairos 激活时再追加 assistant addendum。

证据：

- [`main.tsx:2197`](../src/main.tsx#L2197)
- [`main.tsx:2206`](../src/main.tsx#L2206)

这说明 Kairos 的核心技术不是传统 workflow engine，而是“prompt orchestration + tool runtime”。

### 4. Tick 驱动的自治循环

`scheduleProactiveTick()` 会把 `<tick>` 作为 meta prompt 重新注入队列，模型由此继续运行。

证据：

- [`cli/print.ts:1831`](../src/cli/print.ts#L1831)

这和 OpenClaw heartbeat 相似的地方在于：都是“定时/周期性唤醒”。

但差别在于：

- OpenClaw heartbeat 更像外部调度制度
- Claude Code tick 更像会话内部的自治续航机制

### 5. BriefTool 作为用户可见输出通道

`BriefTool` 的注释写得非常明确：它是“primary visible output channel”。

证据：

- [`tools/BriefTool/BriefTool.ts:136`](../src/tools/BriefTool/BriefTool.ts#L136)

这意味着 Kairos 并不只是“默默在后台想”，而是有专门的用户通知工具层。

### 6. Bash 自动后台化

在 assistant 模式下，如果主线程上的 shell 命令运行过久，会自动 background，避免主 agent 被阻塞。

证据：

- [`tools/BashTool/BashTool.tsx:973`](../src/tools/BashTool/BashTool.tsx#L973)

这是一种很典型的“自治 agent runtime 工程化”设计：不是只靠模型提示，而是从工具执行层保障持续响应。

### 7. 长期记忆：daily log + dream consolidation

Kairos 模式不会一直 live rewrite `MEMORY.md`，而是：

1. 先 append 到按日期命名的 daily log
2. 再由 nightly `/dream` 整理成 topic files + `MEMORY.md`

证据：

- [`memdir/memdir.ts:318`](../src/memdir/memdir.ts#L318)
- [`memdir/paths.ts:237`](../src/memdir/paths.ts#L237)
- [`services/autoDream/autoDream.ts:95`](../src/services/autoDream/autoDream.ts#L95)

这说明 Kairos 的记忆策略是：

- 热路径：追加写日志
- 冷路径：后台归档整合

这比普通单文件记忆更适合“长期在线助手”。

### 8. 持续会话与桥接

Kairos 模式下：

- worker type 会变成 `claude_code_assistant`
- perpetual session continuity 会强制某些 bridge 路径走 v1/env-based 方案
- 会通过 session history API 恢复历史事件

证据：

- [`bridge/initReplBridge.ts:473`](../src/bridge/initReplBridge.ts#L473)
- [`commands/bridge/bridge.tsx:482`](../src/commands/bridge/bridge.tsx#L482)
- [`assistant/sessionHistory.ts:30`](../src/assistant/sessionHistory.ts#L30)

这说明 Kairos 从产品定位上就是“长期在线、会记住、会续上一次状态”的助手。

## 普通 Proactive 模式是什么

普通 `Proactive` 模式的核心比较纯粹：

1. 打开自治 prompt
2. 允许收到周期性 `<tick>`
3. 没事时调用 `Sleep`
4. 有事就继续行动

它强调的是：

- 主动探索
- 少确认，多行动
- tick 到来时别闲聊，没事就 sleep

证据：

- [`constants/prompts.ts:860`](../src/constants/prompts.ts#L860)

从 prompt 文案看，普通 Proactive 更像“自治工作模式”，但还不是“具备长期助手身份、长期记忆、专用通知通道、专用桥接策略的 assistant product mode”。

## Kairos 和普通 Proactive 模式到底差在哪

这是最关键的对比。

### 1. 产品定位不同

`Proactive`：

- 给当前会话增加主动性
- 重点是“别等用户每一步都来指挥”

`Kairos`：

- 把会话升级成长期在线助手
- 重点是“持续存在、持续记忆、持续汇报、持续协调”

### 2. 激活方式不同

`Proactive`：

- 更像一个会话内行为模式开关

`Kairos`：

- 需要 assistant mode
- 需要 trust gate
- 需要运行时 gate
- 激活后会改写更多系统行为

### 3. Prompt 层级不同

`Proactive`：

- 主要追加一段自治 prompt

`Kairos`：

- 既继承 proactive prompt
- 又会追加 assistant 专属 system prompt addendum

这意味着 Kairos 不是 Proactive 的同义词，而是“在 Proactive 之上叠一层 assistant-specific contract”。

### 4. 用户可见输出机制不同

`Proactive`：

- 用户通常看到普通文本输出

`Kairos`：

- 强依赖 `BriefTool`
- Brief 在 assistant mode 下是被强制打开的

这让 Kairos 更像“会主动汇报进度的助手”，而不是“偶尔自己做点事的会话”。

### 5. 工具运行策略不同

`Proactive`：

- 会主动做事，但并没有明显证据表明它会对 shell 阻塞做 assistant 专属运行时治理

`Kairos`：

- 明确对 Bash 长阻塞做自动后台化

这类差别很工程化，也很关键，因为长期助手如果不能保持响应，就很难真的长期在线。

### 6. 记忆层不同

`Proactive`：

- 没看到它强制切到 daily-log memory 模式

`Kairos`：

- 明确切到 daily log append-only
- 并依赖 dream consolidation

这说明 Proactive 是“会主动工作”，Kairos 是“会长期积累记忆并周期整理”。

### 7. 会话连续性不同

`Proactive`：

- 还是偏当前会话增强

`Kairos`：

- 明确和 assistant session continuity、bridge worker type、session history 恢复挂钩

### 8. 生态扩展面不同

从 feature flag 命名可以看出，Kairos 还预留或关联了：

- `KAIROS_CHANNELS`
- `KAIROS_DREAM`
- `KAIROS_GITHUB_WEBHOOKS`
- `KAIROS_PUSH_NOTIFICATION`

这类能力明显已经超出“普通 proactive mode”。

## Kairos 与 OpenClaw Heartbeat 的本质区别

### 1. 抽象层不同

`OpenClaw heartbeat`：

- 调度机制
- 重点是“多久触发一次主会话 turn”

`Kairos`：

- 助手运行模式
- 重点是“被触发后如何作为长期助手持续工作”

### 2. 消息契约不同

`OpenClaw heartbeat`：

- 有显式 `HEARTBEAT_OK` ack contract
- 有 `ackMaxChars`
- 有 `showOk/showAlerts/useIndicator`

`Kairos`：

- 没看到类似 `HEARTBEAT_OK` 这种产品级 ack 契约
- 更依赖 `Sleep`、`BriefTool`、普通输出和工具行为

### 3. 投递/路由能力不同

`OpenClaw heartbeat`：

- `target: last | none | <channel>`
- `to`
- `accountId`
- `directPolicy`

这是一个明确面向外部消息渠道的调度设计。

`Kairos`：

- 当前可见源码更偏本地 REPL/bridge/runtime
- 虽然存在 channels / push / webhook feature flag 痕迹，但这一构建里主体代码不可见

### 4. 成本控制手段不同

`OpenClaw heartbeat`：

- 官方直接提供 `lightContext`
- `isolatedSession`
- 可选更便宜模型

`Kairos`：

- 当前可见设计更多是靠 prompt cache、sleep、后台任务、memory consolidation 去降低长期运行负担
- 没看到和 heartbeat 同级别的“每次定时 turn 明确瘦身”的对外配置界面

### 5. 没事时的默认行为不同

`OpenClaw heartbeat`：

- 倾向 `HEARTBEAT_OK`
- 并通过规则 suppress 掉无意义 ack，避免打扰

`Kairos` / `Proactive`：

- 倾向调用 `Sleep`
- 没事时不应该输出“still waiting”之类的文本

两者本质上都在解决“无事发生时如何安静”，但机制不同：

- OpenClaw 用 ACK token
- Claude Code 用 Sleep tool + tick loop

## 如果硬要找“对位关系”

最接近的对位关系其实是这样：

- `OpenClaw heartbeat`
  对位
  `Claude Code Proactive/Kairos tick + Sleep + Brief 的组合`

而不是：

- `OpenClaw heartbeat`
  对位
  `Kairos 整体`

因为 `Kairos` 比 heartbeat 大一层。

## 一张对比表

| 维度 | OpenClaw Heartbeat | Claude Code Proactive | Claude Code Kairos |
| --- | --- | --- | --- |
| 核心定位 | 定时主会话 turn 调度 | 当前会话自治模式 | 长期在线自动助手模式 |
| 触发机制 | 配置化定时 heartbeat | `<tick>` 注入 | 继承 `<tick>`，再叠加 assistant runtime |
| 无事时行为 | `HEARTBEAT_OK` ack | 强制 `Sleep` | 强制 `Sleep` 或通过 Brief/assistant 规则收敛 |
| 用户通知 | 目标路由、channel 投递 | 普通输出为主 | `BriefTool` 是主用户可见通道 |
| 长期记忆 | 依赖 OpenClaw 自身 session/task 设计 | 无明显专属长期记忆层 | daily log + dream consolidation |
| 会话连续性 | 可选择 main session / isolated session | 会话级自治 | assistant continuity + session history |
| 工程治理 | activeHours、lightContext、isolatedSession、ack contract | tick + sleep | tick + brief + auto-background + memory + bridge |
| 适合场景 | 周期巡检、提醒、外部通知 | 当前任务自己往前推进 | 长期驻留、主动汇报、连续协作 |

## 对你最有价值的实操判断

如果你以后自己设计一个“自动助手”系统，可以借鉴两边不同的优点：

### 更像 OpenClaw 的部分

适合拿来做：

- 稳定的定时巡检
- 外部渠道通知
- 明确的“没事就别吵我”产品契约
- 上下文成本可控的定时 agent turn

### 更像 Kairos 的部分

适合拿来做：

- 长期在线 coding assistant
- 有持续记忆的项目助手
- 会自己协调、汇报、安排后台任务的 agent
- 带长期 session continuity 的伴随式助手

## 最终判断

### 一句话版本

`OpenClaw heartbeat` 是“定时叫醒 agent 看看有没有事”，`Kairos` 是“把 agent 变成一个长期在线、会记忆、会汇报、会协调的助手人格和运行时”。

### 更准确一点的判断

- 如果你关心“调度机制”，看 OpenClaw heartbeat。
- 如果你关心“长期自治助手 runtime”，看 Kairos。
- 如果你关心“Claude Code 里最像 heartbeat 的东西”，看 `Proactive`/`Kairos` 共享的 `<tick>` 循环，而不是 Kairos 全体。

## 参考证据

### 本地源码

- `claude-code-source/build.ts`
- `../src/main.tsx`
- `../src/commands.ts`
- `../src/cli/print.ts`
- `../src/constants/prompts.ts`
- `../src/tools/BriefTool/BriefTool.ts`
- `../src/tools/BashTool/BashTool.tsx`
- `../src/tools/ScheduleCronTool/prompt.ts`
- `../src/memdir/memdir.ts`
- `../src/memdir/paths.ts`
- `../src/services/autoDream/autoDream.ts`
- `../src/bridge/initReplBridge.ts`
- `../src/commands/bridge/bridge.tsx`
- `../src/assistant/sessionHistory.ts`
- `../src/cli/transports/ccrClient.ts`

### 外部官方资料

- OpenClaw Heartbeat:
  - https://docs.openclaw.ai/heartbeat
  - https://docs.openclaw.ai/gateway/heartbeat

## 附：一句工程视角总结

OpenClaw heartbeat 更像“scheduler + routing + ACK policy”，而 Kairos 更像“assistant operating system”。
