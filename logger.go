package main

import (
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
	ID               int64         `json:"id"`
	Time             time.Time     `json:"time"`
	Format           string        `json:"format"`
	Method           string        `json:"method"`
	Path             string        `json:"path"`
	StatusCode       int           `json:"status_code"`
	LatencyMs        int64         `json:"latency_ms"`
	Transformed      bool          `json:"transformed"`
	HasSystemPrompt  bool          `json:"has_system_prompt"`
	ResponseHeaders  map[string]string `json:"response_headers,omitempty"`
	OriginalBody     string        `json:"original_body,omitempty"`
	TransformedBody  string        `json:"transformed_body,omitempty"`
	ResponseBody     string        `json:"response_body,omitempty"`
	TokenUsage       *TokenUsage   `json:"token_usage,omitempty"`
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
	defer l.mu.Unlock()
	for i := len(l.entries) - 1; i >= 0; i-- {
		if l.entries[i].ID == id {
			l.entries[i].ResponseBody = respBody
			return
		}
	}
}

func (l *Logger) UpdateTokenUsage(id int64, usage *TokenUsage) {
	l.mu.Lock()
	for i := len(l.entries) - 1; i >= 0; i-- {
		if l.entries[i].ID == id {
			l.entries[i].TokenUsage = usage
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
	copy(result, l.entries[start:end])

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
			return &l.entries[i]
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
