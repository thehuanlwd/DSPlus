package main

import (
	"bytes"
	"fmt"
	"strings"
)

// extractTextContent 从消息的 content 字段中提取纯文本，兼容 string 与
// Anthropic 风格的 text 块数组两种形式。无法提取（如 content 为 null 或
// 非文本块）时返回空字符串——调用方据此判断该消息是否可被重组/注入。
func extractTextContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var texts []string
		for _, block := range v {
			if b, ok := block.(map[string]interface{}); ok {
				if t, _ := b["type"].(string); t == "text" {
					if text, ok := b["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
		}
		return strings.Join(texts, "\n")
	default:
		return ""
	}
}

// userMessageIndices 返回消息切片中第一条与最后一条 user 消息的下标。
// 若找不到 user 消息，返回 (-1, -1)。
func userMessageIndices(messages []interface{}) (first, last int) {
	first = -1
	last = -1
	for i, mRaw := range messages {
		m, ok := mRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role == "user" {
			if first == -1 {
				first = i
			}
			last = i
		}
	}
	return
}

// resolveTarget maps a placement policy and the first/last user indices to a
// concrete message index.  Returns -1 when no suitable message exists.
func resolveTarget(first, last int, placement string) int {
	if placement == "last" {
		return last
	}
	return first
}

// buildSystemBlock wraps systemText in <system_prompt> tags.
func buildSystemBlock(systemText string) string {
	return "\n\n<system_prompt>\n" + systemText + "\n</system_prompt>"
}

// buildExtraBlock wraps extraPrompt in <supreme_instruction> tags.
func buildExtraBlock(extraPrompt string) string {
	return "\n\n<supreme_instruction>\n" + extraPrompt + "\n</supreme_instruction>"
}

// applyInjections writes the system and/or extra-prompt blocks into the
// appropriate user message(s) of the messages slice.
func applyInjections(messages []interface{}, sysTarget, extraTarget int, systemText, extraPrompt string) {
	if sysTarget == extraTarget && sysTarget >= 0 {
		// Both injections go into the same message — single block.
		block := buildSystemBlock(systemText) + buildExtraBlock(extraPrompt)
		appendToUserContent(messages[sysTarget], block)
	} else {
		if sysTarget >= 0 {
			appendToUserContent(messages[sysTarget], buildSystemBlock(systemText))
		}
		if extraTarget >= 0 {
			appendToUserContent(messages[extraTarget], buildExtraBlock(extraPrompt))
		}
	}
}

// appendToUserContent appends text to a message's content, handling both
// plain string and Anthropic content-array formats.  For unexpected content
// types (e.g. null, number) it falls back to a string representation instead
// of silently dropping the injected block.
func appendToUserContent(userMsg interface{}, text string) {
	m, ok := userMsg.(map[string]interface{})
	if !ok {
		return
	}
	switch content := m["content"].(type) {
	case nil:
		m["content"] = text
	case string:
		m["content"] = content + text
	case []interface{}:
		m["content"] = append(content, map[string]interface{}{
			"type": "text",
			"text": text,
		})
	default:
		// 非预期的 content 类型：转为字符串拼接，避免注入块被静默丢弃导致提示词丢失。
		m["content"] = fmt.Sprintf("%v", content) + text
	}
}

// ── OpenAI format transformation ─────────────────────────────────────────────

// transformOpenAIInPlace removes system-role messages and appends their
// content (wrapped in <system_prompt> tags) to a user message.  Extra prompt
// is appended similarly as <supreme_instruction>.  The data map is modified
// in-place.  Returns true if any transformation was applied.
func transformOpenAIInPlace(data map[string]interface{}, sysPlacement string, extraPrompt string, extraPlacement string) bool {
	messagesRaw, ok := data["messages"]
	if !ok {
		return false
	}
	messages, ok := messagesRaw.([]interface{})
	if !ok {
		return false
	}

	// 第一遍：仅收集系统提示词文本，绝不破坏原始 messages。
	var systemTexts []string
	for _, mRaw := range messages {
		m, ok := mRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role == "system" {
			// 兼容 string 与 text 块数组两种系统提示词形式；无法提取时
			// systemTexts 为空，injectSystem 为 false，原 system 消息被保留。
			if txt := extractTextContent(m["content"]); txt != "" {
				systemTexts = append(systemTexts, txt)
			}
		}
	}

	injectSystem := sysPlacement != "none" && len(systemTexts) > 0
	injectExtra := extraPlacement != "none" && extraPrompt != ""

	// 无需任何注入时直接返回，保持原始请求完全不动。
	if !injectSystem && !injectExtra {
		return false
	}

	// 第二遍：构建新 messages。仅当真正要重组系统提示词（injectSystem）
	// 时才丢弃原始 system 消息；placement == "none" 时原样保留 system 消息。
	dropSystem := injectSystem
	newMessages := make([]interface{}, 0, len(messages))
	for _, mRaw := range messages {
		m, ok := mRaw.(map[string]interface{})
		if !ok {
			newMessages = append(newMessages, mRaw)
			continue
		}
		if role, _ := m["role"].(string); role == "system" && dropSystem {
			continue
		}
		newMessages = append(newMessages, mRaw)
	}

	firstUserIdx, lastUserIdx := userMessageIndices(newMessages)

	sysTarget := -1
	extraTarget := -1

	if injectSystem {
		sysTarget = resolveTarget(firstUserIdx, lastUserIdx, sysPlacement)
		if sysTarget == -1 {
			return false
		}
	}
	if injectExtra {
		extraTarget = resolveTarget(firstUserIdx, lastUserIdx, extraPlacement)
		if extraTarget == -1 {
			return false
		}
	}

	systemText := strings.Join(systemTexts, "\n")
	applyInjections(newMessages, sysTarget, extraTarget, systemText, extraPrompt)

	data["messages"] = newMessages
	return true
}

// ── Anthropic format transformation ──────────────────────────────────────────

// transformAnthropicInPlace extracts the top-level "system" field and
// appends it to a user message (wrapped in <system_prompt> tags), then
// deletes "system" from the root.  Extra prompt is handled similarly.
// The data map is modified in-place.  Returns true if any transformation
// was applied.
func transformAnthropicInPlace(data map[string]interface{}, sysPlacement string, extraPrompt string, extraPlacement string) bool {
	systemRaw, hasSystem := data["system"]
	systemText := ""
	if hasSystem {
		systemText = extractAnthropicSystem(systemRaw)
	}

	messagesRaw, ok := data["messages"]
	if !ok {
		return false
	}
	messages, ok := messagesRaw.([]interface{})
	if !ok || len(messages) == 0 {
		return false
	}

	injectSystem := sysPlacement != "none" && systemText != ""
	injectExtra := extraPlacement != "none" && extraPrompt != ""

	if !injectSystem && !injectExtra {
		return false
	}

	firstUserIdx, lastUserIdx := userMessageIndices(messages)

	sysTarget := -1
	extraTarget := -1

	if injectSystem {
		sysTarget = resolveTarget(firstUserIdx, lastUserIdx, sysPlacement)
		if sysTarget == -1 {
			return false
		}
	}
	if injectExtra {
		extraTarget = resolveTarget(firstUserIdx, lastUserIdx, extraPlacement)
		if extraTarget == -1 {
			return false
		}
	}

	applyInjections(messages, sysTarget, extraTarget, systemText, extraPrompt)

	// 仅当真正重组了系统提示词（injectSystem）时才删除顶层 system 字段；
	// placement == "none" 时保留原始 system 字段，避免系统提示词丢失。
	if injectSystem && hasSystem {
		delete(data, "system")
	}
	data["messages"] = messages
	return true
}

func extractAnthropicSystem(system interface{}) string {
	switch v := system.(type) {
	case string:
		return v
	case []interface{}:
		var texts []string
		for _, block := range v {
			if b, ok := block.(map[string]interface{}); ok {
				if t, _ := b["type"].(string); t == "text" {
					if text, ok := b["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
		}
		return strings.Join(texts, "\n")
	default:
		return ""
	}
}

// replaceDSMLMarkers replaces all forms of full-width DSML markers (both literal and Unicode escaped) with half-width markers.
func replaceDSMLMarkers(input string) string {
	if strings.Contains(input, "｜") || strings.Contains(input, "\\u") || strings.Contains(input, "\\uFF") || strings.Contains(input, "\\uff") {
		// Replace escaped forms first
		input = strings.ReplaceAll(input, `\uff5c\uff5cDSML\uff5c\uff5c`, "||DSML||")
		input = strings.ReplaceAll(input, `\uFF5C\uFF5CDSML\uFF5C\uFF5C`, "||DSML||")
		input = strings.ReplaceAll(input, `\uff5c`, "|")
		input = strings.ReplaceAll(input, `\uFF5C`, "|")

		// Replace literal forms
		input = strings.ReplaceAll(input, "｜｜DSML｜｜", "||DSML||")
		input = strings.ReplaceAll(input, "｜", "|")
	}
	return input
}

// replaceDSMLMarkersBytes replaces all forms of full-width DSML markers in a byte slice.
func replaceDSMLMarkersBytes(b []byte) []byte {
	if bytes.Contains(b, []byte("｜")) || bytes.Contains(b, []byte("\\u")) {
		s := replaceDSMLMarkers(string(b))
		return []byte(s)
	}
	return b
}

