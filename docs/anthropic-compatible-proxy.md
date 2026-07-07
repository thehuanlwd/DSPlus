# Anthropic Compatible Proxy for DeepSeek V4

**DeepSeek V4 的 Anthropic 兼容代理：DSPlus**

DSPlus also works as an **Anthropic compatible proxy** for DeepSeek V4. Clients using the Anthropic `/v1/messages` format are auto-detected and transformed.

## Supported Style

| Style | Recognition | Default Upstream |
|---|---|---|
| Anthropic style | Top-level `system`, `/v1/messages`, compatible message structure | `https://api.deepseek.com/anthropic` |

## Client Example

```batch
set ANTHROPIC_BASE_URL=http://127.0.0.1:8188
set ANTHROPIC_AUTH_TOKEN=any-value
```

The DeepSeek API Key is configured inside the DSPlus GUI, not in the client.

## Why Use DSPlus

- Same Prompt Guard and Anti-Loop benefits as the OpenAI path
- Stage-aware injection avoids breaking Anthropic-style tool protocols
- One local port serves both proxy and GUI

## Related

- [OpenAI Compatible Proxy](openai-compatible-proxy.md)
- [Claude Code with DSPlus](claude-code.md)
- [Main README](../README.md)
