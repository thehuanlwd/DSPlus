package main

import (
	"strings"
	"testing"
)

// TestTransformOpenAINonePlacementKeepsSystem 回归测试：系统提示词不动
// (placement=none) 且启用额外注入时，原始 system 消息不应被丢弃。
func TestTransformOpenAINonePlacementKeepsSystem(t *testing.T) {
	data := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "你是一个助手"},
			map[string]interface{}{"role": "user", "content": "你好"},
		},
	}
	ok := transformOpenAIInPlace(data, "none", "额外指令", "last")
	if !ok {
		t.Fatal("预期返回 true（注入了额外提示词），实际 false")
	}
	msgs := data["messages"].([]interface{})
	// 系统消息必须仍存在
	foundSystem := false
	for _, m := range msgs {
		if mm, ok := m.(map[string]interface{}); ok {
			if mm["role"] == "system" {
				foundSystem = true
			}
		}
	}
	if !foundSystem {
		t.Fatal("placement=none 时系统提示词被丢弃（回归 bug）")
	}
	// 最后一条 user 消息应包含额外注入标签
	lastUser := msgs[len(msgs)-1].(map[string]interface{})
	if !strings.Contains(lastUser["content"].(string), "<supreme_instruction>") {
		t.Fatal("额外提示词未注入到最后一条 user 消息")
	}
}

// TestTransformOpenAIFirstPlacementMovesSystem 验证 placement=first 时系统提示词
// 被移入首条 user 消息并打上 <system_prompt> 标签，且不再有独立 system 消息。
func TestTransformOpenAIFirstPlacementMovesSystem(t *testing.T) {
	data := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "系统规则"},
			map[string]interface{}{"role": "user", "content": "问题"},
			map[string]interface{}{"role": "assistant", "content": "回答"},
			map[string]interface{}{"role": "user", "content": "追问"},
		},
	}
	ok := transformOpenAIInPlace(data, "first", "", "none")
	if !ok {
		t.Fatal("预期返回 true，实际 false")
	}
	msgs := data["messages"].([]interface{})
	for _, m := range msgs {
		if mm, ok := m.(map[string]interface{}); ok {
			if mm["role"] == "system" {
				t.Fatal("placement=first 后仍存在独立 system 消息")
			}
		}
	}
	firstUser := msgs[0].(map[string]interface{})
	if !strings.Contains(firstUser["content"].(string), "<system_prompt>") {
		t.Fatal("系统提示词未注入首条 user 消息")
	}
}

// TestTransformOpenAINullContentTarget 验证目标 user 消息 content 为 null 时，
// 注入块不应被静默丢弃（回归 #1）。
func TestTransformOpenAINullContentTarget(t *testing.T) {
	data := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": nil},
		},
	}
	ok := transformOpenAIInPlace(data, "none", "额外指令", "last")
	if !ok {
		t.Fatal("预期返回 true，实际 false")
	}
	msgs := data["messages"].([]interface{})
	content, _ := msgs[0].(map[string]interface{})["content"].(string)
	if !strings.Contains(content, "<supreme_instruction>") {
		t.Fatal("content 为 null 时注入块被静默丢弃（回归 #1）")
	}
}

// TestTransformAnthropicNonePlacementKeepsSystem 验证 Anthropic 格式下
// placement=none + 额外注入时，顶层 system 字段不被删除。
func TestTransformAnthropicNonePlacementKeepsSystem(t *testing.T) {
	data := map[string]interface{}{
		"system": "系统规则",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "你好"},
		},
	}
	ok := transformAnthropicInPlace(data, "none", "额外指令", "last")
	if !ok {
		t.Fatal("预期返回 true，实际 false")
	}
	if _, has := data["system"]; !has {
		t.Fatal("Anthropic placement=none 时顶层 system 字段被删除（回归 bug）")
	}
	if !strings.Contains(data["messages"].([]interface{})[0].(map[string]interface{})["content"].(string), "<supreme_instruction>") {
		t.Fatal("额外提示词未注入 Anthropic user 消息")
	}
}

// TestInjectMaxTokensCap 锁定 max_tokens 上限 384000：自定义值超出应被钳制，
// 且 384000 档位应精确生效。
func TestInjectMaxTokensCap(t *testing.T) {
	// 自定义超出上限 -> 钳制到 384000
	cfg := Config{MaxTokensMode: "custom", MaxTokensCustom: 999999}
	s := &ProxyServer{config: NewSafeConfig(cfg)}
	data := map[string]interface{}{}
	s.injectMaxTokens(data)
	if got, _ := data["max_tokens"].(int); got != 384000 {
		t.Fatalf("自定义超出上限应被钳制到 384000，实际 %d", got)
	}

	// 384000 档位精确生效
	cfg2 := Config{MaxTokensMode: "384000"}
	s2 := &ProxyServer{config: NewSafeConfig(cfg2)}
	data2 := map[string]interface{}{}
	s2.injectMaxTokens(data2)
	if got, _ := data2["max_tokens"].(int); got != 384000 {
		t.Fatalf("384000 档位应为 384000，实际 %d", got)
	}

	// 低于上限的自定义值保持不变
	cfg3 := Config{MaxTokensMode: "custom", MaxTokensCustom: 16000}
	s3 := &ProxyServer{config: NewSafeConfig(cfg3)}
	data3 := map[string]interface{}{}
	s3.injectMaxTokens(data3)
	if got, _ := data3["max_tokens"].(int); got != 16000 {
		t.Fatalf("16000 自定义应保持 16000，实际 %d", got)
	}
}

// TestInjectMaxTokensOff 锁定「不发送」档：强制删除 max_tokens（即便原始请求携带）。
func TestInjectMaxTokensOff(t *testing.T) {
	cfg := Config{MaxTokensMode: "off"}
	s := &ProxyServer{config: NewSafeConfig(cfg)}
	data := map[string]interface{}{"max_tokens": 32000}
	s.injectMaxTokens(data)
	if _, ok := data["max_tokens"]; ok {
		t.Fatal("「不发送」档应强制删除 max_tokens，但其仍然存在")
	}

	// 原始请求本就不含 max_tokens 时，删除操作应为无副作用
	data2 := map[string]interface{}{}
	s.injectMaxTokens(data2)
	if _, ok := data2["max_tokens"]; ok {
		t.Fatal("「不发送」档在原本无 max_tokens 时不应凭空添加")
	}
}
