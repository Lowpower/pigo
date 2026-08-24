# pigo 实现状态

本文件记录 pigo（用 Go 复刻 [pi](https://github.com/earendil-works/pi)）当前**已实现**与**尚未实现**的内容，对照 [`docs/migration-plan.md`](migration-plan.md) 的分层。所有已实现模块均以 pi 真实源码为准直译，且带离线测试。

> 一句话：**P0–P7 的执行骨干已全部落地并端到端串通**（TUI → agent 循环 → provider → 工具 → 会话 → 扩展；compaction 库）。配好 `OPENCODE_API_KEY` 即可连真模型日常使用。方案「必须」层还差 **slash 命令 / skills / themes** 三块面向用户的功能。

## 已实现

| 模块 | 包 | 对应 pi 来源 | 说明 |
| --- | --- | --- | --- |
| Cloud Agent 环境 | `.cursor/` | — | Go 1.27 安装脚本 + golangci-lint + pi 参考仓库克隆（幂等） |
| 依赖固化 | `tools/pin.go` | — | 整套技术栈锁定版本（`go build -tags tools ./...` 全栈编过） |
| AI 流式脊柱 | `internal/ai` | `packages/ai` | `AssistantMessageEvent` 事件模型、`StreamFn` + ctx 感知 `EventStream`、partial-json 解析 |
| Anthropic 适配器 | `internal/ai/anthropic.go` | `api/anthropic-messages.ts` | 原生 HTTP/SSE → 事件；`ANTHROPIC_BASE_URL`/`ANTHROPIC_API_KEY` 可配 |
| OpenAI 适配器 | `internal/ai/openai.go` | `api/openai-completions.ts` | Chat Completions SSE → 事件（Bearer） |
| OpenCode provider | `internal/ai/opencode.go` | `providers/opencode*.ts` | 读 `OPENCODE_API_KEY`/`OPENCODE_BASE_URL`，按模型路由（`claude-*`→messages，其余→chat/completions） |
| agent 循环 | `internal/agent` | `agent-loop.ts` | 回合循环、工具调度（sequential/parallel、源序）、ctx 取消、`AgentEvent` 流 |
| 七个内置工具 | `internal/tools` | `core/tools/*` | read/write/edit(go-diff)/bash(进程组+超时)/grep/find/ls；JSON-Schema 由 `invopop/jsonschema` 生成；globstar 匹配 |
| 会话持久化 | `internal/session` | `session-manager.ts` | pi 兼容 JSONL：`--<cwd>--/` 目录、`<ISO>_<uuid>.jsonl`、`version:3`、`parentId` 树、缓冲到 assistant 再落盘 |
| 交互式 TUI | `internal/tui` | `tui` + `modes/interactive` | bubbletea 编辑框接 agent 循环；回复流式上屏，回合结束整块过 glamour；工具执行内联；Ctrl+C 中断 |
| 扩展系统（子进程 RPC） | `internal/protocol` + `internal/ext` | `packages/protocol` + `core/extensions` | `[u32 大端长度][JSON]` 帧；`Host` spawn/握手/分发；`Serve` SDK；示例 `examples/extensions/reverse` |
| 会话压缩 | `internal/compaction` | `core/compaction` | `ShouldCompact`/`FindCutIndex`/`Summarize`/`Compact`；`ceil(chars/4)` 估算 |

**测试**：每个包都有离线单测；多处端到端集成——agent + 真实工具 + 会话往返；TUI 经本地 OpenCode 网关跑通（录像）；agent 循环 + 真实扩展子进程（`reverse`：`pigo`→`ogip`）；压缩往返。`go build`/`vet`/`test`/`golangci-lint` 全绿（Go 1.27）。

## 尚未实现

### 方案「必须」层缺口（面向用户的功能）
- **slash 命令系统**：目前只有 TUI 里硬编码的 `/quit`、`/exit`；无 `/model`、`/settings`、`/compact`、扩展命令等。对应 pi `core/slash-commands.ts`。
- **skills（markdown）**：无 `SKILL.md` 加载/发现/注入。对应 pi `core/skills.ts`。
- **themes（主题）**：仅 glamour 自动配色；无主题数据文件/切换。对应 pi `modes/interactive/theme`。

### 集成 / 收尾缺口
- **compaction 接进 TUI 回合**：库已就绪，但尚未在每回合前按模型 context window 自动触发（缺模型目录，需用 config 配默认值）。
- **`--extension` 运行时加载**：`cmd/pi` 尚未接扩展加载 flag（`internal/ext` 已可用）。
- **agent tool 结果回填上下文**：当前把 tool result 当 user 文本重放（简化），非完全保真于 pi 的 tool_result 消息形状。

### 保真修正（见 migration-plan.md §8）
- **config 路径/键名**：当前 `internal/config` 用 `~/.pi` + `provider/model`；pi 真相是 `~/.pi/agent/settings.json` + `defaultProvider/defaultModel`。
- **CLI flag**：当前 `--mode interactive|print`；pi 是 `--mode text|json|rpc` 且 flag 更多。

### 后置（方案 Phase 7 / §4.9）
- **headless server / RPC 模式**：`internal/server` 仍是空壳（`internal/protocol` 已就绪，成本低）。
- **sqlite 会话后端**：未写（`modernc.org/sqlite` 依赖已固化）。
- **provider 长尾**：`openai-responses`（OpenCode 的 `gpt-*` 走 `/v1/responses`）、`google-generative-ai`、`bedrock-converse-stream`、以及对照模型目录的**通用 provider 注册层**。
- **TUI 打磨**：自动补全、~30fps token 批量刷新。
- **遥测**：方案明确不做。

## 快速上手

```bash
go run ./cmd/pi                 # 交互式 TUI（无 key 时用内置 mock provider）
go run ./cmd/pi -p "hello"      # 单次非交互，流式输出
go test ./... && golangci-lint run ./...
```

接真模型：把 `OPENCODE_API_KEY`（需要时加 `OPENCODE_BASE_URL`）配成环境变量/密钥即可。
