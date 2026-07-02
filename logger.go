package main

import (
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type TokenUsage struct {
	Prompt     int64 `json:"prompt"`
	Completion int64 `json:"completion"`
	Total      int64 `json:"total"`
	CacheHit   int64 `json:"cache_hit"`
	CacheMiss  int64 `json:"cache_miss"`
}

type LogEntry struct {
	ID               int64             `json:"id"`
	Time             time.Time         `json:"time"`
	Format           string            `json:"format"`
	RequestType      string            `json:"request_type,omitempty"`  // "" = proxy, "antiloop_analyzer", "antiloop_retry"
	Method           string            `json:"method"`
	Path             string            `json:"path"`
	StatusCode       int               `json:"status_code"`
	LatencyMs        int64             `json:"latency_ms"`
	Stream           bool              `json:"stream"`
	Transformed      bool              `json:"transformed"`
	HasSystemPrompt  bool              `json:"has_system_prompt"`
	SemanticType     string            `json:"semantic_type,omitempty"`
	ResponseHeaders  map[string]string `json:"response_headers,omitempty"`
	OriginalBody     string            `json:"original_body,omitempty"`
	TransformedBody  string            `json:"transformed_body,omitempty"`
	ResponseBody     string            `json:"response_body,omitempty"`
	TokenUsage       *TokenUsage       `json:"token_usage,omitempty"`
	Status           string            `json:"status,omitempty"`
}

type Logger struct {
	mu          sync.RWMutex
	entries     []LogEntry
	maxSize     int
	nextID      int64
	subscriber  chan LogEntry
}

func NewLogger(maxSize int) *Logger {
	return &Logger{
		entries:    make([]LogEntry, 0, maxSize),
		maxSize:    maxSize,
		subscriber: make(chan LogEntry, 100),
	}
}

func (l *Logger) Add(e LogEntry) int64 {
	id := atomic.AddInt64(&l.nextID, 1)
	e.ID = id
	if e.Time.IsZero() {
		e.Time = time.Now()
	}

	l.mu.Lock()
	if len(l.entries) >= l.maxSize {
		l.entries = l.entries[1:]
	}
	l.entries = append(l.entries, e)
	l.mu.Unlock()

	select {
	case l.subscriber <- e:
	default:
	}
	return id
}

func (l *Logger) UpdateLastResponse(id int64, respBody string) {
	l.mu.Lock()
	var matchedEntry *LogEntry
	for i := len(l.entries) - 1; i >= 0; i-- {
		if l.entries[i].ID == id {
			l.entries[i].ResponseBody = respBody
			matchedEntry = &l.entries[i]
			break
		}
	}
	l.mu.Unlock()

	if matchedEntry != nil {
		l.writeDebugLog(*matchedEntry)
	}
}

func (l *Logger) UpdateTokenUsage(id int64, usage *TokenUsage) {
	l.UpdateTokenUsageAndStatus(id, usage, "")
}

func (l *Logger) UpdateTokenUsageAndStatus(id int64, usage *TokenUsage, status string) {
	l.mu.Lock()
	for i := len(l.entries) - 1; i >= 0; i-- {
		if l.entries[i].ID == id {
			l.entries[i].TokenUsage = usage
			if status != "" {
				l.entries[i].Status = status
			}
			// broadcast updated entry so frontend gets token stats without refresh
			select {
			case l.subscriber <- l.entries[i]:
			default:
			}
			l.mu.Unlock()
			return
		}
	}
	l.mu.Unlock()
}

func (l *Logger) UpdateOnResponse(id int64, statusCode int, latencyMs int64, status string, headers map[string]string, originalBody, transformedBody string) {
	l.mu.Lock()
	var matchedEntry *LogEntry
	for i := len(l.entries) - 1; i >= 0; i-- {
		if l.entries[i].ID == id {
			l.entries[i].StatusCode = statusCode
			l.entries[i].LatencyMs = latencyMs
			if status != "" {
				l.entries[i].Status = status
			}
			if headers != nil {
				l.entries[i].ResponseHeaders = headers
			}
			if originalBody != "" {
				l.entries[i].OriginalBody = originalBody
			}
			if transformedBody != "" {
				l.entries[i].TransformedBody = transformedBody
			}
			select {
			case l.subscriber <- l.entries[i]:
			default:
			}
			matchedEntry = &l.entries[i]
			break
		}
	}
	l.mu.Unlock()

	if matchedEntry != nil && matchedEntry.Status == "completed" && (matchedEntry.StatusCode >= 400 || matchedEntry.ResponseBody == "") {
		l.writeDebugLog(*matchedEntry)
	}
}

func (l *Logger) UpdateSemanticType(id int64, semanticType string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.entries) - 1; i >= 0; i-- {
		if l.entries[i].ID == id {
			l.entries[i].SemanticType = semanticType
			select {
			case l.subscriber <- l.entries[i]:
			default:
			}
			return
		}
	}
}

func (l *Logger) Subscribe() <-chan LogEntry {
	return l.subscriber
}

func (l *Logger) GetAll(limit, offset int) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	start := len(l.entries) - offset - limit
	if start < 0 {
		start = 0
	}
	end := len(l.entries) - offset
	if end > len(l.entries) {
		end = len(l.entries)
	}
	if start >= end {
		return nil
	}

	result := make([]LogEntry, end-start)
	for i, entry := range l.entries[start:end] {
		entryCopy := entry
		if entryCopy.TokenUsage != nil {
			usageCopy := *entryCopy.TokenUsage
			entryCopy.TokenUsage = &usageCopy
		}
		if entryCopy.ResponseHeaders != nil {
			headersCopy := make(map[string]string, len(entryCopy.ResponseHeaders))
			for k, v := range entryCopy.ResponseHeaders {
				headersCopy[k] = v
			}
			entryCopy.ResponseHeaders = headersCopy
		}
		result[i] = entryCopy
	}

	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func (l *Logger) Get(id int64) *LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for i := len(l.entries) - 1; i >= 0; i-- {
		if l.entries[i].ID == id {
			entryCopy := l.entries[i]
			if entryCopy.TokenUsage != nil {
				usageCopy := *entryCopy.TokenUsage
				entryCopy.TokenUsage = &usageCopy
			}
			if entryCopy.ResponseHeaders != nil {
				headersCopy := make(map[string]string, len(entryCopy.ResponseHeaders))
				for k, v := range entryCopy.ResponseHeaders {
					headersCopy[k] = v
				}
				entryCopy.ResponseHeaders = headersCopy
			}
			return &entryCopy
		}
	}
	return nil
}

func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = l.entries[:0]
}

func (l *Logger) Stats() map[string]int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	total := len(l.entries)
	today := 0
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	for _, e := range l.entries {
		if e.Time.After(todayStart) {
			today++
		}
	}

	return map[string]int{
		"total": total,
		"today": today,
	}
}

func (l *Logger) writeDebugLog(e LogEntry) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	_ = os.MkdirAll("test", 0755)
	f, err := os.OpenFile("test/proxy_debug_logs.jsonl", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}
