# Claude Code Tool Execution / Streaming Orchestration 深度解读

## 1. 这篇文档在讲什么

Claude Code 的工具系统，不是“模型输出一个函数调用，程序跑一下，再把结果塞回去”这么简单。

它真正做的是一整套工具运行时：

1. 工具定义层
2. 单次工具调用生命周期
3. 流式工具执行器
4. 并发控制
5. 权限与 hook 接入
6. 工具结果持久化与压缩
7. UI / SDK / telemetry 联动

如果你要学的是“像样的 agent runtime 怎么跑工具”，这一块是非常核心的。

通俗一点说：

- 很多 demo 的工具像普通函数
- Claude Code 的工具更像一个“带权限、带事件、带中断、带结果序列化”的小协议

---

## 2. 先给一个总图

一次典型的工具调用主链路，大致是：

1. 模型在 assistant message 里产出 `tool_use`
2. `query()` 发现 tool blocks
3. 进入工具编排层
4. 选择串行还是并发执行
5. 单个工具调用 `runToolUse()`
6. 里面再走：
   - 找工具定义
   - 校验输入
   - 权限决策
   - pre hooks
   - 真正执行
   - post hooks
   - 构造成 `tool_result`
7. 结果回到消息流，继续驱动下一轮 agent loop

关键文件：

- [Tool.ts](../src/Tool.ts)
- [toolExecution.ts](../src/services/tools/toolExecution.ts)
- [StreamingToolExecutor.ts](../src/services/tools/StreamingToolExecutor.ts)
- [toolOrchestration.ts](../src/services/tools/toolOrchestration.ts)

---

## 3. `Tool` 抽象为什么这么大

先看：

- [Tool.ts](../src/Tool.ts)

很多人第一眼会觉得 `Tool` 接口为什么这么长。其实这正说明 Claude Code 把工具当成正式运行时对象，而不是普通函数。

### 3.1 一个工具不只是 `call()`

除了 `call()`，一个工具还可能有：

- `inputSchema`
- `description()`
- `isConcurrencySafe()`
- `isReadOnly()`
- `isDestructive()`
- `interruptBehavior()`
- `checkPermissions()`
- `validateInput()`
- `mapToolResultToToolResultBlockParam()`
- `renderToolUseMessage()`
- `renderToolResultMessage()`
- `extractSearchText()`
- `getActivityDescription()`

这意味着 Claude Code 里一个工具同时服务于：

1. 模型推理
2. 权限判断
3. 并发调度
4. UI 展示
5. transcript 检索
6. SDK / MCP 透传

通俗解释：

- 它不是一个“函数实现”
- 而是一张“完整能力卡片”

---

## 4. `ToolUseContext` 为什么是工具系统的核心

`ToolUseContext` 在 [Tool.ts](../src/Tool.ts) 里非常长，这不是偶然。

### 4.1 它里面有哪些关键东西

它携带了：

- 当前可用 tools / commands / mcpClients
- abortController
- readFileState
- getAppState / setAppState
- handleElicitation
- setToolJSX
- in-progress tool ids
- response length
- updateFileHistoryState
- updateAttributionState
- messages
- queryTracking
- contentReplacementState
- renderedSystemPrompt

### 4.2 这说明什么

Claude Code 里的工具不是“纯函数”。

工具调用时，它既能：

- 读环境
- 改状态
- 发 UI
- 参与中断
- 影响持久化

通俗理解：

- `ToolUseContext` 就像工具执行时的“操作台”
- 不是给它参数，让它默默算完返回

### 4.3 为什么这很值得学

很多 agent 系统的工具层做得太薄，导致后面会不断补洞：

- 想加权限，补一套
- 想加流式进度，再补一套
- 想加 UI，又补一套

Claude Code 一开始就把工具放进统一 context 中，所以系统演进空间很大。

---

## 5. 单次工具调用 `runToolUse()` 的真实生命周期

真正的单次工具执行主函数在：

- [toolExecution.ts](../src/services/tools/toolExecution.ts#L337)

### 5.1 第一步：先找工具

它先用 `findToolByName()` 在当前可用工具里找。

如果找不到，还会尝试：

- deprecated alias fallback

这说明 Claude Code 很在意老 transcript / 老工具名兼容。

### 5.2 第二步：提取上下文与 telemetry 信息

它会提前拿到：

- requestId
- messageId
- mcp server type
- mcp base url

这说明工具调用不是黑盒，它一开始就进入了可观测体系。

### 5.3 第三步：如果工具不存在，直接产出规范化错误 `tool_result`

不是抛异常给外层就完，而是构造一个模型可理解的错误结果：

- `<tool_use_error>...</tool_use_error>`

这非常重要，因为 agent loop 需要的是：

- 一条继续可消费的消息

而不是：

- 进程异常

---

## 6. 工具执行不是“直接 call”，中间有很多闸门

`runToolUse()` 中间真正重要的是各种检查链。

### 6.1 输入校验

工具先按 `inputSchema` 做校验。

这一步解决的是：

- 模型参数流不完整
- 字段类型不对
- schema 不匹配

### 6.2 tool-specific validateInput

这一步是更细的输入合法性判断。

不只是“类型对不对”，还会看：

- 当前路径可不可以
- 当前输入有没有逻辑问题

### 6.3 权限决策

不是每个工具都直接跑，而是要经过：

- 全局权限模式
- 工具自身 `checkPermissions()`
- hook 决策
- classifier / policy / config

这说明工具调用在 Claude Code 里本质是：

- “受监管的执行请求”

### 6.4 pre / post hooks

`toolExecution.ts` 还会调用：

- `runPreToolUseHooks`
- `runPostToolUseHooks`
- `runPostToolUseFailureHooks`

这意味着工具调用生命周期可以被外部扩展逻辑拦截和增强。

通俗解释：

- 工具不是直接进车间开工
- 而是要先过安检，再过工头，再开工，完了还有验收

---

## 7. 为什么工具结果要被包装成 `tool_result block`

Claude Code 里工具执行完，最终不是随便返回字符串，而是要进：

- `mapToolResultToToolResultBlockParam()`

### 7.1 这么做的好处

可以统一支持：

- 普通文本结果
- 结构化结果
- MCP `_meta`
- 持久化后的预览结果
- 错误结果

### 7.2 这不是 UI 需求，而是 agent 协议需求

模型下一轮看到的不是“程序日志”，而是规范化的 `tool_result`。

所以工具层的输出必须对模型友好、对 transcript 友好、对 SDK 友好。

这就是为什么 Claude Code 很重视结果规范化。

---

## 8. `toolOrchestration.ts`：经典编排层

这部分比较像“第一代工具调度器”。

对应：

- [toolOrchestration.ts](../src/services/tools/toolOrchestration.ts)

### 8.1 它做了什么

它会把一批 tool calls 分成：

1. concurrency-safe 批次
2. 非 concurrency-safe 批次

规则很清楚：

- 可并发的连续工具放一起
- 不可并发工具单独串行

### 8.2 `partitionToolCalls()` 很值得学

这个函数说明 Claude Code 不是写死“某些工具永远并发”，而是：

- 先 parse input
- 再调用工具的 `isConcurrencySafe(input)`

所以并发安全不是工具种类级别死属性，而是：

- 输入相关的语义属性

通俗解释：

- 同样是 Bash，不同命令不一定都能并发
- 不是“工具名字叫 Bash 就一刀切”

---

## 9. `StreamingToolExecutor`：更高级的边流边执行器

对应：

- [StreamingToolExecutor.ts](../src/services/tools/StreamingToolExecutor.ts)

这是 Claude Code 工具系统里非常有工程味的一块。

### 9.1 它解决什么问题

在流式模型输出里，tool_use block 不是一次性全部到齐的。

这时系统需要：

1. 工具一边流出来，一边判断能不能开跑
2. 可并发工具尽快并行
3. 不可并发工具避免冲突
4. 结果还要按接收顺序稳定吐出

这和“等所有工具都生成完再统一执行”不是一个层级。

### 9.2 它维护了什么状态

`TrackedTool` 里会跟：

- id
- assistantMessage
- status
- isConcurrencySafe
- results
- pendingProgress
- contextModifiers

这说明每个工具调用都像一个小任务对象，而不是一个临时 promise。

### 9.3 它的状态机很重要

状态有：

- `queued`
- `executing`
- `completed`
- `yielded`

这意味着流式工具不是“开始/结束”两态，而是：

- 有明确队列与产出阶段

这对正确处理顺序、重试、fallback 非常关键。

---

## 10. 并发控制为什么这么细

### 10.1 `canExecuteTool()`

并发规则是：

- 当前没有执行中的工具，当然可以跑
- 如果当前执行中的都是 concurrency-safe，而这个新工具也是 safe，也可以一起跑
- 否则就不行

这其实是一种“读共享、写独占”的调度模型。

通俗解释：

- 多个安全读操作可以一起上
- 一个可能改状态的操作要单独占线

### 10.2 为什么要维护顺序产出

即使并发跑，Claude Code 仍然尽量保证：

- 工具结果的产出顺序与工具被接收到的顺序一致

因为对模型来说，消息顺序很重要。

如果顺序乱掉，后果可能是：

- tool_result 对不上 tool_use
- 上下文语义混乱

---

## 11. 为什么 Bash 错误会取消兄弟工具，而别的工具不一定会

这点在 `StreamingToolExecutor` 里写得很直白。

当某个工具报错时：

- 不是所有工具都一定连坐
- 只有 Bash 错误特别容易触发 sibling cancel

### 11.1 为什么 Bash 特别对待

因为 Bash 常常表示一串隐式依赖操作。

例如：

- `mkdir` 失败，后面的写入就很可能没有意义

而像：

- Read
- WebFetch

很多时候彼此是独立的，一个失败不该把全部并发工具都杀掉。

### 11.2 这说明 Claude Code 很理解“工具语义”

它不是只看 API 层面上的成功/失败，而是在根据工具语义做联动策略。

这非常值得借鉴。

---

## 12. 中断机制为什么分 parent abort 和 sibling abort

`StreamingToolExecutor` 里有个很巧妙的设计：

- `toolUseContext.abortController`
- `siblingAbortController`

### 12.1 parent abort 是整个 query 层的中断

例如：

- 用户新输入打断
- turn 被取消

### 12.2 sibling abort 是工具组内部的联动取消

例如：

- 并发 Bash 里一个关键工具出错
- 兄弟工具应尽快停掉

### 12.3 为什么要分开

因为“取消整个 turn”和“只取消并发兄弟工具”不是一回事。

如果混成一个 abort：

- 太容易误杀整个 query loop

这就是很典型的多层 abort 设计。

通俗解释：

- 一个是“全局停工”
- 一个是“这个小组先停”

---

## 13. synthetic error message 为什么很重要

当流式 fallback、兄弟工具出错、用户中断时，Claude Code 不会只是内部记个状态，而是生成：

- synthetic `tool_result` error message

为什么这重要？

因为 agent loop 是消息驱动的。

如果工具被取消但模型什么都看不到：

- 下一轮推理会失真

所以 Claude Code 会明确告诉模型：

- 这个工具因为某原因没正常完成

这是一种“把运行时事实显式化”的设计。

---

## 14. `interruptBehavior()` 为什么是工具定义的一部分

这点特别值得学。

在 [Tool.ts](../src/Tool.ts) 里，每个工具可以定义：

- `cancel`
- `block`

### 14.1 这表示什么

当用户新提交消息时：

- 某些工具应该立刻取消
- 某些工具应该继续跑完

例如：

- 有的编辑操作可以安全停
- 有的系统操作中途停反而更危险

所以中断策略必须是工具语义的一部分，而不是 runtime 一刀切。

---

## 15. `contextModifier` 为什么只在某些情况下生效

工具执行除了产出消息，还可能通过：

- `contextModifier`

修改后续工具上下文。

这通常只适合串行阶段，因为：

- 并发阶段多个 modifier 同时改 context 容易冲突

所以在 `toolOrchestration.ts` 里你能看到：

- 并发工具先把 modifiers 排队
- 最后按原顺序统一应用

这非常细致，也非常正确。

---

## 16. Claude Code 的工具层为什么不是简单 RPC

把上面合起来，你会发现 Claude Code 的工具系统本质上是：

1. 模型协议层
2. 权限决策层
3. 并发调度层
4. 中断控制层
5. 结果规范化层
6. 观测与 UI 反馈层

这已经远远不是“调个函数”。

通俗解释：

- 每个工具调用都像一个被 runtime 管理的小工作流

---

## 17. 最值得借鉴的设计点

### 17.1 工具接口不要只设计 `call()`

可借鉴点：

- 工具最好原生携带权限、并发、安全、展示、结果映射等元信息

### 17.2 并发安全要看输入语义，不要只看工具名字

可借鉴点：

- `isConcurrencySafe(input)` 比静态白名单更强

### 17.3 工具取消要分层

可借鉴点：

- 全局中断
- 小组联动中断
- 工具级 interrupt behavior

最好分开建模。

### 17.4 取消和失败都应该显式反馈给模型

可借鉴点：

- 生成 synthetic tool_result，而不是只在运行时内部记状态

### 17.5 工具结果要统一规范化

可借鉴点：

- 用统一 result block 协议承接错误、结构化结果、持久化预览、meta

### 17.6 工具调度层和执行层最好分开

可借鉴点：

- 单次调用逻辑归 `runToolUse`
- 批次和并发归 orchestration / streaming executor

---

## 18. 难点白话解释

### 18.1 `ToolUseContext` 像什么

白话版：

- 像工具执行时随身带的一整套工作台
- 里面不只有参数，还有权限、状态、UI、消息、可中断能力

### 18.2 `StreamingToolExecutor` 到底在干嘛

白话版：

- 像一个边收边派工的调度员
- 工具调用刚冒头，就判断能不能马上开工
- 还要保证最后回报别乱序

### 18.3 为什么 Bash 错误要特别处理

白话版：

- 因为 Bash 经常是一串强依赖步骤
- 前面坏了，后面继续跑往往是浪费甚至危险

### 18.4 synthetic error message 为什么不能省

白话版：

- 因为模型要知道“这个工具没正常完成”
- 不然它会以为世界上什么都没发生

### 18.5 为什么说工具不是函数

白话版：

- 因为它有前置检查、权限、并发、安全、中断、进度、结果映射、UI
- 已经更像一个可调度任务单元

---

## 19. 一句话总结

Claude Code 的工具执行系统，本质上是在把“模型发起的工具调用”升级成“可监管、可并发、可流式、可恢复、可观测的运行时任务”。

这也是为什么它比很多 agent demo 稳得多。因为它不是把工具当函数，而是当协议和任务系统来做。
