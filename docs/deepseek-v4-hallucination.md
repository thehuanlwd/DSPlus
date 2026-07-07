# DeepSeek V4 Hallucination: How DSPlus Reduces Unsupported Answers

**DeepSeek V4 幻觉问题：DSPlus 如何降低胡编乱造**

Searching for "DeepSeek V4 hallucination" or "DeepSeek V4 幻觉"? DSPlus is a local API proxy that helps reduce unsupported claims, fabricated citations, and confident-but-wrong answers from DeepSeek V4.

## The Problem: DeepSeek V4 Hallucinates

DeepSeek V4 is cheap and capable, but in real use it can:

- Invent sources, papers, links, or references that don't exist
- State uncertain facts with high confidence
- Fabricate settings or timelines in long roleplay
- Treat things the user never said as facts

These are classic **DeepSeek V4 hallucinations** (DeepSeek V4 幻觉 / 胡编 / 乱编).

## How DSPlus Reduces DeepSeek V4 Hallucinations

DSPlus sits between your client and the DeepSeek API as a local proxy:

- **Prompt Guard** — reorganizes the system prompt and injects high-priority guardrails so constraints stay in the model's effective context.
- **Intent confirmation (experimental)** — re-injects the latest user intent right before the final answer, re-aligning the model with the actual question to reduce logic drift.
- **Anti-Loop** — detects runaway reasoning and retries with guidance instead of repeating unstable thoughts.
- **Long-conversation stability** — keeps persona and settings from drifting after many turns.

> Note: DSPlus cannot *guarantee* zero hallucinations. It is designed to **reduce** them through prompt structure, stronger instruction following, and loop detection.

## Quick Setup

```batch
DSPlus.exe
```

Point your client's API Base URL to `http://127.0.0.1:8188`, then fill your DeepSeek API Key in the DSPlus GUI.

## Related

- [DeepSeek V4 System Prompt Not Working](deepseek-v4-system-prompt.md)
- [Anti-Loop: Reasoning Loop Control](anti-loop.md)
- [Main README](../README.md)
