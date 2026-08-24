# pigo 迁移方案（以 pi 源码为准）

> 本文件是 pigo（用 Go 复刻 [pi](https://github.com/earendil-works/pi)）的实现总纲。
> 所有实现必须**以 pi 真实源码为唯一准绳**，本文中的路径/签名均已对照实际源码核对。
> 若本文与 pi 源码冲突，**以源码为准**，并回来修正本文。

## 0. 开发原则

1. **源码为准**：参考仓库只读克隆在 `~/deps/pi`（环境由 `.cursor/install.sh` 自动准备）。
   开发任一模块前，先读对应的 pi 源文件，按其真实行为直译，不凭记忆。
2. **引用可追溯**：每个 Go 模块头部注释标注对应的 pi 文件路径，便于比对。
3. **离线黄金测试**：涉及流式解析（partial-json）、provider 适配、会话格式的地方，
   录制真实响应/搬运 pi 测试夹具做离线回放测试，不在 CI 打真 API。pi 是 MIT 协议，搬用例合法。
4. **竖切优先**：每个 Phase 交付一个端到端可运行、可测试的竖切，而不是横向铺半成品。
5. **格式兼容**：会话 JSONL、配置目录尽量与 pi 保持读写兼容，免费获得迁移路径和测试语料。

## 1. 目标与边界

| 层级 | 内容 | 策略 |
| --- | --- | --- |
| 必须 | agent 循环、流式 LLM、7 个内置工具、TUI、会话持久化 | 直译 |
| 必须 | slash 命令、skills(markdown)、主题(数据文件)、compaction | 直译/微调 |
| 重新设计 | 扩展机制（子进程 RPC）、自定义 provider 注册 | 新协议 |
| 后置 | headless server / RPC 模式、遥测 | Phase 7 |

## 2. 技术栈定版（Go 侧，我们的选择）

| 领域 | 选型 |
| --- | --- |
| CLI 框架 | `spf13/cobra` + `spf13/viper`（config） |
| TUI | `charmbracelet/bubbletea` + `bubbles` + `lipgloss` |
| Markdown | `charmbracelet/glamour` |
| LLM SDK | `anthropic-sdk-go`(官方) / `openai-go`(官方) / `google.golang.org/genai`(官方) / `aws-sdk-go-v2` bedrockruntime |
| Schema | `invopop/jsonschema`（Go struct → JSON Schema，对应 pi 的 TypeBox） |
| Diff/Edit | `sergi/go-diff` |
| SQLite(后置) | `modernc.org/sqlite`（纯 Go，免 CGO） |
| glob/路径 | 标准库 + `gobwas/glob` |
| 分发 | `goreleaser` 单二进制交叉编译 |

依赖策略：**用到再引**，避免 `go mod tidy` 删空依赖。当前已引入 `cobra` + `viper`。

## 3. 模块映射（pigo internal/ ← pi 真实路径）

> pi 是 npm monorepo，核心在 `packages/*`；`coding-agent` 之下才有 `core/`、`modes/`。

| pigo 包 | pi 真实来源 | 说明 |
| --- | --- | --- |
| `internal/ai` | `packages/ai/src/`（`types.ts`、`utils/event-stream.ts`、`utils/json-parse.ts`、`api/*`、`providers/*`、`models.ts`） | 流协议 + provider 适配 + partial-json |
| `internal/agent` | `packages/agent/src/`（`types.ts`、`agent-loop.ts`、`agent.ts`、`stream-fn.ts`） | StreamFn、循环、队列、工具调度 |
| `internal/tools` | `packages/coding-agent/src/core/tools/`（`read.ts` `bash.ts` `edit.ts` `write.ts` `grep.ts` `find.ts` `ls.ts` `index.ts`） | 7 个内置工具 + 注册表 |
| `internal/session` | `packages/coding-agent/src/core/session-manager.ts`（1715 行） | JSONL 追加日志 |
| `internal/compaction` | `packages/coding-agent/src/core/compaction/`（`compaction.ts` `branch-summarization.ts` `utils.ts`） | 历史压缩 |
| `internal/tui` | `packages/tui/src/` + `packages/coding-agent/src/modes/interactive/`（`interactive-mode.ts` 6548 行、`theme/`） | bubbletea 实现 |
| `internal/ext` | `packages/coding-agent/src/core/extensions/`（`types.ts` `loader.ts` `runner.ts`） | 重新设计为子进程 RPC |
| `internal/protocol` | `packages/protocol/src/`（`schemas.ts` `framing.ts` `codec.ts` `cbor/`） | 线格式（帧 + CBOR） |
| `internal/server` | `packages/server` + `packages/client` + `packages/coding-agent/src/modes/rpc/` | headless RPC（后置） |
| `internal/config` | `packages/coding-agent/src/config.ts`、`core/settings-manager.ts`、`auth/`、`auth-storage.ts` | 设置/认证存储 |
| `cmd/pi` | `packages/coding-agent/src/cli.ts` `main.ts` `cli/args.ts` | 入口、flag、mode 分发 |

## 4. 关键保真契约（务必按源码，勿臆造）

### 4.1 流协议 —— pi 的事件是 snake_case
`packages/ai/src/types.ts`（~L535）定义 `AssistantMessageEvent`，事件类型：
`start` / `text_start` `text_delta` `text_end` / `thinking_start` `thinking_delta` `thinking_end` /
`toolcall_start` `toolcall_delta` `toolcall_end` / `done` / `error`。
**没有独立的 `Usage`/`Done` chunk**——usage 挂在 `done`/`error` 里最终的 `AssistantMessage` 上。
（注意：这与最初草案里 `TextDelta|Usage|Done|Error` 的命名不同，以源码为准。）

### 4.2 StreamFn 签名 —— 不是 `func(ctx, req) (<-chan Event, error)`
`packages/agent/src/types.ts`（~L28）：
```ts
export type StreamFn = (
  model: Model<Api>,
  context: Context,
  options?: SimpleStreamOptions,
) => AssistantMessageEventStream | Promise<AssistantMessageEventStream>;
```
Go 直译建议：
```go
type StreamFn func(ctx context.Context, model Model, mctx Context, opts *StreamOptions) (EventStream, error)
```
默认注册在 `packages/agent/src/stream-fn.ts` / `coding-agent/src/core/sdk.ts`（`setDefaultStreamFn(streamSimple)`）。
`Models.streamSimple()`（`packages/ai/src/models.ts` ~L690）按 `model.api` 分发到 provider。

### 4.3 agent-loop 语义
`packages/agent/src/agent-loop.ts`（796 行）。要点：
- 回合：`agent_start → turn_start → streamAssistantResponse → 收集 tool call → 执行 → 结果回填 → turn_end`。
- 工具执行模式 `toolExecution: "sequential" | "parallel"`，默认 **parallel**（`agent.ts` ~L237）；单工具可用 `AgentTool.executionMode` 覆盖。parallel 用 `errgroup`，但**工具结果消息按 assistant 源顺序**发出。
- 中途插话队列 `QueueMode: "all" | "one-at-a-time"`（`types.ts` ~L50）；`steeringMode`/`followUpMode` 默认均 `one-at-a-time`，持久化在 `settings.json`。
- 中断 = `AbortSignal`（Go 用 `context.Cancel` 贯穿到底，避免 goroutine 泄漏——JS 靠 GC 兜底，Go 不会）。
- 事件多播：loop `emit` → `Agent.processEvents` → `AgentSession._handleAgentEvent`（持久化 + 扩展）→ `InteractiveMode.subscribeToAgent`（TUI 渲染）。

### 4.4 会话 JSONL 格式
`packages/coding-agent/src/core/session-manager.ts`：
- 目录：`~/.pi/agent/sessions/--<编码后的 cwd>--/`（`--` 包裹，cwd 去首斜杠、`:`/`/`→`-`）。
- 文件名：`${ISO时间戳(:.→-)}_${sessionId}.jsonl`，例 `2025-12-08T23-24-22-379Z_<uuid>.jsonl`。
- 首行 `SessionHeader`：`{ type:"session", version:3, id, timestamp, cwd, parentSession? }`。
- 条目类型：`message` `thinking_level_change` `model_change` `compaction` `branch_summary` `custom` `custom_message` `label` `session_info`；靠 `id`+`parentId` 组成树。
- 追加：首个 assistant 消息出现后才落盘；首次 `openSync(...,"wx")` 全量写，之后 `appendFileSync` 单行追加；分支重写用 `_rewriteFile()`。

### 4.5 配置 / 认证路径（注意：根是 `~/.pi/agent/`，不是 `~/.pi/`）
`packages/coding-agent/src/config.ts`（可用 `PI_CODING_AGENT_DIR` 覆盖）：
- `~/.pi/agent/settings.json`：设置（键名如 `defaultProvider` `defaultModel` `theme` `steeringMode` `followUpMode` `compaction`…）。
- `~/.pi/agent/auth.json`：凭据（0600，`Record<providerId, Credential>`，`api_key`|`oauth`）。
- 还有 `models.json` `sessions/` `themes/` `skills/` `prompts/` `tools/` `bin/`。
- 项目级 `<cwd>/.pi/settings.json` 与全局深合并（global ← project）。

### 4.6 protocol 帧
`packages/protocol`：`[u32 大端长度][CBOR 负载]`，默认上限 16 MiB；`PROTOCOL_VERSION = 1`。
Command：`list` `create` `attach` `detach` `prompt` `steer` `abort` `set_model` `set_thinking`。
Server 事件：`server_snapshot` `session_snapshot` `session_progress`(`item_started`/`assistant_delta`/`item_updated`/`item_finished`) `session_removed`。

### 4.7 partial-json
`packages/ai/src/utils/json-parse.ts`（124 行，非 200）：`repairJson` / `parseJsonWithRepair` / `parseStreamingJson`（依赖 npm `partial-json@0.1.7` + repair 兜底）。
被 `api/anthropic-messages.ts`、`openai-completions.ts`、`openai-responses-shared.ts`、`bedrock-converse-stream.ts`、`mistral-conversations.ts`、`pi-messages.ts` 使用。
Go 侧需自实现等价的“不完整 JSON 解析 + 修复”，黄金测试从 pi 的流式测试夹具（`packages/ai/test/`）搬运。

### 4.8 provider API 家族（`model.api` 取值）
`anthropic-messages` / `openai-responses` / `openai-completions` / `openai-codex-responses` /
`google-generative-ai` / `google-vertex` / `bedrock-converse-stream` / `mistral-conversations` / `pi-messages`。
每个在 `packages/ai/src/api/*.ts`，消费原生 SSE → 产出 `AssistantMessageEvent`。

### 4.9 CLI flag（真实清单，远多于 `-p/--mode`）
入口 `packages/coding-agent/src/cli.ts → main.ts`，flag 在 `cli/args.ts`：
`--mode text|json|rpc`、`--print|-p`、`--continue|-c`、`--resume|-r`、`--session*`、`--model`、`--provider`、
`--tools|-t`、`--thinking`、`--extension|-e`、`--skill`、`--theme`、`--approve|-a`、`--offline`、`--version|-v`…
子命令：`install` `remove` `update` `list` `config` `auth <cmd>`。
mode 判定（`main.ts` ~L109）：`--mode rpc` → RPC；`--mode json`/`--print`/非 TTY → print；否则 interactive TUI。

## 5. 分阶段实施

> 每个 Phase 完成一个可测竖切。以下阶段号对应 `internal/*/doc.go` 里的标注。

- **Phase 0 骨架（当前已完成脚手架）**：cobra 解析参数、viper 读配置、`internal/` 目录成型。
  下一步竖切：bubbletea 起一个可输入编辑框，`pi` 进入交互界面（对照 `packages/tui/src/components/editor.ts`）。
- **Phase 1 AI 层（脊柱）**：先定 `internal/ai` 的事件模型（对照 4.1）+ StreamFn（4.2），
  先只接 **Anthropic** 一个 provider 端到端打通“发消息→流式上屏”；同时手写 partial-json + 黄金测试（4.7，离线）。
  > 需要 `ANTHROPIC_API_KEY`（作为 Secret 注入）才能跑真流；离线部分（事件解析、partial-json）无需 key。
- **Phase 2 Agent 循环**：直译 `agent-loop.ts`（4.3）：回合、sequential|parallel（errgroup）、QueueMode、ctx 取消、事件多播。
- **Phase 3 七个工具**：`internal/tools`（对照 `core/tools/*`），schema 用 `invopop/jsonschema`；
  bash 用 `os/exec` + 进程组（Unix `setpgid`；Windows 差异记入 README）；edit 用 `go-diff`；grep/find v1 直接遍历 + glob。
- **Phase 4 会话持久化**：`internal/session`，严格按 4.4 的目录/文件名/条目格式，保持与 pi 读写兼容。
- **Phase 5 TUI 完整化**：编辑区 + 自动补全（对照 `tui/src/autocomplete.ts`、`modes/interactive` 的 `createBaseAutocompleteProvider`）；
  流式渲染坑：glamour 整块渲染 → 流式期间只渲染纯文本，回合结束整块过一次 glamour；token delta 按 ~30fps 批量 flush。
- **Phase 6 扩展系统 v1（子进程 RPC，最大一块）**：扩展=独立二进制/脚本，宿主 spawn 经 stdin/stdout 通信；
  帧格式照抄 `packages/protocol`（4.6）。最小消息集：握手/注册(tool|command|provider)/订阅(before_tool_call 等)/浅 UI(notify/status/select)/生命周期(心跳+超时强杀)。
  明确砍掉 `ctx.ui.custom` 自绘组件，待真实需求再设计。
- **Phase 7 补齐**：headless server（protocol 已就绪）、sqlite 会话后端、OAuth 自定义 provider。

第一个里程碑（建议）：**Anthropic 官方 SDK 流式对话 + read/edit/bash 三工具 + JSONL 会话** 的竖切跑通。

## 6. 刻意不做

- 不复刻 pi 的 `pi-tui`（bubbletea 取代，这是换 Go 的收益）。
- 不做 npm 式扩展分发；v1 就是 manifest 指向本地路径或 git URL，宿主负责 build/spawn。
- 不做遥测、browser smoke、shrinkwrap 等发布链设施。

## 7. 风险表

| 风险 | 缓解 |
| --- | --- |
| Go SDK 流式字段不全（cache tokens、usage 细节） | 对照 pi 的 usage 字段逐个验证，缺的从原始 SSE 补 |
| partial-json 边界情况 | 搬 pi 测试夹具做黄金测试 |
| bash 工具 Windows 行为差异 | v1 明确只支持 Unix，README 写清 |
| 扩展 RPC 每工具一次往返延迟 | 工具本身是秒级 IO，IPC 可忽略；热路径（provider 流）不经扩展 |
| glamour 整块渲染闪烁 | 见 Phase 5 方案 |

## 8. 当前脚手架 vs pi 真相：待修正项

> 这些是 Phase 0 脚手架里和 pi 源码不一致、后续实现时需按源码修正的点：

1. **config 路径/键名**：当前 `internal/config` 默认 `~/.pi` + 键 `provider/model/theme`；
   pi 真相是 `~/.pi/agent/settings.json` + 键 `defaultProvider/defaultModel/theme`（见 4.5）。Phase 1/4 前对齐。
2. **CLI flag/mode**：当前只有 `-p/--mode(interactive|print)`；pi 的 `--mode` 取值是 `text|json|rpc` 且 flag 远更多（见 4.9）。按需扩展时对齐。
3. **事件/StreamFn 命名**：`internal/ai` 落地时严格按 4.1/4.2 的 snake_case 与真实签名，勿用最初草案命名。
