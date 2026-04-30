package main

import (
	"encoding/json"
	"strings"
)

func transformOpenAI(body []byte, sysPlacement string, extraPrompt string, extraPlacement string) (bool, []byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		return false, body, err
	}

	messagesRaw, ok := req["messages"]
	if !ok {
		return false, body, nil
	}
	messages, ok := messagesRaw.([]interface{})
	if !ok {
		return false, body, nil
	}

	var systemTexts []string
	var newMessages []interface{}
	firstUserIdx := -1
	lastUserIdx := -1

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
			continue
		}
		if role == "user" {
			if firstUserIdx == -1 {
				firstUserIdx = len(newMessages)
			}
			lastUserIdx = len(newMessages)
		}
		newMessages = append(newMessages, mRaw)
	}

	injectSystem := sysPlacement != "none" && len(systemTexts) > 0
	injectExtra := extraPlacement != "none" && extraPrompt != ""

	if !injectSystem && !injectExtra {
		return false, body, nil
	}

	sysTarget := -1
	extraTarget := -1

	if injectSystem {
		if sysPlacement == "last" {
			sysTarget = lastUserIdx
		} else {
			sysTarget = firstUserIdx
		}
		if sysTarget == -1 {
			return false, body, nil
		}
	}

	if injectExtra {
		if extraPlacement == "last" {
			extraTarget = lastUserIdx
		} else {
			extraTarget = firstUserIdx
		}
		if extraTarget == -1 {
			return false, body, nil
		}
	}

	if sysTarget == extraTarget && injectSystem && injectExtra {
		joinedSystem := strings.Join(systemTexts, "\n")
		block := "\n\n<system_prompt>\n" + joinedSystem + "\n</system_prompt>"
		block += "\n\n<supreme_instruction>\n" + extraPrompt + "\n</supreme_instruction>"
		if userMsg, ok := newMessages[sysTarget].(map[string]interface{}); ok {
			if content, ok := userMsg["content"].(string); ok {
				userMsg["content"] = content + block
			}
		}
	} else {
		if injectSystem {
			joinedSystem := strings.Join(systemTexts, "\n")
			block := "\n\n<system_prompt>\n" + joinedSystem + "\n</system_prompt>"
			if userMsg, ok := newMessages[sysTarget].(map[string]interface{}); ok {
				if content, ok := userMsg["content"].(string); ok {
					userMsg["content"] = content + block
				}
			}
		}
		if injectExtra {
			block := "\n\n<supreme_instruction>\n" + extraPrompt + "\n</supreme_instruction>"
			if userMsg, ok := newMessages[extraTarget].(map[string]interface{}); ok {
				if content, ok := userMsg["content"].(string); ok {
					userMsg["content"] = content + block
				}
			}
		}
	}

	req["messages"] = newMessages
	result, err := json.Marshal(req)
	if err != nil {
		return false, body, err
	}
	return true, result, nil
}

func transformAnthropic(body []byte, sysPlacement string, extraPrompt string, extraPlacement string) (bool, []byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		return false, body, err
	}

	systemRaw, hasSystem := req["system"]
	systemText := ""
	if hasSystem {
		systemText = extractAnthropicSystem(systemRaw)
	}

	messagesRaw, ok := req["messages"]
	if !ok {
		return false, body, nil
	}
	messages, ok := messagesRaw.([]interface{})
	if !ok || len(messages) == 0 {
		return false, body, nil
	}

	injectSystem := sysPlacement != "none" && systemText != ""
	injectExtra := extraPlacement != "none" && extraPrompt != ""

	if !injectSystem && !injectExtra {
		return false, body, nil
	}

	firstUserIdx := -1
	lastUserIdx := -1
	for i, mRaw := range messages {
		m, ok := mRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role == "user" {
			if firstUserIdx == -1 {
				firstUserIdx = i
			}
			lastUserIdx = i
		}
	}

	sysTarget := -1
	extraTarget := -1

	if injectSystem {
		if sysPlacement == "last" {
			sysTarget = lastUserIdx
		} else {
			sysTarget = firstUserIdx
		}
		if sysTarget == -1 {
			return false, body, nil
		}
	}

	if injectExtra {
		if extraPlacement == "last" {
			extraTarget = lastUserIdx
		} else {
			extraTarget = firstUserIdx
		}
		if extraTarget == -1 {
			return false, body, nil
		}
	}

	if sysTarget == extraTarget && injectSystem && injectExtra {
		block := "\n\n<system_prompt>\n" + systemText + "\n</system_prompt>"
		block += "\n\n<supreme_instruction>\n" + extraPrompt + "\n</supreme_instruction>"
		injectUserMsg(messages[sysTarget], block)
	} else {
		if injectSystem {
			block := "\n\n<system_prompt>\n" + systemText + "\n</system_prompt>"
			injectUserMsg(messages[sysTarget], block)
		}
		if injectExtra {
			block := "\n\n<supreme_instruction>\n" + extraPrompt + "\n</supreme_instruction>"
			injectUserMsg(messages[extraTarget], block)
		}
	}

	if hasSystem {
		delete(req, "system")
	}
	req["messages"] = messages
	result, err := json.Marshal(req)
	if err != nil {
		return false, body, err
	}
	return true, result, nil
}

func injectUserMsg(userMsg interface{}, block string) {
	if m, ok := userMsg.(map[string]interface{}); ok {
		if content, ok := m["content"].(string); ok {
			m["content"] = content + block
		} else if contentBlocks, ok := m["content"].([]interface{}); ok {
			contentBlocks = append(contentBlocks, map[string]interface{}{
				"type": "text",
				"text": block,
			})
			m["content"] = contentBlocks
		}
	}
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
