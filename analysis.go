package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ChatMessage struct {
	Role             string   `json:"role"`
	Content          string   `json:"content,omitempty"`
	ReasoningContent string   `json:"reasoning_content,omitempty"`
	ToolCalls        []string `json:"tool_calls,omitempty"`
}

// TraceEvent 代表代理过程中的一次结构化事实事件
type TraceEvent struct {
	ID          string        `json:"id"`
	ParentID    string        `json:"parent_id,omitempty"` // 用于关联重试或分析子事件
	Time        time.Time     `json:"time"`
	LogID       int64         `json:"log_id"`
	SessionID   string        `json:"session_id"`
	TurnID      int           `json:"turn_id"`
	Phase       string        `json:"phase"` // primary, analyzer, retry, debug
	Format      string        `json:"format"`
	Route       string        `json:"route"`
	Status      int           `json:"status"`
	LatencyMs   int64         `json:"latency_ms"`
	Model       string        `json:"model"`
	Upstream    string        `json:"upstream"`
	Request     RequestMeta   `json:"request"`
	Response    ResponseMeta  `json:"response"`
	RawRequest  string        `json:"raw_request,omitempty"`  // 详细记录时可用，持久化可选择性丢弃
	RawResponse string        `json:"raw_response,omitempty"` // 详细记录时可用，持久化可选择性丢弃
	ChatHistory []ChatMessage `json:"chat_history,omitempty"` // 持久化清洗后的聊天历史
}

// RequestMeta 存储请求关键元数据
type RequestMeta struct {
	Model                   string         `json:"model"`
	Stream                  bool           `json:"stream"`
	ThinkingMode            string         `json:"thinking_mode"`
	ReasoningEffort         string         `json:"reasoning_effort"`
	MaxTokens               int            `json:"max_tokens"`
	MessageCount            int            `json:"message_count"`
	RoleCounts              map[string]int `json:"role_counts"`
	LastUserSummary         string         `json:"last_user_summary"`
	SystemPromptTransformed bool           `json:"system_prompt_transformed"`
	ExtraPromptInjected     bool           `json:"extra_prompt_injected"`
	Tools                   []string       `json:"tools,omitempty"`
}

// ResponseMeta 存储响应元数据
type ResponseMeta struct {
	FinishReason      string   `json:"finish_reason"`
	PromptTokens      int      `json:"prompt_tokens"`
	CompletionTokens  int      `json:"completion_tokens"`
	TotalTokens       int      `json:"total_tokens"`
	CacheHitTokens    int      `json:"cache_hit_tokens"`
	CacheMissTokens   int      `json:"cache_miss_tokens"`
	ReasoningTokens   int      `json:"reasoning_tokens"`
	ReasoningContent  string   `json:"reasoning_content"`
	Content           string   `json:"content,omitempty"`
	ToolCalls         []string `json:"tool_calls,omitempty"`
	AntiLoopTriggered bool     `json:"antiloop_triggered"`
	AnalyzerJudgment  string   `json:"analyzer_judgment"`
	RetryModel        string   `json:"retry_model"`
}

// ConversationTurn 代表一轮完整的对话（包含主请求，潜在的防循环判定，重试等）
type ConversationTurn struct {
	TurnID            int           `json:"turn_id"`
	StartTime         time.Time     `json:"start_time"`
	UserMessage       string        `json:"user_message"`
	SystemModified    bool          `json:"system_modified"`
	ExtraInjected     bool          `json:"extra_injected"`
	Events            []*TraceEvent `json:"events"`
	AssistantResponse string        `json:"assistant_response"`
	ReasoningContent  string        `json:"reasoning_content"`
	ToolCalls         []string      `json:"tool_calls"`
	ChatHistory       []ChatMessage `json:"chat_history,omitempty"` // 新增：保存清洗后的整条请求交互历史
}

// ConversationSession 代表会话维度的会话对象
type ConversationSession struct {
	ID              string                    `json:"id"`
	StartTime       time.Time                 `json:"start_time"`
	EndTime         time.Time                 `json:"end_time"`
	RequestCount    int                       `json:"request_count"`
	Models          []string                  `json:"models"`
	PromptTokens    int                       `json:"prompt_tokens"`
	CompletionTokens int                       `json:"completion_tokens"`
	TotalTokens     int                       `json:"total_tokens"`
	CacheHitTokens  int                       `json:"cache_hit_tokens"`
	CacheMissTokens int                       `json:"cache_miss_tokens"`
	Turns           map[int]*ConversationTurn `json:"turns"`
	Errors          int                       `json:"errors"`
	Retries         int                       `json:"retries"`
	GroupingReason  string                    `json:"grouping_reason"`
}

// SessionSummary 返回给 UI 的紧凑结构
type SessionSummary struct {
	ID               string    `json:"id"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	RequestCount     int       `json:"request_count"`
	Models           string    `json:"models"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CacheHitRatio    float64   `json:"cache_hit_ratio"`
	Errors           int       `json:"errors"`
	Retries          int       `json:"retries"`
	SummaryText      string    `json:"summary_text"`
	Format           string    `json:"format"`
	Status           int       `json:"status"`
}

// AnalysisService 核心分析服务
type AnalysisService struct {
	config     *SafeConfig
	lock       sync.RWMutex
	sessions   map[string]*ConversationSession
	eventChan  chan *TraceEvent
	logDir     string
	shutdownCh chan struct{}
}

var (
	globalAnalysisService *AnalysisService
	analysisOnce          sync.Once
)

// InitAnalysisService 初始化全局分析服务
func InitAnalysisService(cfg *SafeConfig) *AnalysisService {
	analysisOnce.Do(func() {
		exe, _ := os.Executable()
		logDir := filepath.Join(filepath.Dir(exe), "analysis_logs")

		globalAnalysisService = &AnalysisService{
			config:     cfg,
			sessions:   make(map[string]*ConversationSession),
			eventChan:  make(chan *TraceEvent, 2000), // 异步事件缓冲区
			logDir:     logDir,
			shutdownCh: make(chan struct{}),
		}

		if cfg.Get().AnalysisEnabled {
			// 创建日志目录
			if err := os.MkdirAll(logDir, 0755); err != nil {
				log.Printf("[analysis] failed to create directory %s: %v", logDir, err)
			}
			// 重启时同步从磁盘加载历史日志还原内存会话
			globalAnalysisService.loadHistoryFromDisk()
			
			go globalAnalysisService.runWorker()
			go globalAnalysisService.cleanupOldLogs()
		}
	})
	return globalAnalysisService
}

// GetAnalysisService 获取全局分析服务实例
func GetAnalysisService() *AnalysisService {
	return globalAnalysisService
}

// SubmitEvent 提交 TraceEvent 至服务，非阻塞
func (s *AnalysisService) SubmitEvent(ev *TraceEvent) {
	if s == nil || !s.config.Get().AnalysisEnabled {
		return
	}
	// 仅过滤聊天 completions/messages 相关的路由
	route := ev.Route
	if !strings.HasSuffix(route, "/v1/chat/completions") && !strings.HasSuffix(route, "/v1/messages") {
		return
	}
	select {
	case s.eventChan <- ev:
	default:
		log.Printf("[analysis] warning: event queue full, dropping event %s", ev.ID)
	}
}

// Stop 停止分析服务
func (s *AnalysisService) Stop() {
	if s == nil {
		return
	}
	close(s.shutdownCh)
}

func (s *AnalysisService) runWorker() {
	for {
		select {
		case ev := <-s.eventChan:
			s.processEvent(ev)
		case <-s.shutdownCh:
			return
		}
	}
}

func (s *AnalysisService) processEvent(ev *TraceEvent) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// 1. 进行 Session 归拢与推导
	s.inferSession(ev)

	// 2. 内存聚合
	sess, exists := s.sessions[ev.SessionID]
	if !exists {
		sess = &ConversationSession{
			ID:             ev.SessionID,
			StartTime:      ev.Time,
			EndTime:        ev.Time,
			Turns:          make(map[int]*ConversationTurn),
			GroupingReason: ev.Phase,
		}
		s.sessions[ev.SessionID] = sess
	}

	if ev.Time.Before(sess.StartTime) {
		sess.StartTime = ev.Time
	}
	if ev.Time.After(sess.EndTime) {
		sess.EndTime = ev.Time
	}

	// 累计计数
	sess.RequestCount++
	sess.PromptTokens += ev.Response.PromptTokens
	sess.CompletionTokens += ev.Response.CompletionTokens
	sess.TotalTokens += ev.Response.TotalTokens
	sess.CacheHitTokens += ev.Response.CacheHitTokens
	sess.CacheMissTokens += ev.Response.CacheMissTokens

	if ev.Status >= 400 {
		sess.Errors++
	}
	if ev.Phase == "retry" {
		sess.Retries++
	}

	// 更新模型列表
	foundModel := false
	for _, m := range sess.Models {
		if m == ev.Model {
			foundModel = true
			break
		}
	}
	if !foundModel && ev.Model != "" {
		sess.Models = append(sess.Models, ev.Model)
	}

	// 合流至 Turn 结构
	turn, turnExists := sess.Turns[ev.TurnID]
	if !turnExists {
		turn = &ConversationTurn{
			TurnID:    ev.TurnID,
			StartTime: ev.Time,
		}
		sess.Turns[ev.TurnID] = turn
	}

	turn.Events = append(turn.Events, ev)

	// 收集文本段以展示聊天流
	if ev.Phase == "primary" {
		if ev.Request.LastUserSummary == "" && ev.RawRequest != "" {
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(ev.RawRequest), &data); err == nil {
				ev.Request = buildRequestMeta(data, ev.Format, ev.Request.SystemPromptTransformed, ev.Request.ExtraPromptInjected)
			}
		}
		turn.UserMessage = ev.Request.LastUserSummary
		turn.SystemModified = ev.Request.SystemPromptTransformed
		turn.ExtraInjected = ev.Request.ExtraPromptInjected
	}

	// 如果 primary 包含 response_body 或者是 retry 阶段的 response_body，我们将其作为 assistant 的回复
	if ev.Phase == "primary" || ev.Phase == "retry" {
		if ev.Response.ReasoningContent != "" {
			turn.ReasoningContent = ev.Response.ReasoningContent
		} else if ev.RawResponse != "" {
			if reasoning := parseAssistantReasoning(ev.RawResponse, ev.Format); reasoning != "" {
				turn.ReasoningContent = reasoning
			}
		}

		// 优先使用 Response.Content，否则从 RawResponse 提取
		if ev.Response.Content != "" {
			turn.AssistantResponse = ev.Response.Content
		} else if ev.RawResponse != "" {
			if content := parseAssistantContent(ev.RawResponse, ev.Format); content != "" {
				turn.AssistantResponse = content
			}
		}

		// 收集 ToolCalls
		if ev.RawResponse != "" {
			if tools := parseAssistantToolCalls(ev.RawResponse, ev.Format); len(tools) > 0 {
				turn.ToolCalls = tools
				ev.Response.ToolCalls = tools
			}
		}

		// 提取清洗后的 ChatHistory 并存入 turn 和 ev
		if ev.RawRequest != "" {
			turn.ChatHistory = extractChatHistory(ev.RawRequest, ev.Format, turn.AssistantResponse, turn.ReasoningContent, turn.ToolCalls)
			ev.ChatHistory = turn.ChatHistory
		}
	}

	// 3. 持久化落盘 (如果配置开启)
	if s.config.Get().AnalysisPersistence {
		s.writeToDisk(ev)
	}

	// 4. 清空内存中庞大的原始数据以释放内存
	ev.RawRequest = ""
	ev.RawResponse = ""

	s.dumpAllSessionsToHTMLDataLocked()
}

// inferSession 推导和归拢会话 ID 以及轮次（TurnID）
func (s *AnalysisService) inferSession(ev *TraceEvent) {
	// 放弃会话合并，每次请求均为全新 SessionID
	ev.SessionID = "sess_" + strconv.FormatInt(ev.Time.UnixNano(), 36)
	ev.TurnID = 1
}

// NewTraceEvent 是 TraceEvent 的统一构建工厂方法
func NewTraceEvent(
	startTime time.Time,
	logID int64,
	sessionID string,
	turnID int,
	phase string,
	format string,
	route string,
	status int,
	latencyMs int64,
	model string,
	upstream string,
	reqMeta RequestMeta,
	respMeta ResponseMeta,
	rawRequest string,
	rawResponse string,
) *TraceEvent {
	return &TraceEvent{
		ID:          "ev_" + strconv.FormatInt(startTime.UnixNano(), 36),
		Time:        startTime,
		LogID:       logID,
		SessionID:   sessionID,
		TurnID:      turnID,
		Phase:       phase,
		Format:      format,
		Route:       route,
		Status:      status,
		LatencyMs:   latencyMs,
		Model:       model,
		Upstream:    upstream,
		Request:     reqMeta,
		Response:    respMeta,
		RawRequest:  rawRequest,
		RawResponse: rawResponse,
	}
}

func getLastUserMessage(ev *TraceEvent) string {
	if ev.RawRequest == "" {
		return ""
	}
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(ev.RawRequest), &body); err != nil {
		return ""
	}

	messages, ok := body["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return ""
	}

	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "user" {
			return extractMessageText(msg)
		}
	}
	return ""
}

func extractMessageText(msg map[string]interface{}) string {
	if content, ok := msg["content"].(string); ok {
		return content
	}
	if contentArr, ok := msg["content"].([]interface{}); ok {
		var textParts []string
		for _, part := range contentArr {
			if pMap, ok := part.(map[string]interface{}); ok {
				if txt, ok := pMap["text"].(string); ok {
					textParts = append(textParts, txt)
				}
			}
		}
		return strings.Join(textParts, " ")
	}
	return ""
}

func (s *AnalysisService) writeToDisk(ev *TraceEvent) {
	// 无论配置如何，写盘时都强制抹除体积庞大的 RawRequest 与 RawResponse，以防日志空间爆炸
	evCopy := *ev
	evCopy.RawRequest = ""
	evCopy.RawResponse = ""

	line, err := json.Marshal(evCopy)
	if err != nil {
		return
	}

	fileName := ev.Time.Format("2006-01-02") + ".jsonl"
	filePath := filepath.Join(s.logDir, fileName)

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("[analysis] failed to write event to disk: %v", err)
		return
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		log.Printf("[analysis] failed to write event bytes to disk: %v", err)
	}
}

func (s *AnalysisService) cleanupOldLogs() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	cleanup := func() {
		s.lock.Lock()
		defer s.lock.Unlock()

		files, err := os.ReadDir(s.logDir)
		if err != nil {
			return
		}

		cutoff := time.Now().AddDate(0, 0, -s.config.Get().AnalysisRetentionDays)

		for _, file := range files {
			if filepath.Ext(file.Name()) != ".jsonl" {
				continue
			}
			info, err := file.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				path := filepath.Join(s.logDir, file.Name())
				os.Remove(path)
				log.Printf("[analysis] cleaned up expired log file: %s", file.Name())
			}
		}

		// 清理内存中的过期数据
		for id, sess := range s.sessions {
			if sess.EndTime.Before(cutoff) {
				delete(s.sessions, id)
			}
		}
	}

	// 启动时清理一次
	cleanup()

	for {
		select {
		case <-ticker.C:
			cleanup()
		case <-s.shutdownCh:
			return
		}
	}
}

func (s *AnalysisService) loadHistoryFromDisk() {
	s.lock.Lock()
	defer s.lock.Unlock()

	files, err := os.ReadDir(s.logDir)
	if err != nil {
		return
	}

	var logFiles []string
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".jsonl" {
			logFiles = append(logFiles, file.Name())
		}
	}
	sort.Strings(logFiles)

	cutoff := time.Now().AddDate(0, 0, -s.config.Get().AnalysisRetentionDays)

	for _, fileName := range logFiles {
		filePath := filepath.Join(s.logDir, fileName)
		info, err := os.Stat(filePath)
		if err == nil && info.ModTime().Before(cutoff) {
			continue
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("[analysis] failed to read log file %s: %v", fileName, err)
			continue
		}

		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var ev TraceEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}

			if ev.Time.Before(cutoff) {
				continue
			}

			s.reconstructEventInMemory(&ev)
		}
	}
	log.Printf("[analysis] loaded history from disk: %d sessions active", len(s.sessions))
	s.dumpAllSessionsToHTMLDataLocked()
}

func (s *AnalysisService) dumpAllSessionsToHTMLDataLocked() {
	var details []interface{}
	var keys []string
	for k := range s.sessions {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return s.sessions[keys[i]].StartTime.After(s.sessions[keys[j]].StartTime)
	})

	for _, k := range keys {
		sess := s.sessions[k]
		details = append(details, sess)
	}

	data, err := json.MarshalIndent(details, "", "  ")
	if err != nil {
		log.Printf("[analysis] failed to marshal html data: %v", err)
		return
	}

	filePath := filepath.Join(s.logDir, "html数据")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Printf("[analysis] failed to write html数据 file: %v", err)
	}
}

func (s *AnalysisService) reconstructEventInMemory(ev *TraceEvent) {
	sess, exists := s.sessions[ev.SessionID]
	if !exists {
		sess = &ConversationSession{
			ID:             ev.SessionID,
			StartTime:      ev.Time,
			EndTime:        ev.Time,
			Turns:          make(map[int]*ConversationTurn),
			GroupingReason: ev.Phase,
		}
		s.sessions[ev.SessionID] = sess
	}

	if ev.Time.Before(sess.StartTime) {
		sess.StartTime = ev.Time
	}
	if ev.Time.After(sess.EndTime) {
		sess.EndTime = ev.Time
	}

	sess.RequestCount++
	sess.PromptTokens += ev.Response.PromptTokens
	sess.CompletionTokens += ev.Response.CompletionTokens
	sess.TotalTokens += ev.Response.TotalTokens
	sess.CacheHitTokens += ev.Response.CacheHitTokens
	sess.CacheMissTokens += ev.Response.CacheMissTokens

	if ev.Status >= 400 {
		sess.Errors++
	}
	if ev.Phase == "retry" {
		sess.Retries++
	}

	foundModel := false
	for _, m := range sess.Models {
		if m == ev.Model {
			foundModel = true
			break
		}
	}
	if !foundModel && ev.Model != "" {
		sess.Models = append(sess.Models, ev.Model)
	}

	turn, turnExists := sess.Turns[ev.TurnID]
	if !turnExists {
		turn = &ConversationTurn{
			TurnID:    ev.TurnID,
			StartTime: ev.Time,
		}
		sess.Turns[ev.TurnID] = turn
	}

	turn.Events = append(turn.Events, ev)

	if ev.Phase == "primary" {
		if ev.Request.LastUserSummary != "" {
			turn.UserMessage = ev.Request.LastUserSummary
		}
		turn.SystemModified = ev.Request.SystemPromptTransformed
		turn.ExtraInjected = ev.Request.ExtraPromptInjected
	}

	if ev.Phase == "primary" || ev.Phase == "retry" {
		if ev.Response.ReasoningContent != "" {
			turn.ReasoningContent = ev.Response.ReasoningContent
		}
		if ev.Response.Content != "" {
			turn.AssistantResponse = ev.Response.Content
		}
		if len(ev.Response.ToolCalls) > 0 {
			turn.ToolCalls = ev.Response.ToolCalls
		}
		if len(ev.ChatHistory) > 0 {
			turn.ChatHistory = ev.ChatHistory
		}
	}

	if len(turn.ChatHistory) == 0 {
		var history []ChatMessage
		if turn.UserMessage != "" {
			history = append(history, ChatMessage{
				Role:    "user",
				Content: turn.UserMessage,
			})
		}
		if turn.AssistantResponse != "" || turn.ReasoningContent != "" || len(turn.ToolCalls) > 0 {
			history = append(history, ChatMessage{
				Role:             "assistant",
				Content:          turn.AssistantResponse,
				ReasoningContent: turn.ReasoningContent,
				ToolCalls:        turn.ToolCalls,
			})
		}
		turn.ChatHistory = history
	}
}

// ClearHistory 清空磁盘日志文件以及内存中的所有分析会话记录
func (s *AnalysisService) ClearHistory() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	files, err := os.ReadDir(s.logDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".jsonl" {
			filePath := filepath.Join(s.logDir, file.Name())
			if err := os.Remove(filePath); err != nil {
				log.Printf("[analysis] failed to delete log file %s: %v", file.Name(), err)
			}
		}
	}

	s.sessions = make(map[string]*ConversationSession)
	log.Printf("[analysis] all analysis history cleared")
	return nil
}

// AssignSessionAndTurn 给新到的请求分配 SessionID 和 TurnID
func (s *AnalysisService) AssignSessionAndTurn(startTime time.Time, rawRequest string, format string, route string) (string, int) {
	if s == nil || !s.config.Get().AnalysisEnabled {
		return "", 0
	}
	// 仅过滤聊天 completions/messages 相关的路由
	if !strings.HasSuffix(route, "/v1/chat/completions") && !strings.HasSuffix(route, "/v1/messages") {
		return "", 0
	}
	s.lock.Lock()
	defer s.lock.Unlock()

	tempEv := &TraceEvent{
		Time:       startTime,
		RawRequest: rawRequest,
		Format:     format,
	}

	s.inferSession(tempEv)

	// 在内存中进行预占，以防止因异步落盘延迟导致并发/流式请求匹配失败
	sess, exists := s.sessions[tempEv.SessionID]
	if !exists {
		sess = &ConversationSession{
			ID:             tempEv.SessionID,
			StartTime:      tempEv.Time,
			EndTime:        tempEv.Time,
			Turns:          make(map[int]*ConversationTurn),
			GroupingReason: "pre-assigned",
		}
		s.sessions[tempEv.SessionID] = sess
	}
	if tempEv.Time.After(sess.EndTime) {
		sess.EndTime = tempEv.Time
	}

	turn, turnExists := sess.Turns[tempEv.TurnID]
	if !turnExists {
		turn = &ConversationTurn{
			TurnID:    tempEv.TurnID,
			StartTime: tempEv.Time,
		}
		sess.Turns[tempEv.TurnID] = turn
	}

	// 提取当前轮次的 UserMessage 写入占位的 Turn 中
	userMsg := getLastUserMessage(tempEv)
	if userMsg != "" && turn.UserMessage == "" {
		turn.UserMessage = userMsg
	}

	return tempEv.SessionID, tempEv.TurnID
}

// 辅助 unmarshal 工具函数
func parseAssistantContent(rawText string, format string) string {
	if rawText == "" {
		return ""
	}

	// 判断是否是流式
	if strings.Contains(rawText, "data: ") {
		var sb strings.Builder
		lines := strings.Split(rawText, "\n")
		for _, line := range lines {
			if delta, err := ParseSSELine(line); err == nil && delta != nil {
				if delta.Content != "" {
					sb.WriteString(delta.Content)
				}
			}
		}
		return sb.String()
	} else {
		// 非流式，单体 JSON
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(rawText), &data); err != nil {
			return ""
		}
		// 兼容 OpenAI
		choices, _ := data["choices"].([]interface{})
		if len(choices) > 0 {
			choice, _ := choices[0].(map[string]interface{})
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				content, _ := msg["content"].(string)
				return content
			}
		}
		// 兼容 Anthropic 非流式
		if contentArr, ok := data["content"].([]interface{}); ok {
			var sb strings.Builder
			for _, item := range contentArr {
				if m, ok := item.(map[string]interface{}); ok {
					if t, _ := m["type"].(string); t == "text" {
						if text, ok := m["text"].(string); ok {
							sb.WriteString(text)
						}
					}
				}
			}
			return sb.String()
		}
		return ""
	}
}

func parseAssistantReasoning(rawText string, format string) string {
	if rawText == "" {
		return ""
	}

	if strings.Contains(rawText, "data: ") {
		var sb strings.Builder
		lines := strings.Split(rawText, "\n")
		for _, line := range lines {
			if delta, err := ParseSSELine(line); err == nil && delta != nil {
				if delta.ReasoningContent != "" {
					sb.WriteString(delta.ReasoningContent)
				}
			}
		}
		return sb.String()
	} else {
		// 非流式
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(rawText), &data); err != nil {
			return ""
		}
		choices, _ := data["choices"].([]interface{})
		if len(choices) > 0 {
			choice, _ := choices[0].(map[string]interface{})
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				reasoning, _ := msg["reasoning_content"].(string)
				return reasoning
			}
		}
		// 兼容 Anthropic 非流式
		if contentArr, ok := data["content"].([]interface{}); ok {
			var sb strings.Builder
			for _, item := range contentArr {
				if m, ok := item.(map[string]interface{}); ok {
					if t, _ := m["type"].(string); t == "thinking" {
						if thinking, ok := m["thinking"].(string); ok {
							sb.WriteString(thinking)
						}
					}
				}
			}
			return sb.String()
		}
		return ""
	}
}

func parseAssistantToolCalls(rawText string, format string) []string {
	if rawText == "" {
		return nil
	}

	var toolCalls []string
	if strings.Contains(rawText, "data: ") {
		if format == "anthropic" {
			type anthropicTcBuilder struct {
				Name  strings.Builder
				Input strings.Builder
			}
			builders := make(map[int]*anthropicTcBuilder)

			lines := strings.Split(rawText, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "data: ") {
					dataStr := strings.TrimPrefix(line, "data: ")
					if dataStr == "[DONE]" {
						continue
					}
					var chunk map[string]interface{}
					if err := json.Unmarshal([]byte(dataStr), &chunk); err == nil {
						cType, _ := chunk["type"].(string)
						idxVal, hasIdx := chunk["index"].(float64)
						idx := int(idxVal)

						if cType == "content_block_start" {
							if cb, ok := chunk["content_block"].(map[string]interface{}); ok {
								cbType, _ := cb["type"].(string)
								if cbType == "tool_use" {
									tb := &anthropicTcBuilder{}
									if name, _ := cb["name"].(string); name != "" {
										tb.Name.WriteString(name)
									}
									builders[idx] = tb
								}
							}
						} else if cType == "content_block_delta" && hasIdx {
							if tb, exists := builders[idx]; exists {
								if delta, ok := chunk["delta"].(map[string]interface{}); ok {
									dType, _ := delta["type"].(string)
									if dType == "input_json_delta" {
										if pj, _ := delta["partial_json"].(string); pj != "" {
											tb.Input.WriteString(pj)
										}
									}
								}
							}
						}
					}
				}
			}

			var keys []int
			for k := range builders {
				keys = append(keys, k)
			}
			sort.Ints(keys)
			for _, k := range keys {
				tb := builders[k]
				toolCalls = append(toolCalls, fmt.Sprintf("%s(%s)", tb.Name.String(), tb.Input.String()))
			}
		} else {
			type tcBuilder struct {
				Name strings.Builder
				Args strings.Builder
			}
			builders := make(map[int]*tcBuilder)

			lines := strings.Split(rawText, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "data: ") {
					dataStr := strings.TrimPrefix(line, "data: ")
					if dataStr == "[DONE]" {
						continue
					}
					var chunk map[string]interface{}
					if err := json.Unmarshal([]byte(dataStr), &chunk); err == nil {
						if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
							if choice, ok := choices[0].(map[string]interface{}); ok {
								if delta, ok := choice["delta"].(map[string]interface{}); ok {
									if tcs, ok := delta["tool_calls"].([]interface{}); ok {
										for _, tc := range tcs {
											tcm, _ := tc.(map[string]interface{})
											idxVal, ok := tcm["index"].(float64)
											if !ok {
												continue
											}
											idx := int(idxVal)
											tb, exists := builders[idx]
											if !exists {
												tb = &tcBuilder{}
												builders[idx] = tb
											}
											if fn, ok := tcm["function"].(map[string]interface{}); ok {
												if name, _ := fn["name"].(string); name != "" {
													tb.Name.WriteString(name)
												}
												if args, _ := fn["arguments"].(string); args != "" {
													tb.Args.WriteString(args)
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
			var keys []int
			for k := range builders {
				keys = append(keys, k)
			}
			sort.Ints(keys)
			for _, k := range keys {
				tb := builders[k]
				toolCalls = append(toolCalls, fmt.Sprintf("%s(%s)", tb.Name.String(), tb.Args.String()))
			}
		}
	} else {
		if format == "anthropic" {
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(rawText), &data); err != nil {
				return nil
			}
			content, _ := data["content"].([]interface{})
			for _, partVal := range content {
				if part, ok := partVal.(map[string]interface{}); ok {
					pType, _ := part["type"].(string)
					if pType == "tool_use" {
						name, _ := part["name"].(string)
						var inputStr string
						if input, ok := part["input"].(map[string]interface{}); ok {
							if b, err := json.Marshal(input); err == nil {
								inputStr = string(b)
							}
						}
						toolCalls = append(toolCalls, fmt.Sprintf("%s(%s)", name, inputStr))
					}
				}
			}
		} else {
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(rawText), &data); err != nil {
				return nil
			}
			choices, _ := data["choices"].([]interface{})
			if len(choices) > 0 {
				choice, _ := choices[0].(map[string]interface{})
				if msg, ok := choice["message"].(map[string]interface{}); ok {
					tcs, _ := msg["tool_calls"].([]interface{})
					for _, tc := range tcs {
						tcm, _ := tc.(map[string]interface{})
						if fn, ok := tcm["function"].(map[string]interface{}); ok {
							name, _ := fn["name"].(string)
							args, _ := fn["arguments"].(string)
							toolCalls = append(toolCalls, fmt.Sprintf("%s(%s)", name, args))
						}
					}
				}
			}
		}
	}
	return toolCalls
}

func formatCacheRatio(hit, miss int) string {
	total := hit + miss
	if total <= 0 {
		return "0%"
	}
	if miss == 0 {
		return "100%"
	}
	if hit == 0 {
		return "0%"
	}
	pct := float64(hit) / float64(total) * 100
	if pct > 99.9 {
		return "99.9%"
	}
	if pct < 0.1 {
		return "0.1%"
	}
	return fmt.Sprintf("%.1f%%", pct)
}


// GetSessionSummaries 获取会话列表摘要，以 StartTime 倒序排序
func (s *AnalysisService) GetSessionSummaries(limit, offset int) []SessionSummary {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var summaries []SessionSummary
	for id, sess := range s.sessions {
		hitTotal := sess.CacheHitTokens + sess.CacheMissTokens
		ratio := 0.0
		if hitTotal > 0 {
			ratio = float64(sess.CacheHitTokens) / float64(hitTotal)
		}

		// 生成 200 字摘要
		var lastUserText string
		var lastAssistantText string
		// 寻找最后一个非空 turn
		var lastTurnID int
		for tid := range sess.Turns {
			if tid > lastTurnID {
				lastTurnID = tid
			}
		}
		if t, ok := sess.Turns[lastTurnID]; ok {
			lastUserText = t.UserMessage
			lastAssistantText = t.AssistantResponse
		}

		summaryText := fmt.Sprintf("User: %s | AI: %s", lastUserText, lastAssistantText)
		if len(summaryText) > 200 {
			summaryText = summaryText[:200] + "..."
		}
		if summaryText == "User:  | AI: " {
			summaryText = "（无对话内容摘要）"
		}

		var format string = "openai"
		var status int = 200
		if len(sess.Turns) > 0 {
			var firstTurnID int = 999999
			var lastTurnIDVal int = 0
			for tid := range sess.Turns {
				if tid < firstTurnID {
					firstTurnID = tid
				}
				if tid > lastTurnIDVal {
					lastTurnIDVal = tid
				}
			}
			if t, ok := sess.Turns[firstTurnID]; ok && len(t.Events) > 0 {
				format = t.Events[0].Format
			}
			if t, ok := sess.Turns[lastTurnIDVal]; ok && len(t.Events) > 0 {
				status = t.Events[len(t.Events)-1].Status
			}
		}

		summaries = append(summaries, SessionSummary{
			ID:               id,
			StartTime:        sess.StartTime,
			EndTime:          sess.EndTime,
			RequestCount:     sess.RequestCount,
			Models:           strings.Join(sess.Models, ", "),
			PromptTokens:     sess.PromptTokens,
			CompletionTokens: sess.CompletionTokens,
			TotalTokens:      sess.TotalTokens,
			CacheHitRatio:    ratio,
			Errors:           sess.Errors,
			Retries:          sess.Retries,
			SummaryText:      summaryText,
			Format:           format,
			Status:           status,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].StartTime.After(summaries[j].StartTime)
	})

	// 分页
	if offset >= len(summaries) {
		return []SessionSummary{}
	}
	end := offset + limit
	if end > len(summaries) {
		end = len(summaries)
	}
	return summaries[offset:end]
}

// GetSessionDetails 获取特定 Session 详情
func (s *AnalysisService) GetSessionDetails(id string) *ConversationSession {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.sessions[id]
}

// ExportMarkdown 导出特定 Session 的 Markdown 报告
func (s *AnalysisService) ExportMarkdown(id string) (string, error) {
	s.lock.RLock()
	sess, exists := s.sessions[id]
	s.lock.RUnlock()

	if !exists {
		return "", fmt.Errorf("session %s not found", id)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# DSPlus 会话诊断报告 (#%s)\n\n", sess.ID))

	// 1. 摘要信息
	sb.WriteString("## 1. 基础摘要\n\n")
	sb.WriteString(fmt.Sprintf("- **开始时间**: %s\n", sess.StartTime.Local().Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("- **结束时间**: %s\n", sess.EndTime.Local().Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("- **请求次数**: %d 次\n", sess.RequestCount))
	sb.WriteString(fmt.Sprintf("- **关联模型**: %s\n", strings.Join(sess.Models, ", ")))
	sb.WriteString(fmt.Sprintf("- **总 Token 消耗**: %d (输入: %d, 输出: %d)\n", sess.TotalTokens, sess.PromptTokens, sess.CompletionTokens))

	ratioStr := formatCacheRatio(sess.CacheHitTokens, sess.CacheMissTokens)
	sb.WriteString(fmt.Sprintf("- **缓存命中率**: %s (命中: %d, 未命中: %d)\n", ratioStr, sess.CacheHitTokens, sess.CacheMissTokens))
	sb.WriteString(fmt.Sprintf("- **系统异常数**: %d\n", sess.Errors))
	sb.WriteString(fmt.Sprintf("- **反循环重试数**: %d\n\n", sess.Retries))

	// 2. 对话时间线
	sb.WriteString("## 2. 轮次对话时间线\n\n")

	// 排序 Turns
	var keys []int
	for k := range sess.Turns {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	for _, k := range keys {
		turn := sess.Turns[k]
		sb.WriteString(fmt.Sprintf("### 轮次 %d (%s)\n\n", turn.TurnID, turn.StartTime.Local().Format("15:04:05")))

		if turn.UserMessage != "" {
			sb.WriteString(fmt.Sprintf("**🧑 用户**: %s\n\n", turn.UserMessage))
		}

		// 检查系统修改
		if turn.SystemModified || turn.ExtraInjected {
			sb.WriteString("> **⚙️ 系统干预**:\n")
			if turn.SystemModified {
				sb.WriteString("> - System Prompt 拼接位置已重组优化\n")
			}
			if turn.ExtraInjected {
				sb.WriteString("> - 注入了额外定义的最高准则指令\n")
			}
			sb.WriteString("\n")
		}

		// 输出干预事件流
		for _, ev := range turn.Events {
			if ev.Phase == "analyzer" {
				sb.WriteString(fmt.Sprintf("> **🔍 反循环分析器**: 判定结果=`%s` | `%s`\n", ev.Response.AnalyzerJudgment, ev.Response.FinishReason))
			} else if ev.Phase == "retry" {
				sb.WriteString(fmt.Sprintf("> **🔄 自动重试**: 重试模型=`%s` | `%s` | 耗时=%dms\n", ev.Response.RetryModel, ev.Response.FinishReason, ev.LatencyMs))
			}
		}

		if turn.ReasoningContent != "" {
			sb.WriteString("<details>\n<summary>🧠 <b>模型深度思考过程</b></summary>\n\n")
			sb.WriteString("```\n" + turn.ReasoningContent + "\n```\n")
			sb.WriteString("</details>\n\n")
		}

		if len(turn.ToolCalls) > 0 {
			sb.WriteString("**🛠️ 工具调用**:\n")
			for _, tc := range turn.ToolCalls {
				sb.WriteString(fmt.Sprintf("- `%s`\n", tc))
			}
			sb.WriteString("\n")
		}

		if turn.AssistantResponse != "" {
			sb.WriteString(fmt.Sprintf("**🤖 模型回复**:\n\n%s\n\n", turn.AssistantResponse))
		}

		sb.WriteString("---\n\n")
	}

	return sb.String(), nil
}

// buildRequestMeta 构造 RequestMeta 元数据，供代理层调用
func buildRequestMeta(data map[string]interface{}, format string, transformed, extraInjected bool) RequestMeta {
	meta := RequestMeta{
		SystemPromptTransformed: transformed,
		ExtraPromptInjected:     extraInjected,
		RoleCounts:              make(map[string]int),
	}
	if data == nil {
		return meta
	}

	meta.Model, _ = data["model"].(string)
	meta.Stream, _ = data["stream"].(bool)

	if thinking, ok := data["thinking"].(map[string]interface{}); ok {
		meta.ThinkingMode, _ = thinking["type"].(string)
	}
	meta.ReasoningEffort, _ = data["reasoning_effort"].(string)
	if outputCfg, ok := data["output_config"].(map[string]interface{}); ok {
		if effort, ok := outputCfg["effort"].(string); ok {
			meta.ReasoningEffort = effort
		}
	}
	if mt, ok := data["max_tokens"].(float64); ok {
		meta.MaxTokens = int(mt)
	}

	// 提取 tools
	if toolsArr, ok := data["tools"].([]interface{}); ok {
		var tools []string
		for _, tVal := range toolsArr {
			if tMap, ok := tVal.(map[string]interface{}); ok {
				if tType, _ := tMap["type"].(string); tType == "function" {
					if fnMap, ok := tMap["function"].(map[string]interface{}); ok {
						if fnName, _ := fnMap["name"].(string); fnName != "" {
							tools = append(tools, fnName)
						}
					}
				}
			}
		}
		meta.Tools = tools
	}

	messages, ok := data["messages"].([]interface{})
	if ok {
		meta.MessageCount = len(messages)
		for _, msg := range messages {
			m, ok := msg.(map[string]interface{})
			if !ok {
				continue
			}
			role, _ := m["role"].(string)
			meta.RoleCounts[role]++
		}
		// 提取最后一条 user 消息
		for i := len(messages) - 1; i >= 0; i-- {
			m, ok := messages[i].(map[string]interface{})
			if !ok {
				continue
			}
			role, _ := m["role"].(string)
			if role == "user" {
				if content, ok := m["content"].(string); ok {
					meta.LastUserSummary = content
				} else if contentArr, ok := m["content"].([]interface{}); ok {
					// 兼容多模态/列表格式的 content 字段提取文本
					var textParts []string
					for _, part := range contentArr {
						if pMap, ok := part.(map[string]interface{}); ok {
							if txt, ok := pMap["text"].(string); ok {
								textParts = append(textParts, txt)
							}
						}
					}
					meta.LastUserSummary = strings.Join(textParts, " ")
				}
				break
			}
		}
	}
	return meta
}

func detectModel(body string) string {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(body), &data); err == nil {
		if model, ok := data["model"].(string); ok {
			return model
		}
	}
	return "deepseek-chat"
}

func extractChatHistory(rawRequest string, format string, finalReply string, finalReasoning string, finalTools []string) []ChatMessage {
	var history []ChatMessage
	if rawRequest == "" {
		return history
	}

	var body map[string]interface{}
	if err := json.Unmarshal([]byte(rawRequest), &body); err != nil {
		return history
	}

	messages, ok := body["messages"].([]interface{})
	if !ok {
		return history
	}

	for _, msgVal := range messages {
		msg, ok := msgVal.(map[string]interface{})
		if !ok {
			continue
		}

		role, _ := msg["role"].(string)
		content := ""
		reasoning := ""
		var toolCalls []string

		if cStr, ok := msg["content"].(string); ok {
			content = cStr
		} else if cArr, ok := msg["content"].([]interface{}); ok {
			var textParts []string
			for _, part := range cArr {
				if pMap, ok := part.(map[string]interface{}); ok {
					pType, _ := pMap["type"].(string)
					if pType == "text" {
						if txt, ok := pMap["text"].(string); ok {
							textParts = append(textParts, txt)
						}
					} else if pType == "thinking" {
						if thk, ok := pMap["thinking"].(string); ok {
							reasoning = thk
						}
					} else if pType == "tool_result" {
						role = "tool"
						if toolRes, ok := pMap["content"].(string); ok {
							content = toolRes
						}
					} else if pType == "tool_use" {
						name, _ := pMap["name"].(string)
						var inputStr string
						if input, ok := pMap["input"].(map[string]interface{}); ok {
							if b, err := json.Marshal(input); err == nil {
								inputStr = string(b)
							}
						}
						toolCalls = append(toolCalls, fmt.Sprintf("%s(%s)", name, inputStr))
					}
				}
			}
			if len(textParts) > 0 {
				content = strings.Join(textParts, " ")
			}
		}

		if rc, ok := msg["reasoning_content"].(string); ok && rc != "" {
			reasoning = rc
		}
		if tcs, ok := msg["tool_calls"].([]interface{}); ok {
			for _, tcVal := range tcs {
				if tc, ok := tcVal.(map[string]interface{}); ok {
					if fn, ok := tc["function"].(map[string]interface{}); ok {
						name, _ := fn["name"].(string)
						args, _ := fn["arguments"].(string)
						toolCalls = append(toolCalls, fmt.Sprintf("%s(%s)", name, args))
					}
				}
			}
		}

		history = append(history, ChatMessage{
			Role:             role,
			Content:          content,
			ReasoningContent: reasoning,
			ToolCalls:        toolCalls,
		})
	}

	if finalReply != "" || finalReasoning != "" || len(finalTools) > 0 {
		history = append(history, ChatMessage{
			Role:             "assistant",
			Content:          finalReply,
			ReasoningContent: finalReasoning,
			ToolCalls:        finalTools,
		})
	}

	return history
}

