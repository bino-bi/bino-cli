package logx

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type jsonLine struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Channel string `json:"channel"`
	Msg     string `json:"msg"`
}

func decodeLine(t *testing.T, buf *bytes.Buffer) jsonLine {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	var entry jsonLine
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("output is not a JSON line: %q: %v", line, err)
	}
	return entry
}

func TestJSONLogger_Levels(t *testing.T) {
	tests := []struct {
		name      string
		log       func(Logger)
		wantLevel string
		wantMsg   string
		stderr    bool
	}{
		{name: "info", log: func(l Logger) { l.Infof("hello %s", "world") }, wantLevel: "info", wantMsg: "hello world"},
		{name: "success", log: func(l Logger) { l.Successf("done") }, wantLevel: "info", wantMsg: "done"},
		{name: "warn", log: func(l Logger) { l.Warnf("careful") }, wantLevel: "warn", wantMsg: "careful", stderr: true},
		{name: "error", log: func(l Logger) { l.Errorf("boom") }, wantLevel: "error", wantMsg: "boom", stderr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			tt.log(NewJSON(&stdout, &stderr, false))

			out := &stdout
			if tt.stderr {
				out = &stderr
				if stdout.Len() != 0 {
					t.Errorf("unexpected stdout output: %q", stdout.String())
				}
			} else if stderr.Len() != 0 {
				t.Errorf("unexpected stderr output: %q", stderr.String())
			}

			entry := decodeLine(t, out)
			if entry.Level != tt.wantLevel {
				t.Errorf("level = %q, want %q", entry.Level, tt.wantLevel)
			}
			if entry.Msg != tt.wantMsg {
				t.Errorf("msg = %q, want %q", entry.Msg, tt.wantMsg)
			}
			if entry.Time == "" {
				t.Error("time field is empty")
			}
		})
	}
}

func TestJSONLogger_ChannelNesting(t *testing.T) {
	var stdout, stderr bytes.Buffer
	logger := NewJSON(&stdout, &stderr, false).Channel("serve").Channel("server")
	logger.Infof("ready")

	entry := decodeLine(t, &stdout)
	if entry.Channel != "serve/server" {
		t.Errorf("channel = %q, want %q", entry.Channel, "serve/server")
	}
}

func TestJSONLogger_DebugGatedOnVerbose(t *testing.T) {
	var stdout, stderr bytes.Buffer
	NewJSON(&stdout, &stderr, false).Debugf("hidden")
	if stdout.Len() != 0 {
		t.Errorf("debug emitted without verbose: %q", stdout.String())
	}

	NewJSON(&stdout, &stderr, true).Debugf("shown")
	entry := decodeLine(t, &stdout)
	if entry.Level != "debug" || entry.Msg != "shown" {
		t.Errorf("entry = %+v, want debug/shown", entry)
	}
}

func TestJSONLoggerRecordFields(t *testing.T) {
	var stdout, stderr bytes.Buffer
	l := NewJSON(&stdout, &stderr, false).Channel("build").(*JSONLogger)
	l.Record("info", "Generated r", map[string]any{"path": "/out/r.pdf", "msg": "ignored"})
	l.Record("error", "boom", map[string]any{"line": 3})

	var info map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatalf("stdout is not one JSON line: %q", stdout.String())
	}
	if info["msg"] != "Generated r" || info["path"] != "/out/r.pdf" || info["channel"] != "build" || info["level"] != "info" {
		t.Errorf("info record = %v", info)
	}
	var errRec map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &errRec); err != nil {
		t.Fatalf("stderr is not one JSON line: %q", stderr.String())
	}
	if errRec["line"] != float64(3) || errRec["level"] != "error" {
		t.Errorf("error record = %v", errRec)
	}
}
