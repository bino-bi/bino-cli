// Package buildlog provides build logging and execution tracking for bino reports.
package buildlog

import (
	"fmt"
	"sync"
	"time"
)

// Well-known step name constants for common build phases.
const (
	StepLoadManifests      = "load_manifests"
	StepValidateManifests  = "validate_manifests"
	StepBuildGraph         = "build_graph"
	StepCollectDatasources = "collect_datasources"
	StepExecuteDatasets    = "execute_datasets"
	StepRenderHTML         = "render_html"
	StepGeneratePDF        = "generate_pdf"
	StepSignPDF            = "sign_pdf"
	StepWriteOutputs       = "write_outputs"
)

// Step status constants.
const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusSkipped   = "skipped"
)

// ExecutionStep represents a single step in the build execution plan.
type ExecutionStep struct {
	ID         string    `json:"id"`                // unique step ID (e.g., "step-001")
	Name       string    `json:"name"`              // step name (e.g., "collect_datasources")
	Phase      string    `json:"phase"`             // parent phase (e.g., "artefact:monthly-report")
	StartTime  time.Time `json:"start_time"`        // when step started
	EndTime    time.Time `json:"end_time"`          // when step ended
	DurationMs int64     `json:"duration_ms"`       // duration in milliseconds
	Status     string    `json:"status"`            // "running", "completed", "failed", "skipped"
	Details    string    `json:"details,omitempty"` // optional details
	Error      string    `json:"error,omitempty"`   // error message if failed
}

// ExecutionPlan tracks the execution of build steps with timing information.
type ExecutionPlan struct {
	mu      sync.Mutex
	Steps   []ExecutionStep `json:"steps"`
	stepSeq int             // for generating unique IDs
}

// ExecutionPlanOptions configures execution plan tracking behavior.
type ExecutionPlanOptions struct {
	Enabled bool // Whether to track detailed execution plan
}

// NewExecutionPlan creates a new ExecutionPlan instance.
func NewExecutionPlan() *ExecutionPlan {
	return &ExecutionPlan{
		Steps: make([]ExecutionStep, 0),
	}
}

// StartStep starts a new step and returns its unique ID.
// The step is recorded with status "running" and the current time as StartTime.
func (p *ExecutionPlan) StartStep(name, phase string) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stepSeq++
	stepID := fmt.Sprintf("step-%03d", p.stepSeq)

	step := ExecutionStep{
		ID:        stepID,
		Name:      name,
		Phase:     phase,
		StartTime: time.Now(),
		Status:    StatusRunning,
	}

	p.Steps = append(p.Steps, step)
	return stepID
}

// EndStep completes a step identified by stepID.
// If err is nil, the step is marked as "completed"; otherwise, it is marked as "failed"
// with the error message recorded. If the stepID is not found, this method is a no-op.
func (p *ExecutionPlan) EndStep(stepID string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.Steps {
		if p.Steps[i].ID == stepID {
			p.Steps[i].EndTime = time.Now()
			p.Steps[i].DurationMs = p.Steps[i].EndTime.Sub(p.Steps[i].StartTime).Milliseconds()

			if err != nil {
				p.Steps[i].Status = StatusFailed
				p.Steps[i].Error = err.Error()
			} else {
				p.Steps[i].Status = StatusCompleted
			}
			return
		}
	}
}

// SkipStep records a step that was skipped with an optional reason.
func (p *ExecutionPlan) SkipStep(name, phase, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stepSeq++
	stepID := fmt.Sprintf("step-%03d", p.stepSeq)

	now := time.Now()
	step := ExecutionStep{
		ID:         stepID,
		Name:       name,
		Phase:      phase,
		StartTime:  now,
		EndTime:    now,
		DurationMs: 0,
		Status:     StatusSkipped,
		Details:    reason,
	}

	p.Steps = append(p.Steps, step)
}

// GetSteps returns a copy of all steps in the execution plan.
// This method is thread-safe.
func (p *ExecutionPlan) GetSteps() []ExecutionStep {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make([]ExecutionStep, len(p.Steps))
	copy(result, p.Steps)
	return result
}

// GetStepsByPhase returns a copy of all steps belonging to the specified phase.
// This method is thread-safe.
func (p *ExecutionPlan) GetStepsByPhase(phase string) []ExecutionStep {
	p.mu.Lock()
	defer p.mu.Unlock()

	var result []ExecutionStep
	for _, step := range p.Steps {
		if step.Phase == phase {
			result = append(result, step)
		}
	}
	return result
}
