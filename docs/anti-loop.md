# DeepSeek V4 Reasoning Loop: DSPlus Anti-Loop Retry

**DeepSeek V4 思维循环：DSPlus 的 Anti-Loop 防循环机制**

Searching "DeepSeek V4 reasoning loop" or "DeepSeek V4 思维循环"? DSPlus includes an **Anti-Loop** engine that detects runaway reasoning and retries automatically.

## The Problem: Reasoning Loops

DeepSeek V4 sometimes gets stuck in the reasoning stage:

- Keeps reasoning, never outputs body text
- Repeatedly verifies the same conclusion
- Overly cautious, afraid to proceed
- Output truncated by `max_tokens` / length
- In coding agents: keeps analyzing, never executes

## How DSPlus Anti-Loop Works

1. Capture streaming / non-streaming responses.
2. Record `reasoning`, `content`, and `finish_reason`.
3. When a threshold is hit or output is truncated, start analysis.
4. Judge loop / excessive / normal.
5. Build a retry request with guidance.
6. Use an independent retry model and `thinking` config.
7. If it exceeds the limit again, return a clear fallback message.

## Enable It

Set `anti_loop_enabled: true` in `config.json`, and optionally tune `antiloop_check_tokens`, `antiloop_retry_model`, `antiloop_retry_thinking`, `antiloop_retry_effort`.

## Related

- [DeepSeek V4 Hallucination](deepseek-v4-hallucination.md)
- [OpenAI Compatible Proxy](openai-compatible-proxy.md)
- [Main README](../README.md)
