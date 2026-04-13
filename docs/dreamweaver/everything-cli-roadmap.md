# everything-cli 路线图（ideas 第 5 条）

## 现状

- **未实现**：仓库内无 `everything-cli` 独立模块或与 Everything 搜索工具的绑定。
- **本仓库替代能力**：`goclaw` 主 CLI 已覆盖网关、技能、市场、changelog 等；与「Everything 风格全局索引」无直接对应。

## 若纳入产品时的建议方向

1. **明确边界**：是作为 **Windows Everything 的查询前端**、还是 **goclaw 内的全局 `file_search` 工具**、或 **独立 `goclaw everything` 子命令**。
2. **接口**：若对接 Everything SDK/IPC，需单独二进制与平台条件编译；若仅复用 `goclaw` 配置，可做成 **插件工具** 或 **MCP 资源**。
3. **分期**：先 **文档与 issue 模板** → 再 **最小 POC**（只读搜索、无写盘）→ 再 **与 agent 工具链集成**。

当前文档仅作 **占位与立项参考**；实现前请在 issue 中锁定需求与平台范围。
