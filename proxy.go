package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ProxyServer struct {
	config *Config
	logger *Logger
	client *http.Client
}

func NewProxyServer(cfg *Config, l *Logger) *ProxyServer {
	return &ProxyServer{
		config: cfg,
		logger: l,
		client: &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
	}
}

func (s *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/" || r.URL.Path == "/index.html" {
		handleGUI(w, r, s.logger, s.config)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/ws") {
		handleWebSocket(w, r, s.logger)
		return
	}

	s.handleProxy(w, r)
}

func (s *ProxyServer) handleProxy(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	originalBody := string(body)

	format := detectFormat(originalBody)
	upstream := s.selectUpstream(format)

	var transformed bool
	var hasSystemPrompt bool
	var transformedBody []byte

	if format == "openai" {
		hasSystemPrompt = strings.Contains(originalBody, `"role":"system"`) ||
			strings.Contains(originalBody, `"role": "system"`)
		if hasSystemPrompt {
			var tErr error
			transformed, transformedBody, tErr = transformOpenAI(body, s.config.SystemPromptPlacement, s.config.ExtraPrompt)
			if tErr != nil {
				log.Printf("[transform] openai error: %v", tErr)
				transformedBody = body
				transformed = false
			}
		}
	} else if format == "anthropic" {
		hasSystemPrompt = strings.Contains(originalBody, `"system":`)
		if hasSystemPrompt {
			var tErr error
			transformed, transformedBody, tErr = transformAnthropic(body, s.config.SystemPromptPlacement, s.config.ExtraPrompt)
			if tErr != nil {
				log.Printf("[transform] anthropic error: %v", tErr)
				transformedBody = body
				transformed = false
			}
		}
	} else {
		transformed = false
		hasSystemPrompt = false
		transformedBody = body
	}

	if !transformed {
		transformedBody = body
	}

	if format == "openai" || format == "anthropic" {
		transformedBody = s.injectThinkingParams(transformedBody, format)
	}

	upstreamURL := upstream + r.URL.Path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(transformedBody))
	if err != nil {
		http.Error(w, "Failed to create upstream request", http.StatusInternalServerError)
		return
	}

	copyHeaders(proxyReq.Header, r.Header)
	proxyReq.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	proxyReq.Header.Del("x-api-key")
	proxyReq.Header.Set("Content-Length", strconv.Itoa(len(transformedBody)))
	proxyReq.Host = ""

	resp, err := s.client.Do(proxyReq)
	if err != nil {
		log.Printf("[proxy] upstream error: %v", err)
		http.Error(w, "Upstream connection failed", http.StatusBadGateway)
		entry := LogEntry{
			Time:            startTime,
			Format:          format,
			Method:          r.Method,
			Path:            r.URL.Path,
			StatusCode:      502,
			LatencyMs:       time.Since(startTime).Milliseconds(),
			Transformed:     transformed,
			HasSystemPrompt: hasSystemPrompt,
		}
		if s.config.VerboseLogging {
			entry.OriginalBody = truncateBody(originalBody)
			entry.TransformedBody = truncateBody(string(transformedBody))
		}
		s.logger.Add(entry)
		return
	}
	defer resp.Body.Close()

	respHeaders := make(map[string]string)
	for k, vv := range resp.Header {
		if len(vv) > 0 {
			respHeaders[k] = strings.Join(vv, ", ")
		}
	}

	latency := time.Since(startTime)

	logID := s.logger.Add(LogEntry{
		Time:             startTime,
		Format:           format,
		Method:           r.Method,
		Path:             r.URL.Path,
		StatusCode:       resp.StatusCode,
		LatencyMs:        latency.Milliseconds(),
		Transformed:      transformed,
		HasSystemPrompt:  hasSystemPrompt,
		ResponseHeaders:  respHeaders,
		OriginalBody:     condStr(s.config.VerboseLogging, truncateBody(originalBody), ""),
		TransformedBody:  condStr(s.config.VerboseLogging, truncateBody(string(transformedBody)), ""),
	})

	isStream := resp.Header.Get("Content-Type") == "text/event-stream" ||
		strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	if isStream {
		s.forwardStream(w, resp, logID)
	} else {
		s.forwardBuffered(w, resp, logID)
	}
}

func (s *ProxyServer) injectThinkingParams(body []byte, format string) []byte {
	mode := s.config.ThinkingMode
	if mode == "" {
		return body
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return body
	}

	injected := false

	if format == "openai" {
		if mode == "disabled" {
			data["thinking"] = map[string]interface{}{"type": "disabled"}
			injected = true
		} else if mode == "enabled" {
			data["thinking"] = map[string]interface{}{"type": "enabled"}
			injected = true
			if s.config.ReasoningEffort != "" {
				data["reasoning_effort"] = s.config.ReasoningEffort
			}
		}
	} else if format == "anthropic" && mode == "enabled" {
		if s.config.ReasoningEffort != "" {
			data["output_config"] = map[string]interface{}{"effort": s.config.ReasoningEffort}
			injected = true
		}
	}

	if injected {
		if b, err := json.Marshal(data); err == nil {
			return b
		}
	}
	return body
}

func (s *ProxyServer) forwardStream(w http.ResponseWriter, resp *http.Response, logID int64) {
	flusher, canFlush := w.(http.Flusher)
	capture := newCaptureBuffer(1048576)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 65536), 1048576)

	var lastUsage *TokenUsage

	for scanner.Scan() {
		line := scanner.Text()
		w.Write([]byte(line + "\n"))
		capture.Write([]byte(line + "\n"))
		if canFlush {
			flusher.Flush()
		}

		if lastUsage == nil && strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data != "[DONE]" {
				var chunk map[string]interface{}
				if err := json.Unmarshal([]byte(data), &chunk); err == nil {
					if u := parseUsageFromMap(chunk); u != nil {
						lastUsage = u
					}
				}
			}
		}
	}

	if lastUsage != nil {
		s.logger.UpdateTokenUsage(logID, lastUsage)
	}
	if s.config.VerboseLogging {
		s.logger.UpdateLastResponse(logID, capture.String())
	}
}

func (s *ProxyServer) forwardBuffered(w http.ResponseWriter, resp *http.Response, logID int64) {
	var buf bytes.Buffer
	tee := io.TeeReader(resp.Body, &buf)
	io.Copy(w, tee)

	if u := extractUsageFromBody(buf.String()); u != nil {
		s.logger.UpdateTokenUsage(logID, u)
	}
	if s.config.VerboseLogging {
		s.logger.UpdateLastResponse(logID, truncateBody(buf.String()))
	}
}

func (s *ProxyServer) selectUpstream(format string) string {
	if format == "anthropic" {
		return s.config.AnthropicUpstream
	}
	return s.config.OpenAIUpstream
}

func detectFormat(body string) string {
	if !isJSON(body) {
		return "unknown"
	}

	hasMessages := strings.Contains(body, `"messages"`)
	if !hasMessages {
		return "unknown"
	}

	hasRoleSystem := strings.Contains(body, `"role":"system"`) ||
		strings.Contains(body, `"role": "system"`)

	hasMaxTokens := strings.Contains(body, `"max_tokens"`)

	if hasMaxTokens && !hasRoleSystem {
		return "anthropic"
	}
	if hasMessages {
		return "openai"
	}
	return "unknown"
}

func isJSON(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) > 0 && s[0] == '{'
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func isHopByHop(key string) bool {
	switch strings.ToLower(key) {
	case "connection", "keep-alive", "proxy-authenticate",
		"proxy-authorization", "te", "trailers", "transfer-encoding":
		return true
	}
	return false
}

func truncateBody(s string) string {
	const maxLen = 524288
	if len(s) > maxLen {
		return s[:maxLen] + "\n\n... [truncated]"
	}
	return s
}

func condStr(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func extractUsageFromBody(body string) *TokenUsage {
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil
	}
	return parseUsageFromMap(resp)
}

func parseUsageFromMap(data map[string]interface{}) *TokenUsage {
	usageRaw, ok := data["usage"]
	if !ok {
		return nil
	}
	usage, ok := usageRaw.(map[string]interface{})
	if !ok {
		return nil
	}

	u := &TokenUsage{}

	if v, ok := usage["total_tokens"]; ok {
		u.Total = toInt64(v)
	}
	if v, ok := usage["prompt_tokens"]; ok {
		u.Prompt = toInt64(v)
	}
	if v, ok := usage["completion_tokens"]; ok {
		u.Completion = toInt64(v)
	}
	if v, ok := usage["prompt_cache_hit_tokens"]; ok {
		u.CacheHit = toInt64(v)
	}
	if v, ok := usage["prompt_cache_miss_tokens"]; ok {
		u.CacheMiss = toInt64(v)
	}

	if v, ok := usage["input_tokens"]; ok {
		u.Prompt = toInt64(v)
		u.CacheMiss = u.Prompt
	}
	if v, ok := usage["output_tokens"]; ok {
		u.Completion = toInt64(v)
	}
	if v, ok := usage["cache_read_input_tokens"]; ok {
		u.CacheHit = toInt64(v)
		u.Prompt += u.CacheHit
	}
	if v, ok := usage["cache_creation_input_tokens"]; ok {
		u.CacheMiss += toInt64(v)
	}

	if u.Total == 0 && (u.Prompt > 0 || u.Completion > 0) {
		u.Total = u.Prompt + u.Completion
	}

	return u
}

func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

type captureBuffer struct {
	buf bytes.Buffer
	cap int
}

func newCaptureBuffer(cap int) *captureBuffer {
	return &captureBuffer{cap: cap}
}

func (c *captureBuffer) Write(p []byte) (int, error) {
	remaining := c.cap - c.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		c.buf.Write(p[:remaining])
	} else {
		c.buf.Write(p)
	}
	return len(p), nil
}

func (c *captureBuffer) String() string {
	s := c.buf.String()
	if c.buf.Len() >= c.cap {
		s += "\n\n... [stream truncated]"
	}
	return s
}
