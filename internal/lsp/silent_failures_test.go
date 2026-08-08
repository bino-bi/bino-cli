package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
)

type failingBackend struct{ *fakeBackend }

func (f *failingBackend) MergedSchema(context.Context) (json.RawMessage, error) {
	return nil, errors.New("backend down")
}

func (f *failingBackend) Index(context.Context) ([]IndexDoc, error) {
	return nil, errors.New("backend down")
}

// Regression: schema/index fetch failures were logged at Debug, which an
// editor-spawned server (no --verbose) never shows — completion silently
// returned nothing with no trail. They must be visible without verbose.
func TestSchemaAndIndexFailuresLoggedWithoutVerbose(t *testing.T) {
	var out, errOut bytes.Buffer
	log := logx.NewTerminalWithColor(&out, &errOut, false, true).Channel("test")
	s := NewServer(&failingBackend{&fakeBackend{}}, log, true, "/proj")

	_ = s.getSchema(context.Background())
	_ = s.getIndex(context.Background())

	logged := out.String() + errOut.String()
	if !strings.Contains(logged, "merged schema unavailable") {
		t.Errorf("schema fetch failure left no non-verbose log line; got: %q", logged)
	}
	if !strings.Contains(logged, "index unavailable") {
		t.Errorf("index fetch failure left no non-verbose log line; got: %q", logged)
	}
}
