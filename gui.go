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

var (
	currentLogger *Logger
	currentConfig *Config
)

func handleGUI(w http.ResponseWriter, r *http.Request, l *Logger, cfg *Config) {
	currentLogger = l
	currentConfig = cfg

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
		handleAPIStatus(w, r, l)
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

func handleAPIStatus(w http.ResponseWriter, r *http.Request, l *Logger) {
	stats := l.Stats()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"port":     currentConfig.Port,
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
	if v, ok := updates["api_key"]; ok {
		if s, ok := v.(string); ok && s != "" && s != maskAPIKey(cfg.APIKey) {
			cfg.APIKey = s
			changed = true
		}
	}
	if v, ok := updates["port"]; ok {
		if f, ok := v.(float64); ok && f > 0 && f < 65536 {
			cfg.Port = int(f)
			changed = true
		}
	}
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
	if v, ok := updates["verbose_logging"]; ok {
		if b, ok := v.(bool); ok {
			cfg.VerboseLogging = b
			changed = true
		}
	}
	if v, ok := updates["auto_open_gui"]; ok {
		if b, ok := v.(bool); ok {
			cfg.AutoOpenGUI = b
			changed = true
		}
	}
	if v, ok := updates["thinking_mode"]; ok {
		if s, ok := v.(string); ok {
			cfg.ThinkingMode = s
			changed = true
		}
	}
	if v, ok := updates["reasoning_effort"]; ok {
		if s, ok := v.(string); ok {
			cfg.ReasoningEffort = s
			changed = true
		}
	}

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

func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return key
	}
	return key[:5] + strings.Repeat("*", len(key)-8) + key[len(key)-3:]
}
