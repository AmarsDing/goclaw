# MCP 协议集成 (Model Context Protocol)

## 功能概述

MCP 是一套标准化协议，允许外部服务器为 Claude 提供工具、命令和资源。Claude Code 支持多种 MCP 传输（stdio、SSE、HTTP、WebSocket、SDK），支持多来源配置（用户/项目/企业/插件/CLI），并在运行时动态连接和管理 MCP 服务器。

---

## 从 main 函数入口的实现流程

### 1. MCP 配置聚合

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/services/mcp/config.ts` | `getClaudeCodeMcpConfigs(dynamicServers)` | 聚合所有 MCP 配置源（企业/用户/项目/本地/插件），去重，策略过滤 |
| `src/services/mcp/config.ts` | `getAllMcpConfigs()` | 返回完整的合并配置 |
| `src/services/mcp/config.ts` | `parseMcpConfig()` / `parseMcpConfigFromFilePath()` | 解析 MCP 配置 JSON |
| `src/services/mcp/config.ts` | `addMcpConfig()` / `removeMcpConfig()` | 添加/删除 MCP 配置（写入 `.mcp.json`） |
| `src/services/mcp/config.ts` | `filterMcpServersByPolicy()` | 按企业策略过滤服务器 |
| `src/services/mcp/config.ts` | `isMcpServerDisabled()` / `setMcpServerEnabled()` | 启用/禁用单个服务器 |
| `src/services/mcp/config.ts` | `dedupPluginMcpServers()` | 插件 MCP 去重 |

**配置来源优先级**（低 → 高）：
1. 插件提供的 MCP 配置
2. 用户级 (`~/.claude/settings.json` 中 `mcpServers`)
3. 项目级 (`.mcp.json` 或项目 settings)
4. 本地级
5. CLI `--mcp-config` 参数（`scope: 'dynamic'`）
6. 企业级 (`managed-mcp.json`，最高优先级)

### 2. MCP 类型系统

| 文件路径 | 关键导出 | 职责 |
|---------|---------|------|
| `src/services/mcp/types.ts` | `McpServerConfig` | 单个服务器配置（传输、命令、参数、环境变量） |
| `src/services/mcp/types.ts` | `ScopedMcpServerConfig` | 带作用域和插件来源的配置 |
| `src/services/mcp/types.ts` | `ConnectedMCPServer` / `FailedMCPServer` | 运行时连接状态 |
| `src/services/mcp/types.ts` | `McpJsonConfig` | `.mcp.json` 文件格式 |

**支持的传输方式**：
| 传输类型 | 说明 |
|---------|------|
| `stdio` | 标准输入输出（子进程） |
| `sse` | Server-Sent Events |
| `http` | HTTP Streamable |
| `ws` | WebSocket |
| `sdk` | SDK 内置连接 |
| `claudeai-proxy` | Claude AI 代理 |

### 3. MCP 客户端连接

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/services/mcp/client.ts` | `connectToServer(config)` | 连接单个 MCP 服务器（memoized） |
| `src/services/mcp/client.ts` | `ensureConnectedClient(config)` | 确保客户端已连接 |
| `src/services/mcp/client.ts` | `getMcpToolsCommandsAndResources(configs, callback)` | 批量连接 → 获取工具/命令/资源 |
| `src/services/mcp/client.ts` | `prefetchAllMcpResources(configs)` | 预取所有 MCP 资源 |
| `src/services/mcp/client.ts` | `reconnectMcpServerImpl()` | 重连 MCP 服务器 |
| `src/services/mcp/client.ts` | `clearServerCache()` | 清除服务器缓存 |
| `src/services/mcp/client.ts` | `callMCPToolWithUrlElicitationRetry()` | 调用 MCP 工具（含 URL elicitation 重试） |
| `src/services/mcp/client.ts` | `transformMCPResult()` | 转换 MCP 结果格式 |
| `src/services/mcp/client.ts` | `setupSdkMcpClients()` | 设置 SDK MCP 客户端 |

### 4. MCP 连接管理器（UI 层）

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/services/mcp/MCPConnectionManager.tsx` | `MCPConnectionManager` | React 上下文 Provider |
| `src/services/mcp/MCPConnectionManager.tsx` | `useMcpReconnect()` | 重连 Hook |
| `src/services/mcp/MCPConnectionManager.tsx` | `useMcpToggleEnabled()` | 启用/禁用 Hook |
| `src/services/mcp/useManageMCPConnections.ts` | `useManageMCPConnections()` | 交互式 MCP 生命周期管理 |

### 5. MCP 认证与安全

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/services/mcp/auth.ts` | MCP OAuth 认证 | MCP 服务器级 OAuth |
| `src/services/mcp/channelPermissions.ts` | 权限中继 | Telegram/Discord 等渠道的权限提示转发 |
| `src/services/mcp/channelNotification.ts` | 通知转发 | 权限对话框转发到外部渠道 |
| `src/services/mcp/elicitationHandler.ts` | Elicitation 处理 | MCP elicitation 回调 |
| `src/services/mcp/channelAllowlist.ts` | 渠道白名单 | 渠道服务器权限控制 |

### 6. MCP 辅助模块

| 文件路径 | 关键函数 | 职责 |
|---------|---------|------|
| `src/services/mcp/normalization.ts` | 配置规范化 | 统一不同格式的 MCP 配置 |
| `src/services/mcp/envExpansion.ts` | 环境变量展开 | MCP 配置中的 `${VAR}` 展开 |
| `src/services/mcp/officialRegistry.ts` | `prefetchOfficialMcpUrls()` | 预取官方 MCP 注册表 |
| `src/services/mcp/utils.ts` | 工具函数 | MCP 相关辅助 |
| `src/services/mcp/xaa.ts` | XAA 集成 | 企业级 MCP 认证 |

---

## 连接到 main.tsx 的流程

```
main.tsx action 处理器
    │
    ├── 解析 --mcp-config 标志 → dynamicMcpConfig
    │
    ├── getClaudeCodeMcpConfigs(dynamicMcpConfig)
    │     ├── 加载插件 MCP: loadAllPluginsCacheOnly() → getPluginMcpServers()
    │     ├── 合并: 插件 < 用户 < 项目 < 本地 < 动态/CLI
    │     ├── 去重: dedupPluginMcpServers()
    │     └── 策略过滤: filterMcpServersByPolicy()
    │
    ├── getMcpToolsCommandsAndResources(allMcpConfigs)
    │     ├── 对每个配置: connectToServer() → StdioClientTransport / SSE / HTTP / WS
    │     └── 返回: {tools, commands, resources, clients}
    │
    ├── prefetchAllMcpResources() (交互模式，不阻塞)
    │
    ├── 传入 REPL 的 sessionConfig:
    │     └── dynamicMcpConfig, strictMcpConfig, initialMcpClients
    │
    └── REPL.tsx
          └── <MCPConnectionManager dynamicMcpConfig={...}>
                └── useManageMCPConnections()
                      ├── 监听配置变更
                      ├── 连接新服务器
                      ├── 更新 AppState.mcp
                      └── 管理渠道权限
```
