package buildlog_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bino.bi/bino/internal/report/buildlog"
	"bino.bi/bino/pkg/duckdb"
)

// =============================================================================
// EmbedOptions Tests
// =============================================================================

func TestDefaultEmbedOptions(t *testing.T) {
	opts := buildlog.DefaultEmbedOptions()

	tests := []struct {
		name     string
		got      any
		expected any
	}{
		{"Enable", opts.Enable, true},
		{"MaxRows", opts.MaxRows, 10},
		{"MaxBytes", opts.MaxBytes, 65536},
		{"Base64", opts.Base64, true},
		{"Redact", opts.Redact, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("DefaultEmbedOptions().%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

// =============================================================================
// IsSensitiveColumn Tests
// =============================================================================

func TestIsSensitiveColumn(t *testing.T) {
	tests := []struct {
		name      string
		column    string
		sensitive bool
	}{
		// Sensitive patterns
		{"password", "password", true},
		{"user_password", "user_password", true},
		{"passwd", "passwd", true},
		{"secret", "secret", true},
		{"api_secret", "api_secret", true},
		{"token", "token", true},
		{"access_token", "access_token", true},
		{"api_key", "api_key", true},
		{"private", "private", true},
		{"private_key", "private_key", true},
		{"credential", "credential", true},
		{"credentials", "credentials", true},
		{"auth", "auth", true},
		{"auth_token", "auth_token", true},
		{"key", "key", true},
		{"encryption_key", "encryption_key", true},

		// Case insensitivity
		{"PASSWORD_upper", "PASSWORD", true},
		{"Password_mixed", "Password", true},
		{"API_KEY_upper", "API_KEY", true},
		{"SECRET_KEY_mixed", "Secret_Key", true},

		// Non-sensitive
		{"id", "id", false},
		{"name", "name", false},
		{"email", "email", false},
		{"user_id", "user_id", false},
		{"created_at", "created_at", false},
		{"amount", "amount", false},
		{"description", "description", false},
		{"status", "status", false},
		// Note: "keyboard" and "monkey" contain "key" as substring, so they are sensitive
		{"keyboard", "keyboard", true},             // contains "key" substring
		{"monkey", "monkey", true},                 // contains "key" substring
		{"authentication", "authentication", true}, // contains "auth"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildlog.IsSensitiveColumn(tt.column)
			if got != tt.sensitive {
				t.Errorf("IsSensitiveColumn(%q) = %v, want %v", tt.column, got, tt.sensitive)
			}
		})
	}
}

// =============================================================================
// RedactValue Tests
// =============================================================================

func TestRedactValue(t *testing.T) {
	tests := []struct {
		name     string
		column   string
		value    string
		expected string
	}{
		// Sensitive columns should be redacted
		{"password_redacted", "password", "supersecret123", "[REDACTED]"},
		{"api_key_redacted", "api_key", "sk-1234567890abcdef", "[REDACTED]"},
		{"token_redacted", "access_token", "jwt-token-here", "[REDACTED]"},
		{"secret_redacted", "secret", "my-secret-value", "[REDACTED]"},

		// Non-sensitive columns should pass through
		{"id_passthrough", "id", "12345", "12345"},
		{"name_passthrough", "name", "John Doe", "John Doe"},
		{"email_passthrough", "email", "john@example.com", "john@example.com"},
		{"empty_value", "id", "", ""},

		// Edge cases
		{"empty_column_name", "", "value", "value"},
		{"whitespace_value", "name", "   ", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildlog.RedactValue(tt.column, tt.value)
			if got != tt.expected {
				t.Errorf("RedactValue(%q, %q) = %q, want %q", tt.column, tt.value, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// BuildCSV Tests
// =============================================================================

func TestBuildCSV_Disabled(t *testing.T) {
	opts := buildlog.EmbedOptions{
		Enable: false,
	}
	columns := []string{"id", "name"}
	rows := [][]string{{"1", "Alice"}}

	result := buildlog.BuildCSV(columns, rows, opts)
	if result != nil {
		t.Errorf("BuildCSV with Enable=false should return nil, got %+v", result)
	}
}

func TestBuildCSV_EmptyColumns(t *testing.T) {
	opts := buildlog.EmbedOptions{
		Enable: true,
	}
	columns := []string{}
	rows := [][]string{{"1", "Alice"}}

	result := buildlog.BuildCSV(columns, rows, opts)
	if result != nil {
		t.Errorf("BuildCSV with empty columns should return nil, got %+v", result)
	}
}

func TestBuildCSV_Basic(t *testing.T) {
	opts := buildlog.EmbedOptions{
		Enable:   true,
		MaxRows:  100,
		MaxBytes: 65536,
		Base64:   false,
		Redact:   false,
	}
	columns := []string{"id", "name", "email"}
	rows := [][]string{
		{"1", "Alice", "alice@example.com"},
		{"2", "Bob", "bob@example.com"},
	}

	result := buildlog.BuildCSV(columns, rows, opts)
	if result == nil {
		t.Fatal("BuildCSV returned nil for valid input")
	}

	if result.Encoding != "raw" {
		t.Errorf("Encoding = %q, want %q", result.Encoding, "raw")
	}
	if result.MimeType != "text/csv" {
		t.Errorf("MimeType = %q, want %q", result.MimeType, "text/csv")
	}
	if result.Rows != 2 {
		t.Errorf("Rows = %d, want %d", result.Rows, 2)
	}
	if result.Truncated {
		t.Error("Truncated should be false")
	}

	// Verify CSV content
	if !strings.Contains(result.Data, "id,name,email") {
		t.Error("CSV should contain header row")
	}
	if !strings.Contains(result.Data, "Alice") {
		t.Error("CSV should contain data rows")
	}
}

func TestBuildCSV_Base64Encoding(t *testing.T) {
	opts := buildlog.EmbedOptions{
		Enable:   true,
		MaxRows:  100,
		MaxBytes: 65536,
		Base64:   true,
		Redact:   false,
	}
	columns := []string{"id", "name"}
	rows := [][]string{{"1", "Alice"}}

	result := buildlog.BuildCSV(columns, rows, opts)
	if result == nil {
		t.Fatal("BuildCSV returned nil for valid input")
	}

	if result.Encoding != "base64" {
		t.Errorf("Encoding = %q, want %q", result.Encoding, "base64")
	}

	// Decode and verify
	decoded, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		t.Fatalf("Failed to decode base64 data: %v", err)
	}

	if !strings.Contains(string(decoded), "id,name") {
		t.Error("Decoded CSV should contain header")
	}
	if !strings.Contains(string(decoded), "Alice") {
		t.Error("Decoded CSV should contain data")
	}
}

func TestBuildCSV_TruncationByMaxRows(t *testing.T) {
	opts := buildlog.EmbedOptions{
		Enable:   true,
		MaxRows:  2,
		MaxBytes: 65536,
		Base64:   false,
		Redact:   false,
	}
	columns := []string{"id", "name"}
	rows := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
		{"3", "Charlie"},
		{"4", "Diana"},
	}

	result := buildlog.BuildCSV(columns, rows, opts)
	if result == nil {
		t.Fatal("BuildCSV returned nil for valid input")
	}

	if result.Rows != 2 {
		t.Errorf("Rows = %d, want %d", result.Rows, 2)
	}
	if !result.Truncated {
		t.Error("Truncated should be true when rows exceed MaxRows")
	}

	// Charlie and Diana should not be in the output
	if strings.Contains(result.Data, "Charlie") {
		t.Error("CSV should not contain rows beyond MaxRows")
	}
	if strings.Contains(result.Data, "Diana") {
		t.Error("CSV should not contain rows beyond MaxRows")
	}
}

func TestBuildCSV_TruncationByMaxBytes(t *testing.T) {
	opts := buildlog.EmbedOptions{
		Enable:   true,
		MaxRows:  100,
		MaxBytes: 20, // Very small limit - smaller than header
		Base64:   false,
		Redact:   false,
	}
	columns := []string{"id", "name"}
	rows := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
		{"3", "Charlie"},
	}

	result := buildlog.BuildCSV(columns, rows, opts)
	if result == nil {
		t.Fatal("BuildCSV returned nil for valid input")
	}

	// With a very small MaxBytes, the data should be truncated
	if result.Bytes > 20 {
		t.Errorf("Bytes = %d, should not exceed MaxBytes (20)", result.Bytes)
	}
}

func TestBuildCSV_Redaction(t *testing.T) {
	opts := buildlog.EmbedOptions{
		Enable:   true,
		MaxRows:  100,
		MaxBytes: 65536,
		Base64:   false,
		Redact:   true,
	}
	columns := []string{"id", "name", "password", "api_key"}
	rows := [][]string{
		{"1", "Alice", "secret123", "sk-abc"},
		{"2", "Bob", "hunter2", "sk-xyz"},
	}

	result := buildlog.BuildCSV(columns, rows, opts)
	if result == nil {
		t.Fatal("BuildCSV returned nil for valid input")
	}

	// Sensitive columns should be redacted
	if strings.Contains(result.Data, "secret123") {
		t.Error("CSV should not contain unredacted password")
	}
	if strings.Contains(result.Data, "hunter2") {
		t.Error("CSV should not contain unredacted password")
	}
	if strings.Contains(result.Data, "sk-abc") {
		t.Error("CSV should not contain unredacted api_key")
	}
	if strings.Contains(result.Data, "sk-xyz") {
		t.Error("CSV should not contain unredacted api_key")
	}

	// Non-sensitive columns should be present
	if !strings.Contains(result.Data, "Alice") {
		t.Error("CSV should contain non-sensitive data")
	}
	if !strings.Contains(result.Data, "Bob") {
		t.Error("CSV should contain non-sensitive data")
	}

	// REDACTED marker should be present
	if !strings.Contains(result.Data, "[REDACTED]") {
		t.Error("CSV should contain [REDACTED] markers")
	}
}

func TestBuildCSV_NoRedaction(t *testing.T) {
	opts := buildlog.EmbedOptions{
		Enable:   true,
		MaxRows:  100,
		MaxBytes: 65536,
		Base64:   false,
		Redact:   false,
	}
	columns := []string{"id", "password"}
	rows := [][]string{{"1", "secret123"}}

	result := buildlog.BuildCSV(columns, rows, opts)
	if result == nil {
		t.Fatal("BuildCSV returned nil for valid input")
	}

	// With Redact=false, sensitive data should be present
	if !strings.Contains(result.Data, "secret123") {
		t.Error("CSV should contain password when Redact=false")
	}
}

func TestBuildCSV_EmptyRows(t *testing.T) {
	opts := buildlog.EmbedOptions{
		Enable:   true,
		MaxRows:  100,
		MaxBytes: 65536,
		Base64:   false,
		Redact:   false,
	}
	columns := []string{"id", "name"}
	rows := [][]string{}

	result := buildlog.BuildCSV(columns, rows, opts)
	// Should still return a result with just the header
	if result == nil {
		t.Fatal("BuildCSV returned nil for valid columns with empty rows")
	}

	if result.Rows != 0 {
		t.Errorf("Rows = %d, want 0", result.Rows)
	}
	// Header should still be present
	if !strings.Contains(result.Data, "id,name") {
		t.Error("CSV should contain header even with no data rows")
	}
}

func TestBuildCSV_NilRowInSlice(t *testing.T) {
	opts := buildlog.EmbedOptions{
		Enable:   true,
		MaxRows:  100,
		MaxBytes: 65536,
		Base64:   false,
		Redact:   false,
	}
	columns := []string{"id", "name"}
	rows := [][]string{
		{"1", "Alice"},
		nil, // nil row should be skipped
		{"3", "Charlie"},
	}

	result := buildlog.BuildCSV(columns, rows, opts)
	if result == nil {
		t.Fatal("BuildCSV returned nil for valid input with nil row")
	}

	if strings.Contains(result.Data, "nil") {
		t.Error("CSV should not contain 'nil' string")
	}
}

// =============================================================================
// ExecutionPlan Tests
// =============================================================================

func TestNewExecutionPlan(t *testing.T) {
	plan := buildlog.NewExecutionPlan()
	if plan == nil {
		t.Fatal("NewExecutionPlan returned nil")
	}

	steps := plan.GetSteps()
	if len(steps) != 0 {
		t.Errorf("New execution plan should have 0 steps, got %d", len(steps))
	}
}

func TestExecutionPlan_StartAndEndStep(t *testing.T) {
	plan := buildlog.NewExecutionPlan()

	stepID := plan.StartStep("test_step", "test_phase")
	if stepID == "" {
		t.Error("StartStep should return a non-empty step ID")
	}
	if !strings.HasPrefix(stepID, "step-") {
		t.Errorf("Step ID should start with 'step-', got %q", stepID)
	}

	// Give some time for duration
	time.Sleep(10 * time.Millisecond)

	plan.EndStep(stepID, nil)

	steps := plan.GetSteps()
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	step := steps[0]
	if step.ID != stepID {
		t.Errorf("Step ID = %q, want %q", step.ID, stepID)
	}
	if step.Name != "test_step" {
		t.Errorf("Step Name = %q, want %q", step.Name, "test_step")
	}
	if step.Phase != "test_phase" {
		t.Errorf("Step Phase = %q, want %q", step.Phase, "test_phase")
	}
	if step.Status != buildlog.StatusCompleted {
		t.Errorf("Step Status = %q, want %q", step.Status, buildlog.StatusCompleted)
	}
	if step.DurationMs < 10 {
		t.Errorf("Step DurationMs = %d, expected >= 10", step.DurationMs)
	}
	if step.Error != "" {
		t.Errorf("Step Error should be empty, got %q", step.Error)
	}
}

func TestExecutionPlan_EndStepWithError(t *testing.T) {
	plan := buildlog.NewExecutionPlan()

	stepID := plan.StartStep("failing_step", "error_phase")
	plan.EndStep(stepID, errors.New("something went wrong"))

	steps := plan.GetSteps()
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	step := steps[0]
	if step.Status != buildlog.StatusFailed {
		t.Errorf("Step Status = %q, want %q", step.Status, buildlog.StatusFailed)
	}
	if step.Error != "something went wrong" {
		t.Errorf("Step Error = %q, want %q", step.Error, "something went wrong")
	}
}

func TestExecutionPlan_EndStepNotFound(t *testing.T) {
	plan := buildlog.NewExecutionPlan()
	plan.StartStep("step", "phase")

	// Should not panic on non-existent step ID
	plan.EndStep("non-existent-step", nil)

	steps := plan.GetSteps()
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}
	// Original step should still be running
	if steps[0].Status != buildlog.StatusRunning {
		t.Errorf("Step Status = %q, want %q", steps[0].Status, buildlog.StatusRunning)
	}
}

func TestExecutionPlan_SkipStep(t *testing.T) {
	plan := buildlog.NewExecutionPlan()

	plan.SkipStep("skipped_step", "skip_phase", "cache hit")

	steps := plan.GetSteps()
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	step := steps[0]
	if step.Status != buildlog.StatusSkipped {
		t.Errorf("Step Status = %q, want %q", step.Status, buildlog.StatusSkipped)
	}
	if step.Details != "cache hit" {
		t.Errorf("Step Details = %q, want %q", step.Details, "cache hit")
	}
	if step.DurationMs != 0 {
		t.Errorf("Skipped step DurationMs = %d, want 0", step.DurationMs)
	}
}

func TestExecutionPlan_GetStepsByPhase(t *testing.T) {
	plan := buildlog.NewExecutionPlan()

	// Add steps to different phases
	plan.StartStep("step1", "phase_a")
	plan.StartStep("step2", "phase_a")
	plan.StartStep("step3", "phase_b")
	plan.SkipStep("step4", "phase_a", "")

	phaseASteps := plan.GetStepsByPhase("phase_a")
	if len(phaseASteps) != 3 {
		t.Errorf("Expected 3 steps in phase_a, got %d", len(phaseASteps))
	}

	phaseBSteps := plan.GetStepsByPhase("phase_b")
	if len(phaseBSteps) != 1 {
		t.Errorf("Expected 1 step in phase_b, got %d", len(phaseBSteps))
	}

	nonExistentPhase := plan.GetStepsByPhase("phase_x")
	if len(nonExistentPhase) != 0 {
		t.Errorf("Expected 0 steps in non-existent phase, got %d", len(nonExistentPhase))
	}
}

func TestExecutionPlan_GetStepsReturnsCopy(t *testing.T) {
	plan := buildlog.NewExecutionPlan()
	plan.StartStep("step1", "phase")

	steps1 := plan.GetSteps()
	steps2 := plan.GetSteps()

	// Modifying one should not affect the other
	if len(steps1) > 0 {
		steps1[0].Name = "modified"
	}

	if len(steps2) > 0 && steps2[0].Name == "modified" {
		t.Error("GetSteps should return a copy, not the original slice")
	}
}

func TestExecutionPlan_UniqueStepIDs(t *testing.T) {
	plan := buildlog.NewExecutionPlan()

	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := plan.StartStep("step", "phase")
		if ids[id] {
			t.Errorf("Duplicate step ID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestExecutionPlan_ThreadSafety(t *testing.T) {
	plan := buildlog.NewExecutionPlan()

	var wg sync.WaitGroup
	numGoroutines := 10
	stepsPerGoroutine := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < stepsPerGoroutine; j++ {
				stepID := plan.StartStep("step", "phase")
				plan.EndStep(stepID, nil)
			}
		}(i)
	}

	wg.Wait()

	steps := plan.GetSteps()
	expectedSteps := numGoroutines * stepsPerGoroutine
	if len(steps) != expectedSteps {
		t.Errorf("Expected %d steps, got %d", expectedSteps, len(steps))
	}
}

// =============================================================================
// BuildQueryEntry Tests
// =============================================================================

func TestBuildQueryEntry_Basic(t *testing.T) {
	meta := duckdb.QueryExecMeta{
		Query:      "SELECT * FROM users",
		QueryType:  "dataset_query",
		Dataset:    "users_ds",
		Datasource: "main_db",
		StartTime:  time.Now(),
		DurationMs: 150,
		RowCount:   10,
		Columns:    []string{"id", "name", "email"},
		Rows:       [][]string{{"1", "Alice", "alice@example.com"}},
		Error:      "",
	}

	opts := buildlog.EmbedOptions{Enable: false}
	entry := buildlog.BuildQueryEntry(meta, opts)

	if entry.ID == "" {
		t.Error("Entry ID should not be empty")
	}
	if !strings.HasPrefix(entry.ID, "query-") {
		t.Errorf("Entry ID should start with 'query-', got %q", entry.ID)
	}
	if entry.Query != meta.Query {
		t.Errorf("Entry Query = %q, want %q", entry.Query, meta.Query)
	}
	if entry.QueryType != meta.QueryType {
		t.Errorf("Entry QueryType = %q, want %q", entry.QueryType, meta.QueryType)
	}
	if entry.Dataset != meta.Dataset {
		t.Errorf("Entry Dataset = %q, want %q", entry.Dataset, meta.Dataset)
	}
	if entry.Datasource != meta.Datasource {
		t.Errorf("Entry Datasource = %q, want %q", entry.Datasource, meta.Datasource)
	}
	if entry.DurationMs != meta.DurationMs {
		t.Errorf("Entry DurationMs = %d, want %d", entry.DurationMs, meta.DurationMs)
	}
	if entry.RowCount != meta.RowCount {
		t.Errorf("Entry RowCount = %d, want %d", entry.RowCount, meta.RowCount)
	}
	if entry.CSV != nil {
		t.Error("Entry CSV should be nil when embedding is disabled")
	}
}

func TestBuildQueryEntry_WithEmbedding(t *testing.T) {
	meta := duckdb.QueryExecMeta{
		Query:      "SELECT id, name FROM users",
		QueryType:  "dataset_query",
		StartTime:  time.Now(),
		DurationMs: 50,
		RowCount:   2,
		Columns:    []string{"id", "name"},
		Rows:       [][]string{{"1", "Alice"}, {"2", "Bob"}},
	}

	opts := buildlog.EmbedOptions{
		Enable:   true,
		MaxRows:  10,
		MaxBytes: 65536,
		Base64:   true,
		Redact:   true,
	}
	entry := buildlog.BuildQueryEntry(meta, opts)

	if entry.CSV == nil {
		t.Fatal("Entry CSV should not be nil when embedding is enabled")
	}
	if entry.CSV.Encoding != "base64" {
		t.Errorf("CSV Encoding = %q, want %q", entry.CSV.Encoding, "base64")
	}
	if entry.CSV.Rows != 2 {
		t.Errorf("CSV Rows = %d, want 2", entry.CSV.Rows)
	}
}

func TestBuildQueryEntry_WithError(t *testing.T) {
	meta := duckdb.QueryExecMeta{
		Query:      "SELECT * FROM nonexistent",
		QueryType:  "dataset_query",
		StartTime:  time.Now(),
		DurationMs: 5,
		Error:      "table nonexistent does not exist",
	}

	opts := buildlog.EmbedOptions{Enable: false}
	entry := buildlog.BuildQueryEntry(meta, opts)

	if entry.Error != meta.Error {
		t.Errorf("Entry Error = %q, want %q", entry.Error, meta.Error)
	}
}

func TestBuildQueryEntry_NoColumnsNoRows(t *testing.T) {
	meta := duckdb.QueryExecMeta{
		Query:      "UPDATE users SET active = true",
		QueryType:  "dataset_query",
		StartTime:  time.Now(),
		DurationMs: 20,
		RowCount:   0,
		Columns:    nil,
		Rows:       nil,
	}

	opts := buildlog.EmbedOptions{
		Enable:   true,
		MaxRows:  10,
		MaxBytes: 65536,
	}
	entry := buildlog.BuildQueryEntry(meta, opts)

	// Should not have CSV embedded when no columns/rows
	if entry.CSV != nil {
		t.Error("Entry CSV should be nil when no columns/rows")
	}
}

// =============================================================================
// WriteJSONBuildLog Tests
// =============================================================================

func TestWriteJSONBuildLog_Success(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "build.log")

	startTime := time.Now()
	endTime := startTime.Add(5 * time.Second)

	log := &buildlog.JSONBuildLog{
		RunID:      "test-run-001",
		Started:    startTime,
		Completed:  endTime,
		DurationMs: 5000,
		Workdir:    "/path/to/workdir",
		Documents: []buildlog.DocumentEntry{
			{File: "report.yaml", Position: 0, Kind: "report", Name: "monthly-report"},
		},
		Artefacts: []buildlog.ArtefactEntry{
			{Name: "monthly-report", PDF: "output.pdf", Graph: "graph.svg"},
		},
		Queries: []buildlog.QueryEntry{
			{ID: "query-001", Query: "SELECT 1", QueryType: "dataset_query", DurationMs: 10, RowCount: 1},
		},
		ExecutionPlan: []buildlog.ExecutionStep{
			{ID: "step-001", Name: "load", Phase: "init", Status: buildlog.StatusCompleted, DurationMs: 100},
		},
	}

	err := buildlog.WriteJSONBuildLog(logPath, log)
	if err != nil {
		t.Fatalf("WriteJSONBuildLog failed: %v", err)
	}

	// Verify file exists and is valid JSON
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	var parsed buildlog.JSONBuildLog
	err = json.Unmarshal(data, &parsed)
	if err != nil {
		t.Fatalf("Failed to unmarshal log file: %v", err)
	}

	// Verify content
	if parsed.RunID != log.RunID {
		t.Errorf("RunID = %q, want %q", parsed.RunID, log.RunID)
	}
	if parsed.DurationMs != log.DurationMs {
		t.Errorf("DurationMs = %d, want %d", parsed.DurationMs, log.DurationMs)
	}
	if len(parsed.Documents) != 1 {
		t.Errorf("Documents count = %d, want 1", len(parsed.Documents))
	}
	if len(parsed.Artefacts) != 1 {
		t.Errorf("Artefacts count = %d, want 1", len(parsed.Artefacts))
	}
	if len(parsed.Queries) != 1 {
		t.Errorf("Queries count = %d, want 1", len(parsed.Queries))
	}
	if len(parsed.ExecutionPlan) != 1 {
		t.Errorf("ExecutionPlan count = %d, want 1", len(parsed.ExecutionPlan))
	}
}

func TestWriteJSONBuildLog_NilLog(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "build.log")

	err := buildlog.WriteJSONBuildLog(logPath, nil)
	if err == nil {
		t.Error("WriteJSONBuildLog should return error for nil log")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("Error should mention nil, got: %v", err)
	}
}

func TestWriteJSONBuildLog_InvalidPath(t *testing.T) {
	log := &buildlog.JSONBuildLog{
		RunID: "test",
	}

	// Use a path that should fail (non-existent directory)
	err := buildlog.WriteJSONBuildLog("/nonexistent/directory/build.log", log)
	if err == nil {
		t.Error("WriteJSONBuildLog should return error for invalid path")
	}
}

func TestWriteJSONBuildLog_JSONFormat(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "build.log")

	log := &buildlog.JSONBuildLog{
		RunID:      "test-run",
		DurationMs: 1000,
	}

	err := buildlog.WriteJSONBuildLog(logPath, log)
	if err != nil {
		t.Fatalf("WriteJSONBuildLog failed: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	// Verify it's indented JSON (pretty printed)
	content := string(data)
	if !strings.Contains(content, "\n") {
		t.Error("JSON should be indented (contain newlines)")
	}
	if !strings.Contains(content, "  ") {
		t.Error("JSON should be indented (contain spaces)")
	}
}

// =============================================================================
// Status Constants Tests
// =============================================================================

func TestStatusConstants(t *testing.T) {
	// Verify status constants have expected values
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"StatusRunning", buildlog.StatusRunning, "running"},
		{"StatusCompleted", buildlog.StatusCompleted, "completed"},
		{"StatusFailed", buildlog.StatusFailed, "failed"},
		{"StatusSkipped", buildlog.StatusSkipped, "skipped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

// =============================================================================
// Step Name Constants Tests
// =============================================================================

func TestStepNameConstants(t *testing.T) {
	// Verify well-known step names are defined
	steps := []string{
		buildlog.StepLoadManifests,
		buildlog.StepValidateManifests,
		buildlog.StepBuildGraph,
		buildlog.StepCollectDatasources,
		buildlog.StepExecuteDatasets,
		buildlog.StepRenderHTML,
		buildlog.StepGeneratePDF,
		buildlog.StepSignPDF,
		buildlog.StepWriteOutputs,
	}

	for _, step := range steps {
		if step == "" {
			t.Error("Step name constant should not be empty")
		}
	}
}
