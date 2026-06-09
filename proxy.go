package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ── Tuning constants ─────────────────────────────────────────────────────────

const (
	// HTTP transport.
	dialTimeout          = 30 * time.Second
	keepAlive            = 30 * time.Second
	maxIdleConns         = 100
	idleConnTimeout      = 90 * time.Second
	tlsHandshakeTimeout  = 10 * time.Second
	expectContinueTimeout = 1 * time.Second

	// Streaming / buffering.
	scannerInitialBuf = 65536   // 64 KB
	scannerMaxBuf     = 1048576 // 1 MB
	captureBufCap     = 1048576 // 1 MB

	// Logging.
	truncateBodyMaxLen = 524288 // 512 KB
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
			Timeout: 0, // no timeout (streaming)
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   dialTimeout,
					KeepAlive: keepAlive,
				}).DialContext,
				MaxIdleConns:          maxIdleConns,
				IdleConnTimeout:       idleConnTimeout,
				TLSHandshakeTimeout:   tlsHandshakeTimeout,
				ExpectContinueTimeout: expectContinueTimeout,
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

	// Parse the request body once; all subsequent modifications work
	// on the parsed map in-place to avoid redundant marshal/unmarshal.
	var data map[string]interface{}
	if json.Unmarshal(body, &data) != nil {
		// Not valid JSON — pass through as-is
		data = nil
	}

	if format == "openai" {
		hasSystemPrompt = strings.Contains(originalBody, `"role":"system"`) ||
			strings.Contains(originalBody, `"role": "system"`)
		if data != nil && (hasSystemPrompt || (s.config.ExtraPrompt != "" && s.config.ExtraPromptPlacement != "none")) {
			transformed = transformOpenAIInPlace(data, s.config.SystemPromptPlacement, s.config.ExtraPrompt, s.config.ExtraPromptPlacement)
		}
	} else if format == "anthropic" {
		hasSystemPrompt = strings.Contains(originalBody, `"system":`)
		if data != nil && (hasSystemPrompt || (s.config.ExtraPrompt != "" && s.config.ExtraPromptPlacement != "none")) {
			transformed = transformAnthropicInPlace(data, s.config.SystemPromptPlacement, s.config.ExtraPrompt, s.config.ExtraPromptPlacement)
		}
	}

	// Apply thinking / max_tokens injections on the already-parsed map.
	if data != nil && (format == "openai" || format == "anthropic") {
		s.injectThinkingParams(data, format)
		s.injectMaxTokens(data)
	}

	if data != nil && s.config.AutoReasoningContent {
		injectReasoningContent(data)
	}

	if data != nil {
		if b, err := json.Marshal(data); err == nil {
			transformedBody = b
		} else {
			transformedBody = body
			transformed = false
		}
	} else {
		transformedBody = body
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

	isStream := resp.Header.Get("Content-Type") == "text/event-stream" ||
		strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	// ── Anti-Loop detection ──
	if s.config.AntiLoopEnabled {
		if isStream {
			// Streaming: forward chunks immediately, retry if truncated
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
			copyHeaders(w.Header(), resp.Header)
			w.WriteHeader(resp.StatusCode)
			s.forwardStreamWithAntiLoop(w, resp, logID, transformedBody, format)
			return
		}

		// Non-streaming: buffer and retry (no streaming UX issue)
		bodyBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			log.Printf("[proxy] read upstream body: %v", readErr)
			http.Error(w, "Failed to read upstream response", http.StatusBadGateway)
			return
		}

		fr := detectBufferFinishReason(bodyBytes)
		if fr == "length" || fr == "max_tokens" {
			log.Printf("[antiloop] non-stream finish_reason=%s, triggering retry", fr)
			bodyBytes = s.handleAntiLoop(transformedBody, format, "")
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

		if u := extractUsageFromBody(string(bodyBytes)); u != nil {
			s.logger.UpdateTokenUsage(logID, u)
		}
		if s.config.VerboseLogging {
			s.logger.UpdateLastResponse(logID, truncateBody(string(bodyBytes)))
		}

		copyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		w.Write(bodyBytes)
		return
	}

	// ── Normal path (anti-loop disabled): stream/buffer to client immediately ──
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

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	if isStream {
		s.forwardStream(w, resp, logID)
	} else {
		s.forwardBuffered(w, resp, logID)
	}
}

func (s *ProxyServer) injectThinkingParams(data map[string]interface{}, format string) {
	mode := s.config.ThinkingMode
	if mode == "" {
		return
	}

	if format == "openai" {
		if mode == "disabled" {
			data["thinking"] = map[string]interface{}{"type": "disabled"}
		} else if mode == "enabled" {
			data["thinking"] = map[string]interface{}{"type": "enabled"}
			if s.config.ReasoningEffort != "" {
				data["reasoning_effort"] = s.config.ReasoningEffort
			}
		}
	} else if format == "anthropic" && mode == "enabled" {
		if s.config.ReasoningEffort != "" {
			data["output_config"] = map[string]interface{}{"effort": s.config.ReasoningEffort}
		}
	}
}

func (s *ProxyServer) injectMaxTokens(data map[string]interface{}) {
	mode := s.config.MaxTokensMode
	if mode == "" {
		return
	}

	var maxTokens int
	switch mode {
	case "5000":
		maxTokens = 5000
	case "32000":
		maxTokens = 32000
	case "custom":
		maxTokens = s.config.MaxTokensCustom
	default:
		return
	}

	if maxTokens <= 0 {
		return
	}

	data["max_tokens"] = maxTokens
}

func injectReasoningContent(data map[string]interface{}) {
	messages, ok := data["messages"].([]interface{})
	if !ok {
		return
	}
	for _, msg := range messages {
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role != "assistant" {
			continue
		}
		if _, hasToolCalls := m["tool_calls"]; !hasToolCalls {
			continue
		}
		if _, hasReasoning := m["reasoning_content"]; hasReasoning {
			continue
		}
		m["reasoning_content"] = ""
	}
}

func (s *ProxyServer) forwardStream(w http.ResponseWriter, resp *http.Response, logID int64) {
	flusher, canFlush := w.(http.Flusher)
	capture := newCaptureBuffer(captureBufCap)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, scannerInitialBuf), scannerMaxBuf)

	var lastUsage *TokenUsage

	for scanner.Scan() {
		line := scanner.Text()
		line = replaceDSMLMarkers(line)
		if _, err := w.Write([]byte(line + "\n")); err != nil {
			log.Printf("[proxy] forwardStream write error: %v", err)
			break
		}
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

	if err := scanner.Err(); err != nil {
		log.Printf("[proxy] forwardStream scanner error: %v", err)
	}

	if lastUsage != nil {
		s.logger.UpdateTokenUsage(logID, lastUsage)
	}
	if s.config.VerboseLogging {
		s.logger.UpdateLastResponse(logID, capture.String())
	}
}

func (s *ProxyServer) forwardBuffered(w http.ResponseWriter, resp *http.Response, logID int64) {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[proxy] forwardBuffered read error: %v", err)
		return
	}
	bodyBytes = replaceDSMLMarkersBytes(bodyBytes)
	w.Write(bodyBytes)

	if u := extractUsageFromBody(string(bodyBytes)); u != nil {
		s.logger.UpdateTokenUsage(logID, u)
	}
	if s.config.VerboseLogging {
		s.logger.UpdateLastResponse(logID, truncateBody(string(bodyBytes)))
	}
}

// forwardStreamWithAntiLoop streams the first response in real-time to the client.
// If finish_reason=length is detected, it pauses the stream completion (no [DONE]),
// runs the anti-loop analyzer, then streams the retry response seamlessly.
func (s *ProxyServer) forwardStreamWithAntiLoop(
	w http.ResponseWriter,
	resp *http.Response,
	logID int64,
	transformedBody []byte,
	format string,
) {
	flusher, canFlush := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, scannerInitialBuf), scannerMaxBuf)

	var finishReason string
	var reasoningBuilder strings.Builder
	var contentBuilder strings.Builder
	var lastUsage *TokenUsage
	var streamID, streamModel string
	var streamCreated float64
	capture := newCaptureBuffer(captureBufCap)

	reqID := nextTraceReqID()
	traceKeyvals("event", "phase1_start", "req_id", reqID, "threshold", s.config.AntiLoopCheckTokens,
		"retry_model", s.config.AntiLoopRetryModel, "format", format)

	// ── Proactive parallel analysis state ──
	var completionTokens int              // cumulative from usage chunks
	var analyzeDone chan analyzeResult    // goroutine result channel
	var analyzeTriggered bool             // prevent duplicate triggers
	var earlyStop bool                    // set when parallel analyzer says stop
	var earlyStopAnalysis *AntiLoopAnalysis
	var debugLogID int64 // debug mode: log entry ID for real-time token updates
	chunkCount := 0
	lastTracedTokens := 0
	var watchdogStop chan<- struct{}     // progress watchdog
	var scannerExitedNormally bool       // set to true when scanner.Scan() returns false

	log.Printf("[antiloop] Phase 1 start: streaming first response in real-time, reqID=%d", reqID)

	// ── Debug mode: create a dedicated log entry for real-time token tracking ──
	if s.config.DebugMode {
		debugLogID = s.logger.Add(LogEntry{
			Time:        time.Now(),
			Format:      "debug",
			RequestType: "debug",
			Method:      "TRACE",
			Path:        "/antiloop/tokens",
			StatusCode:  s.config.AntiLoopCheckTokens, // misuse: shows threshold
		})
		log.Printf("[antiloop] debug entry created id=%d for reqID=%d", debugLogID, reqID)
	}

	// ── Phase 1: Stream + monitor tokens + parallel analysis ──
	watchdogStop = startProgressWatchdog(fmt.Sprintf("Phase1(reqID=%d)", reqID), 60*time.Second)
	for scanner.Scan() {
		watchdogStop = resetProgressWatchdog(watchdogStop)

		line := scanner.Text()
		line = replaceDSMLMarkers(line)

		if !strings.HasPrefix(line, "data: ") {
			if _, err := w.Write([]byte(line + "\n")); err != nil {
				log.Printf("[antiloop] reqID=%d write error (non-data): %v", reqID, err)
				traceKeyvals("event", "write_error", "req_id", reqID, "error", err.Error())
				scannerExitedNormally = false
				break
			}
			capture.Write([]byte(line + "\n"))
			if canFlush { flusher.Flush() }
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			if _, werr := w.Write([]byte(line + "\n")); werr != nil {
				log.Printf("[antiloop] reqID=%d write error (unmarshal fallback): %v", reqID, werr)
				traceKeyvals("event", "write_error", "req_id", reqID, "context", "unmarshal_fallback", "error", werr.Error())
				break
			}
			capture.Write([]byte(line + "\n"))
			if canFlush { flusher.Flush() }
			continue
		}

		// Track response metadata from first valid chunk
		if streamID == "" {
			if id, _ := chunk["id"].(string); id != "" { streamID = id }
			if m, _ := chunk["model"].(string); m != "" { streamModel = m }
			if c, ok := chunk["created"].(float64); ok { streamCreated = c }
		}

		// ── Track completion_tokens from usage (may be null in most chunks) ──
		if usage, ok := chunk["usage"].(map[string]interface{}); ok {
			if ct, ok := usage["completion_tokens"].(float64); ok {
				completionTokens = int(ct)
			}
		}

		// ── Proactive check: trigger parallel analysis at threshold ──
		// DeepSeek only sends non-null usage in the final chunk, so we also
		// estimate tokens from accumulated character length as a fallback.
		estimatedTokens := (reasoningBuilder.Len() + contentBuilder.Len()) / 4
		effectiveTokens := completionTokens
		if estimatedTokens > effectiveTokens {
			effectiveTokens = estimatedTokens
		}
		chunkCount++

		// Periodic trace: every ~500 estimated tokens or when usage token count appears
		if effectiveTokens > 0 && (effectiveTokens-lastTracedTokens >= 500 || (completionTokens > 0 && lastTracedTokens == 0)) {
			lastTracedTokens = effectiveTokens
			traceKeyvals("event", "chunk", "n", chunkCount, "usage", completionTokens,
				"est", estimatedTokens, "eff", effectiveTokens,
				"reasoning_chars", reasoningBuilder.Len(), "content_chars", contentBuilder.Len())
		}

		if s.config.AntiLoopCheckTokens > 0 && !analyzeTriggered &&
			effectiveTokens >= s.config.AntiLoopCheckTokens {
			// ── Heuristic: zero content + very high reasoning → auto-intervene ──
			if contentBuilder.Len() == 0 && reasoningBuilder.Len() > s.config.AntiLoopCheckTokens*2 {
				tracelog("[antiloop] HEURISTIC: content=0 reasoning=%d chars → forcing intervention",
					reasoningBuilder.Len())
				traceKeyvals("event", "heuristic_force", "reasoning_chars", reasoningBuilder.Len(),
					"content_chars", 0)
				earlyStop = true
				earlyStopAnalysis = &AntiLoopAnalysis{
					Judgment:       "excessive",
					Guidance:       "你已经思考了极长时间但没有输出任何内容。立即停止思考，直接给出最终结论。",
					EnableThinking: false,
				}
				// Log the heuristic as an analyzer-style entry
				s.logger.Add(LogEntry{
					Time:        time.Now(),
					Format:      "openai",
					RequestType: "antiloop_analyzer",
					Method:      "HEURISTIC",
					Path:        "/antiloop/heuristic (启发式判定)",
					StatusCode:  200,
					LatencyMs:   0,
					OriginalBody: condStr(s.config.VerboseLogging,
						fmt.Sprintf("reasoning_chars=%d content_chars=%d threshold=%d",
							reasoningBuilder.Len(), contentBuilder.Len(), s.config.AntiLoopCheckTokens), ""),
					ResponseBody: condStr(s.config.VerboseLogging,
						"judgment=excessive guidance=立即停止思考", ""),
				})
				resp.Body.Close()
				goto PHASE1_DONE
			}

			analyzeTriggered = true
			analyzeDone = make(chan analyzeResult, 1)
			go s.parallelAnalyze(analyzeDone,
				transformedBody, format,
				reasoningBuilder.String(),
				contentBuilder.String(),
			)
			log.Printf("[antiloop] parallel analysis triggered at %d tokens (usage=%d estimated=%d)",
				effectiveTokens, completionTokens, estimatedTokens)
			traceKeyvals("event", "analyzer_launch", "eff", effectiveTokens, "usage", completionTokens, "est", estimatedTokens)
		}

		// ── Debug: push real-time token stats every chunk ──
		if s.config.DebugMode && debugLogID != 0 {
			s.logger.UpdateTokenUsage(debugLogID, &TokenUsage{
				Total:      int64(effectiveTokens),
				Prompt:     int64(completionTokens),
				Completion: int64(estimatedTokens),
				CacheHit:   int64(reasoningBuilder.Len()),
				CacheMiss:  int64(contentBuilder.Len()),
			})
		}

		// ── Non-blocking check: has the parallel analyzer finished? ──
		if analyzeDone != nil {
			select {
			case result := <-analyzeDone:
				if result.needsIntervention() {
					log.Printf("[antiloop] parallel analyzer says STOP (judgment=%s), intervening",
						result.analysis.Judgment)
					traceKeyvals("event", "analyzer_stop", "judgment", result.analysis.Judgment,
						"tokens", effectiveTokens)
					earlyStop = true
					earlyStopAnalysis = result.analysis
					resp.Body.Close() // stop reading upstream
					goto PHASE1_DONE
				} else {
					log.Printf("[antiloop] parallel analyzer says CONTINUE (judgment=%s)",
						result.analysis.Judgment)
					traceKeyvals("event", "analyzer_continue", "judgment", result.analysis.Judgment)
				}
				analyzeDone = nil
			default:
			}
		}

		// Inspect choices: strip finish_reason, accumulate content
		modified := false
		if choices, ok := chunk["choices"].([]interface{}); ok {
			for _, c := range choices {
				if choice, ok := c.(map[string]interface{}); ok {
					if fr, _ := choice["finish_reason"].(string); fr != "" {
						finishReason = fr
						delete(choice, "finish_reason")
						modified = true
					}
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						if rc, _ := delta["reasoning_content"].(string); rc != "" {
							reasoningBuilder.WriteString(rc)
						}
						if ct, _ := delta["content"].(string); ct != "" {
							contentBuilder.WriteString(ct)
						}
					}
				}
			}
		}

		if u := parseUsageFromMap(chunk); u != nil && lastUsage == nil {
			lastUsage = u
		}

		var outLine string
		if modified {
			b, _ := json.Marshal(chunk)
			outLine = "data: " + string(b) + "\n"
		} else {
			outLine = line + "\n"
		}
		if _, err := w.Write([]byte(outLine)); err != nil {
			log.Printf("[antiloop] reqID=%d write error (data): %v", reqID, err)
			traceKeyvals("event", "write_error", "req_id", reqID, "context", "data_write", "error", err.Error())
			break
		}
		capture.Write([]byte(outLine))
		if canFlush { flusher.Flush() }
	}

	scannerExitedNormally = true

PHASE1_DONE:
	// ── Stop watchdog and check scanner status ──
	if watchdogStop != nil {
		close(watchdogStop)
	}
	if scannerExitedNormally {
		if err := scanner.Err(); err != nil {
			log.Printf("[antiloop] reqID=%d scanner error: %v", reqID, err)
			traceKeyvals("event", "scanner_error", "req_id", reqID, "error", err.Error())
		} else {
			log.Printf("[antiloop] reqID=%d scanner finished normally", reqID)
			traceKeyvals("event", "scanner_eof", "req_id", reqID)
		}
	} else {
		log.Printf("[antiloop] reqID=%d scanner exited abnormally (write error or break)", reqID)
		traceKeyvals("event", "scanner_abort", "req_id", reqID)
	}
	log.Printf("[antiloop] reqID=%d Phase 1 done: finish_reason=%s early_stop=%v tokens=%d",
		reqID, finishReason, earlyStop, completionTokens)
	traceKeyvals("event", "phase1_done", "req_id", reqID, "finish_reason", finishReason, "early_stop", earlyStop,
		"usage_tokens", completionTokens, "est_tokens", (reasoningBuilder.Len()+contentBuilder.Len())/4,
		"reasoning_chars", reasoningBuilder.Len(), "content_chars", contentBuilder.Len())

	// ── Phase 2: Retry (early-stop or length fallback) ──
	needsRetry := earlyStop || finishReason == "length"
	if needsRetry {
		reasoningContent := reasoningBuilder.String()
		phase1Content := contentBuilder.String()

		if streamID == "" { streamID = "dsplus-antiloop" }
		if streamModel == "" { streamModel = "deepseek-chat" }
		if streamCreated == 0 { streamCreated = float64(time.Now().Unix()) }

		if _, err := w.Write([]byte("\n")); err != nil {
			log.Printf("[antiloop] reqID=%d write error (retry separator): %v", reqID, err)
			traceKeyvals("event", "write_error", "req_id", reqID, "context", "retry_separator", "error", err.Error())
			goto FINALIZE
		}
		capture.Write([]byte("\n"))

		if earlyStop {
			log.Printf("[antiloop] reqID=%d early-stop indicator, analysis judgment=%s", reqID, earlyStopAnalysis.Judgment)
		} else {
			log.Printf("[antiloop] reqID=%d length-fallback indicator", reqID)
		}
		s.writeAntiloopIndicator(w, flusher, canFlush, capture, streamID, streamModel, streamCreated)

		if _, err := w.Write([]byte("\n")); err != nil {
			log.Printf("[antiloop] reqID=%d write error (retry indicator separator): %v", reqID, err)
			traceKeyvals("event", "write_error", "req_id", reqID, "context", "retry_indicator_sep", "error", err.Error())
			goto FINALIZE
		}
		capture.Write([]byte("\n"))
		if canFlush { flusher.Flush() }

		var retryBody []byte
		if earlyStop && earlyStopAnalysis != nil {
			log.Printf("[antiloop] reqID=%d building early-stop retry (judgment=%s)", reqID, earlyStopAnalysis.Judgment)
			retryBody = s.buildGuidedRetryRequest(transformedBody, format, earlyStopAnalysis, phase1Content, reasoningContent, true)
		} else if finishReason == "length" {
			log.Printf("[antiloop] reqID=%d building length-fallback retry (reasoning=%d bytes, content=%d bytes)", reqID, len(reasoningContent), len(phase1Content))
			var analyzeErr error
			retryBody, analyzeErr = s.runAntiLoopAnalysis(transformedBody, format, phase1Content, reasoningContent)
			if analyzeErr != nil {
				log.Printf("[antiloop] reqID=%d analyzer failed: %v, using simple retry", reqID, analyzeErr)
				retryBody = s.buildSimpleRetryRequest(transformedBody, format, phase1Content, reasoningContent)
			}
		}

		log.Printf("[antiloop] reqID=%d Phase 2: executing retry request (body_bytes=%d)", reqID, len(retryBody))
		traceKeyvals("event", "retry_start", "req_id", reqID, "body_bytes", len(retryBody), "early_stop", earlyStop)
		retryFR, retryUsage := s.executeAndStreamRetry(w, flusher, canFlush, retryBody, format, capture, streamID)
		log.Printf("[antiloop] reqID=%d Phase 2 end: retry finish_reason=%s retry_usage=%v", reqID, retryFR, retryUsage != nil)
		traceKeyvals("event", "retry_end", "req_id", reqID, "finish_reason", retryFR, "has_usage", retryUsage != nil)

		if retryFR == "length" || retryFR == "max_tokens" {
			log.Printf("[antiloop] reqID=%d retry also hit limit, sending hard-limit message", reqID)
			if _, err := w.Write([]byte("\n")); err != nil {
				log.Printf("[antiloop] reqID=%d write error (hard-limit separator): %v", reqID, err)
			}
			capture.Write([]byte("\n"))
			s.streamHardLimitSSE(w, flusher, canFlush, format, capture, streamID, streamModel, streamCreated)
		}
		if retryUsage != nil { lastUsage = retryUsage }
	} else {
		log.Printf("[antiloop] reqID=%d no retry needed (finish_reason=%s)", reqID, finishReason)
		traceKeyvals("event", "no_retry", "req_id", reqID, "finish_reason", finishReason)
		if finishReason != "" {
			if _, err := w.Write([]byte("\n")); err != nil {
				log.Printf("[antiloop] reqID=%d write error (no-retry separator): %v", reqID, err)
				goto FINALIZE
			}
			capture.Write([]byte("\n"))
			finishChunk := map[string]interface{}{
				"id":      streamID,
				"object":  "chat.completion.chunk",
				"created": streamCreated,
				"model":   streamModel,
				"choices": []map[string]interface{}{
					{
						"index":         0,
						"delta":         map[string]interface{}{},
						"finish_reason": finishReason,
					},
				},
			}
			b, _ := json.Marshal(finishChunk)
			line := "data: " + string(b) + "\n"
			if _, err := w.Write([]byte(line)); err != nil {
				log.Printf("[antiloop] reqID=%d write error (no-retry finish chunk): %v", reqID, err)
				goto FINALIZE
			}
			capture.Write([]byte(line))
			if canFlush { flusher.Flush() }
			if _, err := w.Write([]byte("\n")); err != nil {
				log.Printf("[antiloop] reqID=%d write error (no-retry final newline): %v", reqID, err)
				goto FINALIZE
			}
			capture.Write([]byte("\n"))
		}
	}

FINALIZE:
	// ── Final: send the real [DONE] ──
	if _, err := w.Write([]byte("data: [DONE]\n")); err != nil {
		log.Printf("[antiloop] reqID=%d write error ([DONE]): %v", reqID, err)
	}
	capture.Write([]byte("data: [DONE]\n"))
	if canFlush { flusher.Flush() }
	log.Printf("[antiloop] reqID=%d stream complete, [DONE] sent", reqID)
	trace("stream_done req_id=%d", reqID)

	if lastUsage != nil { s.logger.UpdateTokenUsage(logID, lastUsage) }
	if s.config.VerboseLogging { s.logger.UpdateLastResponse(logID, capture.String()) }
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

	// Parse once and inspect structural fields (avoid fragile string matching)
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return "unknown"
	}

	// Must have a messages array
	messages, hasMessages := data["messages"]
	if !hasMessages {
		return "unknown"
	}
	_, messagesIsArray := messages.([]interface{})
	if !messagesIsArray {
		return "unknown"
	}

	// Strong Anthropic signal: top-level "system" field
	if _, hasSystem := data["system"]; hasSystem {
		return "anthropic"
	}

	// Check for system role inside messages
	hasSystemRole := messagesContainRole(messages.([]interface{}), "system")

	// Anthropic format: has max_tokens + no system role in messages
	// (DeepSeek Anthropic endpoint expects max_tokens)
	_, hasMaxTokens := data["max_tokens"]
	if hasMaxTokens && !hasSystemRole {
		return "anthropic"
	}

	// Everything else with messages is OpenAI format
	return "openai"
}

// messagesContainRole checks if any message in the array has the given role.
func messagesContainRole(messages []interface{}, role string) bool {
	for _, mRaw := range messages {
		m, ok := mRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if r, _ := m["role"].(string); r == role {
			return true
		}
	}
	return false
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
	if len(s) > truncateBodyMaxLen {
		return s[:truncateBodyMaxLen] + "\n\n... [truncated]"
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
