package bootstatus

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

type recordedReporter struct {
	mu      sync.Mutex
	begins  []Status
	prog    []Status
	ends    []Phase
	fails   []failure
	current Status
}

type failure struct {
	phase Phase
	err   error
}

func (r *recordedReporter) Begin(phase Phase, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current = Status{Phase: phase, Message: message}
	r.begins = append(r.begins, r.current)
}
func (r *recordedReporter) Progress(done, total int, item string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current.Done = done
	r.current.Total = total
	r.current.Item = item
	r.prog = append(r.prog, r.current)
}
func (r *recordedReporter) End(phase Phase) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, phase)
}
func (r *recordedReporter) Fail(phase Phase, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fails = append(r.fails, failure{phase, err})
}
func (r *recordedReporter) Snapshot() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

func TestMultiplexerFanOut(t *testing.T) {
	a := &recordedReporter{}
	b := &recordedReporter{}
	mux := NewMultiplexer(a, nil, b)

	mux.Begin(PhaseDuckDB, "Loading extensions")
	mux.Progress(2, 5, "postgres")
	mux.End(PhaseDuckDB)
	mux.Fail(PhaseRendering, errors.New("boom"))

	for _, r := range []*recordedReporter{a, b} {
		if len(r.begins) != 1 || r.begins[0].Phase != PhaseDuckDB {
			t.Errorf("expected one Begin for PhaseDuckDB, got %#v", r.begins)
		}
		if len(r.prog) != 1 || r.prog[0].Done != 2 || r.prog[0].Total != 5 || r.prog[0].Item != "postgres" {
			t.Errorf("expected progress (2/5 postgres), got %#v", r.prog)
		}
		if len(r.ends) != 1 || r.ends[0] != PhaseDuckDB {
			t.Errorf("expected End(PhaseDuckDB), got %#v", r.ends)
		}
		if len(r.fails) != 1 || r.fails[0].phase != PhaseRendering || r.fails[0].err == nil {
			t.Errorf("expected Fail(PhaseRendering), got %#v", r.fails)
		}
	}

	snap := mux.Snapshot()
	if snap.Phase != PhaseError || snap.Error != "boom" {
		t.Errorf("snapshot should reflect last Fail: %#v", snap)
	}
}

func TestFormatProgressMessage(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		d, t int
		item string
		want string
	}{
		{"both", "Loading", 2, 5, "postgres", "Loading (2/5) postgres"},
		{"counts only", "Loading", 2, 5, "", "Loading (2/5)"},
		{"item only", "Parsing", 0, 0, "report.yaml", "Parsing report.yaml"},
		{"plain", "Starting", 0, 0, "", "Starting"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatProgressMessage(c.msg, c.d, c.t, c.item)
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

type recordingBus struct {
	mu       sync.Mutex
	payloads [][]byte
}

func (b *recordingBus) BroadcastBootStatus(payload []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.payloads = append(b.payloads, append([]byte(nil), payload...))
}

func TestSSEReporterBroadcastsAndSnapshots(t *testing.T) {
	bus := &recordingBus{}
	r := NewSSEReporter(bus)

	r.Begin(PhaseManifests, "Loading manifests")
	r.Progress(3, 10, "report.yaml")
	r.End(PhaseManifests)
	r.Fail(PhaseRendering, errors.New("kaput"))

	bus.mu.Lock()
	defer bus.mu.Unlock()
	if len(bus.payloads) != 3 {
		t.Fatalf("expected 3 broadcasts (Begin, Progress, Fail) — End is silent, got %d: %s", len(bus.payloads), bus.payloads)
	}
	statuses := make([]Status, len(bus.payloads))
	for i, p := range bus.payloads {
		if err := jsonUnmarshal(p, &statuses[i]); err != nil {
			t.Fatalf("payload %d not JSON: %v", i, err)
		}
	}
	if statuses[0].Phase != PhaseManifests {
		t.Errorf("first broadcast phase: %v", statuses[0].Phase)
	}
	if statuses[1].Done != 3 || statuses[1].Total != 10 {
		t.Errorf("progress broadcast wrong: %#v", statuses[1])
	}
	if statuses[2].Phase != PhaseError || statuses[2].Error != "kaput" {
		t.Errorf("fail broadcast: %#v", statuses[2])
	}

	if r.Snapshot().Phase != PhaseError {
		t.Errorf("snapshot should reflect Fail: %#v", r.Snapshot())
	}
}
