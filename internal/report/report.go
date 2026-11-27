// Package report defines the restore-evidence report and renders it as JSON,
// HTML and plain text. Rendering is deterministic: the same report value
// always produces the same bytes.
package report

import (
	"encoding/json"
	"io"
)

// SchemaVersion is the version of the JSON report layout.
const SchemaVersion = 1

// Status of a check performed (or not performed) by the tool.
type Status string

const (
	// Verified: the tool observed the restored data and it matched.
	Verified Status = "verified"
	// Failed: the tool observed the restored data and it did not match.
	Failed Status = "failed"
	// NotPerformed: the tool did not make this observation; Reason says why.
	NotPerformed Status = "not_performed"
)

// Outcome summarises the whole report.
type Outcome string

const (
	// OutcomePass: every check was performed and verified, and every supplied
	// result was reported as pass.
	OutcomePass Outcome = "pass"
	// OutcomeFail: at least one check failed or a supplied result was
	// reported as fail.
	OutcomeFail Outcome = "fail"
	// OutcomeIncomplete: nothing failed, but at least one check was not
	// performed or a supplied result was reported as not performed.
	OutcomeIncomplete Outcome = "incomplete"
)

// Check kinds.
const (
	KindPresence      = "presence"
	KindSize          = "size"
	KindSHA256        = "sha256"
	KindFileCount     = "file_count"
	KindTotalBytes    = "total_bytes"
	KindUnlistedFiles = "unlisted_files"
)

// Report is the complete result of one verification run.
type Report struct {
	ReportSchemaVersion int              `json:"report_schema_version"`
	Tool                Tool             `json:"tool"`
	Manifest            ManifestInfo     `json:"manifest"`
	Test                Test             `json:"test"`
	Inputs              Inputs           `json:"inputs"`
	Outcome             Outcome          `json:"outcome"`
	Summary             Summary          `json:"summary"`
	Checks              []Check          `json:"checks"`
	SuppliedResults     []SuppliedResult `json:"supplied_results"`
	Observed            *Observed        `json:"observed,omitempty"`
	Limitations         []string         `json:"limitations"`
}

// Tool identifies the program that produced the report.
type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ManifestInfo identifies the manifest the report was built from. The
// manifest's location on disk is deliberately not recorded.
type ManifestInfo struct {
	SchemaVersion int    `json:"schema_version"`
	SHA256        string `json:"sha256"`
	FileEntries   int    `json:"file_entries"`
}

// Test repeats the operator-supplied description of the restore test.
type Test struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	System             string `json:"system,omitempty"`
	Environment        string `json:"environment,omitempty"`
	BackupReference    string `json:"backup_reference,omitempty"`
	BackupTakenAt      string `json:"backup_taken_at,omitempty"`
	RestoreStartedAt   string `json:"restore_started_at,omitempty"`
	RestoreCompletedAt string `json:"restore_completed_at,omitempty"`
	PerformedBy        string `json:"performed_by,omitempty"`
	Notes              string `json:"notes,omitempty"`
}

// Inputs records how the run was configured. The restored directory's
// location is deliberately not recorded.
type Inputs struct {
	RestoredDirectorySupplied bool `json:"restored_directory_supplied"`
	HashingEnabled            bool `json:"hashing_enabled"`
	PathsRedacted             bool `json:"paths_redacted"`
}

// Summary counts checks by status and supplied results by outcome.
type Summary struct {
	Verified             int `json:"verified"`
	Failed               int `json:"failed"`
	NotPerformed         int `json:"not_performed"`
	SuppliedPass         int `json:"supplied_pass"`
	SuppliedFail         int `json:"supplied_fail"`
	SuppliedNotPerformed int `json:"supplied_not_performed"`
}

// Check is one observation the tool made, or was asked to make but did not.
type Check struct {
	Kind string `json:"kind"`
	// Subject is the manifest path the check is about, a redaction label such
	// as "[redacted file #3]", or "" for directory-wide checks.
	Subject  string `json:"subject,omitempty"`
	Status   Status `json:"status"`
	Expected string `json:"expected,omitempty"`
	Observed string `json:"observed,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// SuppliedResult is copied from the manifest. The tool did not verify it.
type SuppliedResult struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Outcome     string `json:"reported_outcome"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
	Command     string `json:"command,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

// Observed describes the restored directory as the tool saw it.
type Observed struct {
	RegularFiles        int64    `json:"regular_files"`
	TotalBytes          int64    `json:"total_bytes"`
	UnlistedFiles       int64    `json:"unlisted_files"`
	UnlistedSample      []string `json:"unlisted_sample,omitempty"`
	SymlinksNotFollowed int64    `json:"symlinks_not_followed"`
	OtherNonRegular     int64    `json:"other_non_regular"`
	UnreadableEntries   int64    `json:"unreadable_entries"`
}

// Limitations is the fixed statement of what a report does and does not show.
// It is included in every report.
var Limitations = []string{
	"This report records only the checks listed in it, made by this tool at the time it ran, against the manifest and directory it was given.",
	"Supplied results are copied from the manifest as reported by the operator. This tool did not observe or verify them.",
	"A verified SHA-256 digest shows that the restored file matches the digest in the manifest. It is only as trustworthy as the source of the manifest and does not show where the data came from.",
	"The report does not show that the backup can be restored again, under other conditions, or by other people, and it does not show that the restored data is complete or correct beyond the listed checks.",
	"The report is not a compliance certification and is not tamper-evident: anyone with write access can edit or regenerate it.",
}

// Finalize fills in the summary and outcome from the checks and supplied
// results.
func (r *Report) Finalize() {
	var s Summary
	for _, c := range r.Checks {
		switch c.Status {
		case Verified:
			s.Verified++
		case Failed:
			s.Failed++
		default:
			s.NotPerformed++
		}
	}
	for _, sr := range r.SuppliedResults {
		switch sr.Outcome {
		case "pass":
			s.SuppliedPass++
		case "fail":
			s.SuppliedFail++
		default:
			s.SuppliedNotPerformed++
		}
	}
	r.Summary = s
	switch {
	case s.Failed > 0 || s.SuppliedFail > 0:
		r.Outcome = OutcomeFail
	case s.NotPerformed > 0 || s.SuppliedNotPerformed > 0:
		r.Outcome = OutcomeIncomplete
	default:
		r.Outcome = OutcomePass
	}
	if r.Checks == nil {
		r.Checks = []Check{}
	}
	if r.SuppliedResults == nil {
		r.SuppliedResults = []SuppliedResult{}
	}
	r.Limitations = Limitations
	r.ReportSchemaVersion = SchemaVersion
}

// WriteJSON writes the report as indented JSON followed by a newline.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
