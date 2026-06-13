# DSPlus Analysis 无损记录与大会话性能修复设计

## 背景

DSPlus 的主价值是透传式中转、提示词注入强化和防循环能力。Analysis 是附加服务，必须服从两个底线：

- 不影响请求透传、接口信息传输、提示词强化和防循环。
- 开启会话 Analysis 服务后，历史记录应尽可能完整、忠实、可追溯。

当前分析页在打开超长会话时会卡死。定位结果显示，问题不是单纯的单条文本未折叠，而是数据重组后出现了两个放大点：

- 后端会话合并后，当前实现会在部分场景让每个 turn 的 `chat_history` 保存累计历史，前端又逐 turn 渲染，导致重复 DOM 接近平方级增长。
- `/api/analysis/sessions/{id}` 直接返回完整 session，前端一次性解析、持有并渲染大量数据。

现有日志设计已经具备一部分无损去重能力：`analysis_logs/YYYY-MM-DD.jsonl` 中对重复的大段 system prompt 使用日内自包含哈希引用。但当前实现还不是完整的无损正文存储层：

- 去重范围主要覆盖 `ChatHistory` 中较长的 system 内容。
- `RawRequest` 和 `RawResponse` 在 `writeToDisk` 中被清空。
- `AnalysisPersistRawBodies` 是旧语义，不应再决定会话 Analysis 是否保存完整记录。
- 没有 gzip/blob 正文层，也没有按需读取完整正文的 API。

## 目标

1. Analysis 开启即保存会话分析历史；`VerboseLogging` 只影响首页仪表盘右侧抽屉详情。
2. Analysis 保存必须无损。原始请求、重组后上游请求、原始响应、分析器请求响应、重试请求响应都应能追溯。
3. 记录层不使用不可逆截断。压缩、哈希引用、字典化、blob 拆分都必须可还原。
4. 代理链路不等待 Analysis 写盘。Analysis 失败只能影响分析历史，不影响代理请求。
5. 分析页默认只加载索引、preview 和可视区域 DOM，展开或导出时按需读取完整正文。
6. 兼容既有 JSONL 日志，包括已存在的 `$ref_dict:md5_...` system prompt 引用和累计型 `chat_history`。

## 非目标

- 不引入 SQLite 或外部数据库。
- 不改变代理核心转发语义。
- 不把 UI preview 作为历史记录本体。
- 不依赖 `VerboseLogging` 保存 Analysis 原始正文。
- 不追求跨日期全局去重；当前优先保持每日日志可独立迁移和清理。

## 方案对比

### 方案 A：继续全量 JSONL

所有原文都直接写入每日 JSONL。

优点：实现最简单，单文件直观。

缺点：重复内容多，文件巨大；读取某个 session 容易搬运整段大 JSON；分析页卡顿会继续复发。

### 方案 B：升级现有 JSONL 去重为无损内容存储层

保留每日 JSONL 作为事件索引和恢复入口。大正文写入每日内容目录，用 hash 引用和 gzip 压缩保存。JSONL 中保存元数据、preview、hash、大小和路径引用。

优点：符合现有设计方向；记录无损；文件可按日期清理；UI 和 API 可以默认轻量。

缺点：需要新增内容引用结构、按需读取 API、历史兼容转换和引用清理。

### 方案 C：嵌入式数据库

用 SQLite 管理事件、消息、blob 引用和分页查询。

优点：查询能力最强。

缺点：引入迁移、锁、损坏恢复和发布复杂度，超过当前修复需要。

推荐采用方案 B。它不是推翻现有实现，而是把现有“每日 JSONL 自包含哈希去重”补齐为真正的无损内容存储。

## 推荐架构

### 1. 存储分层

Analysis 持久化分两层：

- 索引层：`analysis_logs/YYYY-MM-DD.jsonl`
- 正文层：`analysis_logs/content/YYYY-MM-DD/<prefix>/<sha256>.json.gz`

JSONL 继续按事件追加，每行仍是可反序列化的结构化事件，但大字段不再直接内联。大字段以 `ContentRef` 引用正文层。

新增结构：

```go
type ContentRef struct {
    Kind           string `json:"kind"`
    Hash           string `json:"hash"`
    Path           string `json:"path"`
    SizeBytes      int64  `json:"size_bytes"`
    CompressedBytes int64 `json:"compressed_bytes,omitempty"`
    Preview        string `json:"preview,omitempty"`
}
```

`Kind` 用于区分：

- `raw_client_request`
- `raw_upstream_request`
- `raw_response`
- `chat_message_content`
- `reasoning_content`
- `tool_calls`
- `tool_result`
- `analyzer_request`
- `analyzer_response`
- `retry_request`
- `retry_response`

正文 blob 内部保存完整内容及基本校验信息：

```json
{
  "kind": "raw_client_request",
  "hash": "sha256_...",
  "encoding": "utf-8",
  "content": "完整原文"
}
```

gzip 压缩只作用于落盘文件，不改变内容语义。

### 2. 日内自包含原则

内容文件按日期目录保存。当天 JSONL 引用当天 content 目录中的文件。这样：

- 单日日志可以独立备份和迁移。
- 保留天数清理可以按日期目录进行。
- 跨天重复内容允许重复保存，换取简单和鲁棒。

现有 `$ref_dict:md5_...` 继续支持读取，不作为新写入的主要格式。新写入统一使用 `ContentRef`。

### 3. 原始请求与重组请求

当前代理上下文中已有：

- `ctx.OriginalBody`
- `ctx.TransformedBody`
- `ctx.Transformed`

Analysis 事件应分别记录：

- 入站原始请求：客户端发给 DSPlus 的请求体。
- 上游请求：DSPlus 注入/重组后实际发给模型服务的请求体。
- 上游原始响应：模型服务返回给 DSPlus 的响应体。

对于未重组的请求，入站原始请求和上游请求可以引用同一个内容 hash。

### 4. 内存模型

内存中的 `ConversationSession` 不保存完整正文，只保存：

- session、turn、event 元数据。
- role、时间、phase、token、状态码、模型、缓存命中等索引字段。
- preview。
- `ContentRef`。

每个 turn 的 `ChatHistory` 只保存本轮增量消息，不保存累计完整历史。需要完整会话历史时，由后端按 turn 顺序动态拼接索引；需要完整正文时，再按 `ContentRef` 读取 blob。

### 5. API 设计

保留现有 API，但调整返回边界：

- `GET /api/analysis/sessions`：返回 session 摘要列表。
- `GET /api/analysis/sessions/{id}`：返回 session 摘要、turn/event 计数、统计信息，不返回完整大正文。
- `GET /api/analysis/sessions/{id}/timeline?offset=0&limit=100`：返回时间线分页，包含 preview 和 content refs。
- `GET /api/analysis/sessions/{id}/content?ref=<hash>&kind=<kind>`：返回某个引用的完整正文。
- `GET /api/analysis/sessions/{id}/export.md`：按需读取 blob，生成完整 Markdown 报告。

兼容期内，前端可以先只使用新的 timeline API；旧的 session detail API 保留必要字段，避免一次性返回完整文本。

### 6. 前端渲染

分析页时间线改为两级优化：

- 数据分页：只请求当前范围的 timeline items。
- DOM 虚拟化：只渲染可视窗口附近的消息节点。

消息气泡默认显示 preview。用户展开时调用 content API 获取完整正文，并只把该条内容填入 DOM。折叠后清空完整正文 DOM，保留 preview 和引用。

这不是精简记录。完整记录仍在正文层，UI 只是按需显示。

## 兼容策略

### 旧 JSONL 读取

加载旧日志时支持三类情况：

1. 明文 `chat_history`：按旧字段读取，生成内存索引。
2. `$ref_dict:md5_...`：按现有每日字典逻辑回填 system prompt。
3. 累计型 `chat_history`：如果当前 turn 的历史以前一 turn 的历史为前缀，则只保留后面的增量，避免重复渲染。

旧日志不强制迁移。首次打开时以内存索引兼容。本次修复不提供显式“重建索引”功能。

### 旧配置语义

- `AnalysisEnabled`：开启 Analysis 服务，并保存完整会话历史。
- `AnalysisPersistence`：进入兼容过渡期。若存在旧配置，可在保存配置时与 `AnalysisEnabled` 对齐，或在 UI 中隐藏。
- `AnalysisPersistRawBodies`：不再决定 Analysis 是否保存完整原文。可保留为废弃字段，避免旧配置解析失败。
- `VerboseLogging`：只影响首页仪表盘日志抽屉，不影响 Analysis 持久化。

## 错误处理

Analysis 不得阻塞代理路径。

- `SubmitEvent` 仍保持非阻塞。
- 写 JSONL 或 blob 失败时记录错误和计数，不影响代理响应。
- 若 blob 写入失败，该事件 JSONL 中写入 `storage_error` 字段，说明该事件记录不完整。
- 若读取 content ref 失败，API 返回明确错误，不伪造空内容。
- 若 gzip 或 hash 校验失败，导出报告中标记该引用损坏。

队列满时仍不能阻塞代理。后续实施可以增大队列、减少内存正文驻留时间，并暴露 dropped event 计数，但不能为了记录完整性牺牲透传底线。

## 保留与清理

清理以日期为单位：

- 删除过期 `YYYY-MM-DD.jsonl`。
- 同步清理 `analysis_logs/content/YYYY-MM-DD/`。

清理逻辑必须只作用于 Analysis 日志目录内部。实现时需要校验路径位于 `analysis_logs` 下，避免误删。

## 测试计划

### 后端单元测试

- Analysis 开启时，事件写入 JSONL 索引和 gzip blob。
- 同一日重复正文复用 hash 引用。
- 原始请求、重组请求、响应均可无损读回。
- `$ref_dict:md5_...` 旧日志仍可回填。
- 累计型旧 `chat_history` 可转换为增量。
- `VerboseLogging=false` 时，Analysis 仍保存完整原文。
- 写盘失败不影响 `SubmitEvent` 返回。

### API 测试

- session detail 不返回完整大正文。
- timeline 支持 offset/limit。
- content API 按 ref 返回完整正文。
- export.md 可包含完整内容。
- 损坏 ref 返回明确错误。

### 前端行为验证

- 大会话打开时不会一次性创建全部 DOM。
- 滚动加载 timeline 分页。
- 展开单条消息时按需拉取完整正文。
- 折叠后释放完整正文 DOM。

视觉观感由用户最终判断；自动验证只覆盖资源加载、接口状态和明显运行错误。

## 实施顺序

1. 定义 `ContentRef` 和正文存储接口。
2. 调整 TraceEvent，区分原始入站请求、上游请求和原始响应引用。
3. 改造 `writeToDisk`，写 JSONL 索引和 gzip blob。
4. 改造加载逻辑，兼容旧 JSONL 和 `$ref_dict`。
5. 修复 turn 历史累计问题，内存中只保留增量。
6. 新增 timeline/content API。
7. 改造前端分析页为分页和虚拟渲染。
8. 补充测试和大日志回归验证。

## 成功标准

- 开启 Analysis 后，可以从日志中无损获取完整原始请求、重组请求和响应。
- `VerboseLogging` 关闭不影响 Analysis 完整记录。
- 打开包含几十万字历史的会话时，页面不会因全量 DOM 或全量 JSON 卡死。
- 代理请求在 Analysis 写盘失败、队列满或导出失败时仍正常透传。
- 旧日志仍可查看，且累计历史不会重复渲染。
