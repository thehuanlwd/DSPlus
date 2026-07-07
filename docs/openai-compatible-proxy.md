# OpenAI Compatible Proxy for DeepSeek V4

**DeepSeek V4 的 OpenAI 兼容代理：DSPlus**

DSPlus is an **OpenAI compatible proxy** for DeepSeek V4. Any client that speaks the OpenAI API can use it by changing only the Base URL.

## Supported Endpoints

| Path | Purpose |
|---|---|
| `/chat/completions` | OpenAI-style proxy entry |
| `/v1/chat/completions` | OpenAI-style proxy entry |
| `/v1/messages` | Anthropic-style proxy entry |
| `/` | Local GUI |
| `/ws` | WebSocket real-time logs |

Unrecognized paths are passed through transparently to the configured upstream.

## Client Example

```python
from openai import OpenAI

client = OpenAI(
    api_key="any-value",
    base_url="http://127.0.0.1:8188"
)
```

## Why Use DSPlus as Your Proxy

- Helps reduce instruction-following failures and logic drift (factual-hallucination reduction relies mainly on the experimental intent-confirmation feature and is not guaranteed)
- Strengthens system prompt following
- Repairs empty content after tool calls
- Adds a local GUI with logs, tokens, and session analysis

## Related

- [Anthropic Compatible Proxy](anthropic-compatible-proxy.md)
- [DeepSeek V4 Hallucination](deepseek-v4-hallucination.md)
- [Main README](../README.md)
