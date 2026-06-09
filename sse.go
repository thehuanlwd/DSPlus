package main

import (
	"encoding/json"
	"strings"
)

// StreamDelta 统一表示从 SSE 行中解析出的增量数据
type StreamDelta struct {
	Content          string
	ReasoningContent string
	FinishReason     string
	Usage            *TokenUsage
	RawChunk         map[string]interface{} // 原始 JSON 映射，便于局部重写
}

// ParseSSELine 解析单行 SSE 协议数据。如果是合法的 data chunk 则提取关键字段，否则返回 nil
func ParseSSELine(line string) (*StreamDelta, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "data: ") {
		return nil, nil
	}
	dataStr := strings.TrimPrefix(line, "data: ")
	dataStr = strings.TrimSpace(dataStr)
	if dataStr == "[DONE]" || dataStr == "" {
		return nil, nil
	}

	var chunk map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
		return nil, err
	}

	delta := &StreamDelta{
		RawChunk: chunk,
	}

	// 1. 解析 Usage（OpenAI / Anthropic 都可能有）
	if u := parseUsageFromMap(chunk); u != nil {
		delta.Usage = u
	}

	// 2. 兼容 OpenAI choices 格式
	if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if fr, _ := choice["finish_reason"].(string); fr != "" {
				delta.FinishReason = fr
			}
			if d, ok := choice["delta"].(map[string]interface{}); ok {
				if rc, _ := d["reasoning_content"].(string); rc != "" {
					delta.ReasoningContent = rc
				}
				if ct, _ := d["content"].(string); ct != "" {
					delta.Content = ct
				}
			}
		}
	}

	// 3. 兼容 Anthropic chunk 格式
	if typeVal, _ := chunk["type"].(string); typeVal == "content_block_delta" {
		if d, ok := chunk["delta"].(map[string]interface{}); ok {
			if rc, _ := d["thinking"].(string); rc != "" {
				delta.ReasoningContent = rc
			}
			if ct, _ := d["text"].(string); ct != "" {
				delta.Content = ct
			}
		}
	}
	if typeVal, _ := chunk["type"].(string); typeVal == "message_delta" {
		if d, ok := chunk["delta"].(map[string]interface{}); ok {
			if sr, _ := d["stop_reason"].(string); sr != "" {
				delta.FinishReason = sr
			}
		}
	}
	if sr, ok := chunk["stop_reason"].(string); ok && sr != "" {
		delta.FinishReason = sr
	}

	return delta, nil
}
