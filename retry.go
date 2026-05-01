package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// AntiLoopAnalysis is the result returned by the sub-agent analyzer.
type AntiLoopAnalysis struct {
	Judgment       string `json:"judgment"`        // "loop" | "excessive" | "normal"
	Guidance       string `json:"guidance"`         // guidance text injected into retry
	EnableThinking bool   `json:"enable_thinking"`  // whether retry should enable thinking
}

const antiLoopAnalyzerPrompt = `You are a thinking-process analyzer for an AI assistant. Your task: analyze a truncated thinking process to determine whether it got stuck in a repetitive loop or excessive reasoning.

## Judgment Criteria
- "loop": Clear repetitive pattern detected — the same conclusion was verified ≥3 times, or the same reasoning step was repeated ≥3 times without progress.
- "excessive": Thinking is very long but NOT looping — reasoning makes progress but is unnecessarily verbose or over-cautious.
- "normal": The reasoning appears normal and productive; it was merely cut off by the token limit.

## Response Format
You MUST respond with ONLY a valid JSON object (no markdown fences, no extra text):
{
  "judgment": "loop|excessive|normal",
  "guidance": "Instructions to the model for the retry, in Chinese. For 'loop': point out where the loop occurred and tell it to skip repeated verification. For 'excessive': tell it to be concise and avoid over-analysis. For 'normal': tell it to continue from where it left off.",
  "enable_thinking": true or false
}

## Important
- guidance must be in Chinese, speaking directly to the model.
- For "loop" or "excessive", recommend enable_thinking=false (disable thinking on retry to speed up output).
- For "normal", recommend enable_thinking=true (keep thinking on retry).
- Keep guidance under 200 characters.`

// hardLimitMessages provides the fixed error content when the retry also hits token limit.
func hardLimitMessages(format string) []byte {
	content := "抱歉，当前任务超出了处理上限。请尝试将问题拆分为更小的步骤后重新提问。"
	now := time.Now().Unix()

	if format == "openai" {
		resp := map[string]interface{}{
			"id":      "dsplus-antiloop",
			"object":  "chat.completion",
			"created": now,
			"model":   "deepseek-chat",
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"finish_reason": "stop",
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": content,
					},
				},
			},
			"usage": map[string]interface{}{
				"total_tokens":      0,
				"prompt_tokens":     0,
				"completion_tokens": 0,
			},
		}
		b, _ := json.Marshal(resp)
		return b
	}

	// Anthropic format
	resp := map[string]interface{}{
		"id":         "dsplus-antiloop",
		"type":       "message",
		"role":       "assistant",
		"model":      "deepseek-chat",
		"stop_reason": "end_turn",
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": content,
			},
		},
		"usage": map[string]interface{}{
			"input_tokens":  0,
			"output_tokens": 0,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// detectStreamFinishReason parses the buffered SSE stream to extract
// the finish_reason and accumulated reasoning_content.
func detectStreamFinishReason(buf []byte) (finishReason string, reasoningContent string) {
	lines := strings.Split(string(buf), "\n")
	var reasoningBuilder strings.Builder

	for _, line := range lines {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// Extract finish_reason from choices
		if choices, ok := chunk["choices"].([]interface{}); ok {
			for _, c := range choices {
				if choice, ok := c.(map[string]interface{}); ok {
					if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
						finishReason = fr
					}
					// Accumulate reasoning_content from delta
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						if rc, ok := delta["reasoning_content"].(string); ok {
							reasoningBuilder.WriteString(rc)
						}
					}
				}
			}
		}
	}

	return finishReason, reasoningBuilder.String()
}

// detectBufferFinishReason extracts finish_reason from a non-streaming JSON response.
func detectBufferFinishReason(body []byte) string {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	if choices, ok := resp["choices"].([]interface{}); ok {
		for _, c := range choices {
			if choice, ok := c.(map[string]interface{}); ok {
				if fr, ok := choice["finish_reason"].(string); ok {
					return fr
				}
			}
		}
	}
	// Also check Anthropic stop_reason
	if sr, ok := resp["stop_reason"].(string); ok {
		return sr
	}
	return ""
}

// handleAntiLoop orchestrates the retry flow (non-streaming path).
// It analyzes, builds, executes, and returns the final response body.
func (s *ProxyServer) handleAntiLoop(transformedBody []byte, format string, reasoningContent string) []byte {
	log.Printf("[antiloop] detected finish_reason=length for %s request, starting analysis", format)

	retryBody, err := s.runAntiLoopAnalysis(transformedBody, format, "", reasoningContent)
	if err != nil {
		log.Printf("[antiloop] analyzer failed: %v, falling back to simple retry", err)
		retryBody = s.buildSimpleRetryRequest(transformedBody, format, "", reasoningContent)
	}

	respBody, fr := s.executeRetry(retryBody, format)
	if fr == "length" || fr == "max_tokens" {
		log.Printf("[antiloop] retry also hit limit, giving up")
		return hardLimitMessages(format)
	}

	log.Printf("[antiloop] retry succeeded, finish_reason=%s", fr)
	return respBody
}

// runAntiLoopAnalysis calls the sub-agent analyzer and builds the retry request.
// phase1Content: the assistant's truncated output from Phase 1.
// reasoningContent: the raw reasoning_content from Phase 1 (sent to analyzer AND injected in retry).
// Returns the retry request body ready for execution, or an error if analysis failed.
func (s *ProxyServer) runAntiLoopAnalysis(transformedBody []byte, format string, phase1Content string, reasoningContent string) ([]byte, error) {
	analysis, err := s.callAntiLoopAnalyzer(transformedBody, format, reasoningContent)
	if err != nil {
		return nil, err
	}

	log.Printf("[antiloop] analysis: judgment=%s enable_thinking=%v", analysis.Judgment, analysis.EnableThinking)
	retryBody := s.buildGuidedRetryRequest(transformedBody, format, analysis, phase1Content, reasoningContent)
	return retryBody, nil
}

// writeAntiloopIndicator sends a proper SSE chunk (with full metadata fields)
// to the client so they know the system is re-analyzing before the retry.
func (s *ProxyServer) writeAntiloopIndicator(w http.ResponseWriter, flusher http.Flusher, canFlush bool, capture *captureBuffer, id, model string, created float64) {
	chunk := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"delta": map[string]interface{}{
					"role":    "assistant",
					"content": "\n\n[检测到输出被截断，正在重新整理思路...]\n\n",
				},
				"finish_reason": nil,
			},
		},
	}
	b, _ := json.Marshal(chunk)
	line := "data: " + string(b) + "\n"
	w.Write([]byte(line))
	capture.Write([]byte(line))
	if canFlush {
		flusher.Flush()
	}
}

// executeAndStreamRetry makes the retry API call and streams the SSE response
// chunks directly to the client writer, overriding the chunk id with streamID
// for continuity with Phase 1. Returns the finish_reason and token usage.
func (s *ProxyServer) executeAndStreamRetry(
	w http.ResponseWriter,
	flusher http.Flusher,
	canFlush bool,
	body []byte,
	format string,
	capture *captureBuffer,
	streamID string,
) (string, *TokenUsage) {
	startTime := time.Now()

	retryResp, err := s.executeRetryCall(body, format)
	if err != nil {
		log.Printf("[antiloop] retry call failed: %v", err)
		s.logger.Add(LogEntry{
			Time:        startTime,
			Format:      format,
			RequestType: "antiloop_retry",
			Method:      "POST",
			Path:        "/chat/completions (防循环重试-stream)",
			StatusCode:  502,
			LatencyMs:   time.Since(startTime).Milliseconds(),
			OriginalBody:    condStr(s.config.VerboseLogging, truncateBody(string(body)), ""),
		})
		return "", nil
	}
	defer retryResp.Body.Close()

	scanner := bufio.NewScanner(retryResp.Body)
	scanner.Buffer(make([]byte, 0, 65536), 1048576)

	var finishReason string
	var lastUsage *TokenUsage
	var respCapture bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				continue
			}
			var chunk map[string]interface{}
			if json.Unmarshal([]byte(data), &chunk) == nil {
				// Override id to match Phase 1 for stream continuity
				chunk["id"] = streamID

				if choices, ok := chunk["choices"].([]interface{}); ok {
					for _, c := range choices {
						if choice, ok := c.(map[string]interface{}); ok {
							if fr, _ := choice["finish_reason"].(string); fr != "" {
								finishReason = fr
							}
						}
					}
				}
				if u := parseUsageFromMap(chunk); u != nil {
					lastUsage = u
				}

				b, _ := json.Marshal(chunk)
				line = "data: " + string(b)
			}
		}

		w.Write([]byte(line + "\n"))
		capture.Write([]byte(line + "\n"))
		respCapture.Write([]byte(line + "\n"))
		if canFlush {
			flusher.Flush()
		}
	}

	// Log the retry call
	retryLogID := s.logger.Add(LogEntry{
		Time:        startTime,
		Format:      format,
		RequestType: "antiloop_retry",
		Method:      "POST",
		Path:        "/chat/completions (防循环重试-stream)",
		StatusCode:  retryResp.StatusCode,
		LatencyMs:   time.Since(startTime).Milliseconds(),
		OriginalBody:    condStr(s.config.VerboseLogging, truncateBody(string(body)), ""),
		ResponseBody:    condStr(s.config.VerboseLogging, truncateBody(respCapture.String()), ""),
	})
	if lastUsage != nil {
		s.logger.UpdateTokenUsage(retryLogID, lastUsage)
	}

	return finishReason, lastUsage
}

// streamHardLimitSSE sends the "triggered output limit" message as a proper SSE chunk.
func (s *ProxyServer) streamHardLimitSSE(w http.ResponseWriter, flusher http.Flusher, canFlush bool, format string, capture *captureBuffer, id, model string, created float64) {
	chunk := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"delta": map[string]interface{}{
					"role":    "assistant",
					"content": "抱歉，当前任务超出了处理上限。请尝试将问题拆分为更小的步骤后重新提问。",
				},
				"finish_reason": "stop",
			},
		},
	}
	b, _ := json.Marshal(chunk)
	line := "data: " + string(b) + "\n"
	w.Write([]byte(line))
	capture.Write([]byte(line))
	if canFlush {
		flusher.Flush()
	}
}

// executeRetryCall makes the retry HTTP request and returns the response for streaming.
// Caller is responsible for closing resp.Body.
func (s *ProxyServer) executeRetryCall(body []byte, format string) (*http.Response, error) {
	upstream := s.config.OpenAIUpstream
	path := "/chat/completions"
	if format == "anthropic" {
		upstream = s.config.AnthropicUpstream
		path = "/v1/messages"
	}

	req, err := http.NewRequest("POST", upstream+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.config.APIKey)

	return s.client.Do(req)
}
func (s *ProxyServer) callAntiLoopAnalyzer(transformedBody []byte, format string, reasoningContent string) (*AntiLoopAnalysis, error) {
	startTime := time.Now()

	// Build analysis request: extract messages and add reasoning context
	analysisBody, err := buildAnalyzerRequest(transformedBody, format, reasoningContent)
	if err != nil {
		return nil, fmt.Errorf("build analyzer request: %w", err)
	}

	upstreamURL := s.config.OpenAIUpstream + "/chat/completions"

	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(analysisBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.config.APIKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Log failed analyzer call
		s.logger.Add(LogEntry{
			Time:        startTime,
			Format:      "openai",
			RequestType: "antiloop_analyzer",
			Method:      "POST",
			Path:        "/chat/completions (思维分析)",
			StatusCode:  502,
			LatencyMs:   time.Since(startTime).Milliseconds(),
		})
		return nil, fmt.Errorf("analyzer API call: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Add(LogEntry{
			Time:        startTime,
			Format:      "openai",
			RequestType: "antiloop_analyzer",
			Method:      "POST",
			Path:        "/chat/completions (思维分析)",
			StatusCode:  resp.StatusCode,
			LatencyMs:   time.Since(startTime).Milliseconds(),
		})
		return nil, fmt.Errorf("read analyzer response: %w", err)
	}

	// ── Log the analyzer call ──
	analyzerLogID := s.logger.Add(LogEntry{
		Time:        startTime,
		Format:      "openai",
		RequestType: "antiloop_analyzer",
		Method:      "POST",
		Path:        "/chat/completions (思维分析)",
		StatusCode:  resp.StatusCode,
		LatencyMs:   time.Since(startTime).Milliseconds(),
		OriginalBody:    condStr(s.config.VerboseLogging, truncateBody(string(analysisBody)), ""),
		ResponseBody:    condStr(s.config.VerboseLogging, truncateBody(string(respBytes)), ""),
	})

	// Parse OpenAI response
	var oaiResp map[string]interface{}
	if err := json.Unmarshal(respBytes, &oaiResp); err != nil {
		return nil, fmt.Errorf("parse analyzer response: %w", err)
	}

	// Extract token usage for analyzer log
	if u := parseUsageFromMap(oaiResp); u != nil {
		s.logger.UpdateTokenUsage(analyzerLogID, u)
	}

	choices, ok := oaiResp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return nil, fmt.Errorf("no choices in analyzer response")
	}

	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid choice format")
	}

	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no message in analyzer choice")
	}

	content, _ := message["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("empty analyzer response")
	}

	// Strip markdown fences if present
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var analysis AntiLoopAnalysis
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		return nil, fmt.Errorf("parse analysis JSON: %w (content: %s)", err, truncateBody(content))
	}

	return &analysis, nil
}

// buildAnalyzerRequest creates the request body for the sub-agent analyzer.
func buildAnalyzerRequest(transformedBody []byte, format string, reasoningContent string) ([]byte, error) {
	var original map[string]interface{}
	if err := json.Unmarshal(transformedBody, &original); err != nil {
		return nil, err
	}

	// Extract messages from the original request
	var messages []interface{}
	if msgs, ok := original["messages"].([]interface{}); ok {
		messages = msgs
	} else {
		// Anthropic format: wrap in user message
		messages = []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": fmt.Sprintf("Original request (Anthropic format):\n%s", string(transformedBody)),
			},
		}
	}

	// Serialize messages for the analyzer
	messagesJSON, _ := json.MarshalIndent(messages, "", "  ")

	// Build truncated reasoning context (max 8000 chars to keep analyzer request small)
	truncatedReasoning := reasoningContent
	const maxReasoningLen = 8000
	if len(truncatedReasoning) > maxReasoningLen {
		// Keep beginning and end (beginning has early thinking, end has the loop pattern)
		half := maxReasoningLen / 2
		truncatedReasoning = truncatedReasoning[:half] + "\n\n... [中间省略] ...\n\n" + truncatedReasoning[len(truncatedReasoning)-half:]
	}

	userContent := fmt.Sprintf(`## 完整对话上下文
%s

## 被截断的思考过程 (reasoning_content)
%s

请分析上述思考过程是否陷入死循环或过度推理，并返回 JSON 结果。`, string(messagesJSON), truncatedReasoning)

	analyzerReq := map[string]interface{}{
		"model": "deepseek-chat",
		"messages": []map[string]interface{}{
			{
				"role":    "system",
				"content": antiLoopAnalyzerPrompt,
			},
			{
				"role":    "user",
				"content": userContent,
			},
		},
		"max_tokens":        1024,
		"response_format":   map[string]interface{}{"type": "json_object"},
		"thinking":          map[string]interface{}{"type": "disabled"},
	}

	return json.Marshal(analyzerReq)
}

// buildGuidedRetryRequest creates the retry request body with full context from Phase 1,
// the analyzer's guidance, and the configured retry model/thinking settings.
func (s *ProxyServer) buildGuidedRetryRequest(transformedBody []byte, format string, analysis *AntiLoopAnalysis, phase1Content string, reasoningContent string) []byte {
	var data map[string]interface{}
	if err := json.Unmarshal(transformedBody, &data); err != nil {
		return transformedBody
	}

	// 1. Replace model with configured retry model
	data["model"] = s.config.AntiLoopRetryModel

	// 2. Remove max_tokens to give full room for retry
	delete(data, "max_tokens")

	// 3. Apply retry thinking configuration
	s.applyRetryThinking(data, analysis)

	// 4. Append Phase 1 context as new messages
	s.injectRetryContext(data, format, phase1Content, reasoningContent, analysis)

	b, err := json.Marshal(data)
	if err != nil {
		return transformedBody
	}
	return b
}

// applyRetryThinking sets thinking/effort on the retry request based on config + analysis.
func (s *ProxyServer) applyRetryThinking(data map[string]interface{}, analysis *AntiLoopAnalysis) {
	mode := s.config.AntiLoopRetryThinking
	if mode == "" {
		// Not set: use analyzer's recommendation
		if !analysis.EnableThinking {
			data["thinking"] = map[string]interface{}{"type": "disabled"}
			delete(data, "reasoning_effort")
			delete(data, "output_config")
		}
		return
	}
	if mode == "disabled" {
		data["thinking"] = map[string]interface{}{"type": "disabled"}
		delete(data, "reasoning_effort")
		delete(data, "output_config")
	} else if mode == "enabled" {
		data["thinking"] = map[string]interface{}{"type": "enabled"}
		data["reasoning_effort"] = s.config.AntiLoopRetryEffort
	}
}

// injectRetryContext appends Phase 1's output and the analyzer's guidance as new messages.
func (s *ProxyServer) injectRetryContext(data map[string]interface{}, format string, phase1Content string, reasoningContent string, analysis *AntiLoopAnalysis) {
	messagesRaw, ok := data["messages"]
	if !ok {
		return
	}
	messages, ok := messagesRaw.([]interface{})
	if !ok {
		return
	}

	// Append assistant message: the truncated Phase 1 output
	if phase1Content != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "assistant",
			"content": phase1Content,
		})
	}

	// Build guidance: analysis + reasoning summary
	guidanceText := "你的上一轮回答因输出超长被截断，但已取得部分进展。\n\n"
	if analysis != nil {
		guidanceText += "分析判定：" + analysis.Judgment + "\n"
		guidanceText += "改进指导：" + analysis.Guidance + "\n\n"
	} else {
		guidanceText += "请精简思考过程，直接给出最终结论。\n\n"
	}

	// Include condensed reasoning summary (max 3000 chars)
	if len(reasoningContent) > 0 {
		summary := reasoningContent
		const maxSummary = 3000
		if len(summary) > maxSummary {
			half := maxSummary / 2
			summary = summary[:half] + "\n\n... [中间省略] ...\n\n" + summary[len(summary)-half:]
		}
		guidanceText += "你的思考过程摘要：\n" + summary + "\n\n"
	}

	guidanceText += "请从断点继续完成任务，不要重复已经说过的内容。对于已经得出的正确结论可以直接引用。"

	// Append user message with guidance
	messages = append(messages, map[string]interface{}{
		"role":    "user",
		"content": guidanceText,
	})

	data["messages"] = messages
}

// buildSimpleRetryRequest creates a retry request with generic guidance (no analyzer).
func (s *ProxyServer) buildSimpleRetryRequest(transformedBody []byte, format string, phase1Content string, reasoningContent string) []byte {
	var data map[string]interface{}
	if err := json.Unmarshal(transformedBody, &data); err != nil {
		return transformedBody
	}

	// Replace model with configured retry model
	data["model"] = s.config.AntiLoopRetryModel

	// Remove max_tokens
	delete(data, "max_tokens")

	// Apply retry thinking config (disabled by default)
	if s.config.AntiLoopRetryThinking == "enabled" {
		data["thinking"] = map[string]interface{}{"type": "enabled"}
		data["reasoning_effort"] = s.config.AntiLoopRetryEffort
	} else {
		data["thinking"] = map[string]interface{}{"type": "disabled"}
		delete(data, "reasoning_effort")
		delete(data, "output_config")
	}

	// Inject Phase 1 context
	s.injectRetryContext(data, format, phase1Content, reasoningContent, nil)

	b, err := json.Marshal(data)
	if err != nil {
		return transformedBody
	}
	return b
}

// injectIntoLastUserMessage appends text to the last user-role message in the messages array.
func injectIntoLastUserMessage(data map[string]interface{}, text string) {
	messagesRaw, ok := data["messages"]
	if !ok {
		return
	}
	messages, ok := messagesRaw.([]interface{})
	if !ok || len(messages) == 0 {
		return
	}

	// Find last user message
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if msg, ok := messages[i].(map[string]interface{}); ok {
			if role, _ := msg["role"].(string); role == "user" {
				lastUserIdx = i
				break
			}
		}
	}

	if lastUserIdx < 0 {
		return
	}

	userMsg, ok := messages[lastUserIdx].(map[string]interface{})
	if !ok {
		return
	}

	// Append to string content
	if content, ok := userMsg["content"].(string); ok {
		userMsg["content"] = content + text
	} else if contentBlocks, ok := userMsg["content"].([]interface{}); ok {
		// Anthropic content array format
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type": "text",
			"text": text,
		})
		userMsg["content"] = contentBlocks
	}
}

// executeRetry sends the retry request to the upstream API and returns the
// full response body along with the finish_reason.
func (s *ProxyServer) executeRetry(body []byte, format string) (responseBody []byte, finishReason string) {
	startTime := time.Now()

	upstream := s.config.OpenAIUpstream
	path := "/chat/completions"
	if format == "anthropic" {
		upstream = s.config.AnthropicUpstream
		path = "/v1/messages"
	}

	req, err := http.NewRequest("POST", upstream+path, bytes.NewReader(body))
	if err != nil {
		log.Printf("[antiloop] retry request error: %v", err)
		return hardLimitMessages(format), "stop"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.config.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("[antiloop] retry upstream error: %v", err)
		s.logger.Add(LogEntry{
			Time:        startTime,
			Format:      format,
			RequestType: "antiloop_retry",
			Method:      "POST",
			Path:        path + " (防循环重试)",
			StatusCode:  502,
			LatencyMs:   time.Since(startTime).Milliseconds(),
			OriginalBody:    condStr(s.config.VerboseLogging, truncateBody(string(body)), ""),
		})
		return hardLimitMessages(format), "stop"
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[antiloop] retry read error: %v", err)
		return hardLimitMessages(format), "stop"
	}

	// Check Content-Type to determine if streaming
	contentType := resp.Header.Get("Content-Type")
	isStream := strings.Contains(contentType, "text/event-stream")

	var fr string
	if isStream {
		fr, _ = detectStreamFinishReason(respBytes)
	} else {
		fr = detectBufferFinishReason(respBytes)
	}

	// ── Log the retry call ──
	retryLogID := s.logger.Add(LogEntry{
		Time:        startTime,
		Format:      format,
		RequestType: "antiloop_retry",
		Method:      "POST",
		Path:        path + " (防循环重试)",
		StatusCode:  resp.StatusCode,
		LatencyMs:   time.Since(startTime).Milliseconds(),
		OriginalBody:    condStr(s.config.VerboseLogging, truncateBody(string(body)), ""),
		ResponseBody:    condStr(s.config.VerboseLogging, truncateBody(string(respBytes)), ""),
	})

	// Extract and update token usage
	if u := extractUsageFromBody(string(respBytes)); u != nil {
		s.logger.UpdateTokenUsage(retryLogID, u)
	}

	return respBytes, fr
}
