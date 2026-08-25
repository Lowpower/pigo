# pigo 实现状态

本文件记录 pigo（用 Go 复刻 [pi](https://github.com/earendil-works/pi)）当前**已实现**与**尚未实现**的内容。权威缺口清单是 [`docs/parity-gaps.md`](parity-gaps.md)。所有已实现模块均以 pi 真实源码为准，且带离线测试。

> 一句话：**执行骨干与面向用户的 slash / skills / themes / templates / RPC 子集已落地。** 配好 API key（或 `OPENCODE_API_KEY`）即可连真模型。剩余项是 OAuth、npm 包管理、额外 provider、交互式 tree/model picker、sqlite、主题化 HTML/gist。

## 已实现

| 模块 | 包 | 对应 pi 来源 | 说明 |
| --- | --- | --- | --- |
| AI 流式脊柱 | `internal/ai` | `packages/ai` | Anthropic Messages + OpenAI Completions + OpenCode 路由；tool_use/tool_result 保真 |
| agent 循环 | `internal/agent` | `agent.ts` / agent-loop | 回合循环、并行工具、length-stop 失败截断 tool、steering/follow-up |
| 队列模式 | `internal/runtime` | `PendingMessageQueue.drain` | 默认 `one-at-a-time`，可设 `all` |
| 七个内置工具 | `internal/tools` | `core/tools/*` | read/write/edit/bash/grep/find/ls |
| 会话 | `internal/session` | `session-manager.ts` | JSONL v3、parentId 树、fork/clone、HTML 导出、resume/import |
| 交互式 TUI | `internal/tui` | `modes/interactive` | bubbletea；slash；Ctrl+P 切模型；Shift+Tab 切 thinking；Alt+Enter follow-up |
| slash / skills / templates / themes | `internal/slash` 等 | `slash-commands.ts`, `skills.ts`, `prompt-templates.ts` | `/help` 列出命令；未知 `/name` 走 skill 或 prompt template |
| 扩展 | `internal/ext` + `internal/protocol` | protocol + extensions | 子进程 RPC |
| 压缩 | `internal/compaction` | `core/compaction` | 回合前自动触发 |
| CLI / RPC | `cmd/pi`, `internal/runtime` | `cli/args.ts`, `rpc-types.ts` | `--mode text\|json\|rpc`；pi 两字母别名；`@file` |

**测试**：`go test ./...` 离线全绿。`golangci-lint run ./...` 应无新问题。

## 尚未实现

见 [`docs/parity-gaps.md`](parity-gaps.md) 的 10 个可粘贴 GitHub issue（OAuth、npm、额外 provider、交互式 tree navigator、TUI chrome 长尾、sqlite、主题化 HTML/gist、trust/sandbox/Windows/图片、`/scoped-models` 编辑器、剩余 RPC）。

## 快速上手

```bash
go run ./cmd/pi                 # 交互式 TUI
go run ./cmd/pi -p "hello"      # 单次非交互
go run ./cmd/pi --mode rpc      # JSONL RPC
go test ./... && golangci-lint run ./...
```
