# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

DSPlus 是一个本地运行的轻量级 API 中转代理，专为 DeepSeek V4 模型设计。核心功能是将 System Prompt 重组到用户消息中（因为 DeepSeek V4 的系统提示词优先级弱于首条用户消息），并内置防思维循环引擎。

## 常用命令

### 编译构建

```bash
# 无 CGO 编译（浏览器 GUI）- 推荐
CGO_ENABLED=0 go build -ldflags="-s -w" -o DSPlus.exe .

# CGO 编译（内嵌 WebView 窗口）
CGO_ENABLED=1 go build -ldflags="-H windowsgui -s -w" -o DSPlus.exe .

# Windows 批处理
build.bat
```

### 运行

```bash
DSPlus.exe                  # 默认端口 8188
DSPlus.exe --port=9999      # 自定义端口
DSPlus.exe --no-gui         # 纯服务模式
```

### 测试

```bash
go test ./...
go test -v -run TestTransformOpenAIInPlace ./...  # 单个测试
```

## 架构设计

### 请求处理流程

```
客户端请求 → 格式检测 → System Prompt 重组 → 参数注入 → 转发 DeepSeek API
                 ↓
           防循环引擎（可选）→ 检测到死循环 → 子Agent分析 → 重试
```

### 核心模块职责

| 文件 | 职责 | 关键函数 |
|------|------|---------|
| `config.go` | 配置管理（JSON 持久化） | `LoadConfig()`, `SaveConfig()`, `SafeConfig` |
| `transform.go` | System Prompt 重组（OpenAI + Anthropic） | `transformOpenAIInPlace()`, `transformAnthropicInPlace()` |
| `proxy.go` | HTTP 代理核心 + 防循环流引擎 | `forwardStreamWithAntiLoop()`, `injectThinkingParams()` |
| `retry.go` | 防循环：子Agent分析、并行检测、重试 | `callAntiLoopAnalyzerWith()`, `parallelAnalyze()` |
| `logger.go` | 环形缓冲日志 + Token 统计 + WebSocket 广播 | `NewLogger()`, `UpdateTokenUsage()` |
| `gui.go` | 内嵌 Web 前端 + REST/WebSocket API | `handleGUI()` |
| `gui_webview.go` | WebView2 桌面窗口（CGO 模式） | `openGUI()`, `hasGUI()` |
| `gui_fallback.go` | 浏览器回退（非 CGO 模式） | `openGUI()` |
| `ws.go` | WebSocket 实时推送 | `wsHub`, `handleWebSocket()` |

### 关键设计模式

1. **SafeConfig 线程安全**：配置读写使用 `sync.RWMutex` 保护
2. **环形缓冲日志**：`Logger` 固定容量（2000条），自动淘汰旧日志
3. **双格式支持**：通过结构化解析自动判断 OpenAI/Anthropic 格式
4. **防循环三级防线**：
   - 启发式判定（content==0 && reasoning 过高）
   - 并行分析器（goroutine，不中断主流程）
   - finish_reason="length" 兜底

### API 端点

| 路径 | 用途 |
|------|------|
| `/chat/completions` | OpenAI 格式代理入口 |
| `/v1/messages` | Anthropic 格式代理入口 |
| `/` | GUI 仪表盘 |
| `/api/status` | 服务状态 JSON |
| `/api/logs` | 请求日志列表 |
| `/api/config` | 获取/更新配置 |
| `/ws` | WebSocket 实时推送 |

## 配置说明

配置文件 `config.json` 自动生成在 `DSPlus.exe` 同目录，关键字段：

- `api_key`: DeepSeek API Key（必填）
- `port`: 监听端口（默认 8188）
- `system_prompt_placement`: 拼接位置（`first`/`last`/`none`）
- `thinking_mode`: 思考模式（`enabled`/`disabled`/空）
- `anti_loop_enabled`: 防循环总开关
- `antiloop_check_tokens`: 主动检测阈值（0=关闭）

## 注意事项

1. **CGO 依赖**：内嵌 WebView 需要 MinGW 编译器（推荐 WinLibs）
2. **API Key 安全**：明文存储在 `config.json`，不要提交到版本控制
3. **日志文件**：`antiloop_trace.log` 自动追加写入，定期清理
4. **防循环成本**：每次重试额外消耗 API 调用，按需开启

## 依赖包

- `github.com/gorilla/websocket`: WebSocket 实时推送
- `github.com/webview/webview_go`: 内嵌桌面窗口（CGO 模式）
