package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

var traceFile *os.File

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
	ts := time.Now().Format("15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] %s\n", ts, msg)
	traceFile.WriteString(line)
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
	ts := time.Now().Format("15:04:05.000")
	s := fmt.Sprintf("[%s]", ts)
	for i := 0; i+1 < len(pairs); i += 2 {
		s += fmt.Sprintf(" %v=%-v", pairs[i], pairs[i+1])
	}
	s += "\n"
	traceFile.WriteString(s)
	// Also log to console for convenience
	log.Print(s)
}
