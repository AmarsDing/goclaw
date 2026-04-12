# Claude Code 内部实现原理详解

> 🤖 为了让你更好地理解 AI Agent 的工作原理，这是一份通俗易懂的解析文档

---

## 一句话概括

**Claude Code 是一个能在终端里帮你写代码的 AI 助手，它通过"工具"来执行具体操作，通过"对话"来理解你的需求。**

---

## 核心架构图

```
            用户输入 (你说的话)
                    │
                    ▼
┌─────────────────────────────────────────────────────────────┐
│                     CLI 入口 (cli.tsx)                       │
│                  负责解析命令、启动程序                       │
└─────────────────────────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────────────────────┐
│                    主循环 (main.tsx)                         │
│         👉 这是 Claude Code 的"大脑"                         │
│    1. 接收用户消息                                           │
│    2. 决定用什么工具                                         │
│    3. 执行工具                                               │
│    4. 把结果发给 AI 模型                                     │
│    5. 重复...                                                │
└─────────────────────────────────────────────────────────────┘
                    │
          ┌─────────┴─────────┐
          ▼                   ▼
   ┌────────────┐      ┌────────────┐
   │  工具系统   │      │   AI 模型   │
   │ (184个)    │◄─────│ (Claude)   │
   └────────────┘      └────────────┘
          │
          ▼
   ┌────────────────────────────────────────┐
   │              工具示例                   │
   │  📖 Read - 读取文件                      │
   │  ✏️ Write - 写入文件                     │
   │  ✂️ Edit - 编辑文件                      │
   │  💻 Bash - 执行命令                     │
   │  🔍 Grep - 搜索内容                      │
   │  📁 Glob - 查找文件                      │
   │  🤖 Agent - 创建子任务                  │
   │  ... (共184个)                          │
   └────────────────────────────────────────┘
```

---

## 第一部分：启动流程

### 1.1 你输入 `claude` 后发生了什么？

当你打开 Claude Code 时，整个启动过程就像这样：

```
用户执行 ./dist/cli.js
        │
        ▼
┌───────────────────┐
│ cli.tsx (入口)    │  👉 第一个接触点
│ - 检查参数        │
│ - 处理 --version │
│ - 加载配置        │
└───────────────────┘
        │
        ▼
┌───────────────────┐
│ main.tsx (主逻辑) │  👉 真正的开始
│ - 初始化遥测      │
│ - 加载系统提示词  │
│ - 启动 REPL       │
└───────────────────┘
        │
        ▼
┌───────────────────┐
│ launchRepl()      │  👉 进入交互模式
│ - 等待用户输入    │
│ - 处理每条消息    │
└───────────────────┘
```

**📝 通俗解释**：就像你打开微信一样，app 启动 → 登录 → 进入聊天界面。Claude Code 也是：入口 → 初始化 → 进入对话模式。

### 1.2 快速路径优化

代码中有一个"快速通道"设计：

```typescript
// cli.tsx 中的快速路径
if (args[0] === '--version') {
  // 不加载任何模块，直接输出版本号
  console.log(`${MACRO.VERSION} (Claude Code)`);
  return;
}
```

**为什么这样做？** `--version` 是最常用的检查命令，不需要加载整个程序，启动速度更快。

---

## 第二部分：工具系统 (Tools)

### 2.1 什么是工具？

**工具 = AI 可以调用的函数**

想象一下：如果你是一个指挥官，AI 就是你的士兵。你说"去把那个文件给我拿过来"，士兵需要知道：
- **用什么方式拿？** （用手？用传送？用飞的？）
- **拿到了怎么给你？** （扔过来？放桌上？）

**工具就是定义这些"方式"的东西。**

### 2.2 Claude Code 的工具箱

Claude Code 内置了 **184 个工具**，可以分为几大类：

| 类别 | 例子 | 数量 |
|------|------|------|
| 📁 文件操作 | Read, Write, Edit, Glob, Grep | ~30 |
| 💻 命令执行 | Bash, PowerShell, REPL | ~10 |
| 🤖 任务管理 | Agent, TaskCreate, TaskRead | ~20 |
| 🔌 扩展能力 | MCPTool, MCPResources | ~15 |
| 🔧 其他 | Config, Schedule, ExitPlanMode | ~100+ |

### 2.3 一个具体的例子：BashTool

让我们看看最常用的 `BashTool`（执行命令行命令）是怎么工作的：

```typescript
// 简化后的流程
class BashTool {
  // 1. 验证命令是否安全
  async validate(command: string): Promise<ValidationResult> {
    // 检查是否是危险命令
    if (command.includes('rm -rf /')) {
      return { result: false, message: '危险命令！' };
    }
    // 检查是否需要用户确认
    if (isDestructive(command)) {
      return { result: false, needsConfirmation: true };
    }
    return { result: true };
  }

  // 2. 执行命令
  async execute(command: string, context: ToolUseContext) {
    // 创建子进程执行命令
    const child = spawnShellTask(command);
    
    // 实时捕获输出
    child.on('stdout', (data) => {
      context.reportProgress(data);
    });
    
    // 返回结果
    return { stdout: output, stderr: error };
  }
}
```

**🔍 关键设计点**：

1. **权限检查**：每个命令执行前都要检查权限
2. **语义分析**：自动判断命令是"读"还是"写"
   - `ls`, `cat`, `grep` → 读操作（可折叠显示）
   - `rm`, `mv`, `cp` → 写操作（需确认）
3. **超时控制**：防止命令卡死
4. **沙箱隔离**：危险命令在沙箱中运行

### 2.4 工具的执行流程

```
用户说："帮我运行 npm install"
        │
        ▼
┌─────────────────────────┐
│ main.tsx 主循环         │
│                         │
│ 1. 把用户消息发给 AI    │
│    "帮我运行 npm install"│
└─────────────────────────┘
        │
        ▼
┌─────────────────────────┐
│ AI 模型思考             │
│ 👉: 我应该调用 BashTool │
│     参数: "npm install" │
└─────────────────────────┘
        │
        ▼
┌─────────────────────────┐
│ 权限检查 (hooks)        │
│ - 检查是否有权限执行    │
│ - 是否需要用户确认      │
└─────────────────────────┘
        │
        ▼
┌─────────────────────────┐
│ 执行工具 (BashTool)     │
│ - spawnShellTask        │
│ - 捕获输出              │
│ - 返回结果              │
└─────────────────────────┘
        │
        ▼
┌─────────────────────────┐
│ 把结果发回给 AI         │
│ (输出 + 继续对话)       │
└─────────────────────────┘
        │
        ▼
        🔄 循环
```

---

## 第三部分：任务系统 (Tasks)

### 3.1 为什么要独立于工具？

**工具是同步的，任务是异步的。**

- **工具**：你调用它，它立即返回结果（像函数调用）
- **任务**：你启动它，它在后台运行，你可以继续做其他事（像启动一个进程）

### 3.2 任务类型

Claude Code 支持多种任务类型：

| 类型 | 前缀 | 说明 |
|------|------|------|
| `local_bash` | b | 本地 shell 命令 |
| `local_agent` | a | 本地子 Agent |
| `remote_agent` | r | 远程 Agent |
| `local_workflow` | w | 工作流 |
| `monitor_mcp` | m | MCP 监控 |

### 3.3 任务的生死周期

```
创建任务
    │
    ▼
┌─────────┐
│ pending │ ← 等待执行
└─────────┘
    │
    ▼
┌─────────┐
│ running │ ← 正在执行
└─────────┘
    │
    ├── 成功 ──► completed
    │
    └── 失败 ──► failed
    │
    └── 被杀 ──► killed
```

**任务 ID 生成**：
```typescript
// 使用 8 字节随机 ID，前缀表示类型
// local_bash: b + 8字符 = "b3a7f2c1d"
// local_agent: a + 8字符 = "a9f2e1b3c"
const taskId = generateTaskId('local_bash'); // => "b3a7f2c1d"
```

---

## 第四部分：AI 对话循环

### 4.1 主循环工作原理

这是 Claude Code 最核心的部分，理解它就理解了整个系统：

```typescript
// 简化版的 main.ts 主循环
async function mainLoop() {
  while (true) {
    // 1. 等待用户输入
    const userMessage = await readUserInput();
    
    // 2. 把消息发送给 AI 模型
    const response = await claude.sendMessage(userMessage, {
      system: getSystemPrompt(),  // 系统提示词
      tools: getTools(),          // 可用工具列表
    });
    
    // 3. 处理 AI 的响应
    for (const block of response.content) {
      if (block.type === 'text') {
        // 普通文本，直接显示
        displayText(block.text);
      }
      else if (block.type === 'tool_use') {
        // AI 调用了工具
        const result = await executeTool(block.name, block.input);
        
        // 4. 把工具结果发回给 AI（让它继续处理）
        claude.continue(result);
      }
    }
  }
}
```

### 4.2 系统提示词 (System Prompt)

每次对话开始时，AI 会收到一个"说明书"：

```typescript
// 系统提示词的简化版
const systemPrompt = `
你是一个名为 Claude Code 的 AI 编程助手。

## 你的能力
- 读取和修改文件
- 执行命令行命令
- 创建和执行子任务
- 使用各种开发工具

## 工作方式
1. 理解用户需求
2. 决定使用哪个工具
3. 执行工具并获取结果
4. 根据结果决定下一步

## 重要规则
- 先思考再行动
- 每次操作前解释你的意图
- 遇到问题及时告知用户
`;
```

---

## 第五部分：技能系统 (Skills)

### 5.1 什么是技能？

**技能 = 预定义的工作流程**

就像手机里的"快捷指令"：
- 普通方式：打开地图 → 搜索餐厅 → 叫车 → 付款
- 技能方式：说"帮我叫辆车" → 自动完成所有步骤

### 5.2 技能目录结构

```
src/skills/
├── loadSkillsDir.ts      // 加载技能目录
├── mcpSkillBuilders.ts   // MCP 技能构建器
├── bundled/              // 内置技能
│   └── index.ts
└── bundledSkills.ts      // 技能定义
```

### 5.3 技能的工作方式

```typescript
// 技能加载流程
async function loadSkills() {
  // 1. 扫描技能目录
  const skillDirs = await glob('./skills/*/');
  
  // 2. 加载每个技能
  for (const dir of skillDirs) {
    const skill = await loadSkillFromDir(dir);
    
    // 3. 注册到工具列表
    registerTool(skill.toTool());
  }
}
```

---

## 第六部分：记忆系统 (memdir)

### 6.1 为什么需要记忆？

如果你和 AI 的对话超过几轮，AI 会"忘记"之前说的话。
**记忆系统就是为了解决这个问题。**

### 6.2 记忆类型

Claude Code 有多层记忆：

| 层级 | 存储位置 | 内容 |
|------|----------|------|
| 短期 | 内存 | 当前对话上下文 |
| 中期 | memdir | 最近几次对话的摘要 |
| 长期 | 文件系统 | 用户偏好、项目信息 |

### 6.3 记忆压缩 (Compaction)

当对话太长时，Claude Code 会"压缩"记忆：

```
原始对话 (10000 token)
        │
        ▼ [压缩]
记忆摘要 (2000 token)
        │
        ▼ [继续对话]
新的对话 + 记忆摘要
```

**这样做的好处**：大幅减少 token 消耗，降低成本。

---

## 第七部分：MCP (Model Context Protocol)

### 7.1 什么是 MCP？

**MCP = AI 界的 USB 接口**

就像 USB 让电脑可以连接各种外设，MCP 让 AI 可以连接各种工具和服务。

### 7.2 MCP 在 Claude Code 中的角色

```
┌─────────────────────────────────────┐
│         Claude Code                 │
│                                     │
│  ┌─────────┐   ┌─────────┐          │
│  │ 工具们   │   │  MCP    │◄───┐    │
│  └─────────┘   │  客户端  │    │    │
└────────────────┴──────────┘    │    │
                                │    │
                    ┌───────────┴┐  │
                    │  MCP 服务器  │  │
                    │ (外部服务)   │  │
                    └────────────┘  │
                    
常见的 MCP 服务器：
- 文件系统
- GitHub
- 数据库
- Slack
- 等等...
```

### 7.3 MCP 工具类型

```typescript
// src/services/mcp/types.ts
type McpServerConfig = {
  command: string;      // 启动命令
  args?: string[];      // 参数
  env?: Record<string, string>;  // 环境变量
};

type McpTool = {
  name: string;
  description: string;
  inputSchema: object;
};
```

---

## 第八部分：UI 系统 (Ink)

### 8.1 为什么用 Ink？

Claude Code 是一个终端应用，但 UI 很炫酷。它用的是 **Ink**（React 的 CLI 版本）。

### 8.2 Ink vs React

| React | Ink |
|-------|-----|
| 浏览器 DOM | 终端字符渲染 |
| `<div>`, `<span>` | `<Text>`, `<Box>` |
| CSS 布局 | Flexbox 布局 |

### 8.3 UI 组件示例

```typescript
// 一个简单的 Ink 组件
import { Box, Text } from 'ink';

const MyComponent = ({ name }) => (
  <Box flexDirection="column">
    <Text bold>你好，{name}！</Text>
    <Text color="green">✓ 任务完成</Text>
  </Box>
);
```

渲染结果：
```
你好，Claude！
✓ 任务完成
```

---

## 第九部分：特性开关系统

### 9.1 什么是特性开关？

代码中有 **90+ 个编译开关**，可以在构建时开启/关闭某些功能：

```typescript
// build.ts 中的开关配置
const featureFlags = {
  BRIDGE_MODE: false,         // IDE 桥接
  COORDINATOR_MODE: false,   // 多代理协调
  BUILTIN_EXPLORE_PLAN_AGENTS: true,  // 内置探索代理
  TOKEN_BUDGET: true,        // Token 预算显示
  // ... 等
};
```

### 9.2 死代码消除 (Dead Code Elimination)

```typescript
// 源码中这样写
const coordinatorModule = feature('COORDINATOR_MODE')
  ? require('./coordinator/coordinatorMode.js')
  : null;

// 构建时，如果 COORDINATOR_MODE = false
// 整个 require 语句会被删除，不影响性能
```

**通俗解释**：就像盖房子时，有些房间不想要的门窗，提前打好招呼，施工时直接不建，而不是建好了再拆。

---

## 第十部分：完整的数据流

```
用户输入
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│ cli.tsx (入口)                                          │
│ - 解析参数                                             │
│ - 快速路径优化                                          │
└─────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│ main.tsx (主循环)                                      │
│                                                         │
│  while(等待消息) {                                       │
│    1. 获取系统提示词                                     │
│    2. 获取可用工具列表                                   │
│    3. 发送给 AI                                         │
│                                                         │
│    for (响应中的每个块) {                                │
│      if (是工具调用) {                                   │
│        → 权限检查 (hooks)                                │
│        → 执行工具 (tools/)                               │
│        → 返回结果                                        │
│        → 继续循环 (把结果发给 AI)                        │
│      } else {                                           │
│        → 显示文本                                        │
│      }                                                  │
│    }                                                    │
│  }                                                      │
└─────────────────────────────────────────────────────────┘
    │
    ├──► tools/ (184个工具)
    │     │
    │     ├── BashTool (命令行)
    │     ├── FileReadTool (读文件)
    │     ├── FileWriteTool (写文件)
    │     ├── AgentTool (子代理)
    │     └── MCPTool (扩展)
    │
    ├──► tasks/ (后台任务)
    │     │
    │     ├── LocalShellTask
    │     └── AgentTask
    │
    ├──► services/ (核心服务)
    │     │
    │     ├── api/ (API 调用)
    │     ├── mcp/ (MCP 协议)
    │     └── analytics/ (统计)
    │
    └──► components/ (UI 组件)
          │
          ├── Ink 渲染
          └── 终端输出
```

---

## 总结：Claude Code 是什么？

| 维度 | 说明 |
|------|------|
| **核心** | 一个可以在终端里帮你写代码的 AI 助手 |
| **工作方式** | 接收用户消息 → 调用工具 → 返回结果 → 循环 |
| **工具** | 184 个内置工具（文件、命令、任务等） |
| **扩展** | 通过 MCP 连接外部服务 |
| **记忆** | 多层记忆系统 + 压缩 |
| **UI** | 基于 Ink 的炫酷终端界面 |

---

## 对比 OpenClaw

| 特性 | Claude Code | OpenClaw |
|------|------------|----------|
| **开源** | ❌ 逆向工程 | ✅ 完全开源 |
| **架构** | 单体应用 | Gateway + Node |
| **工具系统** | 184 个硬编码工具 | 技能系统 (SKILL.md) |
| **记忆管理** | memdir 模块 | 文件式 (MEMORY.md) |
| **扩展方式** | MCP | AgentSkills |
| **学习价值** | 高（看高手怎么写） | 高（可以随便改） |

---

*文档生成时间: 2026-04-01*