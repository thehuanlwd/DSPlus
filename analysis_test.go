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

func TestFormatCacheRatio(t *testing.T) {
	tests := []struct {
		hit  int
		miss int
		want string
	}{
		{100, 0, "100%"},
		{0, 100, "0%"},
		{0, 0, "0%"},
		{9999, 1, "99.9%"}, // 接近但不等于100%
		{1, 9999, "0.1%"}, // 接近但不等于0%
		{50, 50, "50.0%"},
		{75, 25, "75.0%"},
	}

	for _, tc := range tests {
		got := formatCacheRatio(tc.hit, tc.miss)
		if got != tc.want {
			t.Errorf("formatCacheRatio(%d, %d) = %q, want %q", tc.hit, tc.miss, got, tc.want)
		}
	}
}

