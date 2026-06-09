package main

import (
	"bytes"
	"strings"
)

// in the messages slice.  Returns (-1, -1) if no user message is found.
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
// plain string and Anthropic content-array formats.
func appendToUserContent(userMsg interface{}, text string) {
	m, ok := userMsg.(map[string]interface{})
	if !ok {
		return
	}
	if content, ok := m["content"].(string); ok {
		m["content"] = content + text
	} else if contentBlocks, ok := m["content"].([]interface{}); ok {
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type": "text",
			"text": text,
		})
		m["content"] = contentBlocks
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

	var systemTexts []string
	newMessages := make([]interface{}, 0, len(messages))

	for _, mRaw := range messages {
		m, ok := mRaw.(map[string]interface{})
		if !ok {
			newMessages = append(newMessages, mRaw)
			continue
		}
		role, _ := m["role"].(string)
		if role == "system" {
			if content, ok := m["content"].(string); ok {
				systemTexts = append(systemTexts, content)
			}
			continue // drop system message
		}
		newMessages = append(newMessages, mRaw)
	}

	injectSystem := sysPlacement != "none" && len(systemTexts) > 0
	injectExtra := extraPlacement != "none" && extraPrompt != ""

	if !injectSystem && !injectExtra {
		return false
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

	if hasSystem {
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

