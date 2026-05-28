package log

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

// Level represents a log severity.
type Level string

const (
	Debug Level = "DEBUG"
	Info  Level = "INFO"
	Warn  Level = "WARN"
	Error Level = "ERROR"
)

// Logger emits structured JSON log lines to stderr.
type Logger struct {
	mu     sync.Mutex
	minLvl Level
}

// New returns a logger that writes at or above minLvl.
func New(minLvl Level) *Logger {
	return &Logger{minLvl: minLvl}
}

func (l *Logger) log(level Level, msg string, fields map[string]interface{}) {
	if !l.enabled(level) {
		return
	}

	_, file, line, ok := runtime.Caller(2)
	caller := "unknown"
	if ok {
		// Shorten: just filename:line
		for i := len(file) - 1; i >= 0; i-- {
			if file[i] == '/' {
				file = file[i+1:]
				break
			}
		}
		caller = fmt.Sprintf("%s:%d", file, line)
	}

	rec := map[string]interface{}{
		"ts":     time.Now().UTC().Format(time.RFC3339),
		"lvl":    string(level),
		"msg":    msg,
		"caller": caller,
	}
	for k, v := range fields {
		rec[k] = v
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	enc := json.NewEncoder(os.Stderr)
	enc.Encode(rec)
}

func (l *Logger) enabled(level Level) bool {
	order := map[Level]int{Debug: 0, Info: 1, Warn: 2, Error: 3}
	return order[level] >= order[l.minLvl]
}

func (l *Logger) Debug(msg string, fields map[string]interface{}) { l.log(Debug, msg, fields) }
func (l *Logger) Info(msg string, fields map[string]interface{})  { l.log(Info, msg, fields) }
func (l *Logger) Warn(msg string, fields map[string]interface{})  { l.log(Warn, msg, fields) }
func (l *Logger) Error(msg string, fields map[string]interface{}) { l.log(Error, msg, fields) }
