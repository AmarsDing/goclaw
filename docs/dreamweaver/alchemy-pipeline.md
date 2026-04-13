# 炼化（Alchemy）流水线与冒烟

## 现状

- **库**：`internal/alchemy` 从仓库分析生成 `plugin.json` 与 **Go `wrapper.go`**（可 `go build`），见 `generator.go`。
- **自动化测试**：`internal/alchemy/generator_go_test.go` 中 `TestGenerateGoWrapper_Compiles` 在临时目录生成并编译 wrapper。
- **一键沙箱 / 发布到 CI**：未在仓库中绑定具体 GitHub Actions 模板；团队可按 `scripts/alchemy-smoke.*` 接入。

## 冒烟脚本

| 脚本 | 说明 |
|------|------|
| `scripts/alchemy-smoke.sh` | Unix：在仓库根执行 `go test ./internal/alchemy/... -run TestGenerateGoWrapper_Compiles` |
| `scripts/alchemy-smoke.ps1` | Windows PowerShell：同上 |

在 CI 中增加一步调用上述脚本即可作为 **炼化最小门禁**。

## 后续（产品级）

- 对指定 Git URL 克隆 → 分析 → 生成 → 推送到 marketplace：建议用 **独立 job** 或 `goclaw marketplace push` 编排，与网关 token、对象存储配置一并管理。
