# Claude Permission UI / Approval Interaction Flow Guide

## 1. 这篇只讲真正关键的部分

Claude Code 的权限系统很复杂，但如果我们从“最关键、最有用”的角度看，其实核心是这条主线：

**模型想做某件事 -> 系统判断能不能直接做 -> 如果不能，怎么把这件事变成用户能理解、能操作、还能沉淀成规则的一次审批。**

这一篇重点不是把所有权限组件都列一遍，而是回答这几个真正重要的问题：

1. Claude Code 的 permission 不是一个弹窗，而是一条决策链，这条链怎么走
2. 它的 UI 为什么拆成很多层，而不是一个大对话框
3. “允许一次 / 本次会话允许 / 永久允许 / 拒绝并给反馈” 这些交互背后真正对应的系统动作是什么
4. 哪些设计特别值得你以后做 agent 产品时借鉴

关键源码主要看这些：

- [services/tools/toolExecution.ts](../src/services/tools/toolExecution.ts)
- [utils/permissions/permissions.ts](../src/utils/permissions/permissions.ts)
- [utils/permissions/permissionSetup.ts](../src/utils/permissions/permissionSetup.ts)
- [components/permissions/PermissionRequest.tsx](../src/components/permissions/PermissionRequest.tsx)
- [components/permissions/PermissionPrompt.tsx](../src/components/permissions/PermissionPrompt.tsx)
- [components/permissions/FallbackPermissionRequest.tsx](../src/components/permissions/FallbackPermissionRequest.tsx)
- [components/permissions/FilePermissionDialog/usePermissionHandler.ts](../src/components/permissions/FilePermissionDialog/usePermissionHandler.ts)

---

## 2. 最通俗的一句话

Claude Code 的 permission，不是“要不要弹窗”。

它更准确地说是：

**把模型的动作请求，翻译成一套可被系统检查、可被用户审批、可被规则沉淀、可被后续复用的决策流程。**

这里有四个关键词：

- 检查
- 审批
- 沉淀
- 复用

很多系统只做到前两个，Claude Code 把后两个也做了。

---

## 3. 先抓主调用链

最核心的链路可以这样记：

```text
模型产生 tool_use
  -> toolExecution.ts 开始执行工具
  -> 权限系统判断：allow / ask / deny / passthrough
  -> 如果 ask，则构造 PermissionRequest
  -> PermissionRequest 选择具体权限 UI 组件
  -> 用户选择 allow / reject / always allow / session allow / ...
  -> 转成 PermissionUpdate / PermissionDecision
  -> 写回 ToolPermissionContext / settings / session
  -> 工具继续执行或中止
```

如果再简化一点：

```text
tool request
  -> decision engine
  -> approval UI
  -> permission update
  -> execution continues
```

这是理解全部权限流最重要的一张图。

---

## 4. 为什么说 permission 是“决策链”而不是“弹窗”

看 [utils/permissions/permissions.ts](../src/utils/permissions/permissions.ts) 和 [services/tools/toolExecution.ts](../src/services/tools/toolExecution.ts) 就会发现：

Claude Code 在真正进入 UI 前，已经考虑了很多层：

- 当前 permission mode
- allow / deny / ask rules
- hook 的决定
- classifier 的决定
- sandbox override
- working dir 限制
- safety check
- async agent 特殊原因
- permissionPromptTool 这类外部 host 决策

也就是说，UI 只是在：

**系统已经无法自动做出最终决定时，才把决定权交给用户。**

这就是成熟设计。

通俗解释：

不是一有工具调用就问用户。

而是系统会先尽量自己判断：

- 明显能放行的，直接放
- 明显要拒绝的，直接拒
- 真正模糊或高风险的，才来问你

这样用户不会被烦死。

---

## 5. `PermissionDecisionReason` 为什么很重要

在 [utils/permissions/permissions.ts](../src/utils/permissions/permissions.ts) 里，有一个非常关键但很容易被忽略的设计：

- `PermissionDecisionReason`

它的重要性在于：

**Claude Code 不只关心最后是 allow 还是 deny，还关心“为什么”。**

比如原因可能是：

- classifier
- hook
- rule
- subcommandResults
- permissionPromptTool
- sandboxOverride
- workingDir
- safetyCheck
- mode
- asyncAgent

这非常关键。

因为如果没有“原因”，后面很多事情都会做不好：

- UI 不知道怎么解释给用户
- telemetry 不知道怎么统计
- audit 不知道是用户拒绝还是规则拒绝
- 远程 host 不知道发生了什么

通俗解释：

“不让做”不够。

还得知道：

- 是公司政策不让做
- 是你自己之前配过规则不让做
- 是分类器觉得危险
- 还是当前模式要求必须确认

只有这样，系统才是可解释的。

---

## 6. `createPermissionRequestMessage()` 说明了一个很成熟的产品思路

在 `permissions.ts` 里，`createPermissionRequestMessage()` 会根据不同 `decisionReason` 生成不同文案。

这很值得注意。

Claude Code 不是用统一一句：

- “Claude wants permission”

糊弄过去。

而是尽量把“为什么要问你”说清楚。

例如：

- classifier 触发审批
- 某条规则要求审批
- 某个 hook 阻止了自动执行
- 当前 permission mode 要求审批
- 这个 bash 命令里有多个子操作，哪些子操作需要审批

这非常产品化。

通俗解释：

不是只问你“可不可以”。

而是告诉你：

“我为什么现在来问你。”

这种解释会显著降低用户的不安和误判。

---

## 7. PermissionRequest.tsx 的角色，不是“弹窗组件”

[PermissionRequest.tsx](../src/components/permissions/PermissionRequest.tsx) 真正扮演的是：

**权限审批的路由器。**

它做的关键事情有三件：

1. 根据 tool 类型选择对应的权限组件
2. 统一处理中断和通知
3. 给不同权限 UI 组件一个统一协议

它有个很关键的函数：

- `permissionComponentForTool(tool)`

会把不同 tool 映射到不同组件，比如：

- `FileEditTool -> FileEditPermissionRequest`
- `BashTool -> BashPermissionRequest`
- `WebFetchTool -> WebFetchPermissionRequest`
- `ExitPlanModeTool -> ExitPlanModePermissionRequest`
- `Glob/Grep/FileRead -> FilesystemPermissionRequest`
- 兜底 -> `FallbackPermissionRequest`

这说明 Claude Code 的权限 UI 不是一个“大一统表单”。

而是：

**统一入口 + 各工具特化审批体验。**

---

## 8. 为什么 Claude Code 要做“按工具专门化”的权限 UI

因为不同工具的风险模型、用户关心的信息、可配置的审批范围完全不同。

例如：

- `Bash`
  用户可能关心具体命令、是否危险、是否以后都允许

- `FileEdit`
  用户更关心改哪个文件、是改一次还是整个目录、是否允许 Claude 改自己的配置

- `WebFetch`
  用户更关心访问哪个 URL / 网络连接

- `ExitPlanMode`
  用户关心的是“计划能否执行”

如果全塞进一个通用框，信息既不够，也不易懂。

通俗解释：

不是所有“审批”都一样。

批准开门、批准上网、批准改文件、批准执行计划，这四件事用户会看重的信息根本不同。

Claude Code 很清楚这一点。

---

## 9. `ToolUseConfirm` 为什么这么关键

`PermissionRequest.tsx` 里的 `ToolUseConfirm` 类型很关键，因为它其实就是审批流的“工作单”。

里面带了：

- `assistantMessage`
- `tool`
- `description`
- `input`
- `toolUseContext`
- `toolUseID`
- `permissionResult`
- `permissionPromptStartTimeMs`
- classifier 状态
- workerBadge
- `onAllow`
- `onReject`
- `recheckPermission`

这说明 Claude Code 不是在 UI 里临时拼字符串。

而是先把一次审批需要的上下文封成一个结构化对象，再交给 UI。

这个设计很值得学。

通俗解释：

用户看到的是一个确认框。

但系统内部看到的是一张完整工单：

- 申请人是谁
- 想干什么
- 为什么要审批
- 批准后怎么继续
- 拒绝后怎么收尾

这比临时塞参数稳太多了。

---

## 10. `PermissionPrompt.tsx` 真正解决了什么问题

[PermissionPrompt.tsx](../src/components/permissions/PermissionPrompt.tsx) 是一个很值得学的共享组件。

它抽出来的不是“选择框”，而是：

**一个带反馈能力的审批交互骨架。**

它统一处理：

- options 展示
- focus
- keybindings
- feedback input mode
- cancel
- analytics
- “Tab to amend” 这种交互提示

最重要的是它支持：

- accept feedback
- reject feedback

也就是：

- 允许时告诉 Claude 接下来怎么做
- 拒绝时告诉 Claude 应该改成什么方式

这非常有价值。

---

## 11. 为什么“批准时也能给反馈”是个好设计

很多系统只在拒绝时允许用户说理由。

Claude Code 不是这样。

它支持：

- 同意，但附加说明
- 拒绝，也附加说明

这个设计很聪明。

因为很多时候用户真正想表达的不是：

- “不能做”

而是：

- “可以做，但换个方式”
- “可以做，但只改这部分”
- “可以执行，但别碰配置文件”

这正是 agent 产品里非常高价值的信号。

通俗解释：

审批不只是 yes/no。

它还是一次“给模型纠偏”的机会。

Claude Code 显然把这件事当真了。

---

## 12. `FallbackPermissionRequest` 展示了权限系统最基础的闭环

[FallbackPermissionRequest.tsx](../src/components/permissions/FallbackPermissionRequest.tsx) 很适合拿来理解最基础权限流。

它提供的选项很典型：

- `yes`
- `yes-dont-ask-again`
- `no`

而这些选项背后不是简单关闭弹窗，而是不同系统动作：

### 12.1 `yes`

- 本次允许
- 调 `toolUseConfirm.onAllow(...)`
- 不写永久规则

### 12.2 `yes-dont-ask-again`

- 允许
- 同时生成 `PermissionUpdate`
- 往 `localSettings` 加 `allow` 规则

### 12.3 `no`

- `onReject(...)`
- 可以附带反馈

这就把 UI 选择和权限系统真正接起来了。

---

## 13. “永不再问”在系统里到底意味着什么

这个点很关键。

在 `FallbackPermissionRequest.tsx` 里，“don't ask again”不是一个抽象 flag。

它会实际生成一条 update：

- `type: 'addRules'`
- `behavior: 'allow'`
- `destination: 'localSettings'`

也就是说：

**用户不是点了一个“偏好按钮”，而是实际往权限规则系统里写了一条规则。**

这就是为什么 Claude Code 的审批不会浪费。

每次审批，都可能沉淀为规则。

通俗解释：

这不是“记住我的选择”那么模糊。

而是系统真的把你的决定写进了“以后遇到什么情况自动怎么处理”的规则表里。

---

## 14. 文件权限流为什么值得单独学

[FilePermissionDialog/usePermissionHandler.ts](../src/components/permissions/FilePermissionDialog/usePermissionHandler.ts) 非常值得学，因为它把“审批结果 -> 权限更新”的映射做得很清楚。

这里的处理方式是：

- `accept-once`
- `accept-session`
- `reject`

而 `accept-session` 不只是放行，还会根据路径与操作类型生成 permission suggestions。

还支持特殊 scope，比如：

- `claude-folder`
- `global-claude-folder`

这非常有价值，因为文件操作审批最适合做“范围化授权”。

不是每次 edit 都重新问一次，而是可以升级成：

- 本目录这轮都允许
- 某类路径允许
- Claude 自己配置目录允许

这就是把用户的选择转成更高阶权限语义。

---

## 15. 为什么范围化授权很重要

如果没有范围化授权，文件编辑类工具会把用户烦死。

想象一下：

- 改一个文件问一次
- 再改第二个文件再问一次
- 再读同目录又问一次

这样系统就不可用了。

Claude Code 的思路是：

**在高频、低惊喜的操作类型上，把用户的一次审批升级为一个合理范围内的规则。**

这是非常实用的设计。

通俗解释：

你不是在批准“这一刀”，而是在批准“这台手术在这个区域继续进行”。

这对连续编码工作非常重要。

---

## 16. `permissionSetup.ts` 说明了系统不是只关心“允许规则”，还关心“危险规则”

[permissionSetup.ts](../src/utils/permissions/permissionSetup.ts) 有一大段很值得学：

- `isDangerousBashPermission(...)`
- `isDangerousPowerShellPermission(...)`
- `isDangerousTaskPermission(...)`

这意味着 Claude Code 不只是支持用户加 allow rule，还会判断：

**某些 allow rule 本身是否过于危险，尤其在 auto mode 下。**

例如：

- `Bash(*)`
- `python:*`
- `node:*`
- `powershell:*`
- Agent 子代理自动允许

这些都可能绕过真正的安全判断。

这非常关键。

通俗解释：

权限系统不只是“帮你记住你允许过什么”。

它还要问：

“你允许的这个范围，会不会大到把整个安全体系架空？”

这是非常成熟的安全观。

---

## 17. 为什么 auto mode 下要特别防“危险 allow”

因为 auto mode 的前提就是系统会更积极地自己推进工作。

这时候如果再给它过于宽泛的 allow rule，比如：

- 任意 bash
- 任意脚本解释器
- 任意子代理

那分类器和审批机制基本就被绕过去了。

Claude Code 明显非常警惕这种情况。

这说明它不是把 permission 当 UX 配件，而是真的当成行为边界系统。

这点非常值得借鉴。

---

## 18. `toolExecution.ts` 为什么是权限链里的关键节点

[toolExecution.ts](../src/services/tools/toolExecution.ts) 是权限系统和工具执行真正接上的地方。

它说明权限并不是独立 subsystem，而是工具执行流水线的一部分。

从 import 就能看出来，这里会接：

- `resolveHookPermissionDecision`
- `runPreToolUseHooks`
- `runPostToolUseHooks`
- `runPostToolUseFailureHooks`
- permission denied hooks
- telemetry

这意味着：

**工具执行前后的权限决策、hook 决策、日志、失败处理，全是在同一条主流水线上编排的。**

所以 permission 不是 UI 附件，而是工具运行态的一部分。

---

## 19. 为什么 Claude Code 的权限 UI 不是“隔离组件”，而是执行协议的一部分

这是很关键的一点。

在 Claude Code 里，权限 UI 不是只负责画界面。

它真正承担的是：

- 把用户决定转换成 `onAllow` / `onReject`
- 可选地产生 `PermissionUpdate`
- 记录反馈
- 更新规则目标（session / localSettings 等）
- 驱动工具继续执行或中止

也就是说，这些 UI 组件其实是“人类审批适配器”。

通俗解释：

系统前半段负责算：

- 该不该问人

权限 UI 这段负责把“人怎么回答”翻译回系统可执行语言。

这就是为什么我说它是执行协议的一部分。

---

## 20. Esc / 中断为什么也被认真处理

`PermissionPrompt.tsx` 和 `PermissionRequest.tsx` 都很认真处理：

- `Esc`
- `app:interrupt`
- cancel

这不是小事。

因为在长期运行的 agent 系统里，“用户没有批准”本身就是一个重要决策。

Claude Code 会把这种行为纳入：

- analytics
- reject 流程
- 清理 / 继续逻辑

通俗解释：

不按按钮，不代表系统可以装作没发生。

在 agent 系统里：

- 取消
- 超时
- 逃逸

都要有定义。

这点很成熟。

---

## 21. 为什么要给审批加 telemetry 和 analytics

从这些组件里能看到很多类似：

- `tengu_accept_submitted`
- `tengu_reject_submitted`
- `tengu_permission_request_escape`
- feedback mode entered / collapsed

这说明 Claude Code 不只是做权限功能，还在研究：

- 用户到底怎么审批
- 用户会不会频繁拒绝某类工具
- 用户是否会在 accept/reject 时给反馈
- 哪类权限 UI 体验不好

这很有产品意识。

通俗解释：

不是“功能做出来就算了”。

而是系统会继续观察：

- 用户到底是怎么和这套审批机制相处的

这有助于迭代更合理的默认值和更少打扰的体验。

---

## 22. 最值得借鉴的设计点

如果你以后要做自己的 agent 产品，这几条最值得直接学。

### 22.1 权限一定要做成“决策链”，不要做成“弹窗函数”

不要只写：

```ts
if (dangerous) showDialog()
```

要先定义：

- rule
- mode
- hook
- classifier
- safety
- host override

然后统一决定是：

- allow
- ask
- deny

### 22.2 不只记录结果，还要记录原因

`PermissionDecisionReason` 这个设计真的很值。

没有原因，后面所有可解释性、审计、遥测都会变差。

### 22.3 审批选择要能沉淀成规则

这是 Claude Code 很成熟的一点。

每次审批不是一次性浪费，而是可能变成：

- session rule
- localSettings rule
- 某路径范围规则

这样系统才会越用越顺手。

### 22.4 支持“允许，但附带反馈”

这是 agent 产品特别值得学的一点。

因为用户很多时候不是想阻止，而是想纠偏。

### 22.5 不同风险类型要有专用审批 UI

文件、bash、网络、计划、子代理，不要强行用同一个通用弹窗。

---

## 23. 哪些地方不要机械照抄

Claude Code 的权限系统已经很成熟，但不是所有项目都要第一天就做到这么细。

### 23.1 一开始不用把所有工具都做专门化 UI

第一版可以：

- 通用 fallback
- 再为最关键的 1-2 个高风险工具做专门化

### 23.2 如果你的系统没有长期会话，就先别做太复杂的规则沉淀

session / local / project / managed rules 这些层次很有用，但也增加复杂度。

### 23.3 analytics 可以后补，但 decision reason 最好第一天就有

原因字段越晚补越痛苦，因为很多调用链都会依赖它。

---

## 24. 一段最通俗的总结

Claude Code 的权限系统，本质上不是“阻止危险操作”这么简单。

它真正做的是：

**把模型的行动自由，收束进一套可解释、可审批、可学习、可沉淀规则的治理流程。**

所以它强的地方不只是：

- 有确认框

而是：

- 知道为什么需要确认
- 知道该给用户看什么信息
- 知道用户这次选择如何影响以后
- 知道哪些允许本身太危险，不能随便放大

这就是为什么 Claude Code 的 permission 值得认真学。

因为它教你的不是“怎么弹窗”，而是：

**怎么把 agent 的行为边界真正做成一个系统。**
