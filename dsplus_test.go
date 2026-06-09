package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── detectFormat ─────────────────────────────────────────────────────────────

func TestDetectFormat_OpenAI(t *testing.T) {
	tests := []string{
		// OpenAI: messages + system role (with or without max_tokens).
		`{"messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":"hi"}]}`,
		`{"messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":"hi"}],"max_tokens":100}`,
		`{"messages": [{"role": "system", "content": "be nice"}, {"role": "user", "content": "hello"}]}`,
	}
	for _, body := range tests {
		if got := detectFormat(body); got != "openai" {
			t.Errorf("detectFormat(%q) = %q, want openai", body, got)
		}
	}
}

func TestDetectFormat_Anthropic(t *testing.T) {
	tests := []string{
		// Top-level system field is the strongest signal.
		`{"system":"You are helpful","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`,
		`{"system":[{"type":"text","text":"be nice"}],"messages":[{"role":"user","content":"hi"}],"max_tokens":100}`,
	}
	for _, body := range tests {
		if got := detectFormat(body); got != "anthropic" {
			t.Errorf("detectFormat(%q) = %q, want anthropic", body, got)
		}
	}
}

func TestDetectFormat_Unknown(t *testing.T) {
	tests := []string{
		``,
		`not json`,
		`{"no_messages":true}`,
		`{"messages":"not an array"}`,
	}
	for _, body := range tests {
		if got := detectFormat(body); got != "unknown" {
			t.Errorf("detectFormat(%q) = %q, want unknown", body, got)
		}
	}
}

func TestDetectFormat_UserContentDoesNotConfuse(t *testing.T) {
	// "max_tokens" and "role":"system" in user content must not affect detection.
	// This body has a real system role → it's OpenAI regardless of user content.
	body := `{"messages":[{"role":"system","content":"be helpful"},{"role":"user","content":"please use max_tokens and role system"}],"max_tokens":50}`
	if got := detectFormat(body); got != "openai" {
		t.Errorf("detectFormat with keywords in user content = %q, want openai", got)
	}

	// No system role + has max_tokens → Anthropic (by design).
	body2 := `{"messages":[{"role":"user","content":"hello"}],"max_tokens":100}`
	if got := detectFormat(body2); got != "anthropic" {
		t.Errorf("detectFormat(no system + max_tokens) = %q, want anthropic", got)
	}
}

// ── transformOpenAIInPlace ───────────────────────────────────────────────────

func TestTransformOpenAI_Basic(t *testing.T) {
	body := `{"messages":[{"role":"system","content":"be helpful"},{"role":"user","content":"hello"}]}`
	var data map[string]interface{}
	json.Unmarshal([]byte(body), &data)

	changed := transformOpenAIInPlace(data, "first", "", "none")
	if !changed {
		t.Fatal("expected change")
	}

	msgs := data["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after transform, got %d", len(msgs))
	}
	user := msgs[0].(map[string]interface{})
	content := user["content"].(string)
	if !strings.Contains(content, "hello") {
		t.Error("user content should still contain original message")
	}
	if !strings.Contains(content, "<system_prompt>") {
		t.Error("user content should contain system_prompt wrapper")
	}
	if !strings.Contains(content, "be helpful") {
		t.Error("user content should contain system text")
	}
	// System message must be removed.
	if user["role"] != "user" {
		t.Error("only remaining message should be user role")
	}
}

func TestTransformOpenAI_NoSystem(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"hello"}]}`
	var data map[string]interface{}
	json.Unmarshal([]byte(body), &data)

	changed := transformOpenAIInPlace(data, "first", "", "none")
	if changed {
		t.Error("should not change when no system prompt")
	}
}

func TestTransformOpenAI_PlacementLast(t *testing.T) {
	body := `{"messages":[{"role":"system","content":"sys"},{"role":"user","content":"first"},{"role":"user","content":"last"}]}`
	var data map[string]interface{}
	json.Unmarshal([]byte(body), &data)

	transformOpenAIInPlace(data, "last", "", "none")

	msgs := data["messages"].([]interface{})
	// After removing system, we have 2 user messages. The system should be appended to "last".
	lastUser := msgs[1].(map[string]interface{})
	content := lastUser["content"].(string)
	if !strings.Contains(content, "sys") {
		t.Error("system text should be appended to last user message")
	}
}

func TestTransformOpenAI_PlacementNone(t *testing.T) {
	body := `{"messages":[{"role":"system","content":"sys"},{"role":"user","content":"hi"}]}`
	var data map[string]interface{}
	json.Unmarshal([]byte(body), &data)

	changed := transformOpenAIInPlace(data, "none", "", "none")
	if changed {
		t.Error("placement=none should not transform")
	}
}

func TestTransformOpenAI_ExtraPrompt(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	var data map[string]interface{}
	json.Unmarshal([]byte(body), &data)

	changed := transformOpenAIInPlace(data, "first", "EXTRA RULES", "first")
	if !changed {
		t.Fatal("expected change for extra prompt")
	}
	msgs := data["messages"].([]interface{})
	content := msgs[0].(map[string]interface{})["content"].(string)
	if !strings.Contains(content, "<supreme_instruction>") {
		t.Error("extra prompt should be wrapped in supreme_instruction tags")
	}
	if !strings.Contains(content, "EXTRA RULES") {
		t.Error("extra prompt content should appear")
	}
}

// ── transformAnthropicInPlace ────────────────────────────────────────────────

func TestTransformAnthropic_Basic(t *testing.T) {
	body := `{"system":"be helpful","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`
	var data map[string]interface{}
	json.Unmarshal([]byte(body), &data)

	changed := transformAnthropicInPlace(data, "first", "", "none")
	if !changed {
		t.Fatal("expected change")
	}
	if _, hasSystem := data["system"]; hasSystem {
		t.Error("system field should be deleted after transform")
	}
	content := data["messages"].([]interface{})[0].(map[string]interface{})["content"].(string)
	if !strings.Contains(content, "<system_prompt>") {
		t.Error("system text should be injected into user message")
	}
}

func TestTransformAnthropic_SystemContentBlocks(t *testing.T) {
	body := `{"system":[{"type":"text","text":"rule 1"},{"type":"text","text":"rule 2"}],"messages":[{"role":"user","content":"hi"}]}`
	var data map[string]interface{}
	json.Unmarshal([]byte(body), &data)

	transformAnthropicInPlace(data, "first", "", "none")
	content := data["messages"].([]interface{})[0].(map[string]interface{})["content"].(string)
	if !strings.Contains(content, "rule 1") || !strings.Contains(content, "rule 2") {
		t.Error("both system text blocks should be present")
	}
}

func TestTransformAnthropic_UserContentArray(t *testing.T) {
	body := `{"system":"be nice","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`
	var data map[string]interface{}
	json.Unmarshal([]byte(body), &data)

	transformAnthropicInPlace(data, "first", "", "none")
	blocks := data["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})
	// Should have original block + injected block
	if len(blocks) < 2 {
		t.Fatalf("expected >=2 content blocks, got %d", len(blocks))
	}
	lastBlock := blocks[len(blocks)-1].(map[string]interface{})
	if lastBlock["type"] != "text" {
		t.Error("injected block should be text type")
	}
}

// ── parseUsageFromMap ────────────────────────────────────────────────────────

func TestParseUsageFromMap_Standard(t *testing.T) {
	data := map[string]interface{}{
		"usage": map[string]interface{}{
			"total_tokens":      float64(100),
			"prompt_tokens":     float64(60),
			"completion_tokens": float64(40),
		},
	}
	u := parseUsageFromMap(data)
	if u == nil {
		t.Fatal("expected usage")
	}
	if u.Total != 100 || u.Prompt != 60 || u.Completion != 40 {
		t.Errorf("unexpected values: %+v", u)
	}
}

func TestParseUsageFromMap_CacheFields(t *testing.T) {
	data := map[string]interface{}{
		"usage": map[string]interface{}{
			"prompt_tokens":           float64(100),
			"completion_tokens":       float64(50),
			"prompt_cache_hit_tokens": float64(80),
			"prompt_cache_miss_tokens": float64(20),
		},
	}
	u := parseUsageFromMap(data)
	if u.CacheHit != 80 || u.CacheMiss != 20 {
		t.Errorf("cache fields: hit=%d miss=%d", u.CacheHit, u.CacheMiss)
	}
	if u.Total != 150 {
		t.Errorf("total should be computed: got %d", u.Total)
	}
}

func TestParseUsageFromMap_Nil(t *testing.T) {
	if u := parseUsageFromMap(map[string]interface{}{}); u != nil {
		t.Error("expected nil for missing usage")
	}
}

// ── extractAnthropicSystem ───────────────────────────────────────────────────

func TestExtractAnthropicSystem_String(t *testing.T) {
	if got := extractAnthropicSystem("hello"); got != "hello" {
		t.Errorf("string: got %q", got)
	}
}

func TestExtractAnthropicSystem_Blocks(t *testing.T) {
	blocks := []interface{}{
		map[string]interface{}{"type": "text", "text": "first"},
		map[string]interface{}{"type": "text", "text": "second"},
	}
	if got := extractAnthropicSystem(blocks); got != "first\nsecond" {
		t.Errorf("blocks: got %q", got)
	}
}

func TestExtractAnthropicSystem_Empty(t *testing.T) {
	if got := extractAnthropicSystem(42); got != "" {
		t.Errorf("unknown type: got %q", got)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func TestUserMessageIndices(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "sys"},
		map[string]interface{}{"role": "user", "content": "first"},
		map[string]interface{}{"role": "assistant", "content": "reply"},
		map[string]interface{}{"role": "user", "content": "last"},
	}
	first, last := userMessageIndices(messages)
	if first != 1 || last != 3 {
		t.Errorf("first=%d last=%d, want first=1 last=3", first, last)
	}
}

func TestUserMessageIndices_NoUser(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "sys"},
	}
	first, last := userMessageIndices(messages)
	if first != -1 || last != -1 {
		t.Errorf("should be -1,-1, got %d,%d", first, last)
	}
}

func TestResolveTarget(t *testing.T) {
	if got := resolveTarget(1, 3, "first"); got != 1 {
		t.Errorf("first: got %d", got)
	}
	if got := resolveTarget(1, 3, "last"); got != 3 {
		t.Errorf("last: got %d", got)
	}
	if got := resolveTarget(-1, -1, "first"); got != -1 {
		t.Errorf("none: got %d", got)
	}
}

// ── captureBuffer ─────────────────────────────────────────────────────────────

func TestCaptureBuffer(t *testing.T) {
	cb := newCaptureBuffer(100) // large enough to avoid truncation
	cb.Write([]byte("hello"))
	cb.Write([]byte(" world"))
	if s := cb.String(); s != "hello world" {
		t.Errorf("got %q", s)
	}
}

func TestCaptureBuffer_Overflow(t *testing.T) {
	cb := newCaptureBuffer(5)
	cb.Write([]byte("hello world"))
	s := cb.String()
	if !strings.Contains(s, "[stream truncated]") {
		t.Error("overflow should append truncation notice")
	}
}

// ── maskAPIKey ───────────────────────────────────────────────────────────────

func TestMaskAPIKey(t *testing.T) {
	masked := maskAPIKey("sk-1234567890abcdef")
	if !strings.Contains(masked, "sk-12") || !strings.Contains(masked, "def") {
		t.Errorf("masked: %q", masked)
	}
	if len(masked) < len("sk-1234567890abcdef") {
		t.Error("masked key should not be shorter than original")
	}
}

func TestMaskAPIKey_Short(t *testing.T) {
	if maskAPIKey("short") != "short" {
		t.Error("short keys should not be masked")
	}
}

// ── Config round-trip ────────────────────────────────────────────────────────

func TestConfigSaveLoad(t *testing.T) {
	// Verify marshal/unmarshal symmetry.
	cfg := DefaultConfig()
	cfg.Port = 9999
	cfg.APIKey = "sk-test"

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Port != 9999 {
		t.Errorf("Port: %d", restored.Port)
	}
	if restored.APIKey != "sk-test" {
		t.Errorf("APIKey: %q", restored.APIKey)
	}
	if restored.OpenAIUpstream != "https://api.deepseek.com" {
		t.Errorf("OpenAIUpstream: %q", restored.OpenAIUpstream)
	}
}

// Test file-based persistence with a temporary config.
func TestConfigPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	cfg.Port = 5555
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(path, data, 0600)

	// Can't test LoadConfig directly (it reads exe dir), but test the
	// underlying read + unmarshal logic manually.
	read, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var restored Config
	json.Unmarshal(read, &restored)
	if restored.Port != 5555 {
		t.Errorf("Port = %d", restored.Port)
	}
}

// ── injectThinkingParams ─────────────────────────────────────────────────────

func TestInjectThinkingParams_Disabled(t *testing.T) {
	ps := &ProxyServer{config: &Config{ThinkingMode: "disabled"}}
	data := map[string]interface{}{"model": "test"}
	ps.injectThinkingParams(data, "openai")
	thinking := data["thinking"].(map[string]interface{})
	if thinking["type"] != "disabled" {
		t.Errorf("type = %v", thinking["type"])
	}
}

func TestInjectThinkingParams_Enabled(t *testing.T) {
	ps := &ProxyServer{config: &Config{ThinkingMode: "enabled", ReasoningEffort: "max"}}
	data := map[string]interface{}{"model": "test"}
	ps.injectThinkingParams(data, "openai")
	thinking := data["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" {
		t.Errorf("type = %v", thinking["type"])
	}
	if data["reasoning_effort"] != "max" {
		t.Errorf("effort = %v", data["reasoning_effort"])
	}
}

func TestInjectThinkingParams_Empty(t *testing.T) {
	ps := &ProxyServer{config: &Config{ThinkingMode: ""}}
	data := map[string]interface{}{"model": "test"}
	ps.injectThinkingParams(data, "openai")
	if _, exists := data["thinking"]; exists {
		t.Error("empty mode should not inject thinking param")
	}
}

// ── injectMaxTokens ─────────────────────────────────────────────────────────

func TestInjectMaxTokens_Custom(t *testing.T) {
	ps := &ProxyServer{config: &Config{MaxTokensMode: "custom", MaxTokensCustom: 8000}}
	data := map[string]interface{}{}
	ps.injectMaxTokens(data)
	if data["max_tokens"] != 8000 {
		t.Errorf("max_tokens = %v", data["max_tokens"])
	}
}

func TestInjectMaxTokens_Empty(t *testing.T) {
	ps := &ProxyServer{config: &Config{MaxTokensMode: ""}}
	data := map[string]interface{}{"max_tokens": 100}
	ps.injectMaxTokens(data)
	if data["max_tokens"] != 100 {
		t.Error("empty mode should not overwrite existing max_tokens")
	}
}

// ── truncateBody ─────────────────────────────────────────────────────────────

func TestTruncateBody(t *testing.T) {
	short := "hello"
	if truncateBody(short) != short {
		t.Error("short string should not be truncated")
	}
	long := strings.Repeat("x", truncateBodyMaxLen+100)
	result := truncateBody(long)
	expectedMax := truncateBodyMaxLen + len("\n\n... [truncated]")
	if len(result) > expectedMax {
		t.Errorf("len=%d, want <= %d", len(result), expectedMax)
	}
	if !strings.Contains(result, "[truncated]") {
		t.Error("truncated string should have marker")
	}
}

// ── condStr ──────────────────────────────────────────────────────────────────

func TestCondStr(t *testing.T) {
	if condStr(true, "a", "b") != "a" {
		t.Error("true should return a")
	}
	if condStr(false, "a", "b") != "b" {
		t.Error("false should return b")
	}
}

func TestReplaceFullWidthVerticalBars(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"｜｜DSML｜｜"}}`,
			expected: `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"||DSML||"}}`,
		},
		{
			input:    `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"</｜｜DSML｜｜tool_calls>"}}`,
			expected: `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"</||DSML||tool_calls>"}}`,
		},
		{
			input:    `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"\uff5c\uff5cDSML\uff5c\uff5c"}}`,
			expected: `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"||DSML||"}}`,
		},
		{
			input:    `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"</\uFF5C\uFF5CDSML\uFF5C\uFF5Ctool_calls>"}}`,
			expected: `data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"</||DSML||tool_calls>"}}`,
		},
		{
			input:    `some ｜ text ｜`,
			expected: `some | text |`,
		},
		{
			input:    `some \uff5c text \uFF5C`,
			expected: `some | text |`,
		},
		{
			input:    `normal text`,
			expected: `normal text`,
		},
	}

	for _, tc := range tests {
		got := replaceDSMLMarkers(tc.input)
		if got != tc.expected {
			t.Errorf("Replace failed: got %q, want %q", got, tc.expected)
		}
	}
}

func TestHandleAPISaveConfig_LANAccessRequiresRestart(t *testing.T) {
	oldPort := runtimePort
	oldLANAccess := runtimeLANAccess
	oldRestartCh := appRestartCh
	defer func() {
		runtimePort = oldPort
		runtimeLANAccess = oldLANAccess
		appRestartCh = oldRestartCh
	}()

	cfg := DefaultConfig()
	cfg.Port = 8188
	cfg.LANAccess = false
	setRuntimeState(cfg.Port, cfg.LANAccess)

	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"port":8188,"lan_access":true}`))
	rec := httptest.NewRecorder()
	handleAPISaveConfig(rec, req, &cfg)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "saved" {
		t.Fatalf("status = %v", resp["status"])
	}
	if resp["restart_required"] != true {
		t.Fatalf("restart_required = %v", resp["restart_required"])
	}
	reasons, ok := resp["restart_reasons"].([]interface{})
	if !ok || len(reasons) != 1 || reasons[0] != "局域网/WSL 访问" {
		t.Fatalf("restart_reasons = %#v", resp["restart_reasons"])
	}
}


