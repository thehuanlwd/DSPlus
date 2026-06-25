# Bug：推理模型 feedback 循环导致回复 content 永久为空

## 现象

- DeepSeek 推理模型（v4-pro / R1 等）在工具调用多轮后，所有回复的 `content` 字段变为空字符串
- IDE 不展示 `reasoning_content`，用户看到空白回复
- DSPlus 诊断分析页面中 `finish_reason=stop` 但 `content_len=0`

## 根因

### 正常行为

推理模型的回复分两路字段：

| 字段 | 含义 | 用户可见 |
|------|------|:--:|
| `reasoning_content` | 内部思维链 | 否 |
| `content` | 正文输出 | 是 |

工具调用场景中，`content` 为空、`reasoning_content` 非空、`tool_calls` 非空，三者共存是合法的。

### 污染链

1. IDE 把 API 返回的 assistant 消息**原样存入对话历史**
2. 历史中出现大量 `content: ""` + `reasoning_content: "..."` 的先例
3. 模型（强于模仿，弱于服从）回看历史，学到 "本对话中 assistant 的 content 永远为空"
4. 本该输出到 content 的文字被塞进 reasoning
5. 下一轮继续存入，`content: ""` 的先例越来越多
6. 滚雪球 → 最终所有回复都是 content 空，用户看不到任何内容

### 关键链条

```
模型输出 content="" + reasoning="..." (正常，因为工具调用)
  → IDE/DSPlus 原样存入历史
    → 下一轮模型回看，模仿历史
      → 继续 content=""，不再恢复
```

## 修复方案 A：请求侧清洗

### 原理

在转发请求给 DeepSeek API 之前，**从后往前**遍历 `messages` 数组，找到**最后一个**满足条件的 `role=assistant` 消息：

**条件**（两者同时满足才修改）：
1. `content` 为空字符串或不存在
2. `reasoning_content` 非空

**动作**：把 `reasoning_content` 拷贝进 `content`，然后**立即返回**（只修一条）

```
修复前: {"role":"assistant", "content":"", "reasoning_content":"代码问题在于..."}
修复后: {"role":"assistant", "content":"代码问题在于...", "reasoning_content":"代码问题在于..."}
```

**为什么只修一条**：修全部会导致大量工具调用思考过程暴露为正文，刷屏体验差。只修最后一条既打破了"最近消息 content 为空"的反馈循环，又不会引入噪音。

### 为什么这能修

不靠提示词劝模型改行为，而是**断掉污染源**。模型回看历史时不再看到 `content=""` 的先例，自然不会模仿。反馈循环被物理斩断。

### 不影响的情况

| 场景 | 是否修改 | 原因 |
|------|:--:|------|
| 正常文字回复 | 否 | `content` 本就非空 |
| 工具调用中 | 是 | `content` 为空，`reasoning_content` 非空，拷贝以阻断反馈循环 |
| 纯思考中途的历史消息 | 否 | 这种消息在历史中几乎不存在 |

### 实现位置

`proxy.go` — 在 `detectSemanticType` 之后、序列化请求之前（约 line 160-170），或扩展现有 `injectReasoningContent` 函数。

### 配置

建议新增 `auto_fix_empty_content: true` 开关，默认关，用户按需开启。

### 副作用

- 唯一的"副作用"：曾经的思考文字会变成正文暴露给 IDE
- 但这是**预期行为**——比用户看到空白回复好得多
- 不影响 API 兼容性：DeepSeek 允许 reasoning 和 content 共存

## 关联修复（已完成）

### inSession 前缀匹配过滤 system 消息

`analysis.go:386-447` — `inferSession` 中新增 `filterNonSystemMessages()`，前缀匹配时跳过 `role=="system"` 的消息。解决 IDE 工作记忆机制频繁修改 system prompt 导致的会话碎片化。

## 配置建议

运行 DSPlus 时的建议配置：

```json
{
  "thinking_mode": "disabled",
  "auto_reasoning_content": true,
  "anti_loop_enabled": false,
  "analysis_enabled": true
}
```

- `thinking_mode: disabled` — 让 IDE 自行控制思考开关，避免 DSPlus 强制注入 `thinking: enabled`
- `auto_reasoning_content: true` — 保持工具调用的格式兼容性
- `anti_loop_enabled: false` — 非推理模型无需反循环

## 相关文件

| 文件 | 涉及 |
|------|------|
| `proxy.go:142` | `detectSemanticType` 调用点，方案 A 插入位置附近 |
| `proxy.go:393-413` | `injectReasoningContent`，可扩展 |
| `analysis.go:387-447` | `inferSession`，已修复 system 消息过滤 |
| `analysis.go:1690-1761` | `detectSemanticType`，分类逻辑 |
