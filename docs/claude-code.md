# Using DSPlus with Claude Code / Coding Agents

**Claude Code、OpenCode 等 Coding Agent + DeepSeek V4：用 DSPlus 防假死与空回复**

In coding agents (Claude Code, OpenCode, OpenAI SDK, Anthropic-style clients), DeepSeek V4's issues are amplified because these scenarios demand strict formatting, tool-call correctness, and instruction following.

## Common Coding-Agent Problems on DeepSeek V4

- Tool calls fail or freeze after tool return
- Repeated calls to the same tool
- Analyzes forever but never executes
- Empty `content` makes the IDE show nothing
- Guesses files/requirements, ignores constraints
- CoT and body text misaligned after tool calls

## How DSPlus Helps

- **Stage recognition** — treats conversation / tool call / tool return as different phases, avoiding injecting disruptive content at the wrong time.
- **Empty content repair** — backfills the most recent empty assistant content, breaking the silent-reply feedback loop that IDEs can amplify.
- **reasoning_content compatibility** — reduces tool-call format issues.
- **Anti-Loop** — retries stuck reasoning with guidance.

## Connect

Point the agent's API Base URL to `http://127.0.0.1:8188` (Anthropic style: `ANTHROPIC_BASE_URL`).

## Related

- [Anti-Loop](anti-loop.md)
- [Anthropic Compatible Proxy](anthropic-compatible-proxy.md)
- [Main README](../README.md)
