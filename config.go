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
}

func DefaultConfig() Config {
	return Config{
		Port:                  8188,
		OpenAIUpstream:        "https://api.deepseek.com",
		AnthropicUpstream:     "https://api.deepseek.com/anthropic",
		VerboseLogging:        false,
		AutoOpenGUI:           true,
		ThinkingMode:          "",
		ReasoningEffort:       "",
		SystemPromptPlacement: "first",
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
