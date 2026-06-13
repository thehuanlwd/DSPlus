package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

//go:embed web
var webFS embed.FS

// All state passed via parameters; no package-level globals needed.
func handleGUI(w http.ResponseWriter, r *http.Request, l *Logger, cfg *SafeConfig, svc *AnalysisService) {
	if r.URL.Path == "/" || r.URL.Path == "/index_v2.html" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		data, err := webFS.ReadFile("web/index_v2.html")
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.Write(data)
		return
	}
	if r.URL.Path == "/index.html" {
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
		return
	}

	// 静态资源分发
	if strings.HasSuffix(r.URL.Path, ".css") || strings.HasSuffix(r.URL.Path, ".js") || strings.HasSuffix(r.URL.Path, ".png") || strings.HasSuffix(r.URL.Path, ".svg") {
		filePath := "web" + r.URL.Path
		data, err := webFS.ReadFile(filePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		switch {
		case strings.HasSuffix(filePath, ".css"):
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case strings.HasSuffix(filePath, ".js"):
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		case strings.HasSuffix(filePath, ".png"):
			w.Header().Set("Content-Type", "image/png")
		case strings.HasSuffix(filePath, ".svg"):
			w.Header().Set("Content-Type", "image/svg+xml")
		}
		w.Write(data)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api")

	switch {
	case path == "/status":
		handleAPIStatus(w, r, l, cfg)
	case path == "/analysis/status":
		handleAPIAnalysisStatus(w, r, cfg)
	case path == "/analysis/sessions" && r.Method == "GET":
		handleAPIAnalysisSessions(w, r, svc)
	case path == "/analysis/sessions" && r.Method == "DELETE":
		handleAPIClearAnalysisHistory(w, r, svc)
	case strings.HasPrefix(path, "/analysis/sessions/") && strings.HasSuffix(path, "/export.md"):
		handleAPIAnalysisExport(w, r, path, svc)
	case strings.HasPrefix(path, "/analysis/sessions/") && strings.HasSuffix(path, "/timeline"):
		handleAPIAnalysisTimeline(w, r, path, svc)
	case strings.HasPrefix(path, "/analysis/sessions/") && strings.HasSuffix(path, "/content"):
		handleAPIAnalysisContent(w, r, path, svc)
	case strings.HasPrefix(path, "/analysis/sessions/"):
		handleAPIAnalysisSessionDetail(w, r, path, svc)
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
	case path == "/restart" && r.Method == "POST":
		handleAPIRestart(w, r)
	default:
		http.NotFound(w, r)
	}
}

func handleAPIStatus(w http.ResponseWriter, r *http.Request, l *Logger, cfg *SafeConfig) {
	c := cfg.Get()
	stats := l.Stats()
	reasons := []string{}
	restartRequired := false
	if c.Port != runtimePort {
		restartRequired = true
		reasons = append(reasons, "服务端口")
	}
	if c.LANAccess != runtimeLANAccess {
		restartRequired = true
		reasons = append(reasons, "局域网/WSL 访问")
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"port":             c.Port,
		"total":            stats["total"],
		"today":            stats["today"],
		"max_logs":         l.maxSize,
		"restart_required": restartRequired,
		"restart_reasons":  reasons,
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

func handleAPIGetConfig(w http.ResponseWriter, r *http.Request, cfg *SafeConfig) {
	cfgCopy := cfg.Get()
	cfgCopy.APIKey = maskAPIKey(cfgCopy.APIKey)
	json.NewEncoder(w).Encode(cfgCopy)
}

func handleAPISaveConfig(w http.ResponseWriter, r *http.Request, cfg *SafeConfig) {
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	var reasons []string
	var restartRequired bool
	var err error
	changed := false

	cfg.Update(func(c *Config) {
		// API key: special handling for masked value comparison.
		if v, ok := updates["api_key"]; ok {
			if s, ok := v.(string); ok && s != "" && s != maskAPIKey(c.APIKey) {
				c.APIKey = s
				changed = true
			}
		}
		// Port: must be in valid range.
		if v, ok := updates["port"]; ok {
			if f, ok := v.(float64); ok && f > 0 && f < 65536 {
				if c.Port != int(f) {
					c.Port = int(f)
					changed = true
				}
			}
		}
		// Upstreams: trim trailing slashes.
		if v, ok := updates["openai_upstream"]; ok {
			if s, ok := v.(string); ok && s != "" {
				trimmed := strings.TrimRight(s, "/")
				if c.OpenAIUpstream != trimmed {
					c.OpenAIUpstream = trimmed
					changed = true
				}
			}
		}
		if v, ok := updates["anthropic_upstream"]; ok {
			if s, ok := v.(string); ok && s != "" {
				trimmed := strings.TrimRight(s, "/")
				if c.AnthropicUpstream != trimmed {
					c.AnthropicUpstream = trimmed
					changed = true
				}
			}
		}

		// The remaining fields follow a simple pattern.
		changed = setStringField(updates, "thinking_mode", &c.ThinkingMode) || changed
		changed = setStringField(updates, "reasoning_effort", &c.ReasoningEffort) || changed
		changed = setStringField(updates, "system_prompt_placement", &c.SystemPromptPlacement) || changed
		changed = setStringField(updates, "extra_prompt", &c.ExtraPrompt) || changed
		changed = setStringField(updates, "extra_prompt_placement", &c.ExtraPromptPlacement) || changed
		changed = setStringField(updates, "max_tokens_mode", &c.MaxTokensMode) || changed
		changed = setStringField(updates, "antiloop_retry_model", &c.AntiLoopRetryModel) || changed
		changed = setStringField(updates, "antiloop_retry_thinking", &c.AntiLoopRetryThinking) || changed
		changed = setStringField(updates, "antiloop_retry_effort", &c.AntiLoopRetryEffort) || changed
		changed = setBoolField(updates, "verbose_logging", &c.VerboseLogging) || changed
		changed = setBoolField(updates, "auto_open_gui", &c.AutoOpenGUI) || changed
		changed = setBoolField(updates, "anti_loop_enabled", &c.AntiLoopEnabled) || changed
		changed = setBoolField(updates, "debug_mode", &c.DebugMode) || changed
		changed = setBoolField(updates, "auto_reasoning_content", &c.AutoReasoningContent) || changed
		changed = setBoolField(updates, "analysis_enabled", &c.AnalysisEnabled) || changed
		changed = setBoolField(updates, "lan_access", &c.LANAccess) || changed
		changed = setIntField(updates, "max_tokens_custom", &c.MaxTokensCustom, 0) || changed
		changed = setIntField(updates, "antiloop_check_tokens", &c.AntiLoopCheckTokens, 0) || changed
		changed = setIntField(updates, "analysis_retention_days", &c.AnalysisRetentionDays, 1) || changed
		if c.AnalysisPersistence != c.AnalysisEnabled {
			c.AnalysisPersistence = c.AnalysisEnabled
			changed = true
		}
		if c.AnalysisPersistRawBodies != c.AnalysisEnabled {
			c.AnalysisPersistRawBodies = c.AnalysisEnabled
			changed = true
		}

		if changed {
			if err = SaveConfig(*c); err != nil {
				return
			}
			if c.Port != runtimePort {
				restartRequired = true
				reasons = append(reasons, "服务端口")
			}
			if c.LANAccess != runtimeLANAccess {
				restartRequired = true
				reasons = append(reasons, "局域网/WSL 访问")
			}
		}
	})

	if err != nil {
		http.Error(w, `{"error":"failed to save config"}`, http.StatusInternalServerError)
		return
	}

	if changed {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":           "saved",
			"restart_required": restartRequired,
			"restart_reasons":  reasons,
		})
	} else {
		json.NewEncoder(w).Encode(map[string]string{"status": "unchanged"})
	}
}

func handleAPIRestart(w http.ResponseWriter, r *http.Request) {
	if appRestartCh != nil {
		select {
		case appRestartCh <- struct{}{}:
		default:
		}
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "restarting"})
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

func handleAPIAnalysisStatus(w http.ResponseWriter, r *http.Request, cfg *SafeConfig) {
	c := cfg.Get()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"analysis_enabled":        c.AnalysisEnabled,
		"analysis_persistence":    c.AnalysisPersistence,
		"analysis_persist_raw":    c.AnalysisPersistRawBodies,
		"analysis_retention_days": c.AnalysisRetentionDays,
	})
}

func handleAPIAnalysisSessions(w http.ResponseWriter, r *http.Request, svc *AnalysisService) {
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50
	offset := 0
	if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
		limit = v
	}
	if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
		offset = v
	}

	if svc == nil {
		json.NewEncoder(w).Encode([]SessionSummary{})
		return
	}

	summaries := svc.GetSessionSummaries(limit, offset)
	if summaries == nil {
		summaries = []SessionSummary{}
	}
	json.NewEncoder(w).Encode(summaries)
}

func handleAPIAnalysisSessionDetail(w http.ResponseWriter, r *http.Request, path string, svc *AnalysisService) {
	id := strings.TrimPrefix(path, "/analysis/sessions/")
	if svc == nil {
		http.Error(w, `{"error":"analysis service not initialized"}`, http.StatusInternalServerError)
		return
	}

	sess := svc.GetSessionDetails(id)
	if sess == nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(sess)
}

func handleAPIAnalysisTimeline(w http.ResponseWriter, r *http.Request, path string, svc *AnalysisService) {
	id := strings.TrimPrefix(path, "/analysis/sessions/")
	id = strings.TrimSuffix(id, "/timeline")
	if svc == nil {
		http.Error(w, `{"error":"analysis service not initialized"}`, http.StatusInternalServerError)
		return
	}

	limit := 100
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}

	page, err := svc.GetTimelinePage(id, offset, limit)
	if err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(page)
}

func handleAPIAnalysisContent(w http.ResponseWriter, r *http.Request, path string, svc *AnalysisService) {
	id := strings.TrimPrefix(path, "/analysis/sessions/")
	id = strings.TrimSuffix(id, "/content")
	_ = id
	if svc == nil {
		http.Error(w, `{"error":"analysis service not initialized"}`, http.StatusInternalServerError)
		return
	}

	ref := ContentRef{
		Kind: r.URL.Query().Get("kind"),
		Hash: r.URL.Query().Get("hash"),
		Path: r.URL.Query().Get("path"),
	}
	if ref.Kind == "" || ref.Hash == "" || ref.Path == "" {
		http.Error(w, `{"error":"missing content ref"}`, http.StatusBadRequest)
		return
	}
	text, err := svc.ResolveContent(ref)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"content": text})
}

func handleAPIAnalysisExport(w http.ResponseWriter, r *http.Request, path string, svc *AnalysisService) {
	id := strings.TrimPrefix(path, "/analysis/sessions/")
	id = strings.TrimSuffix(id, "/export.md")

	if svc == nil {
		http.Error(w, "analysis service not initialized", http.StatusInternalServerError)
		return
	}

	md, err := svc.ExportMarkdown(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="dsplus_session_%s.md"`, id))
	w.Write([]byte(md))
}

func handleAPIClearAnalysisHistory(w http.ResponseWriter, r *http.Request, svc *AnalysisService) {
	if svc == nil {
		http.Error(w, `{"error":"analysis service not initialized"}`, http.StatusInternalServerError)
		return
	}

	if err := svc.ClearHistory(); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
}
