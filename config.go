package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	APIKey                 string `json:"api_key"`
	Port                   int    `json:"port"`
	OpenAIUpstream         string `json:"openai_upstream"`
	AnthropicUpstream      string `json:"anthropic_upstream"`
	VerboseLogging         bool   `json:"verbose_logging"`
	AutoOpenGUI            bool   `json:"auto_open_gui"`
	ThinkingMode           string `json:"thinking_mode"`
	ReasoningEffort        string `json:"reasoning_effort"`
	SystemPromptPlacement  string `json:"system_prompt_placement"`
	ExtraPrompt            string `json:"extra_prompt"`
	ExtraPromptPlacement   string `json:"extra_prompt_placement"`
	MaxTokensMode          string `json:"max_tokens_mode"`
	MaxTokensCustom        int    `json:"max_tokens_custom"`
	AntiLoopEnabled        bool   `json:"anti_loop_enabled"`
	AntiLoopRetryModel     string `json:"antiloop_retry_model"`
	AntiLoopRetryThinking  string `json:"antiloop_retry_thinking"`
	AntiLoopRetryEffort    string `json:"antiloop_retry_effort"`
}

func DefaultConfig() Config {
	return Config{
		Port:                  8188,
		OpenAIUpstream:        "https://api.deepseek.com",
		AnthropicUpstream:     "https://api.deepseek.com/anthropic",
		VerboseLogging:        true,
		AutoOpenGUI:           true,
		ThinkingMode:          "enabled",
		ReasoningEffort:       "max",
		SystemPromptPlacement: "first",
		ExtraPromptPlacement:  "last",
		ExtraPrompt: "# 在你的思考过程（<think>标签内）中，请遵守以下规则：\n- **方向明确时**：果断执行，不反复推翻、不过度谨慎。选定路径后直接推进，除非遇到严重阻碍因素（例如硬性报错，逻辑矛盾，用户明确否定）\n- **方向不明时**：将自我修正循环限制在 2 次尝试以内。如果仍然受阻，立即停止自我推演，直接向用户提问澄清。获取关键信息后再继续，不要自行猜测或罗列平行方案。\n- **核心原则**：思考服务于推进，而非风险防御。用决策代替犹豫，用沟通代替假设。\n- **禁止在思考过程中输出完整文件/函数代码** 如需示意，使用 `// ... 省略 ...` 或 diff 摘要，简短的行内代码引用\n- 进行代码任务时，实际的代码变更必须通过提供的工具执行。不要提前在思考或回答中输出完整的新版代码。\n- 思考的目标是确认逻辑正确，而非展示最终结果。",
		AntiLoopRetryModel:    "deepseek-v4-flash",
		AntiLoopRetryThinking: "",
		AntiLoopRetryEffort:   "high",
	}
}

func configPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "config.json")
}

func LoadConfig() (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), err
	}
	return cfg, nil
}

func SaveConfig(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}
