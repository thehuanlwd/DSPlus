# DSPlus — DeepSeek V4 系统提示词重组代理

DSPlus 是一个本地运行的轻量级 API 中转代理，专为解决 DeepSeek V4 模型**系统提示词优先级弱于首条用户消息**的问题。它将 System Prompt 拼接到第一条 User Message 尾部，其余内容完全透明透传。

同时内置 **防思维循环（Anti-Loop）** 引擎 —— 智能检测模型何时陷入死循环过度推理，自动介入、分析、重试。

## 目录

- [核心原理](#核心原理)
- [项目结构](#项目结构)
- [功能特性](#功能特性)
- [防思维循环](#防思维循环)
- [快速开始](#快速开始)
- [编译构建](#编译构建)
- [接入工具](#接入工具)
- [GUI 使用说明](#gui-使用说明)
- [API 端点](#api-端点)
- [配置说明](#配置说明)
- [注意事项](#注意事项)

## 核心原理

```
原始请求                          DSPlus 重组后
┌─────────────────────────┐       ┌──────────────────────────────┐
│ messages: [              │       │ messages: [                  │
│   {role:"system",        │       │   {role:"user",              │
│    content:"你是助手"}     │  →    │    content:"Hello           │
│   {role:"user",          │       │                             │
│    content:"Hello"}      │       │  <system_prompt>             │
│ ]                        │       │  你是助手                     │
└─────────────────────────┘       │  </system_prompt>"}           │
                                  │ ]                            │
                                  └──────────────────────────────┘
                                          ↓ 转发 DeepSeek API
                                  其余字段(tools/thinking/stream等)完全不变
```

**支持两种 API 格式：**

| 格式 | 识别特征 | 转发目标 |
|------|---------|---------|
| OpenAI | `messages` 数组含 `role: "system"` | `https://api.deepseek.com` |
| Anthropic | 顶层 `system` 字段 | `https://api.deepseek.com/anthropic` |

## 项目结构

```
DSPlus/
├── main.go             # 程序入口，启动代理服务 + 打开 GUI 窗口
├── config.go           # 配置结构体定义、JSON 文件读写
├── transform.go        # System Prompt 重组核心逻辑（OpenAI + Anthropic 双格式）
├── proxy.go            # HTTP 代理核心：请求拦截 → 格式检测 → 转换 → 转发 + 防循环引擎
├── retry.go            # 防思维循环：子Agent分析、并行检测、重试构建、启发式判定
├── logger.go           # 环形缓冲区请求日志、Token 统计、WebSocket 广播
├── trace.go            # 文件追踪日志（antiloop_trace.log），双写到控制台+文件
├── gui.go              # 内嵌 Web 前端 + 内部 REST/WebSocket API
├── ws.go               # WebSocket 实时推送管理
├── gui_webview.go      # 内嵌 WebView2 窗口（需 CGO 编译）
├── gui_fallback.go     # 非 CGO 回退：打开系统默认浏览器
├── web/
│   └── index.html      # 单文件 SPA 前端（仪表盘 + 设置页，暗色主题）
├── antiloop_trace.log  # 防循环追踪日志（自动生成在 exe 同目录）
├── go.mod / go.sum     # Go 模块依赖
├── build.bat           # Windows 一键编译脚本
└── README.md           # 本文档
```

### Go 文件职责

| 文件 | 关键类型/函数 | 说明 |
|------|-------------|------|
| `config.go` | `Config`, `LoadConfig()`, `SaveConfig()` | 配置 JSON 持久化，自动放在 exe 同目录 |
| `transform.go` | `transformOpenAI()`, `transformAnthropic()` | 将 system prompt 拼入首条 user message |
| `proxy.go` | `ProxyServer`, `forwardStreamWithAntiLoop()`, `injectMaxTokens()` | HTTP 代理 + 防循环流引擎 + 参数注入 |
| `retry.go` | `AntiLoopAnalysis`, `callAntiLoopAnalyzer()`, `parallelAnalyze()`, `executeAndStreamRetry()` | 子Agent分析、并行检测、重试执行 |
| `logger.go` | `Logger`, `LogEntry`, `TokenUsage`, `UpdateTokenUsage()` | 内存环形缓冲日志，WS 广播 |
| `trace.go` | `trace()`, `traceKeyvals()`, `tracelog()` | 文件追踪日志，自动双写到控制台+antiloop_trace.log |
| `gui.go` | `handleGUI`, REST API 处理器 | 前端服务 + 配置 API |
| `ws.go` | `wsHub`, `handleWebSocket()` | 实时日志推送 |
| `main.go` | `main()` | 启动所有服务 |

## 功能特性

### System Prompt 重组

DSPlus 将 System Prompt 拼接到用户消息中，支持三种模式：

| 模式 | 拼接位置 | 缓存表现 | 适用场景 |
|------|---------|---------|---------|
| **第一条用户消息后**（默认） | 拼入首条 `role:"user"` 尾部 | 缓存友好 | 日常使用、长对话 |
| **最后一条用户消息后** | 拼入最后一条 `role:"user"` 尾部 | 缓存不友好 | 第一条消息特别简短时 |
| **不修改** | 不拼接，System Prompt 保持原样 | 无影响 | 不想修改请求结构时 |

**额外 Prompt 注入** 与 System Prompt 重组完全独立。两者注入到同一条用户消息时，顺序固定为：原始内容 → `<system_prompt>` → `<supreme_instruction>`。

### Max Tokens 输出限制

限制模型最大输出 Token 数，防止复杂问题时陷入超长思考：

| 选项 | 行为 |
|------|------|
| 不修改 | 不注入 `max_tokens`，由客户端或模型自行决定 |
| 5000 | 硬限制 5000 tokens |
| 32000 | 硬限制 32000 tokens |
| 自定义 | 手动输入任意值 |

### 思考模式控制（三态）

| 设置 | 行为 |
|------|------|
| 不设置 | 请求原样透传，不注入 thinking 参数 |
| 强制关闭思考 | 覆盖写入 `{"thinking":{"type":"disabled"}}` |
| 强制启动思考 | 覆盖写入 `{"thinking":{"type":"enabled"}}` + `reasoning_effort` |

### 请求日志与实时更新

- 每条请求实时记录：时间、格式、端点、状态码、延迟
- **五种日志类型**，不同颜色徽章区分：
  - 🔵 **OpenAI** / 🟣 **anthropic** — 常规透传请求
  - 🔍 **分析器** — 防循环子 Agent 调用
  - 🔄 **重试** — 防循环重试请求
  - 🐛 **Debug** — 实时 Token 追踪数据
- WebSocket 推送，日志和 Token 统计无需手动刷新
- 点击行展开完整请求/响应细节

### Token 统计

鼠标悬停 Token 列可查看详细拆解：输入/输出/缓存命中/未命中。

| 缓存状态 | 显示 | 含义 |
|---------|------|------|
| `cache_hit == 0` | 蓝色 `new` | 全新上下文 |
| `< 35%` | 灰色 | 低缓存命中 |
| `35% ~ 74%` | 黄色 | 中等缓存命中 |
| `≥ 75%` | 绿色 | 高缓存命中 |

## 防思维循环

防思维循环是 DSPlus 的核心智能引擎，专为解决 DeepSeek V4 容易陷入超长思维死循环的问题。

### 三级防线

```
Token 数达到阈值 (antiloop_check_tokens)
      │
      ├── ① 启发式判定
      │      content==0 && reasoning 过高? → 直接介入，跳过分析器
      │
      ├── ② 并行分析器（goroutine，不中断主流程）
      │      快照当前思维 → 子Agent判定 → loop/excessive/normal
      │
      └── ③ finish_reason="length" 兜底
             流结束后检测 → 子Agent分析 → 重试
```

### 工作流程

```
客户端 ←── SSE 流式输出 ──←── DeepSeek（实时透传）
                              │
  Token 达到检测阈值           │
      ├── 启动并行分析器 ──────┘（goroutine，不中断流）
      │     └─ 判定: loop/excessive → 主动停止 + 重试
      │        normal → 继续流式
      │
  finish_reason="length"?
      └─ 兜底分析 → 重试

重试时:
  - 重试请求包含 Phase 1 全部输出 + 思考过程 + 分析指导
  - 使用独立配置的模型、思考模式、思考强度
  - 若重试再次截断 → 返回固定提示
```

### 重试请求结构

重试请求继承原始对话的完整上下文，并追加：

```
messages: [
  ...原始 messages...,
  {role: "assistant", content: "<Phase1 被截断的输出>"},     // 模型已输出的内容
  {role: "user", content: "推理被中断 — 检测到excessive      // 分析指导
                          请从断点继续，不要重复..."}
]

model: 使用 antiloop_retry_model 配置
thinking: 使用 antiloop_retry_thinking + antiloop_retry_effort 配置
max_tokens: 已移除（给重试完整空间）
```

### 相关设置

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| 防思维循环 | 关闭 | 总开关 |
| 重试模型 | `deepseek-v4-flash` | 重试时使用的模型（flash 快速 / pro 强力） |
| 重试时启用思考 | 不启用 | 重试时是否开启思考模式 |
| 重试思考强度 | high | 重试时的推理强度（启用思考时生效） |
| 检测触发Token数 | 0 | 达到后并行分析。0=关闭主动检测，仅依赖 `finish_reason=length` 兜底 |

### 调试

开启「Debug 模式」后，仪表盘会出现 🐛 **Debug** 标签的日志条目，实时显示：
- `completion_tokens`（API 返回的真实值）
- `estimated_tokens`（字符数估算值）
- `reasoning_chars` / `content_chars`（思考/内容字符数）
- 触发状态、阈值对比

所有防循环关键事件自动写入 `antiloop_trace.log`（exe 同目录），无需查看控制台。

## 快速开始

### 1. 启动 DSPlus

```batch
DSPlus.exe                          # 默认端口 8188
DSPlus.exe --port=9999              # 自定义端口
DSPlus.exe --no-gui                 # 不打开 GUI 窗口
```

启动后自动弹出 GUI 窗口（仪表盘页面）。

### 2. 配置 API Key

1. 点击顶部导航「设置」
2. 填入 DeepSeek API Key（从 https://platform.deepseek.com/api_keys 获取）
3. 按需调整监听端口、上游地址
4. 点击「保存配置」

### 3. 接入工具

将工具的 API 地址指向 `http://127.0.0.1:<端口>`：

**Claude Code / Anthropic SDK：**

```batch
set ANTHROPIC_BASE_URL=http://127.0.0.1:8188
set ANTHROPIC_AUTH_TOKEN=any-value
```

**OpenAI SDK / OpenCode：**

```python
client = OpenAI(
    api_key="any-value",
    base_url="http://127.0.0.1:8188"
)
```

**Cherry Studio / ChatBox 等客户端：**

在 API 设置中将地址改为 `http://127.0.0.1:8188`，API Key 填任意值即可。

## 编译构建

### 方式一：无 CGO（浏览器 GUI）
无需安装 C 编译器，GUI 使用系统默认浏览器打开。

```batch
cd /d F:\"AI code"\DSPlus
set CGO_ENABLED=0
go build -ldflags="-s -w" -o DSPlus.exe .
```

生成 ~10MB 的 `DSPlus.exe`。

### 方式二：CGO 启用（内嵌 WebView 窗口）
需要安装 MinGW（推荐 [WinLibs](https://winlibs.com/)）。

```batch
cd /d F:\"AI code"\DSPlus
set CGO_ENABLED=1
go build -ldflags="-H windowsgui -s -w" -o DSPlus.exe .
```

`-H windowsgui` 使 exe 启动时不显示命令行黑窗，直接弹出桌面窗口。

### 依赖

| 包 | 用途 |
|---|------|
| `github.com/gorilla/websocket` | WebSocket 实时推送 |
| `github.com/webview/webview_go` | 内嵌桌面窗口（CGO 模式） |

## GUI 使用说明

### 仪表盘页面

```
┌──────────────────────────────────────────────────────────────┐
│  🟢 运行中  端口:8188  总请求:128  今日:15                   │
├────────┬──────────┬──────┬──────┬──────┬─────┬─────┬─────────┤
│ 时间   │ 格式     │ 重组 │ 状态 │ 延迟 │Token│缓存 │ 端点    │
├────────┼──────────┼──────┼──────┼──────┼─────┼─────┼─────────┤
│ 14:32  │ OpenAI   │ ★是  │ 200  │ 1.2s │ 415 │ 75% │ /chat   │
│ 14:31  │ 🔍 分析器 │  -   │ 200  │ 0.3s │  50 │  -  │ /分析   │
│ 14:30  │ 🔄 重试   │  -   │ 200  │ 8.2s │ 320 │  -  │ /重试   │
│ 14:29  │ 🐛 Debug  │  -   │5000  │  0ms │3120 │  -  │ /tokens │
└────────┴──────────┴──────┴──────┴──────┴─────┴─────┴─────────┘
```

- **格式列**：五种徽章颜色区分请求类型
- **重组列**：绿色 `是` = 执行了 System Prompt 重组
- **Debug 行**：状态码 = 阈值，Token 列 = 有效 Token 数，悬停看拆解
- **点击行**：展开完整请求/响应细节

### 设置页面

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| API Key | 空 | DeepSeek API Key，仅存本地 |
| 监听端口 | 8188 | 修改后需重启 |
| OpenAI 上游 | `https://api.deepseek.com` | OpenAI 格式转发目标 |
| Anthropic 上游 | `https://api.deepseek.com/anthropic` | Anthropic 格式转发目标 |
| 思考模式 | 不设置 | 强制关闭 / 强制启动 / 不设置 |
| 思考强度 | high | 仅在强制启动时生效 |
| Max Tokens | 不修改 | 不修改 / 5000 / 32000 / 自定义 |
| 拼接位置 | 第一条用户消息后 | 第一条后 / 最后一条后 / 不修改 |
| 额外 Prompt 位置 | 不添加 | 不添加 / 第一条后 / 最后一条后 |
| 额外 Prompt | 空 | 最高优先级指令 |
| **防思维循环** | 关闭 | 总开关 |
| ├ 重试模型 | `deepseek-v4-flash` | 重试时使用 |
| ├ 重试时启用思考 | 不启用 | 重试思考开关 |
| ├ 重试思考强度 | high | 启用时生效 |
| └ 检测触发Token数 | 0 | 主动检测阈值 |
| 详细记录 | 关闭 | 完整记录请求/响应体 |
| Debug 模式 | 关闭 | 实时 Token 追踪数据 |
| 启动时打开 GUI | 开启 | 可关闭以纯服务模式运行 |

## API 端点

DSPlus 单端口复用，同时提供代理和 GUI 服务：

| 路径 | 用途 |
|------|------|
| `/chat/completions` | OpenAI 格式代理入口 |
| `/v1/messages` | Anthropic 格式代理入口 |
| 其他路径 | 透传转发至对应上游 |
| `/` | GUI 仪表盘 |
| `/api/status` | 服务状态 JSON |
| `/api/logs` | 请求日志列表（支持 `?limit=&offset=`） |
| `/api/logs/{id}` | 单条日志详情 |
| `/api/config` | 获取/更新配置 |
| `/ws` | WebSocket 实时推送 |

### 格式检测规则

代理通过检查请求体自动判断格式：

- 包含 `"messages"` 且包含 `"max_tokens"` 但不含 `"role":"system"` → **Anthropic 格式**
- 包含 `"messages"` 且不满足上述条件 → **OpenAI 格式**
- 不满足以上 → 直接透传

## 配置说明

配置文件 `config.json` 自动生成在 `DSPlus.exe` 同目录：

```json
{
  "api_key": "sk-xxxxxxxxxxxxxxxx",
  "port": 8188,
  "openai_upstream": "https://api.deepseek.com",
  "anthropic_upstream": "https://api.deepseek.com/anthropic",
  "verbose_logging": false,
  "auto_open_gui": true,
  "thinking_mode": "",
  "reasoning_effort": "high",
  "system_prompt_placement": "first",
  "extra_prompt": "",
  "extra_prompt_placement": "none",
  "max_tokens_mode": "",
  "max_tokens_custom": 0,
  "anti_loop_enabled": false,
  "antiloop_retry_model": "deepseek-v4-flash",
  "antiloop_retry_thinking": "",
  "antiloop_retry_effort": "high",
  "antiloop_check_tokens": 0,
  "debug_mode": false
}
```

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `api_key` | string | - | DeepSeek API Key（必填） |
| `port` | int | 8188 | 监听端口 |
| `openai_upstream` | string | `https://api.deepseek.com` | OpenAI 格式上游 |
| `anthropic_upstream` | string | `https://api.deepseek.com/anthropic` | Anthropic 格式上游 |
| `verbose_logging` | bool | false | 完整记录请求/响应体 |
| `auto_open_gui` | bool | true | 启动时打开 GUI |
| `thinking_mode` | string | `""` | `""` / `"disabled"` / `"enabled"` |
| `reasoning_effort` | string | `"high"` | `"high"` / `"max"` |
| `system_prompt_placement` | string | `"first"` | `"first"` / `"last"` / `"none"` |
| `extra_prompt` | string | `""` | 额外最高优先级指令 |
| `extra_prompt_placement` | string | `"none"` | `"first"` / `"last"` / `"none"` |
| `max_tokens_mode` | string | `""` | `""` / `"5000"` / `"32000"` / `"custom"` |
| `max_tokens_custom` | int | 0 | 自定义 max_tokens 值 |
| `anti_loop_enabled` | bool | false | 防思维循环总开关 |
| `antiloop_retry_model` | string | `"deepseek-v4-flash"` | 重试模型 |
| `antiloop_retry_thinking` | string | `""` | `""` / `"enabled"` / `"disabled"` |
| `antiloop_retry_effort` | string | `"high"` | `"high"` / `"max"` |
| `antiloop_check_tokens` | int | 0 | 主动检测阈值（0=关闭） |
| `debug_mode` | bool | false | 实时 Token 追踪 + 文件日志 |

## 注意事项

1. **API Key 安全**：API Key 明文存储在 `config.json`，请勿分享或提交到版本控制
2. **端口冲突**：如 8188 被占用，可通过 `--port` 或设置页修改
3. **HTTPS**：DSPlus 仅监听 HTTP。工具本身通过 HTTPS 连接 DeepSeek 上游
4. **日志内存**：最多保留 2000 条日志在内存中，重启清空
5. **防循环成本**：每次重试会额外消耗 API 调用（分析器 + 重试请求），建议按需开启
6. **追踪日志**：`antiloop_trace.log` 每次启动追加写入，建议定期清理
7. **思考模式**：强制模式会覆盖请求中原有的 thinking 参数

---

💬 **LINUX DO** - 一个活跃的技术社区

👉 https://linux.do/
