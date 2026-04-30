# DSPlus — DeepSeek V4 系统提示词重组代理

DSPlus 是一个本地运行的轻量级 API 中转代理，专为解决 DeepSeek V4 模型**系统提示词优先级弱于首条用户消息**的问题。它将 System Prompt 拼接到第一条 User Message 尾部，其余内容完全透明透传。

## 目录

- [核心原理](#核心原理)
- [项目结构](#项目结构)
- [功能特性](#功能特性)
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
├── logger.go           # 环形缓冲区请求日志、Token 统计、WebSocket 广播
├── proxy.go            # HTTP 代理核心：请求拦截 → 格式检测 → 转换 → 转发
├── gui.go              # 内嵌 Web 前端 + 内部 REST/WebSocket API
├── ws.go               # WebSocket 实时推送管理
├── gui_webview.go      # 内嵌 WebView2 窗口（需 CGO 编译）
├── gui_fallback.go     # 非 CGO 回退：打开系统默认浏览器
├── web/
│   └── index.html      # 单文件 SPA 前端（仪表盘 + 设置页，暗色主题）
├── go.mod / go.sum     # Go 模块依赖
├── build.bat           # Windows 一键编译脚本
└── README.md           # 本文档
```

### Go 文件职责

| 文件 | 关键类型/函数 | 说明 |
|------|-------------|------|
| `config.go` | `Config`, `LoadConfig()`, `SaveConfig()` | 配置 JSON 持久化，自动放在 exe 同目录 |
| `transform.go` | `transformOpenAI()`, `transformAnthropic()`, `extractAnthropicSystem()` | 将 system prompt 拼入首条 user message |
| `logger.go` | `Logger`, `LogEntry`, `TokenUsage`, `UpdateTokenUsage()` | 内存环形缓冲日志，WS 广播 |
| `proxy.go` | `ProxyServer`, `injectThinkingParams()`, `forwardStream()` | HTTP 代理 + 思考模式参数注入 |
| `gui.go` | `handleGUI`, REST API 处理器 | 前端服务 + 配置 API |
| `ws.go` | `wsHub`, `handleWebSocket()` | 实时日志推送 |
| `main.go` | `main()` | 启动所有服务 |

## 功能特性

### System Prompt 重组

DSPlus 将 System Prompt 拼接到用户消息中，支持三种模式：

| 模式 | 拼接位置 | 缓存表现 | 适用场景 |
|------|---------|---------|---------|
| **第一条用户消息后**（默认） | 拼入首条 `role:"user"` 尾部 | 缓存友好，多轮对话中系统提示词位置固定不变 | 日常使用、长对话 |
| **最后一条用户消息后** | 拼入最后一条 `role:"user"` 尾部 | 缓存不友好，每次新消息到来时系统提示词位置都变化 | 仅当第一条消息特别简短、易被模型忽略时使用 |
| **不修改** | 不拼接，System Prompt 保持原样 | 无影响 | 不想修改请求结构时 |

**为什么「最后一条」会影响缓存命中？**

DeepSeek 的 KV 缓存基于前缀匹配。将 System Prompt 拼在最后一条用户消息后，意味着每发送一条新消息，系统提示词在序列中的位置就会后移一次——前缀结构每次都在变化，缓存完全无法命中。

示意（假设对话已进行到第 3 轮）：

```
第一条用户消息后（缓存友好）：
  user:"Hello\n\n<system_prompt>你是助手</system_prompt>"   ← 固定位置，缓存可复用
  user:"继续"
  user:"再继续"

最后一条用户消息后（缓存不友好）：
  user:"Hello"
  user:"继续"
  user:"再继续\n\n<system_prompt>你是助手</system_prompt>"   ← 每次新消息都移动，缓存失效
```

> 除非你明确知道第一条消息存在被模型忽略的问题，否则推荐使用默认的「第一条用户消息后」。

**额外 Prompt 注入与 System Prompt 重组完全独立**，可分别选择不同的注入位置（或不注入）。两者注入到同一条用户消息时，顺序固定为：原始内容 → `<system_prompt>` → `<supreme_instruction>`（额外 Prompt 始终在最末尾）。

- OpenAI 格式：提取所有 `role: "system"` 消息内容拼接，system 消息本身从 messages 数组中移除
- Anthropic 格式：提取顶层 `system` 字段（支持 string 和 `[{type:"text",text:""}]` 数组），处理后删除原 `system` 字段
- 无 system prompt 时原样透传，不做任何修改
- 重组内容用 `<system_prompt>...</system_prompt>` 包裹

### 思考模式控制（三态）
| 设置 | 行为 |
|------|------|
| 不设置 | 请求原样透传，不注入 thinking 参数 |
| 强制关闭思考 | 覆盖写入 `{"thinking":{"type":"disabled"}}` |
| 强制启动思考 | 覆盖写入 `{"thinking":{"type":"enabled"}}` + `reasoning_effort` |

可在设置页选择思考强度：`high`（标准）/ `max`（最强）。Anthropic 格式仅在「强制启动」时注入 `output_config.effort`。

### 请求日志
- 每条请求实时记录：时间、格式、端点、状态码、延迟
- **重组标识**：清晰标注 System Prompt 是否被重组
- **点击展开**：完整查看原始请求体、重组后请求体、API 响应
- **响应头**：展示 DeepSeek 返回的所有 HTTP 头

### Token 统计
- 自动从流式/非流式响应中提取 `usage` 数据
- 仪表盘显示每条请求的总 Token 数和缓存命中率
- 鼠标悬停显示详细拆解：输入/输出/缓存命中/未命中

| 缓存状态 | 显示 | 含义 |
|---------|------|------|
| `cache_hit == 0` | 蓝色 `new` | 全新上下文，无 KV 缓存命中 |
| `< 35%` | 灰色百分比 | 低缓存命中 |
| `35% ~ 74%` | 黄色百分比 | 中等缓存命中 |
| `≥ 75%` | 绿色百分比 | 高缓存命中 |

### 实时更新
- WebSocket 推送，日志和 Token 统计无需手动刷新
- 流式响应完成后自动更新 token 数据到仪表盘

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
┌──────────────────────────────────────────────────────┐
│  🟢 运行中  端口:8188  总请求:128  今日:15           │
├────────┬────────┬──────┬──────┬──────┬─────┬─────────┤
│ 时间   │ 格式   │ 重组 │ 状态 │ 延迟 │Token│缓存│ 端点  │
├────────┼────────┼──────┼──────┼──────┼─────┼─────────┤
│ 14:32  │ OpenAI │ ★是  │ 200  │ 1.2s │ 415 │ 75%│ /chat │
│ 14:30  │ anthrop│ ★是  │ 200  │ 0.8s │ 120 │ new│ /msg  │
└────────┴────────┴──────┴──────┴──────┴─────┴─────────┘
```

- **重组列**：绿色 `是` = 执行了 System Prompt 重组；灰色 `否` = 直通
- **Token 列**：鼠标悬停查看输入/输出/缓存拆解
- **缓存列**：颜色编码命中率，`new` = 全新上下文
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
| 拼接位置 | 第一条用户消息后 | 最后一条用户消息后 / 第一条用户消息后 / 不修改 |
| 额外 Prompt 位置 | 不添加 | 不添加 / 第一条后 / 最后一条后 |
| 额外 Prompt | 空 | 最高优先级指令，注入位置由上方控制 |
| 详细记录 | 关闭 | 开启后日志记录完整请求/响应体 |
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
  "extra_prompt_placement": "none"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `api_key` | string | 是 | DeepSeek API Key |
| `port` | int | 否 | 监听端口，默认 8188 |
| `openai_upstream` | string | 否 | OpenAI 格式上游地址 |
| `anthropic_upstream` | string | 否 | Anthropic 格式上游地址 |
| `verbose_logging` | bool | 否 | 是否记录完整请求/响应体 |
| `auto_open_gui` | bool | 否 | 启动时是否自动打开 GUI |
| `thinking_mode` | string | 否 | `""` / `"disabled"` / `"enabled"` |
| `reasoning_effort` | string | 否 | `"high"` / `"max"` |
| `system_prompt_placement` | string | 否 | 拼接位置：`"first"`（首条用户消息后）/ `"last"`（末条用户消息后）/ `"none"`（不修改） |
| `extra_prompt` | string | 否 | 额外最高优先级指令内容 |
| `extra_prompt_placement` | string | 否 | 额外指令注入位置：`"first"` / `"last"` / `"none"`（不注入，默认） |

## 注意事项

1. **API Key 安全**：API Key 明文存储在 `config.json`，请勿分享或提交到版本控制
2. **端口冲突**：如 8188 被占用，可通过 `--port` 或设置页修改
3. **HTTPS**：DSPlus 仅监听 HTTP。工具本身通过 HTTPS 连接 DeepSeek 上游
4. **日志内存**：最多保留 2000 条日志在内存中，重启清空
5. **Token 统计**：依赖 DeepSeek 返回的 `usage` 对象，仅在响应完整到达后更新
6. **思考模式**：强制模式会覆盖请求中原有的 thinking 参数，请谨慎使用
7. **reasoning_content**：DSPlus 完全透传思维链内容，不修改多轮拼接规则
