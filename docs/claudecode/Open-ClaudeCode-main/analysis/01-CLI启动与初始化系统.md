# CLI 启动与初始化系统

## 功能概述

Claude Code 的启动系统负责：CLI 参数解析（Commander.js）、快速路径分发（`--version`、`--daemon`、`bridge` 等）、完整初始化（配置、认证、遥测、MCP、插件、Skills）、以及启动交互式 REPL 或 headless 打印模式。

---

## 从 main 函数入口的实现流程

### 1. 入口点

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/entrypoints/cli.tsx` | `main()` | 最顶层入口；解析 `process.argv`，处理快速路径，最后导入并调用 `main.tsx` |

**快速路径分发**（按优先级）：

| 条件 | 处理方式 | 相关导入 |
|------|---------|---------|
| `--version` / `-v` | 直接打印版本号，零模块加载 | 无 |
| `--dump-system-prompt` | 输出系统提示并退出 | `src/utils/config.ts`, `src/constants/prompts.ts` |
| `--claude-in-chrome-mcp` | 启动 Chrome MCP 服务器 | `src/utils/claudeInChrome/mcpServer.ts` |
| `--chrome-native-host` | Chrome 原生消息宿主 | `src/utils/claudeInChrome/chromeNativeHost.ts` |
| `--computer-use-mcp` | 计算机操作 MCP 服务器 | `src/utils/computerUse/mcpServer.ts` |
| `--daemon-worker` | 守护进程工作线程 | `src/daemon/workerRegistry.ts` |
| `remote-control` / `rc` / `bridge` | 远程控制桥接模式 | `src/bridge/bridgeMain.ts` |
| `daemon` | 守护进程主控 | `src/daemon/main.ts` |
| `ps` / `logs` / `attach` / `kill` / `--bg` | 后台会话管理 | `src/cli/bg.ts` |
| `new` / `list` / `reply` | 模板任务 | `src/cli/handlers/templateJobs.ts` |
| `environment-runner` | BYOC 运行器 | `src/environment-runner/main.ts` |
| `self-hosted-runner` | 自托管运行器 | `src/self-hosted-runner/main.ts` |
| `--worktree --tmux` | tmux worktree 模式 | `src/utils/worktree.ts` |

**正常路径**（无特殊标志时）：
```
startCapturingEarlyInput()          // src/utils/earlyInput.ts
import('../main.js').main()         // 动态导入 main.tsx
```

---

### 2. 主启动函数

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/main.tsx` | `main()` (导出为 `cliMain`) | 核心启动函数（~4,700 行）；Commander 参数解析、初始化、REPL 启动 |

**模块加载时的副作用**（在所有 import 之前执行）：
1. `profileCheckpoint('main_tsx_entry')` — `src/utils/startupProfiler.ts`
2. `startMdmRawRead()` — `src/utils/settings/mdm/rawRead.ts`（启动 MDM 子进程并行读取）
3. `startKeychainPrefetch()` — `src/utils/secureStorage/keychainPrefetch.ts`（并行预取 macOS 密钥链）

**Commander.js 配置的主要选项**：

| 选项 | 功能 |
|------|------|
| `--model` / `-m` | 选择模型 (sonnet/opus/haiku) |
| `--permission-mode` | 权限模式 (default/acceptEdits/bypassPermissions/plan) |
| `--settings` | 加载 settings.json 文件 |
| `--plugin-dir` | 加载插件目录 |
| `--mcp-config` | MCP 服务器配置 |
| `-p` / `--print` | 非交互模式（headless） |
| `-c` / `--continue` | 继续上次对话 |
| `-r` / `--resume` | 恢复指定会话 |
| `--output-format` | 输出格式 (text/json/stream-json) |
| `--dangerously-skip-permissions` | 跳过所有权限检查 |

**`preAction` 钩子**（Commander 命令执行前）：
```
ensureKeychainPrefetchCompleted()   // 等待密钥链预取完成
init()                              // src/entrypoints/init.ts
```

---

### 3. 初始化子系统

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/entrypoints/init.ts` | `init()` | 初始化所有子系统（memoized，只执行一次） |

**`init()` 执行的初始化步骤**：

| 顺序 | 调用 | 来源文件 |
|------|------|---------|
| ① | `enableConfigs()` | `src/utils/config.ts` |
| ② | `applySafeConfigEnvironmentVariables()` | `src/utils/managedEnv.ts` |
| ③ | `applyExtraCACertsFromConfig()` | `src/utils/caCertsConfig.ts` |
| ④ | `configureGlobalMTLS()` | `src/utils/mtls.ts` |
| ⑤ | `configureGlobalAgents()` | `src/utils/proxy.ts` |
| ⑥ | `setupGracefulShutdown()` | `src/utils/gracefulShutdown.ts` |
| ⑦ | `setShellIfWindows()` | `src/utils/windowsPaths.ts` |
| ⑧ | `detectCurrentRepository()` | `src/utils/detectRepository.ts` |
| ⑨ | `preconnectAnthropicApi()` | `src/utils/apiPreconnect.ts` |
| ⑩ | `populateOAuthAccountInfoIfNeeded()` | `src/services/oauth/client.ts` |
| ⑪ | `recordFirstStartTime()` | `src/utils/config.ts` |

---

### 4. 插件与 Skills 初始化

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/plugins/bundled/index.ts` | `initBuiltinPlugins()` | 注册内置插件 |
| `src/skills/bundled/index.ts` | `initBundledSkills()` | 注册内置 Skills |
| `src/tools.ts` | `getTools(permissionContext)` | 注册并返回所有可用工具 |
| `src/commands.ts` | `getCommands(cwd)` | 注册并返回所有可用命令（含插件/Skills） |

---

### 5. MCP 配置加载

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/services/mcp/config.ts` | `getClaudeCodeMcpConfigs()` | 聚合所有 MCP 配置（用户/项目/本地/插件/企业） |
| `src/services/mcp/client.ts` | `getMcpToolsCommandsAndResources()` | 连接 MCP 服务器，获取工具/命令/资源 |
| `src/services/mcp/client.ts` | `prefetchAllMcpResources()` | 预取所有 MCP 资源 |
| `src/services/mcp/client.ts` | `connectToServer()` | 连接单个 MCP 服务器 |

---

### 6. 启动 REPL 或 Headless 模式

**交互模式**：
| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/interactiveHelpers.tsx` | `showSetupScreens()` | 显示 TrustDialog、Onboarding 等设置界面 |
| `src/interactiveHelpers.tsx` | `renderAndRun()` | 创建 Ink 渲染上下文并运行 |
| `src/replLauncher.tsx` | `launchRepl()` | 加载 App + REPL 组件，启动 React 渲染 |
| `src/components/App.tsx` | `App` | 顶层 Context Provider 包装 |
| `src/screens/REPL.tsx` | `REPL` | 主交互界面（~5,000 行） |

**Headless 模式**（`-p` 参数）：
| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/main.tsx` | `main()` 内 print 分支 | 构建消息，直接调用 query 循环，输出到 stdout |

---

## 启动流程图

```
node package/cli.js [args]
    │
    ▼
cli.tsx:main()
    ├── --version? → 打印版本号 → 退出
    ├── --daemon-worker? → runDaemonWorker() → 退出
    ├── bridge/remote? → bridgeMain() → 退出
    ├── daemon? → daemonMain() → 退出
    ├── ps/logs/kill? → bg handlers → 退出
    └── (正常路径)
          │
          ├── startCapturingEarlyInput()
          └── import main.tsx → cliMain()
                │
                ├── [模块加载副作用]
                │     ├── profileCheckpoint()
                │     ├── startMdmRawRead()
                │     └── startKeychainPrefetch()
                │
                ├── Commander.js 参数解析
                │
                ├── preAction 钩子
                │     ├── ensureKeychainPrefetchCompleted()
                │     └── init()
                │           ├── enableConfigs()
                │           ├── applySafeConfigEnvironmentVariables()
                │           ├── configureGlobalMTLS()
                │           ├── configureGlobalAgents()
                │           ├── setupGracefulShutdown()
                │           ├── preconnectAnthropicApi()
                │           └── populateOAuthAccountInfoIfNeeded()
                │
                ├── action 处理器
                │     ├── initBuiltinPlugins()
                │     ├── initBundledSkills()
                │     ├── getTools() + getCommands()
                │     ├── getClaudeCodeMcpConfigs()
                │     ├── getMcpToolsCommandsAndResources()
                │     │
                │     ├── (-p 模式) → 构建消息 → query() → stdout
                │     │
                │     └── (交互模式)
                │           ├── showSetupScreens() → TrustDialog
                │           ├── applyConfigEnvironmentVariables()
                │           └── launchRepl()
                │                 └── <App><REPL /></App>
                │                       → React/Ink 事件循环
                └── 等待用户输入...
```
