# Claude Code 10 Key Design Principles

## 1. 为什么要写这份总结

前面的文档已经很多了，但如果最后不能收束成几条真正能带走的原则，学习就很容易停留在：

- 我知道它有哪些模块
- 我看过很多源码

但做系统时真正有价值的是：

- 我能不能提炼出它背后的设计方法

所以这份文档只做一件事：

**把 Claude Code 最值得借鉴的设计思想，收束成 10 条原则。**

这些原则不是“源码目录总结”，而是你以后自己做 agent、工具平台、长期助手时可以直接拿来用的东西。

---

## 2. 原则一：把 Agent 做成 Runtime，不要只做成 Prompt

Claude Code 最值得学的第一件事，就是它不是一个“大 prompt + 一些工具”的 demo。

它是一个 runtime。

这意味着它有：

- 生命周期
- 状态机
- 工具执行协议
- 权限治理
- 消息模型
- 恢复与持久化
- 外部系统接入

通俗解释：

prompt 像大脑里的一段指令。

runtime 像一个能长期运行的操作系统。

Claude Code 的成功，很大程度上来自它把 agent 做成了后者。

**可借鉴点**

- 如果你自己的系统已经开始有多轮、多工具、多状态，就不要再把自己骗成“只是一个 prompt app”。

---

## 3. 原则二：先定义“系统会发生什么”，再定义“模型说什么”

Claude Code 很多模块都体现一个共同思想：

- 先建模生命周期
- 先建模消息类型
- 先建模权限原因
- 先建模状态

再去谈 prompt、UI 和执行。

这和很多项目的顺序正好相反。

很多项目会先写：

- system prompt
- tool schema
- UI

然后再慢慢补规则。

Claude Code 更成熟的地方是：

**先把世界模型定义清楚，再让 prompt 嵌进去。**

**可借鉴点**

- 做 agent 系统时，优先定义 lifecycle、message、permission、state 这些基础对象。

---

## 4. 原则三：把工具调用看成主链路，而不是附属功能

Claude Code 里，tool 不是“模型偶尔会调用一下的扩展”。

它是主链路的一部分。

这体现在：

- query loop 围着 tool_result 继续推进
- permission 系统围着 tool 执行组织
- UI 对 tool use / tool result 做专门建模
- compact / resume 都显式处理 tool pairing

这说明 Claude Code 从一开始就接受一个现实：

**coding agent 的核心不是说话，而是说话和行动的交替。**

**可借鉴点**

- 如果你的 agent 产品真要干活，tool 用法必须是 first-class，不要只把它当外挂。

---

## 5. 原则四：内部表示、UI 表示、API 表示可以不一样

Claude Code 很成熟的一点，是它不执着于“同一份消息对象走天下”。

它允许：

- transcript 持久化表示
- normalized UI 表示
- API payload 表示
- resume 修复后表示

这是非常重要的工程边界。

很多系统的问题恰恰来自于：

- 把存储格式直接拿来渲染
- 把渲染格式直接拿去发 API
- 把恢复后的格式又原样写回存储

最后所有层都相互污染。

Claude Code 给出的答案是：

**不同层的最优表示可以不同，但它们之间要有稳定的转换函数。**

**可借鉴点**

- 只要系统复杂起来，就应该允许“多视图表示”，而不是强迫一个数据结构承担所有职责。

---

## 6. 原则五：权限不是弹窗，而是治理系统

Claude Code 的 permission 设计特别值得学。

它不是：

- 检测危险
- 弹框
- 用户点允许

而是：

- rules
- mode
- classifier
- hook
- safety
- approval UI
- permission update
- 持久化规则

组成的一条完整治理链。

这说明它真正关心的是：

**模型的行动边界如何被持续管理。**

不是只关心某一次点击。

**可借鉴点**

- 一旦你的 agent 能改文件、跑命令、访问网络，权限系统就必须升级成治理系统。

---

## 7. 原则六：长会话问题必须分层解决

Claude Code 在 context/memory/compact 上的设计很有代表性。

它不是只靠一个 summarize 按钮。

而是分层处理：

- microcompact
- structured memory
- compact summary
- cache reset
- resume reconstruction

这说明 Claude Code 认为“上下文过长”不是单点问题，而是系统性问题。

这非常正确。

因为长会话真正带来的问题有很多：

- token 太多
- tool 输出太冗长
- memory 漂移
- compact 后状态失真
- resume 后上下文不一致

**可借鉴点**

- 只要产品要支持长期工作，就不要把“上下文变长”交给一个 summarize 函数单独解决。

---

## 8. 原则七：恢复不是回放，而是重建可工作状态

Claude Code 的 resume 流里最值得学的一点是：

它恢复的不是“原样历史”，而是“可继续工作的当前状态”。

所以它会：

- 过滤坏消息
- 修 unresolved tool uses
- 补 continuation
- 恢复 file history / plan / metadata / worktree / agent

这是一种非常成熟的产品思维。

通俗解释：

用户不需要“看到过去一模一样的中间状态”。

用户真正需要的是：

- 现在能不能接着干活

Claude Code 的恢复设计就是围着这个目标做的。

**可借鉴点**

- 恢复逻辑不要执着于比特级忠实，应该优先服务“继续工作”。

---

## 9. 原则八：外部扩展必须标准化，不要野生生长

Claude Code 的 hooks、MCP、bridge、SDK 这些能力，背后都体现一个共通点：

**外部接入不是随便塞一个 callback，而是走标准化协议。**

这带来的好处非常大：

- 扩展点边界清楚
- 生命周期清楚
- 事件传播清楚
- 权限和审计更容易接

很多系统早期为了快，会让插件和主系统深度耦合，后面就很难收拾。

Claude Code 的做法更稳：

- 先定义合同
- 再允许外部接入

**可借鉴点**

- 一旦你要支持外部扩展，优先做协议而不是做临时接口。

---

## 10. 原则九：UI 不只是显示器，而是 Runtime 的一部分

Claude Code 的 UI 架构很值得学，因为它把终端 UI 当成真正的应用层，而不是纯输出层。

这体现在：

- AppState 是全局运行态
- PromptInput 是控制台入口，不是普通输入框
- Messages 是运行态消息的展示转换层
- Permission UI 参与主执行协议

这意味着：

**UI 不是后贴的皮，而是 runtime 的一个组成部分。**

很多 agent 产品之所以体验差，就是因为：

- 后端有一套逻辑
- 前端只是临时把结果印出来

Claude Code 显然不是这么做的。

**可借鉴点**

- 真正的 agent 产品里，UI 必须参与状态建模、执行反馈和交互闭环。

---

## 11. 原则十：产品化的关键，是把“异常状态”也建模进去

Claude Code 很成熟的一点，是它对各种“非正常流程”处理得非常认真：

- interrupted turn
- unresolved tool_use
- orphaned thinking
- compact itself PTL
- stale worktree path
- permission escape / cancel
- remote reconnect

这说明它不只是把 happy path 做通了，而是：

**把系统迟早会进入的异常状态，也纳入了正式设计。**

这是真正产品化的标志。

很多系统的 fragile 就 fragile 在：

- 只有理想路径有定义
- 一断、一重试、一恢复，就开始乱

Claude Code 明显是反过来的，它默认异常一定会发生。

**可借鉴点**

- 做 agent 时，不要把中断、取消、恢复、失败当边缘情况，它们迟早会变成主流情况。

---

## 12. 如果只记得三条，记哪三条

如果你后面不想背 10 条，只记三条也够用。

### 12.1 Agent 要做成 runtime

不是 prompt app。

### 12.2 系统边界先于功能堆砌

消息、状态、权限、生命周期，要先建模。

### 12.3 长期可用性来自恢复能力和治理能力

不是来自模型一时回答得多聪明。

这三条其实已经概括了 Claude Code 最值得学的骨架。

---

## 13. 最后一段总结

Claude Code 最值得学的，不是某个具体 feature。

而是它体现出来的一整套工程观：

- 把 agent 当系统来做
- 把状态和消息当基础设施来做
- 把权限和恢复当主问题来做
- 把扩展和 UI 当正式架构层来做

这也是为什么它比很多 agent demo 更像一个真正产品。

如果你把这 10 条原则吃透，哪怕以后不再读 Claude Code 的每个细节，你也已经把它最有价值的设计思想带走了。
