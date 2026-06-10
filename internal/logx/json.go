package logx

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// JSONLogger emits one JSON object per line, suitable for log aggregation
// when `bino serve` runs as a production service. It mirrors the Terminal
// logger's stream routing: info/success/debug to stdout, warn/error to
// stderr, so piped command output behaves the same in both formats.
type JSONLogger struct {
	core    *jsonCore
	channel string
}

type jsonCore struct {
	stdout  io.Writer
	stderr  io.Writer
	verbose bool
	mu      sync.Mutex
}

// NewJSON creates a JSONLogger writing to the given streams. Debugf output
// is emitted only when verbose is true, matching the Terminal logger.
func NewJSON(stdout, stderr io.Writer, verbose bool) *JSONLogger {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return &JSONLogger{core: &jsonCore{stdout: stdout, stderr: stderr, verbose: verbose}}
}

func (j *JSONLogger) Channel(name string) Logger {
	if j == nil || j.core == nil {
		return Nop()
	}
	if name == "" {
		return j
	}
	channel := name
	if j.channel != "" {
		channel = j.channel + "/" + name
	}
	return &JSONLogger{core: j.core, channel: channel}
}

func (j *JSONLogger) Infof(format string, args ...any) {
	j.write(j.core.stdout, "info", format, args...)
}

func (j *JSONLogger) Successf(format string, args ...any) {
	j.write(j.core.stdout, "info", format, args...)
}

func (j *JSONLogger) Warnf(format string, args ...any) {
	j.write(j.core.stderr, "warn", format, args...)
}

func (j *JSONLogger) Errorf(format string, args ...any) {
	j.write(j.core.stderr, "error", format, args...)
}

func (j *JSONLogger) Debugf(format string, args ...any) {
	if j == nil || j.core == nil || !j.core.verbose {
		return
	}
	j.write(j.core.stdout, "debug", format, args...)
}

func (j *JSONLogger) write(out io.Writer, level, format string, args ...any) {
	if j == nil || j.core == nil {
		return
	}
	entry := struct {
		Time    string `json:"time"`
		Level   string `json:"level"`
		Channel string `json:"channel,omitempty"`
		Msg     string `json:"msg"`
	}{
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Level:   level,
		Channel: j.channel,
		Msg:     fmt.Sprintf(format, args...),
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	j.core.mu.Lock()
	defer j.core.mu.Unlock()
	fmt.Fprintf(out, "%s\n", line)
}
