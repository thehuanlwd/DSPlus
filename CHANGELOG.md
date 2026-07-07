# Changelog

All notable changes to DSPlus are documented here. This project follows the [Keep a Changelog](https://keepachangelog.com/) format.

---

## [0.2.0] - 2026-07-07

> 🎉 **DSPlus v0.2.0 is here — this is the product's true debut.**
>
> DSPlus evolves from a minimal system-prompt proxy into a complete local **analysis + enhancement system** for DeepSeek V4: fewer hallucinations, stronger instruction following, no reasoning loops, no tool-call freezes, with built-in session analysis and a local GUI.

### Added

- **Prompt Guard** — reorganize the system prompt (place after the *first* or *last* user message, or leave unchanged) and inject high-priority global instructions so constraints stay in DeepSeek V4's effective context. Greatly reduces format dropouts, failed prohibitions, and persona drift.
- **Anti-Loop engine** — detects runaway reasoning, analyzes in parallel, and retries with an independent retry model + guided prompt when stuck in a loop or truncated by `max_tokens`.
- **Empty content repair** — auto-backfills empty assistant `content` after tool calls, breaking the silent-reply feedback loop that IDEs can amplify.
- **reasoning_content auto-completion** — improves tool-call format compatibility for reasoning models.
- **Intent confirmation (experimental)** — re-injects the latest user intent right before the final answer to reduce logic drift / logic hallucinations. Note: token usage can roughly double, so it is off by default.
- **Long-conversation stability** — maintains persona and settings across long context and reduces empty / short-output drift.
- **CoT stabilization** — clearer boundaries between `reasoning_content`, body text, and tool calls.
- **Session analysis** — aggregates proxy traffic into Sessions / Turns, keeps lossless hash-deduplicated archives, and exports Markdown diagnostic reports.
- **Local GUI** — live dashboard, settings, token / cache-hit stats, retry view, and basic diagnostics; serves both the OpenAI-compatible and Anthropic-compatible proxy on a single port.
- **Real-time logs** — ring-buffer logging with WebSocket broadcast.
- **i18n** — Chinese / English GUI and bilingual documentation.
- **Themes** — YoRHa and Classic (GitHub Dark) GUI themes.
- **Cross-platform build** — Windows (WebView2 window + tray on CGO build, or browser fallback on non-CGO) and headless builds for Linux / macOS.
- **Client guides** — SillyTavern, Claude Code / coding agents, Open WebUI, and OpenAI / Anthropic SDK integration.

### Changed

- Restructured request pipeline: format detection → semantic stage recognition → system prompt reorganization → extra prompt injection → `thinking` / `max_tokens` handling → reasoning / empty-content repair → forward to DeepSeek API → streaming capture & logging → optional Anti-Loop / intent confirmation / auto retry.
- GUI auto-opens (WebView2 on CGO build, default browser on non-CGO); the proxy itself runs independently of the GUI.
- Repo renamed to `deepseek-v4-prompt-plus` for clearer discoverability.

### Fixed

- Cross-platform compilation: moved the Windows-only `kernel32` language detection behind `//go:build windows` (into `lang_windows.go`, with `lang_other.go` for other platforms) so Linux / macOS builds succeed.

### Previous release

- **0.1.0-alpha** — initial proof-of-concept: a local DeepSeek V4 system-prompt restructuring proxy.

---

## [0.2.0] 中文 - 2026-07-07

> 🎉 **DSPlus v0.2.0 正式发布——这是本产品的真正亮相。**
>
> DSPlus 从一个最小化的 system prompt 重组代理，演进为面向 DeepSeek V4 的完整本地「分析 + 强化系统」：更少幻觉、更强指令遵循、不再思维循环、不再工具调用假死，并自带会话分析与本地 GUI。

### Added（新增）

- **Prompt Guard（提示词守护）** — 重组 system prompt（拼接在首条或最后一条用户消息后，或不修改），并注入高优先级全局指令，让约束稳定停留在 DeepSeek V4 的有效上下文中。显著减少格式掉落、禁令失效与人设漂移。
- **Anti-Loop 防循环引擎** — 检测推理死循环，并行分析，并在卡住或被 `max_tokens` 截断时用独立重试模型 + 指导语重试。
- **空 content 修复** — 工具调用后自动回填空的 assistant `content`，打断 IDE 会放大的「沉默回复」循环。
- **reasoning_content 自动补全** — 提升推理模型在工具调用场景下的格式兼容性。
- **意图确认（实验性）** — 在最终回答前重新注入最新用户意图，缓解逻辑漂移 / 逻辑幻觉。注意：Token 消耗可能接近翻倍，默认关闭。
- **长对话稳定** — 在长上下文中维持人设与设定，减少输出变空变短的退化。
- **CoT 稳定处理** — 厘清 `reasoning_content`、正文与工具调用之间的边界。
- **会话分析** — 将代理流量聚合为会话（Session）/ 轮次（Turn），无损哈希去重归档，并导出 Markdown 诊断报告。
- **本地 GUI** — 实时仪表盘、设置、Token / 缓存命中统计、重试视图与基础诊断；同一端口同时提供 OpenAI 兼容与 Anthropic 兼容代理。
- **实时日志** — 环形缓冲日志 + WebSocket 实时推送。
- **国际化** — 中文 / 英文 GUI 与双语文档。
- **主题** — YoRHa 与 Classic（GitHub Dark）GUI 主题。
- **跨平台构建** — Windows（CGO 构建为 WebView2 窗口 + 托盘，非 CGO 为浏览器回退）以及 Linux / macOS 无界面构建。
- **客户端接入指南** — SillyTavern、Claude Code / Coding Agent、Open WebUI，以及 OpenAI / Anthropic SDK 接入。

### Changed（变更）

- 重构请求管线：格式检测 → 语义阶段识别 → system prompt 重组 → 额外 Prompt 注入 → `thinking` / `max_tokens` 处理 → reasoning / 空 content 修复 → 转发 DeepSeek API → 流式捕获与日志 → 可选 Anti-Loop / 意图确认 / 自动重试。
- GUI 随启动自动打开（CGO 构建为 WebView2 窗口，非 CGO 为默认浏览器）；代理本身不依赖 GUI 即可运行。
- 仓库更名为 `deepseek-v4-prompt-plus`，提升可发现性。

### Fixed（修复）

- 跨平台编译：将 Windows 专属的 `kernel32` 语言检测移至 `//go:build windows` 约束的 `lang_windows.go`（其它平台由 `lang_other.go` 占位），使 Linux / macOS 构建成功。

### Previous release（历史版本）

- **0.1.0-alpha** — 初始验证版：本地 DeepSeek V4 system prompt 重组代理。
