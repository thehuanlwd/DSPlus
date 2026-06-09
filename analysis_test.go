package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBuildRequestMeta(t *testing.T) {
	reqBody := `{
		"model": "deepseek-reasoner",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello AI"}
		],
		"stream": true,
		"max_tokens": 100
	}`

	var data map[string]interface{}
	json.Unmarshal([]byte(reqBody), &data)

	meta := buildRequestMeta(data, "openai", true, false)

	if meta.Model != "deepseek-reasoner" {
		t.Errorf("expected model deepseek-reasoner, got %s", meta.Model)
	}
	if !meta.Stream {
		t.Errorf("expected stream true, got false")
	}
	if meta.MaxTokens != 100 {
		t.Errorf("expected max_tokens 100, got %d", meta.MaxTokens)
	}
	if meta.LastUserSummary != "Hello AI" {
		t.Errorf("expected LastUserSummary 'Hello AI', got '%s'", meta.LastUserSummary)
	}
	if !meta.SystemPromptTransformed {
		t.Errorf("expected system prompt transformed true")
	}
}

func TestSessionInference(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AnalysisEnabled = true
	svc := InitAnalysisService(NewSafeConfig(cfg))

	now := time.Now()
	
	// 第一个请求
	ev1 := &TraceEvent{
		ID:        "ev1",
		Time:      now,
		Phase:     "primary",
		Format:    "openai",
		RawRequest: `{"messages": [{"role": "user", "content": "first message"}]}`,
	}
	svc.processEvent(ev1)

	if ev1.SessionID == "" {
		t.Fatal("expected session ID to be inferred, got empty")
	}
	if ev1.TurnID != 1 {
		t.Errorf("expected TurnID 1, got %d", ev1.TurnID)
	}

	// 相同的指纹在短时间内，现在由于放弃了会话合并，也应该产生不同的 SessionID
	ev2 := &TraceEvent{
		ID:        "ev2",
		Time:      now.Add(1 * time.Minute),
		Phase:     "primary",
		Format:    "openai",
		RawRequest: `{"messages": [{"role": "user", "content": "first message"}, {"role": "user", "content": "second message"}]}`,
	}
	svc.processEvent(ev2)

	if ev2.SessionID == ev1.SessionID {
		t.Errorf("expected different session ID, got same: %s", ev1.SessionID)
	}
	if ev2.TurnID != 1 {
		t.Errorf("expected TurnID 1, got %d", ev2.TurnID)
	}
}
