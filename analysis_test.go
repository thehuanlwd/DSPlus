package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		ID:         "ev1",
		Time:       now,
		Phase:      "primary",
		Format:     "openai",
		RawRequest: `{"messages": [{"role": "user", "content": "first message"}]}`,
	}
	svc.processEvent(ev1)

	if ev1.SessionID == "" {
		t.Fatal("expected session ID to be inferred, got empty")
	}
	if ev1.TurnID != 1 {
		t.Errorf("expected TurnID 1, got %d", ev1.TurnID)
	}

	// 相同的指纹在短时间内，现在由于实现了会话合并，应该产生相同的 SessionID，且 TurnID 递增
	ev2 := &TraceEvent{
		ID:         "ev2",
		Time:       now.Add(1 * time.Minute),
		Phase:      "primary",
		Format:     "openai",
		RawRequest: `{"messages": [{"role": "user", "content": "first message"}, {"role": "user", "content": "second message"}]}`,
	}
	svc.processEvent(ev2)

	if ev2.SessionID != ev1.SessionID {
		t.Errorf("expected same session ID, got different: %s vs %s", ev1.SessionID, ev2.SessionID)
	}
	if ev2.TurnID != 2 {
		t.Errorf("expected TurnID 2, got %d", ev2.TurnID)
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
		{1, 9999, "0.1%"},  // 接近但不等于0%
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

func TestTruncateString(t *testing.T) {
	s1 := "Hello"
	if got := truncateString(s1, 10); got != s1 {
		t.Errorf("expected %q, got %q", s1, got)
	}

	s2 := "这是一个超长的中文字符串测试"
	got2 := truncateString(s2, 5)
	want2 := "这是一个超...(truncated)"
	if got2 != want2 {
		t.Errorf("expected %q, got %q", want2, got2)
	}
}

func TestExtractChatHistoryIncremental(t *testing.T) {
	rawReq := `{
		"messages": [
			{"role": "user", "content": "msg1"},
			{"role": "assistant", "content": "msg2"},
			{"role": "user", "content": "msg3"}
		]
	}`

	full := extractChatHistory(rawReq, "openai", "finalReply", "", nil, false, 0)
	if len(full) != 4 {
		t.Errorf("expected full history size 4, got %d", len(full))
	}
	if full[0].Content != "msg1" || full[3].Content != "finalReply" {
		t.Errorf("incorrect content in full history")
	}

	inc := extractChatHistory(rawReq, "openai", "finalReply", "", nil, true, 2)
	if len(inc) != 2 {
		t.Errorf("expected incremental size 2, got %d", len(inc))
	}
	if inc[0].Content != "msg3" || inc[1].Content != "finalReply" {
		t.Errorf("incorrect content in incremental history: %v", inc)
	}
}

func TestGetFullChatHistory(t *testing.T) {
	sess := &ConversationSession{
		Turns: make(map[int]*ConversationTurn),
	}
	sess.Turns[1] = &ConversationTurn{
		TurnID: 1,
		ChatHistory: []ChatMessage{
			{Role: "user", Content: "hello"},
		},
	}
	sess.Turns[2] = &ConversationTurn{
		TurnID: 2,
		ChatHistory: []ChatMessage{
			{Role: "assistant", Content: "hi"},
			{Role: "user", Content: "how are you"},
		},
	}

	full := sess.getFullChatHistory()
	if len(full) != 3 {
		t.Errorf("expected 3 messages, got %d", len(full))
	}
	if full[0].Content != "hello" || full[2].Content != "how are you" {
		t.Errorf("incorrect content order in full history: %v", full)
	}
}

func TestLogDeduplication(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.AnalysisEnabled = true

	svc := &AnalysisService{
		config:        NewSafeConfig(cfg),
		sessions:      make(map[string]*ConversationSession),
		eventChan:     make(chan *TraceEvent, 100),
		logDir:        tempDir,
		shutdownCh:    make(chan struct{}),
		writtenHashes: make(map[string]bool),
	}

	// 构造 600 字节的 System Prompt
	longSystemPrompt := "SYSTEM PROMPT: "
	for len(longSystemPrompt) < 600 {
		longSystemPrompt += "This is a very long system prompt used for testing logging deduplication function. "
	}

	now := time.Now()

	// 1. 提交第一个包含明文 System Prompt 的事件
	ev1 := &TraceEvent{
		ID:        "ev1",
		Time:      now,
		SessionID: "sess_1",
		TurnID:    1,
		Phase:     "primary",
		ChatHistory: []ChatMessage{
			{Role: "system", Content: longSystemPrompt},
			{Role: "user", Content: "hello"},
		},
		RawRequest:  `{"messages":[{"role":"system","content":"` + longSystemPrompt + `"}]}`,
		RawResponse: `{"choices":[{"message":{"content":"ok"}}]}`,
	}

	svc.writeToDisk(ev1)

	// 2. 提交第二个具有完全相同 System Prompt 的事件
	ev2 := &TraceEvent{
		ID:        "ev2",
		Time:      now.Add(1 * time.Second),
		SessionID: "sess_2",
		TurnID:    1,
		Phase:     "primary",
		ChatHistory: []ChatMessage{
			{Role: "system", Content: longSystemPrompt},
			{Role: "user", Content: "world"},
		},
		RawRequest:  `{"messages":[{"role":"system","content":"` + longSystemPrompt + `"}]}`,
		RawResponse: `{"choices":[{"message":{"content":"ok"}}]}`,
	}
	svc.writeToDisk(ev2)

	// 3. 读取磁盘上写入的原始文件，验证去重引用
	fileName := now.Format("2006-01-02") + ".jsonl"
	filePath := filepath.Join(tempDir, fileName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read written log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d", len(lines))
	}

	var savedEv1, savedEv2 TraceEvent
	json.Unmarshal([]byte(lines[0]), &savedEv1)
	json.Unmarshal([]byte(lines[1]), &savedEv2)

	if len(savedEv1.ChatHistory) < 1 || savedEv1.ChatHistory[0].Content != longSystemPrompt {
		t.Fatalf("ev1: expected system content to be stored as raw text")
	}
	expectedRef := "$ref_dict:md5_" + getMD5Hash(longSystemPrompt)
	if len(savedEv2.ChatHistory) < 1 || savedEv2.ChatHistory[0].Content != expectedRef {
		t.Fatalf("ev2: expected system content to be stored as reference: %s, got %s", expectedRef, savedEv2.ChatHistory[0].Content)
	}

	// 4. 清空内存状态并调用 loadHistoryFromDisk 加载，验证引用可被还原
	svc.sessions = make(map[string]*ConversationSession)
	svc.loadHistoryFromDisk()

	sess2, ok2 := svc.sessions["sess_2"]
	if !ok2 {
		t.Fatalf("session 2 not restored properly")
	}

	turn2, ok2 := sess2.Turns[1]
	if !ok2 {
		t.Fatalf("turn 2 not restored properly")
	}

	if len(turn2.ChatHistory) < 1 || turn2.ChatHistory[0].Content != longSystemPrompt {
		t.Errorf("turn2: expected system content to be recovered to original text, got: %s", turn2.ChatHistory[0].Content)
	}
}
