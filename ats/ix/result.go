package ix

import (
	"fmt"
	"sort"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/ingestion"
)

const (
	// DefaultIxResultVersion is the default version for ix adapter results
	DefaultIxResultVersion = "v1"
)

// Result represents the structured execution result for ix adapters.
type Result struct {
	Op       string            `json:"op"`
	Inputs   map[string]string `json:"inputs,omitempty"`
	Stats    IxStats           `json:"stats"`
	Items    []IxItem          `json:"items,omitempty"`
	Warnings []IxIssue         `json:"warnings,omitempty"`
	Errors   []IxIssue         `json:"errors,omitempty"`
	Trace    map[string]any    `json:"trace,omitempty"`
	Actor    string            `json:"actor,omitempty"`
	Version  string            `json:"version"`
}

// IxStats captures summary metrics for an ix execution.
type IxStats struct {
	ReadCount    int   `json:"read_count"`
	ParsedCount  int   `json:"parsed_count"`
	WrittenCount int   `json:"written_count"`
	DurationMs   int64 `json:"duration_ms"`
}

// IxItem represents a derived triple from the adapter.
type IxItem struct {
	Subject string            `json:"subject"`
	Pred    string            `json:"pred"`
	Obj     string            `json:"obj"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// IxIssue captures warnings and errors with optional hints.
type IxIssue struct {
	Stage   string   `json:"stage"`
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Hints   []string `json:"hints,omitempty"`
}

// NewResult creates a Result initialized for the provided operation.
func NewResult(op string) Result {
	return Result{
		Op:       op,
		Inputs:   map[string]string{},
		Items:    []IxItem{},
		Warnings: []IxIssue{},
		Errors:   []IxIssue{},
		Trace:    map[string]any{},
		Version:  DefaultIxResultVersion,
	}
}

// AddItem appends an IxItem to the result.
func (r *Result) AddItem(subject, pred, obj string, meta map[string]string) {
	if meta != nil {
		copied := make(map[string]string, len(meta))
		for k, v := range meta {
			copied[k] = v
		}
		meta = copied
	}
	r.Items = append(r.Items, IxItem{Subject: subject, Pred: pred, Obj: obj, Meta: meta})
}

// AddWarning adds a warning issue to the result.
func (r *Result) AddWarning(stage, code, message string, hints ...string) {
	issue := IxIssue{Stage: stage, Code: code, Message: message}
	if len(hints) > 0 {
		issue.Hints = append([]string{}, hints...)
	}
	r.Warnings = append(r.Warnings, issue)
}

// AddError adds an error issue to the result.
func (r *Result) AddError(stage, code, message string, hints ...string) {
	issue := IxIssue{Stage: stage, Code: code, Message: message}
	if len(hints) > 0 {
		issue.Hints = append([]string{}, hints...)
	}
	r.Errors = append(r.Errors, issue)
}

// AddStorageWarnings converts bounded storage warnings to IxIssues and adds them.
// This enables predictive warnings about storage limits to flow through to the UI.
func (r *Result) AddStorageWarnings(warnings []*ats.StorageWarning) {
	for _, w := range warnings {
		message := fmt.Sprintf("Storage approaching limit for actor %s in context %s: %d/%d (%.0f%% full)",
			w.Actor, w.Context, w.Current, w.Limit, w.FillPercent*100)

		hints := []string{
			fmt.Sprintf("Time until full: %s", w.TimeUntilFull),
			"Consider archiving old attestations or increasing limits",
		}

		r.AddWarning("persistence", "STORAGE_LIMIT_APPROACHING", message, hints...)
	}
}

// SortedTraceKeys returns sorted trace keys for deterministic rendering.
func (r *Result) SortedTraceKeys() []string {
	if len(r.Trace) == 0 {
		return nil
	}
	keys := make([]string, 0, len(r.Trace))
	for k := range r.Trace {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// AttestationItem interface implementation for IxItem
// This allows IxItem to be persisted using the ats.BatchPersister

// GetSubject returns the subject of the attestation
func (item IxItem) GetSubject() string {
	return item.Subject
}

// GetPredicate returns the predicate of the attestation
func (item IxItem) GetPredicate() string {
	return item.Pred
}

// GetObject returns the object of the attestation
func (item IxItem) GetObject() string {
	return item.Obj
}

// GetMeta returns the metadata of the attestation
func (item IxItem) GetMeta() map[string]string {
	return item.Meta
}

// Compile-time interface compliance checks
var (
	_ ingestion.Item   = IxItem{}
	_ ingestion.Result = (*Result)(nil)
)

// ingestion.Result interface implementation for Result
// This allows Result to be used polymorphically by any consumer expecting ingestion.Result

// GetItems returns all items as ingestion.Item interface slice
func (r *Result) GetItems() []ingestion.Item {
	items := make([]ingestion.Item, len(r.Items))
	for i := range r.Items {
		items[i] = r.Items[i]
	}
	return items
}

// GetWarnings returns all warnings as ingestion.Issue slice
func (r *Result) GetWarnings() []ingestion.Issue {
	issues := make([]ingestion.Issue, len(r.Warnings))
	for i, w := range r.Warnings {
		issues[i] = ingestion.Issue{
			Stage:   w.Stage,
			Code:    w.Code,
			Message: w.Message,
			Hints:   w.Hints,
		}
	}
	return issues
}

// GetErrors returns all errors as ingestion.Issue slice
func (r *Result) GetErrors() []ingestion.Issue {
	issues := make([]ingestion.Issue, len(r.Errors))
	for i, e := range r.Errors {
		issues[i] = ingestion.Issue{
			Stage:   e.Stage,
			Code:    e.Code,
			Message: e.Message,
			Hints:   e.Hints,
		}
	}
	return issues
}

// GetStats returns execution statistics as ingestion.Stats
func (r *Result) GetStats() ingestion.Stats {
	return ingestion.Stats{
		ReadCount:    r.Stats.ReadCount,
		ParsedCount:  r.Stats.ParsedCount,
		WrittenCount: r.Stats.WrittenCount,
		DurationMs:   r.Stats.DurationMs,
	}
}
