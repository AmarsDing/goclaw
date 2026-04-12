# Claude Code Session Restore / Persistence 深度解读

## 1. 这篇文档在讲什么

Claude Code 的“恢复会话”并不是简单地把一份聊天记录读回来。

它真正要解决的是：

1. 上次会话说到哪了
2. 当前 session 的各种状态是什么
3. 子代理、文件历史、worktree、metadata 能不能接着用
4. 本地和远端会话能不能统一恢复

所以 Claude Code 做的其实是一套：

- 会话持久化系统
- 恢复系统
- 远端回灌系统

通俗一点说：

- 普通聊天软件恢复的是“聊天文本”
- Claude Code 恢复的是“一个正在工作的 agent 运行现场”

---

## 2. 先给一个总图

### 2.1 持久化主链

运行中，大致会走这条线：

1. 用户消息进入 `QueryEngine`
2. `recordTranscript()` 及时写入 transcript
3. 文件历史 / attribution / content replacement / collapse snapshot 等一并写入
4. `flushSessionStorage()` 负责真正落盘

关键文件：

- [QueryEngine.ts](../src/QueryEngine.ts)
- [sessionStorage.ts](../src/utils/sessionStorage.ts)

### 2.2 恢复主链

恢复时，大致走这条线：

1. `print.ts` 判断是 `--continue`、`--resume` 还是 `teleport`
2. 必要时先从远端 hydrate transcript
3. `loadConversationForResume(...)` 读会话数据
4. `restoreSessionStateFromLog(...)` 和 `processResumedConversation(...)` 恢复状态
5. `restoreSessionMetadata()`、`restoreWorktreeForResume()` 等补齐运行现场

关键文件：

- [print.ts](../src/cli/print.ts)
- [sessionRestore.ts](../src/utils/sessionRestore.ts)
- [sessionStorage.ts](../src/utils/sessionStorage.ts)
- [remoteIO.ts](../src/cli/remoteIO.ts)

---

## 3. 为什么 Claude Code 非常重视“尽早写 transcript”

这点在 `QueryEngine.ts` 里体现得特别明显。

对应：

- [QueryEngine.ts](../src/QueryEngine.ts#L436)

### 3.1 用户消息在 API 返回前就会写入

代码里有非常明确的注释：

- 用户消息会在进入 query loop 之前先写 transcript

为什么？

因为如果你发了消息，进程立刻被杀掉，而模型还没来得及回复：

- 如果不提前写
- 那恢复时会发现“像是这条消息从未发生过”

这对用户体验非常糟糕。

### 3.2 这体现的设计哲学

Claude Code 在这里优先保证的是：

- “用户已提交的动作不能丢”

这很像数据库里的：

- accepted command 必须尽快 durable

通俗解释：

- 用户按下发送，就应该算“进入系统了”
- 不应该因为模型还没回，就当没发生过

---

## 4. `recordTranscript()` 不是简单 append

看：

- [sessionStorage.ts](../src/utils/sessionStorage.ts#L1408)

### 4.1 它会去重

`recordTranscript()` 会先看：

- 哪些消息已经在 messageSet 里
- 哪些是新消息

也就是说，它不是盲目重复写。

### 4.2 它会维护 parent chain

这是非常关键的地方。

它会计算：

- `startingParentUuid`
- `lastRecorded`

这说明 transcript 存的不是“平铺文本”，而是带父子关系的消息链。

为什么这重要？

因为：

- compaction 后还要继续
- resume 后还要接着写
- 中间可能有 progress / compact boundary / replay

如果没有链结构，后续恢复很容易接错位置。

通俗解释：

- 这不是记流水账
- 这是在维护一条可以继续往后写的对话分支链

---

## 5. `QueryEngine` 为什么会在多个位置写 transcript

很多人第一次看会觉得：

- 怎么到处都在 `recordTranscript(messages)`？

其实这是很合理的。

### 5.1 不同类型消息到来时都要及时落盘

`QueryEngine` 处理的不是只有 user / assistant：

- user
- assistant
- progress
- attachment
- compact boundary

有些消息如果不及时写，会出恢复问题。

比如源码注释就说得很清楚：

- progress 如果不 inline 记录
- 下一次 dedup / resume 时会把链关系搞错

这说明 Claude Code 对“消息事件一致性”看得非常重。

### 5.2 assistant 为什么常常 fire-and-forget

对 assistant message，常常是：

- `void recordTranscript(messages)`

不是每次都 `await`

原因也写得很清楚：

- 如果这里等太久，会阻塞 ask() 生成器
- 进而影响流式 message_delta

这说明它在平衡两件事：

1. 尽快落盘
2. 不阻塞流式输出

这是很典型的 runtime 工程取舍。

---

## 6. `flushSessionStorage()`：为什么还要单独 flush

对应：

- [sessionStorage.ts](../src/utils/sessionStorage.ts#L1583)

### 6.1 `recordTranscript()` 不等于已经完全 durable

Claude Code 会把很多写入先交给 project 层缓冲和排队。

`flushSessionStorage()` 才是真正要求：

- 把积压写入刷干净

这在这些场景尤其重要：

- eager flush
- cowork
- turn 结束
- resume 前后

### 6.2 为什么要分成 write 和 flush

因为如果每条消息都同步 fs flush：

- 太慢
- 会严重影响交互体验

所以它做的是：

- 平时尽量异步写
- 在关键时刻明确 flush

这和数据库、日志系统的思路非常像。

---

## 7. transcript 之外，还持久化了什么

这是 Claude Code 恢复能力强的核心原因。

它持久化的不只是 messages，还包括：

- file history snapshots
- attribution snapshots
- content replacements
- context collapse commits / snapshots
- queue operations
- worktree session
- agent metadata

这意味着它恢复的是：

- agent 运行现场

不是：

- 一段聊天文本

---

## 8. `print.ts` 里的恢复入口怎么分流

关键在：

- [print.ts](../src/cli/print.ts#L4893)

### 8.1 `--continue`

语义是：

- 当前目录里找最近会话，继续接着跑

特点：

- 偏“本地继续”

### 8.2 `--resume`

语义是：

- 按指定 session id / jsonl / url 恢复

特点：

- 更显式
- 也支持远端 / URL 来源

### 8.3 `teleport`

语义是：

- 从远端 code session 把内容迁回本地继续

特点：

- 更偏跨环境迁移

### 8.4 为什么要区分三种

因为它们面向的不是一个场景：

- continue：本地自然续接
- resume：精确恢复某个会话
- teleport：跨环境带回来

这就是产品层语义很清楚的体现。

---

## 9. 恢复前为什么常常要先 hydrate 远端日志

这块在 `print.ts` 和 `sessionStorage.ts` / `remoteIO.ts` 里都有。

### 9.1 v1 路径：`hydrateRemoteSession()`

对应：

- [sessionStorage.ts](../src/utils/sessionStorage.ts#L1587)

它会：

1. `switchSession(sessionId)`
2. 从 Session Ingress 拉日志
3. 本地创建 transcript 文件
4. 用远端内容覆盖本地 transcript

### 9.2 v2 路径：`hydrateFromCCRv2InternalEvents()`

对应：

- [sessionStorage.ts](../src/utils/sessionStorage.ts#L1632)
- [remoteIO.ts](../src/cli/remoteIO.ts#L140)

它会：

1. 通过注册的 internal event reader 拉 foreground events
2. 再拉 subagent internal events
3. 主会话写主 transcript
4. 每个 subagent 写自己的 transcript

这很厉害，因为它说明：

- 远端恢复不是只恢复主链
- 子代理 sidechain 也一起恢复

---

## 10. `restoreSessionStateFromLog()`：恢复的不是消息，而是状态

对应：

- [sessionRestore.ts](../src/utils/sessionRestore.ts#L99)

### 10.1 它恢复哪些东西

会恢复：

- file history
- attribution state
- context collapse state
- todos

这非常关键。

因为如果只把 `messages` 读出来，实际运行时仍然是不完整的。

例如：

- 文件历史断了
- todo 状态丢了
- collapse 视图不对

那你恢复出来的就只是“像原来”，不是真的原来。

### 10.2 为什么这点很高级

很多 agent 系统恢复做得很浅：

- 只恢复文本

Claude Code 恢复做得更深：

- 恢复会影响后续行为的运行时状态

这就是 runtime 和 chat app 的差别。

---

## 11. `processResumedConversation()`：真正的“恢复现场”

对应：

- [sessionRestore.ts](../src/utils/sessionRestore.ts#L409)

### 11.1 它做的事情非常多

包括：

1. 匹配 coordinator / normal mode
2. 处理 session id 复用或 fork session
3. 恢复 metadata
4. 恢复 worktree
5. `adoptResumedSessionFile()`
6. 恢复 context collapse
7. 恢复 agent setting
8. 刷新 agent definitions
9. 计算 render 前 initial state

这已经不像“读个文件”，而像：

- 恢复一个长期运行程序的上下文

### 11.2 为什么要 `adoptResumedSessionFile()`

这点比较难懂，但很重要。

在恢复时，`resetSessionFilePointer()` 会先把旧指针清掉。

但如果不再把 resumed transcript 重新 adopt 回来，后面退出时：

- 元数据重写可能找不到正确文件

所以 `adoptResumedSessionFile()` 的作用就是：

- 明确告诉系统：以后继续往这份旧 transcript 上写

白话版：

- 相当于“接着用这本老笔记本，不要另外新开一本”

---

## 12. worktree 恢复为什么单独处理

这是非常产品化、也非常工程化的一点。

对应：

- [sessionRestore.ts](../src/utils/sessionRestore.ts#L332)

### 12.1 `restoreWorktreeForResume()`

它会：

1. 看 transcript 里最后记录的 worktree state
2. 如果那个 worktree 还存在，就 `chdir` 回去
3. 同时更新 cwd / originalCwd / caches

### 12.2 为什么这不能简单忽略

因为用户恢复的是一个“工作会话”，而不是纯聊天。

如果上次会话其实是在某个 worktree 里工作，而恢复后你回到了主目录：

- 路径全会错
- 文件状态也会错

这会非常危险。

### 12.3 `exitRestoredWorktree()` 也很聪明

如果 mid-session 又切去别的 session：

- 旧 worktree 状态不能残留

否则会出现：

- 明明切到另一个会话了，但 cwd 还留在旧 worktree

这说明 Claude Code 恢复不仅考虑启动恢复，还考虑“会话内再恢复”的切换场景。

---

## 13. fork session 为什么还要特殊处理 content replacements

这段很工程化，但特别值得学。

在 `processResumedConversation()` 里，如果是：

- `--fork-session`

它会额外处理：

- `contentReplacements`

为什么？

因为 fork 后会产生新 session id，但旧会话的一些 tool result replacement 逻辑还要继续匹配。

如果不把这些 replacement 记录迁过去：

- 恢复后同样的 tool_use_id 对不上 replacement
- 会导致缓存稳定性和内容替换逻辑出问题

通俗解释：

- 不只是把聊天复制到新会话
- 连“这个会话内部哪些内容曾经被替换过”的隐性状态也得一起带过去

---

## 14. remoteIO / CCR v2 为什么是恢复体系的一部分

很多人会把 `remoteIO.ts` 看成纯传输层，但它其实也深度参与恢复。

对应：

- [remoteIO.ts](../src/cli/remoteIO.ts#L140)

### 14.1 它注册了 internal event writer

意思是：

- transcript 消息在 v2 模式下不再只写本地或 v1 ingress
- 而是作为 internal events 发往 CCR

### 14.2 它还注册了 internal event reader

意思是：

- 恢复时，系统可以从 CCR 把这些事件再读回来

这说明远端恢复并不是临时拼接，而是从设计上：

- 把“会话事件流”作为一等公民

这是非常值得借鉴的方向。

---

## 15. Claude Code 恢复系统的核心思想

这部分是理解全局的关键。

Claude Code 实际上把恢复拆成了三层：

### 15.1 文本层恢复

恢复：

- messages

这是最基础的一层。

### 15.2 状态层恢复

恢复：

- file history
- todos
- attribution
- collapse state
- metadata

这是让 agent“行为连续”的关键。

### 15.3 环境层恢复

恢复：

- session id
- transcript file ownership
- cwd / worktree
- remote ingress / internal event reader

这是让 agent“运行位置连续”的关键。

通俗解释：

- 不是只恢复它“说过什么”
- 还恢复它“记着什么”“在哪干活”“接下来该往哪本账上写”

---

## 16. 最值得借鉴的设计点

### 16.1 用户输入要尽早 durable

可借鉴点：

- 不要等模型回复了才写日志
- 用户已提交的输入应尽快持久化

### 16.2 transcript 不只是聊天记录，要维护链关系

可借鉴点：

- 用 parent UUID / append chain
- 让 resume 和 compaction 后续写入可持续

### 16.3 恢复必须恢复运行时状态，不只是消息

可借鉴点：

- 文件历史、todo、attribution、collapse 等状态要和消息一起恢复

### 16.4 远端恢复最好统一回本地表示

可借鉴点：

- 不要本地一套、远端一套各玩各的
- 最终都落到统一 transcript / event model 上

### 16.5 worktree / cwd 必须纳入恢复协议

可借鉴点：

- 对 coding agent 来说，工作目录本身就是状态

### 16.6 fork / copy session 要迁移隐性状态

可借鉴点：

- 复制会话时别只复制消息
- content replacement、metadata、task state 这类隐性状态也要考虑

---

## 17. 难点白话解释

### 17.1 为什么恢复不是“读 jsonl”

白话版：

- 因为 jsonl 里只是事件文本
- 真正的 agent 还依赖很多运行状态
- 不把这些状态一起恢复，恢复出来的就只是个壳

### 17.2 `recordTranscript()` 为什么这么复杂

白话版：

- 因为它不是单纯写日志
- 它还在维护后续能继续接上的消息链

### 17.3 为什么恢复还要管 worktree

白话版：

- coding agent 干活靠的是文件系统位置
- 恢复到错目录，比恢复不了还危险

### 17.4 `adoptResumedSessionFile()` 像什么

白话版：

- 像系统在说：“我们继续往旧会话那本账本里写，不要另起一本”

### 17.5 v2 internal events 为什么重要

白话版：

- 因为它把“会话历史”从一个本地文件，升级成了可远程同步、可分页恢复的事件流

---

## 18. 一句话总结

Claude Code 的 session restore / persistence 设计，真正强的地方在于它恢复的不是“聊天内容”，而是“agent 的工作现场”：

- 会话链
- 任务状态
- 文件历史
- 目录环境
- 远端事件流
- 子代理 sidechain

如果你以后要做一个真正能长期工作的 coding agent，这套设计非常值得反复学习。
