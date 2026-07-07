# DSPlus — DeepSeek 本地分析 + 强化代理（v2 预览版）

**让 DeepSeek 在工具调用和长会话场景中更稳定可靠，并拥有完整的可观测性。**

DSPlus 是一个本地运行的轻量级 API 中转代理，专为 DeepSeek 模型设计。它同时提供**提示词强化**、**防思维循环**能力，以及**专业级的会话分析与诊断系统**。

核心价值不是简单转发，而是把每一次交互都变成可追溯、可诊断的结构化会话，并通过针对性强化显著提升模型在实际使用中的稳定性。

> 本文档为 v2 版本草稿，基于当前完整功能编写。图片占位符已预留，待补充真实截图。

<!-- 截图占位 1：项目 Logo 或主视觉（可选） -->

## 核心能力

DSPlus 把能力分为两个系统：

### 1. 分析系统（最大差异化价值）

- 自动将所有代理流量归类到 **Session（会话）** 和 **Turn（轮次）**
- 无损记录原始请求、响应、工具调用历史
- 自包含哈希去重存储（大段 System Prompt 同日仅保存一次明文）
- 提供时间线视图、完整会话详情
- 一键导出 Markdown 诊断报告，便于复盘和分享

无论你是做 Agent、复杂工具调用，还是长上下文角色扮演，都能获得前所未有的可见性。

<!-- 
截图占位 2：诊断分析页面
建议拍摄：会话列表 + 选中某个会话后的时间线或详情视图
-->
![诊断分析 - 会话与时间线](screenshots/analysis-sessions.png)

<!-- 
截图占位 3：导出报告示例
建议拍摄：导出的 Markdown 报告内容，或导出按钮与结果
-->
![诊断报告导出](screenshots/export-report.png)

### 2. 强化系统

#### 提示词强化（强烈推荐“最后一条”模式）

DSPlus 支持将 System Prompt 重组到用户消息中，显著改善 DeepSeek 对系统指令的遵循度。

**实战最强体验**：把拼接位置设置为 **最后一条用户消息**。

实际使用反馈：
- 能有效压制幻觉、提升指令遵循度
- 虽然会轻微降低前几条消息的缓存命中率，但对长会话来说影响微乎其微（九牛一毛）
- 无论角色扮演还是开发任务，都非常有效

推荐配置：`system_prompt_placement` = `last`

#### 防思维循环（Anti-Loop）

内置三级防线，专门解决推理模型在工具调用场景下的死循环问题：
- 启发式快速判定
- 并行子 Agent 分析（不阻塞主流程）
- finish_reason=length 兜底

检测到问题时自动介入并重试，大幅减少白白消耗 token 的情况。

#### 防止工具调用卡死的三个关键功能

在各种工具（Claude Code、Cursor、Continue 等）中使用 DeepSeek 时，最常见的卡死场景是：
- 模型陷入无限思考
- 历史中出现 content 为空的 assistant 消息
- reasoning_content 缺失导致后续反馈污染

DSPlus 提供以下三个功能，基本可以杜绝这些问题：

| 功能 | 默认状态 | 作用 |
|------|----------|------|
| 防无限思考 (Anti-Loop) | 关闭（按需开启） | 检测并重试循环 |
| 自动补全 reasoning_content | 开启 | 为带 tool_calls 的 assistant 消息补齐 reasoning_content 字段 |
| 空 content 自动修复 | 关闭（推荐 agent 场景开启） | 清理历史中 content 为空的 assistant 消息，避免反馈循环 |

**推荐 agent / 工具调用用户开启后两个功能**，配合 Anti-Loop 使用，体验会明显更顺畅。

### 实验性功能（谨慎使用）

**意图校准（Anti-Hallucination Intent Calibration）**

DSPlus 提供一个可选的意图校准机制（默认关闭）。

开启后，在特定条件下（模型先进行思考、再开始输出正文，且当前请求不带工具），系统会截断当前输出，并额外发起一次模型调用来注入“[意图校准] 用户现在关心的是……”的提示，尝试让模型重新对齐用户最新意图。

**重要提醒**：
- 此功能目前仍在测试阶段
- 会额外发起一次完整的 API 调用，**显著增加 token 消耗**
- 可能存在副作用，效果和稳定性仍在观察中
- 不建议在生产或对成本敏感的场景默认开启

如需尝试，请在设置中单独启用，并密切关注 token 用量。

## 界面预览（YoRHa 军用终端风格 v2）

DSPlus 内置精致的 Web 界面，采用寄叶（YoRHa）风格设计，信息密度高、操作反馈清晰。

主要三个页面：

1. **仪表盘**：实时日志表格（支持行为列与干预事件分离）、Token 缓存命中率可视化、WebSocket 实时推送

<!-- 
截图占位 4：仪表盘主界面
重点拍摄：YoRHa 表格、状态卡片（端口、总请求、今日流量）、行为列、干预事件列、缓存百分比
-->
![仪表盘主界面](screenshots/dashboard.png)

2. **诊断分析**：会话列表、时间线、单会话详情、导出报告

<!-- 
截图占位 5：诊断分析页面（完整视图）
-->
![诊断分析页面](screenshots/analysis.png)

3. **系统设置**：所有强化与分析选项一目了然

<!-- 
截图占位 6：设置页面
建议同时展示 Analysis、Anti-Loop、Auto 修复等开关
-->
![系统设置](screenshots/settings.png)

点击日志任意一行可展开右侧抽屉，查看原始请求与响应详情（无损展示）。

<!-- 
截图占位 7：日志详情抽屉
-->
![日志详情抽屉](screenshots/drawer.png)

## 快速开始

### 1. 启动

```bash
DSPlus.exe                    # 默认 8188 端口，自动打开浏览器界面
DSPlus.exe --port=9999        # 自定义端口
# 注意：GUI 强制默认开启，已移除 --no-gui 支持和设置开关
```

### 2. 配置 API Key

打开设置页面，填入 DeepSeek API Key（仅本地存储）。

### 3. 推荐基础配置（长会话 / Agent 场景）

- System Prompt 拼接位置：**最后一条用户消息后**（强烈推荐）
- 额外 Prompt 位置：最后一条后（可选）
- 思考模式：按需（多数情况可强制启用）
- 防无限思考：按需开启
- 自动补全 reasoning_content：**开启**（默认已开）
- 空 content 自动修复：**推荐开启**（Agent 场景）
- 分析功能：开启 + 持久化（默认开启）

### 4. 接入客户端

将客户端的 API Base URL 指向 DSPlus：

- OpenAI 兼容客户端：`http://127.0.0.1:8188`
- Anthropic 兼容客户端：`http://127.0.0.1:8188`
- API Key 填任意值（DSPlus 会使用自己配置的 Key 转发）

支持 Claude Code、Cherry Studio、Cursor、Continue.dev、SillyTavern 等。

## API 端点

DSPlus 单端口同时提供代理和 GUI：

| 路径 | 说明 |
|------|------|
| `/chat/completions` | OpenAI 格式代理 |
| `/v1/messages` | Anthropic 格式代理 |
| `/` | Web 界面（仪表盘） |
| `/api/analysis/sessions` | 获取会话列表 |
| `/api/analysis/sessions/{id}` | 获取会话详情 |
| `/api/analysis/sessions/{id}/export.md` | 导出 Markdown 诊断报告 |
| `/api/analysis/sessions/{id}/timeline` | 获取会话时间线 |
| `/ws` | WebSocket 实时推送 |

## 配置说明（关键字段）

```json
{
  "system_prompt_placement": "last",     // 推荐 "last"
  "anti_loop_enabled": true,             // 按需
  "auto_reasoning_content": true,        // 推荐保持开启
  "auto_fix_empty_content": true,        // Agent 场景推荐开启
  "anti_hallucination_enabled": false,   // 实验性，默认关闭
  "analysis_enabled": true,
  "analysis_persistence": true
}
```

完整配置请参考程序内设置页面说明。

## 注意事项

- API Key 明文保存在同目录 `config.json`，请妥善保管
- 防循环重试和意图校准都会产生额外 API 调用，请根据需求开启
- 分析日志默认保留 7 天，可在设置中调整
- 建议定期清理 `antiloop_trace.log`

## 项目结构（简要）

```
DSPlus/
├── main.go / proxy.go          # 核心代理 + 强化逻辑
├── analysis.go                 # 会话分析与诊断系统
├── transform.go                # System Prompt 重组
├── retry.go                    # 防循环引擎
├── web/                        # YoRHa v2 前端
├── config.go                   # 配置管理
└── README_v2.md                # 本文档（v2 预览）
```

## 开发与构建

```bash
# 无 CGO（推荐，浏览器 GUI）
CGO_ENABLED=0 go build -ldflags="-s -w" -o DSPlus.exe .

# CGO（内嵌 WebView）
CGO_ENABLED=1 go build -ldflags="-H windowsgui -s -w" -o DSPlus.exe .
```

## 反馈与贡献

欢迎提交 Issue 分享你的使用场景、截图和建议。

特别欢迎提供：
- 使用 “最后一条消息” 拼接后的实际效果对比
- 工具调用场景下的卡死案例（开启/未开启强化后的区别）
- 诊断报告导出的使用反馈

---

**本 README 为 v2 版本独立草稿**，尚未覆盖原 README.md。  
所有图片占位符请替换为实际拍摄的截图后使用。

祝使用愉快！