# Claude Session Restore / Resume Flow Deep Dive

## 1. 这篇为什么是最后值得写的一篇

如果说前面那些文档讲的是 Claude Code“运行时怎么工作”，那这一篇讲的是：

**Claude Code 为什么能在中断之后继续工作。**

这件事非常关键，因为很多 agent demo 都只能：

- 当场回答
- 当前进程活着时还能继续

一旦：

- 崩了
- 退出了
- 切换 session
- 想恢复长会话

它们就会露馅。

Claude Code 在这方面做得很成熟，这也是它更像“产品”而不是“脚本”的重要原因。

关键源码主要看：

- [utils/conversationRecovery.ts](../src/utils/conversationRecovery.ts)
- [utils/sessionRestore.ts](../src/utils/sessionRestore.ts)
- [screens/REPL.tsx](../src/screens/REPL.tsx)
- [screens/ResumeConversation.tsx](../src/screens/ResumeConversation.tsx)
- [utils/sessionStorage.ts](../src/utils/sessionStorage.ts)

---

## 2. 最通俗的一句话

Claude Code 的 resume，不是“把旧聊天记录读出来”。

它更准确地说是：

**把上一次会话的文本历史、工具状态痕迹、文件历史、agent 配置、worktree 位置、元数据和中断状态重新拼起来，让 runtime 看起来像从未断过。**

这里最关键的是“重新拼起来”。

不是简单读取，而是恢复。

---

## 3. 先抓主链路

最核心的链路可以这样记：

```text
选择一个 session / transcript
  -> loadConversationForResume()
  -> 读取完整 log / chain
  -> 恢复 skills / plans / file history
  -> deserialize + 过滤坏消息
  -> 检测是否中断
  -> 注入 resume 相关 hook messages
  -> restoreSessionStateFromLog()
  -> restore metadata / agent / mode / worktree
  -> adopt resumed transcript
  -> 进入新的 REPL runtime
```

如果再白话一点：

```text
读旧会话
  -> 修旧会话
  -> 补旧会话缺的运行时状态
  -> 把当前进程切换成“那场会话的后续”
```

这就是 Claude Code 的恢复本质。

---

## 4. `loadConversationForResume()` 为什么是整个恢复流的核心入口

[conversationRecovery.ts](../src/utils/conversationRecovery.ts) 里的：

- `loadConversationForResume(...)`

是整个恢复过程的中枢。

它支持几种来源：

- `undefined`：继续最近会话
- `sessionId`
- 已经加载好的 `LogOption`
- `.jsonl` transcript path

这说明 Claude Code 一开始就把 resume 设计成“多入口恢复”，而不是只支持单一模式。

这很成熟，因为真实产品里恢复来源确实很多：

- 最近会话
- 指定 session
- 跨目录 transcript
- UI 选中的历史记录

---

## 5. resume 不是只读文件，它还要先挑对会话

在 `loadConversationForResume()` 里，`source === undefined` 的场景会去找“最近可继续的 session”，而且还会刻意跳过某些 live background/daemon sessions。

这说明 Claude Code 很清楚：

**不是最近的会话就一定适合接着继续。**

如果某个会话还活着、还在自己写 transcript，这时再去 resume 它，容易出冲突。

通俗解释：

不是见到最近一份文档就打开接着写。

而是先问：

- 这份文档是不是还在被另一个人实时编辑？

这个边界意识很重要。

---

## 6. resume 的第一层难点：日志并不一定是完整的最终形态

`loadConversationForResume()` 里有：

- `isLiteLog(log)`
- `loadFullLog(log)`

这说明 resume 不是总能直接拿到“完整最终会话对象”。

有时候拿到的是轻量索引，需要再补全。

这很值得注意，因为它说明 Claude Code 的持久化层不是只存一个统一大对象，而是出于性能和存储考虑有轻重分层。

恢复逻辑必须理解这个现实。

---

## 7. 为什么 resume 还要先 `copyPlanForResume()`、`copyFileHistoryForResume()`

在 `loadConversationForResume()` 里，如果确定了 sessionId，会做：

- `copyPlanForResume(...)`
- `copyFileHistoryForResume(...)`

这说明 Claude Code 的恢复不是只恢复消息文本，而是要把“工作辅助状态”也接回来。

尤其是：

- plan slug
- file history

这些东西对继续编码非常重要。

通俗解释：

恢复一场工作，不只是把聊天记录拿回来，还得把：

- 今天的待办单
- 文件改动轨迹
- 计划文档

一起搬回来。

这才是真的“继续工作”。

---

## 8. resume 前为什么要先恢复 skill state

`restoreSkillStateFromMessages(messages)` 是个很有代表性的设计。

它会从 transcript 里的：

- `invoked_skills`
- `skill_listing`

等 attachment 恢复技能状态。

这说明 Claude Code 的某些运行时状态不只存在内存里，也会借由 transcript 附件留下痕迹，供以后恢复。

这里最值得学的是：

**恢复时不仅看 message 内容，还会从 message 附件中重建 process-local state。**

这就是为什么它不像一个普通聊天机器人，而更像一个有工作记忆的系统。

---

## 9. `deserializeMessagesWithInterruptDetection()` 是恢复流里最关键的纠偏器

这个函数特别重要。

它做的事情不只是“反序列化”，而是：

1. 迁移旧 attachment 类型
2. 修正无效 permissionMode
3. 过滤 unresolved tool uses
4. 过滤 orphaned thinking-only messages
5. 过滤纯空白 assistant messages
6. 检测 turn 是否被打断
7. 在必要时补 synthetic continuation message
8. 在必要时补 assistant sentinel

这说明 resume 的本质不是 restore-by-faith，而是：

**restore with repair。**

也就是：

读取历史以后，先修，再继续。

这非常成熟。

---

## 10. 为什么恢复时必须修坏消息

因为 transcript 是在真实运行中逐步写下来的。

而真实运行里会发生很多中间状态：

- tool_use 已经写了，但 tool_result 还没来
- thinking block 写了一半
- 用户中断在半路
- assistant 只输出了空白
- 旧版本 attachment 类型跟现在不同

如果 resume 时把这些半成品当成完整事实，后果会很糟：

- API 上下文不合法
- UI 显示奇怪
- 自动续跑判断错误
- 会话越来越脏

所以 Claude Code 的做法非常对：

- 恢复不是单纯“忠实回放”
- 而是“尽力恢复为可继续工作的有效状态”

---

## 11. turn interruption detection 为什么很有价值

`detectTurnInterruption(...)` 是一个很有代表性的函数。

它会区分：

- `none`
- `interrupted_prompt`
- `interrupted_turn`

这不是为了炫技，而是非常实用。

因为“中断”至少有两种完全不同的含义：

### 11.1 用户刚发出 prompt，Claude 还没开始回答

这时恢复后更像：

- “上一个请求还没开始处理”

### 11.2 Claude 已经在跑到一半，比如 tool_result 还没补完

这时恢复后更像：

- “上一个 turn 在半路断了”

这两者的恢复策略不该一样。

Claude Code 把它们区分开，这是很成熟的运行时意识。

---

## 12. 为什么它会插入 “Continue from where you left off.”

如果检测到 `interrupted_turn`，Claude Code 会注入一个 synthetic user message：

- `Continue from where you left off.`

这是一个非常聪明的办法。

它不是去猜模型内部到底断在哪，而是：

**把“继续刚才的工作”重新转成一条明确、对模型自然的用户指令。**

通俗解释：

系统不是试图回到模型脑子的中间寄存器，而是重新对模型说一句：

- “从你刚停下的地方接着干。”

这是个非常现实、很鲁棒的恢复策略。

---

## 13. 为什么还要插 `NO_RESPONSE_REQUESTED` 这样的 synthetic sentinel

在恢复逻辑里，如果最后相关消息是 user，Claude Code 还会插入一个 synthetic assistant sentinel。

目的是：

**即使恢复后用户不立刻选择自动继续，当前消息序列也仍然是 API 合法的。**

这是非常细的工程处理，但特别重要。

它说明 Claude Code 不是只关心“自动恢复会不会成功”，还关心：

- “如果用户什么都不做，系统现在是不是仍然处于一个合法状态？”

这就是成熟系统的兜底意识。

---

## 14. `restoreSessionStateFromLog()` 告诉你：恢复的不是聊天，而是 runtime

[sessionRestore.ts](../src/utils/sessionRestore.ts) 的：

- `restoreSessionStateFromLog(...)`

非常关键。

它会恢复：

- file history
- attribution
- context-collapse state
- todos

这说明 resume 恢复的是：

- 不是单纯 transcript
- 而是整个工作现场的一部分 runtime state

这也解释了为什么 Claude Code 的 resume 比很多产品更像“回到上次工作现场”。

通俗解释：

不是只把聊天窗口打开。

而是把：

- 你改文件的历史
- 你的待办
- 你的 attribution 状态
- 某些上下文折叠状态

也一起恢复出来。

---

## 15. `restoreAgentFromSession()` 为什么很值得学

这个函数说明 Claude Code 恢复的不只是“内容”，还恢复“身份”。

它会处理：

- 该 session 当时使用的 agentSetting
- 当前 CLI 是否已经显式指定了别的 agent
- 恢复 main thread agent type
- 在合适时恢复 model override

这很重要，因为在 Claude Code 里，不同 agent 不是皮肤，而是真有不同角色/行为配置。

所以 resume 时如果不恢复 agent，上下文连续性就会被破坏。

通俗解释：

不是只把“昨天做了什么”找回来。

还要把“昨天是谁在做这件事、它当时是什么角色配置”找回来。

---

## 16. worktree restore 是非常产品化的一层

`restoreWorktreeForResume(...)` 和 `exitRestoredWorktree()` 非常值得学。

这套逻辑说明 Claude Code 的 resume 不是只针对会话文本，而是对真实项目工作目录有恢复语义。

它会考虑：

- session 上次是否在 worktree 中
- 这个 worktree 目录现在还在不在
- 当前是否已经有新鲜 worktree 优先级更高
- 中途 `/resume` 到别的 session 前，要不要先退出当前恢复出的 worktree

这非常实战。

通俗解释：

Claude Code 不是只把你带回聊天上下文，还会尽量把你带回“当时那个项目工作台”。

这就是为什么它比普通聊天产品更像开发工具。

---

## 17. `adoptResumedSessionFile()` 为什么很关键

恢复会话时，一个很容易忽略的问题是：

**当前进程后续往哪份 transcript 继续写？**

这就是 `adoptResumedSessionFile()` 的意义。

如果不 adopt：

- 你可能只是读了旧会话
- 但后续写入却还写到当前新 session 或旧错误路径

那恢复就会变成“看起来恢复了，实际写到另一份文件”，这会很糟糕。

Claude Code 把这件事显式处理掉，非常正确。

---

## 18. `processResumedConversation()` 说明 resume 其实是一次 session 切换事务

[sessionRestore.ts](../src/utils/sessionRestore.ts) 里的：

- `processResumedConversation(...)`

把这个事实体现得很清楚。

它会做：

- 模式匹配
- session ID 切换
- recording rename
- cost state restore
- metadata restore
- worktree restore
- context-collapse restore
- agent restore
- mode 持久化
- initial state 计算

这说明恢复不是“加载页面”。

而是：

**把当前运行中的进程切换进另一个 session 世界。**

这个理解很重要。

---

## 19. resume 的一个高级点：不是所有恢复都是“继续原 session”

`processResumedConversation()` 里还考虑：

- `forkSession`

也就是说，恢复不一定是完全接管原 session，有时是：

- 以旧 session 为基础，开一个新分叉

这非常成熟。

因为真实使用里经常会有两种需求：

1. 原会话继续
2. 基于原会话另开一条分支

Claude Code 显然两种都考虑了。

这也解释了为什么它对：

- sessionId
- transcript path
- content replacements
- metadata adoption

都这么谨慎。

---

## 20. 这套恢复设计真正厉害在哪

如果只看表面，好像就是：

- 从日志读消息
- 再打开 REPL

但更深层看，它厉害在这几个点。

### 20.1 恢复的是“可工作状态”，不是“原样字节流”

它会修、会过滤、会补 continuation、会校正。

### 20.2 恢复不只针对消息

它同时恢复：

- file history
- attribution
- plans
- todos
- agent
- worktree
- mode

### 20.3 恢复是一笔事务

不是局部 patch，而是一次系统态切换。

### 20.4 恢复考虑了多入口和多作用域

最近会话、指定 session、jsonl path、fork session、main thread、subagent，都是不同情形。

---

## 21. 最值得借鉴的设计点

### 21.1 Resume 一定要有 repair phase

不要把磁盘内容原样读回来就继续跑。

必须先有一层：

- unresolved tool 过滤
- orphan thinking 过滤
- legacy schema 迁移
- interruption 检测

### 21.2 恢复目标要定义成“继续工作”

如果你的目标只是重放聊天记录，那很多状态都不需要恢复。

Claude Code 的目标明显更高，所以它恢复的是工作现场。

### 21.3 中断恢复最好转成模型能理解的自然指令

`Continue from where you left off.` 这个策略很值得学。

它比试图回滚到模型内部状态现实得多。

### 21.4 transcript ownership 要处理清楚

恢复后后续写入去哪，是一个大坑。

`adoptResumedSessionFile()` 这类设计非常必要。

### 21.5 目录/项目态也要纳入恢复

如果你的产品真是开发工具，不要只恢复聊天，要考虑：

- cwd
- worktree
- project metadata

---

## 22. 哪些地方不要机械照抄

### 22.1 小系统没必要一开始恢复这么多外围状态

第一版可以先恢复：

- messages
- interruption
- 基础 metadata

### 22.2 如果没有 transcript side effects，不必一开始就做 transcript adoption

但一旦会继续写日志，这块就必须认真做。

### 22.3 worktree restore 只适合真有项目工作目录语义的系统

如果你的 agent 是纯 API assistant，这块没必要硬搬。

---

## 23. 一段最通俗的总结

Claude Code 的 resume，本质上不是“打开旧聊天记录”。

它更像：

**重新搭起上次中断时的工作现场，然后把当前运行时切换进去。**

所以它恢复的不是只有消息，而是：

- 消息
- 状态
- 角色
- 工作目录
- 计划
- 历史
- 中断点

这也是为什么 Claude Code 值得学。

因为它让你看到，真正的 agent 产品要想长期可用，必须学会：

**不只是会开始一场工作，还要会从中断里把工作接回来。**
