# DeepSeek V4 System Prompt Not Working: DSPlus Prompt Guard

**DeepSeek V4 不遵守 System Prompt：DSPlus 的解决思路**

If you searched "DeepSeek V4 system prompt not working" or "DeepSeek V4 system prompt 不生效", you are not alone. DSPlus is a local Prompt Guard proxy that makes DeepSeek V4 follow your system prompt more reliably.

## The Problem: System Prompt Ignored

Common symptoms of **DeepSeek V4 system prompt not working**:

- Format requirements dropped after a few turns
- Persona / role settings forgotten in long chat
- Prohibited behaviors re-appear
- First-turn style pollutes later output
- Rules decay after multiple rounds

## How DSPlus Fixes System Prompt Following

DSPlus rewrites the request before it reaches DeepSeek V4:

- **System Prompt placement strategies** — append after the *first* user message (default, balances cache hits), after the *last* message (strongest control), or leave unchanged.
- **Extra high-priority prompt** — inject fixed output format, persona, prohibitions, or project rules.
- **Stage-aware injection** — avoids injecting content at the wrong phase (e.g. tool returns) that would break client protocols.

### Placement strategies

| Strategy | When to use |
|---|---|
| After first user message | Default; balances stability with cache hits |
| After last user message | Strongest control; short context or strict rule enforcement |
| No modification | Keep original structure; use other enhancements only |

> DSPlus improves *instruction following* through prompt structure. It does not rewrite the model's knowledge, so it cannot fix factual errors the base model would make on its own.

This directly improves **DeepSeek V4 system prompt following** (system prompt 遵循 / instruction following).

## Quick Setup

```batch
DSPlus.exe
```

Set `system_prompt_placement` and `extra_prompt` in the GUI, then point your client to `http://127.0.0.1:8188`.

## Related

- [DeepSeek V4 Hallucination](deepseek-v4-hallucination.md)
- [Main README](../README.md)
