package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

//go:embed web/index.html
var indexHTML []byte

// All state passed via parameters; no package-level globals needed.

func handleGUI(w http.ResponseWriter, r *http.Request, l *Logger, cfg *Config) {
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api")

	switch {
	case path == "/status":
		handleAPIStatus(w, r, l, cfg)
	case path == "/logs" && r.Method == "DELETE":
		l.Clear()
		json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
	case path == "/logs":
		handleAPILogs(w, r, l)
	case strings.HasPrefix(path, "/logs/"):
		handleAPILogDetail(w, r, l, path)
	case path == "/config" && r.Method == "GET":
		handleAPIGetConfig(w, r, cfg)
	case path == "/config" && r.Method == "POST":
		handleAPISaveConfig(w, r, cfg)
	default:
		http.NotFound(w, r)
	}
}

func handleAPIStatus(w http.ResponseWriter, r *http.Request, l *Logger, cfg *Config) {
	stats := l.Stats()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"port":     cfg.Port,
		"total":    stats["total"],
		"today":    stats["today"],
		"max_logs": l.maxSize,
	})
}

func handleAPILogs(w http.ResponseWriter, r *http.Request, l *Logger) {
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50
	offset := 0
	if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
		offset = v
	}

	entries := l.GetAll(limit, offset)
	if entries == nil {
		entries = []LogEntry{}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
		"total":   l.Stats()["total"],
	})
}

func handleAPILogDetail(w http.ResponseWriter, r *http.Request, l *Logger, path string) {
	idStr := strings.TrimPrefix(path, "/logs/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	entry := l.Get(id)
	if entry == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(entry)
}

func handleAPIGetConfig(w http.ResponseWriter, r *http.Request, cfg *Config) {
	cfgCopy := *cfg
	cfgCopy.APIKey = maskAPIKey(cfgCopy.APIKey)
	json.NewEncoder(w).Encode(cfgCopy)
}

func handleAPISaveConfig(w http.ResponseWriter, r *http.Request, cfg *Config) {
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	changed := false

	// API key: special handling for masked value comparison.
	if v, ok := updates["api_key"]; ok {
		if s, ok := v.(string); ok && s != "" && s != maskAPIKey(cfg.APIKey) {
			cfg.APIKey = s
			changed = true
		}
	}
	// Port: must be in valid range.
	if v, ok := updates["port"]; ok {
		if f, ok := v.(float64); ok && f > 0 && f < 65536 {
			cfg.Port = int(f)
			changed = true
		}
	}
	// Upstreams: trim trailing slashes.
	if v, ok := updates["openai_upstream"]; ok {
		if s, ok := v.(string); ok && s != "" {
			cfg.OpenAIUpstream = strings.TrimRight(s, "/")
			changed = true
		}
	}
	if v, ok := updates["anthropic_upstream"]; ok {
		if s, ok := v.(string); ok && s != "" {
			cfg.AnthropicUpstream = strings.TrimRight(s, "/")
			changed = true
		}
	}

	// The remaining fields follow a simple pattern.
	changed = setStringField(updates, "thinking_mode", &cfg.ThinkingMode) || changed
	changed = setStringField(updates, "reasoning_effort", &cfg.ReasoningEffort) || changed
	changed = setStringField(updates, "system_prompt_placement", &cfg.SystemPromptPlacement) || changed
	changed = setStringField(updates, "extra_prompt", &cfg.ExtraPrompt) || changed
	changed = setStringField(updates, "extra_prompt_placement", &cfg.ExtraPromptPlacement) || changed
	changed = setStringField(updates, "max_tokens_mode", &cfg.MaxTokensMode) || changed
	changed = setStringField(updates, "antiloop_retry_model", &cfg.AntiLoopRetryModel) || changed
	changed = setStringField(updates, "antiloop_retry_thinking", &cfg.AntiLoopRetryThinking) || changed
	changed = setStringField(updates, "antiloop_retry_effort", &cfg.AntiLoopRetryEffort) || changed
	changed = setBoolField(updates, "verbose_logging", &cfg.VerboseLogging) || changed
	changed = setBoolField(updates, "auto_open_gui", &cfg.AutoOpenGUI) || changed
	changed = setBoolField(updates, "anti_loop_enabled", &cfg.AntiLoopEnabled) || changed
	changed = setBoolField(updates, "debug_mode", &cfg.DebugMode) || changed
	changed = setBoolField(updates, "auto_reasoning_content", &cfg.AutoReasoningContent) || changed
	changed = setIntField(updates, "max_tokens_custom", &cfg.MaxTokensCustom, 0) || changed
	changed = setIntField(updates, "antiloop_check_tokens", &cfg.AntiLoopCheckTokens, 0) || changed

	if changed {
		if err := SaveConfig(*cfg); err != nil {
			http.Error(w, `{"error":"failed to save config"}`, http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
	} else {
		json.NewEncoder(w).Encode(map[string]string{"status": "unchanged"})
	}
}

// ── Config field update helpers ────────────────────────────────────────────

func setStringField(updates map[string]interface{}, key string, dest *string) bool {
	v, ok := updates[key]
	if !ok {
		return false
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	*dest = s
	return true
}

func setBoolField(updates map[string]interface{}, key string, dest *bool) bool {
	v, ok := updates[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		return false
	}
	*dest = b
	return true
}

func setIntField(updates map[string]interface{}, key string, dest *int, min int) bool {
	v, ok := updates[key]
	if !ok {
		return false
	}
	f, ok := v.(float64) // JSON numbers unmarshal as float64
	if !ok {
		return false
	}
	val := int(f)
	if val < min {
		return false
	}
	*dest = val
	return true
}

func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return key
	}
	return key[:5] + strings.Repeat("*", len(key)-8) + key[len(key)-3:]
}
