package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

type Config struct {
	APIKey                   string `json:"api_key"`
	Port                     int    `json:"port"`
	LANAccess                bool   `json:"lan_access"`
	OpenAIUpstream           string `json:"openai_upstream"`
	AnthropicUpstream        string `json:"anthropic_upstream"`
	VerboseLogging           bool   `json:"verbose_logging"`
	Language                 string `json:"language"`
	ThinkingMode             string `json:"thinking_mode"`
	ReasoningEffort          string `json:"reasoning_effort"`
	SystemPromptPlacement    string `json:"system_prompt_placement"`
	ExtraPrompt              string `json:"extra_prompt"`
	ExtraPromptPlacement     string `json:"extra_prompt_placement"`
	MaxTokensMode            string `json:"max_tokens_mode"`
	MaxTokensCustom          int    `json:"max_tokens_custom"`
	AntiLoopEnabled          bool   `json:"anti_loop_enabled"`
	AntiLoopRetryModel       string `json:"antiloop_retry_model"`
	AntiLoopRetryThinking    string `json:"antiloop_retry_thinking"`
	AntiLoopRetryEffort      string `json:"antiloop_retry_effort"`
	AntiLoopCheckTokens      int    `json:"antiloop_check_tokens"`
	DebugMode                bool   `json:"debug_mode"`
	AutoReasoningContent     bool   `json:"auto_reasoning_content"`
	AutoFixEmptyContent      bool   `json:"auto_fix_empty_content"`
	AnalysisEnabled          bool   `json:"analysis_enabled"`
	AnalysisPersistence      bool   `json:"analysis_persistence"`
	AnalysisPersistRawBodies bool   `json:"analysis_persist_raw_bodies"`
	AnalysisRetentionDays    int    `json:"analysis_retention_days"`
	AntiHallucinationEnabled bool   `json:"anti_hallucination_enabled"`
	AntiHallucinationPrompt  string `json:"anti_hallucination_prompt"`
}

func DefaultConfig() Config {
	return Config{
		Port:                     8188,
		LANAccess:                false,
		OpenAIUpstream:           "https://api.deepseek.com",
		AnthropicUpstream:        "https://api.deepseek.com/anthropic",
		VerboseLogging:           true,
		Language:                 "",
		ThinkingMode:             "enabled",
		ReasoningEffort:          "max",
		SystemPromptPlacement:    "first",
		ExtraPromptPlacement:     "last",
		ExtraPrompt:              "# 在你的思考过程（<think>标签内）中，请遵守以下规则：\n- **方向明确时**：果断执行，不反复推翻、不过度谨慎。选定路径后直接推进，除非遇到严重阻碍因素（例如硬性报错，逻辑矛盾，用户明确否定）\n- **方向不明时**：将自我修正循环限制在 2 次尝试以内。如果仍然受阻，立即停止自我推演，直接向用户提问澄清。获取关键信息后再继续，不要自行猜测或罗列平行方案。\n- **核心原则**：思考服务于推进，而非风险防御。用决策代替犹豫，用沟通代替假设。\n- **禁止在思考过程中输出完整 file/函数代码** 如需示意，使用 `// ... 省略 ...` 或 diff 摘要，简短的行内代码引用\n- 进行代码任务时，实际的代码变更必须通过提供的工具执行。不要提前在思考或回答中输出完整的新版代码。\n- 思考的目标是确认逻辑正确，而非展示最终结果。",
		AntiLoopRetryModel:       "deepseek-v4-flash",
		AntiLoopRetryThinking:    "",
		AntiLoopRetryEffort:      "high",
		AntiLoopCheckTokens:      0,
		AutoReasoningContent:     true,
		AutoFixEmptyContent:      false,
		AnalysisEnabled:          true,
		AnalysisPersistence:      true,
		AnalysisPersistRawBodies: true,
		AnalysisRetentionDays:    7,
		AntiHallucinationEnabled: false,
		AntiHallucinationPrompt:  "\n\n[意图校准] 用户现在关心的是：{{latest_user_message}}\n\n",
	}
}

func configPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "config.json")
}

// detectSystemLanguage 基于系统环境和 Windows API 自动判断首选语言。
// 仅在首次启动（Language 为空）时使用，后续尊重用户在设置中的选择。
func detectSystemLanguage() string {
	// 优先检查常见环境变量（Unix/macOS/Windows 均可能设置）
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if v := os.Getenv(k); v != "" {
			v = strings.ToLower(strings.Split(v, ".")[0])
			if strings.HasPrefix(v, "zh") {
				return "zh"
			}
			if strings.HasPrefix(v, "en") {
				return "en"
			}
		}
	}

	// Windows 下使用 kernel32.GetUserDefaultUILanguage（与 gui_webview 风格一致，无额外依赖）
	if runtime.GOOS == "windows" {
		kernel32 := syscall.NewLazyDLL("kernel32.dll")
		proc := kernel32.NewProc("GetUserDefaultUILanguage")
		ret, _, _ := proc.Call()
		langID := uint16(ret)
		// Primary language ID 位于低 10 位
		primary := langID & 0x3ff
		switch primary {
		case 0x0004: // LANG_CHINESE
			return "zh"
		case 0x0009: // LANG_ENGLISH
			return "en"
		}
	}

	// 本项目主要面向中文用户，默认中文
	return "zh"
}

func LoadConfig() (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			// 首次启动：自动检测并持久化
			cfg.Language = detectSystemLanguage()
			_ = SaveConfig(cfg)
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), err
	}
	if cfg.Language == "" {
		cfg.Language = detectSystemLanguage()
		_ = SaveConfig(cfg)
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

type SafeConfig struct {
	mu  sync.RWMutex
	cfg Config
}

func NewSafeConfig(cfg Config) *SafeConfig {
	return &SafeConfig{cfg: cfg}
}

func (sc *SafeConfig) Get() Config {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.cfg
}

func (sc *SafeConfig) Update(fn func(*Config)) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	fn(&sc.cfg)
}
