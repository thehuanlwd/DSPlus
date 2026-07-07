# Using DSPlus with SillyTavern

**SillyTavern + DeepSeek V4：用 DSPlus 减少设定漂移与胡编**

SillyTavern users on DeepSeek V4 often hit role/setting drift and hallucinated details in long roleplay. DSPlus helps.

## Common SillyTavern Problems on DeepSeek V4

- Persona / character drift after long context
- Fabricated settings, timelines, or relationships
- Inconsistent tone or forbidden behavior re-appearing
- Repetitive or looping replies

## How to Connect

1. Start DSPlus:

   ```batch
   DSPlus.exe
   ```

2. In SillyTavern, set the API URL to:

   ```text
   http://127.0.0.1:8188
   ```

3. In the DSPlus GUI, set `system_prompt_placement` to `last` for stronger control, and enable Prompt Guard with your character's fixed rules.

## Why It Helps

- Prompt Guard keeps persona and prohibitions in effective context
- Long-conversation stability reduces drift
- Intent confirmation (experimental) re-aligns the model before answering

## Related

- [DeepSeek V4 Hallucination](deepseek-v4-hallucination.md)
- [DeepSeek V4 System Prompt Not Working](deepseek-v4-system-prompt.md)
- [Main README](../README.md)
