package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

var (
	traceFile   *os.File
	traceMu     sync.Mutex
	traceReqSeq atomic.Int64
)

// nextTraceReqID returns a monotonically increasing request ID for correlating
// trace events that belong to the same request.
func nextTraceReqID() int64 {
	return traceReqSeq.Add(1)
}

func initTrace() {
	exe, _ := os.Executable()
	path := filepath.Join(filepath.Dir(exe), "antiloop_trace.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	traceFile = f
	trace("=== session start ===")
}

func trace(format string, args ...interface{}) {
	if traceFile == nil {
		return
	}
	ts := time.Now().Format("2006/01/02 15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] %s\n", ts, msg)
	traceMu.Lock()
	traceFile.WriteString(line)
	traceMu.Unlock()
}

// tracelog writes to BOTH the trace file and the standard logger.
func tracelog(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Print(msg)
	trace("%s", msg)
}

func traceKeyvals(pairs ...interface{}) {
	if traceFile == nil {
		return
	}
	ts := time.Now().Format("2006/01/02 15:04:05.000")
	s := fmt.Sprintf("[%s]", ts)
	for i := 0; i+1 < len(pairs); i += 2 {
		s += fmt.Sprintf(" %v=%-v", pairs[i], pairs[i+1])
	}
	s += "\n"
	traceMu.Lock()
	traceFile.WriteString(s)
	traceMu.Unlock()
	// Also log to console for convenience
	log.Print(s)
}

// startProgressWatchdog logs a warning if stopCh is not closed within timeout.
// Returns the stopCh so the caller can close it when progress is made.
func startProgressWatchdog(name string, timeout time.Duration) chan<- struct{} {
	stopCh := make(chan struct{})
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			log.Printf("[watchdog] %s: no progress for %v", name, timeout)
			trace("watchdog %s: no progress for %v", name, timeout)
		case <-stopCh:
		}
	}()
	return stopCh
}

// resetProgressWatchdog resets the timer by closing the old stop channel and creating a new one.
func resetProgressWatchdog(stopCh chan<- struct{}) chan<- struct{} {
	close(stopCh)
	return startProgressWatchdog("stream", 60*time.Second)
}
