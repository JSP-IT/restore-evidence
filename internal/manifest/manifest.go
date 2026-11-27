// Package manifest loads and validates restore-test manifests.
//
// A manifest is a JSON document written by the person running a restore test.
// It describes what was expected to be restored (files, sizes, SHA-256
// digests, totals) and records results that the operator observed and supplied
// explicitly. The package only parses and validates; it never acts on the
// content. In particular, the optional "command" text of a supplied result is
// kept as a string and is never executed.
package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// SchemaVersion is the only manifest schema version this build understands.
const SchemaVersion = 1

// Default limits. They keep a malformed or hostile manifest from exhausting
// memory; they are well above what a hand-written or generated manifest for a
// typical restore test needs.
const (
	DefaultMaxBytes = 64 << 20 // 64 MiB
	DefaultMaxFiles = 500_000
)

const (
	maxPathBytes  = 4096
	maxShortText  = 512
	maxLongText   = 20_000
	maxSuppliedNo = 10_000
)

// Outcome values allowed for a supplied result.
const (
	OutcomePass         = "pass"
	OutcomeFail         = "fail"
	OutcomeNotPerformed = "not_performed"
)

// Manifest is the decoded form of a schema_version 1 manifest.
type Manifest struct {
	SchemaVersion   int              `json:"schema_version"`
	Test            Test             `json:"test"`
	Expected        Expected         `json:"expected"`
	Files           []File           `json:"files"`
	SuppliedResults []SuppliedResult `json:"supplied_results"`
}

// Test describes the restore test as documented by the operator. All fields
// are free text supplied by the operator and are reported as such.
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

// Expected holds totals for the whole restored directory.
type Expected struct {
	// FileCount is the expected number of regular files in the restored
	// directory, counted recursively.
	FileCount *int64 `json:"file_count,omitempty"`
	// TotalBytes is the expected sum of the sizes of those files.
	TotalBytes *int64 `json:"total_bytes,omitempty"`
	// AllowUnlisted, when true, means the files list is a sample and regular
	// files that are not listed are not reported as failures.
	AllowUnlisted bool `json:"allow_unlisted,omitempty"`
}

// File is one expected file, identified by a slash-separated path relative to
// the restored directory.
type File struct {
	Path      string `json:"path"`
	Size      *int64 `json:"size,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

// SuppliedResult is a result the operator observed and reports in the
// manifest, such as "the application started" or "row counts matched". The
// tool records it as reported; it does not verify it.
type SuppliedResult struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Outcome     string `json:"outcome"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
	// Command is documentation only: the command the operator says they ran.
	// It is never executed.
	Command string `json:"command,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

// Options controls loading limits. Zero values select the defaults.
type Options struct {
	MaxBytes int64
	MaxFiles int
}

func (o Options) maxBytes() int64 {
	if o.MaxBytes > 0 {
		return o.MaxBytes
	}
	return DefaultMaxBytes
}

func (o Options) maxFiles() int {
	if o.MaxFiles > 0 {
		return o.MaxFiles
	}
	return DefaultMaxFiles
}

// Loaded is a validated manifest together with the SHA-256 digest of the
// exact bytes it was read from, so a report can be tied to its input.
type Loaded struct {
	Manifest Manifest
	Digest   string
}

// ValidationError lists every problem found in a manifest.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 1 {
		return "invalid manifest: " + e.Problems[0]
	}
	return fmt.Sprintf("invalid manifest: %d problems:\n  %s", len(e.Problems), strings.Join(e.Problems, "\n  "))
}

// ErrTooLarge is returned when the manifest exceeds Options.MaxBytes.
var ErrTooLarge = errors.New("manifest is larger than the allowed size")

// LoadFile reads and validates the manifest at name. Error messages do not
// include the file's path.
func LoadFile(name string, opts Options) (*Loaded, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, fmt.Errorf("cannot read manifest: %s", describeFSError(err))
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("cannot read manifest: not a regular file")
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("cannot read manifest: %s", describeFSError(err))
	}
	defer f.Close()
	return Load(f, opts)
}

// Load reads and validates a manifest from r.
func Load(r io.Reader, opts Options) (*Loaded, error) {
	limit := opts.maxBytes()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, errors.New("cannot read manifest")
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w (limit %d bytes)", ErrTooLarge, limit)
	}
	m, err := decode(data)
	if err != nil {
		return nil, err
	}
	if err := Validate(m, opts); err != nil {
		return nil, err
	}
	sortFiles(m.Files)
	sum := sha256.Sum256(data)
	return &Loaded{Manifest: *m, Digest: hex.EncodeToString(sum[:])}, nil
}

func decode(data []byte) (*Manifest, error) {
	if !utf8.Valid(data) {
		return nil, &ValidationError{Problems: []string{"manifest is not valid UTF-8"}}
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	// Check the schema version first so that a newer manifest gets a clear
	// message instead of an "unknown field" error.
	var probe struct {
		SchemaVersion json.RawMessage `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, syntaxProblem(data, err)
	}
	if len(probe.SchemaVersion) == 0 {
		return nil, &ValidationError{Problems: []string{"schema_version: required"}}
	}
	var version int
	if err := json.Unmarshal(probe.SchemaVersion, &version); err != nil {
		return nil, &ValidationError{Problems: []string{"schema_version: must be an integer"}}
	}
	if version != SchemaVersion {
		return nil, &ValidationError{Problems: []string{
			fmt.Sprintf("schema_version: %d is not supported (this build supports %d)", version, SchemaVersion),
		}}
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, syntaxProblem(data, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, &ValidationError{Problems: []string{"unexpected data after the manifest object"}}
	}
	return &m, nil
}

func syntaxProblem(data []byte, err error) error {
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		line, col := position(data, syntax.Offset)
		return &ValidationError{Problems: []string{fmt.Sprintf("malformed JSON at line %d, column %d: %s", line, col, syntax.Error())}}
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		field := fieldPath(typeErr.Field)
		return &ValidationError{Problems: []string{fmt.Sprintf("%s: expected %s, found JSON %s", field, typeErr.Type, typeErr.Value)}}
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "json: unknown field ") {
		return &ValidationError{Problems: []string{"unknown field " + strings.TrimPrefix(msg, "json: unknown field ") + " (schema_version 1 does not allow extra fields)"}}
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return &ValidationError{Problems: []string{"malformed JSON: unexpected end of input"}}
	}
	return &ValidationError{Problems: []string{"malformed JSON: " + strings.TrimPrefix(msg, "json: ")}}
}

// fieldPath turns encoding/json's "files.0.size" into "files[0].size".
func fieldPath(f string) string {
	if f == "" {
		return "(root)"
	}
	var b strings.Builder
	for i, part := range strings.Split(f, ".") {
		if _, err := strconv.Atoi(part); err == nil {
			b.WriteString("[" + part + "]")
			continue
		}
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(part)
	}
	return b.String()
}

func position(data []byte, offset int64) (line, col int) {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	line, col = 1, 1
	for _, b := range data[:offset] {
		if b == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

// Validate checks a decoded manifest. Load calls it; it is exported for
// callers that build a Manifest in code.
func Validate(m *Manifest, opts Options) error {
	var p problems

	if m.SchemaVersion != SchemaVersion {
		p.addf("schema_version: %d is not supported (this build supports %d)", m.SchemaVersion, SchemaVersion)
	}

	p.requiredText("test.id", m.Test.ID, maxShortText)
	p.requiredText("test.title", m.Test.Title, maxShortText)
	p.optionalText("test.system", m.Test.System, maxShortText)
	p.optionalText("test.environment", m.Test.Environment, maxShortText)
	p.optionalText("test.backup_reference", m.Test.BackupReference, maxShortText)
	p.optionalText("test.performed_by", m.Test.PerformedBy, maxShortText)
	p.optionalText("test.notes", m.Test.Notes, maxLongText)
	backupAt := p.timestamp("test.backup_taken_at", m.Test.BackupTakenAt)
	started := p.timestamp("test.restore_started_at", m.Test.RestoreStartedAt)
	completed := p.timestamp("test.restore_completed_at", m.Test.RestoreCompletedAt)
	if !started.IsZero() && !completed.IsZero() && completed.Before(started) {
		p.add("test.restore_completed_at: is earlier than test.restore_started_at")
	}
	if !backupAt.IsZero() && !started.IsZero() && started.Before(backupAt) {
		p.add("test.restore_started_at: is earlier than test.backup_taken_at")
	}

	if len(m.Files) == 0 && len(m.SuppliedResults) == 0 {
		p.add("manifest lists no files and no supplied_results; there is nothing to check")
	}
	if len(m.Files) > opts.maxFiles() {
		p.addf("files: %d entries exceed the limit of %d", len(m.Files), opts.maxFiles())
		return p.err() // do not spend time on per-entry checks
	}

	seen := make(map[string]int, len(m.Files))
	var listedBytes int64
	allSized := true
	for i, f := range m.Files {
		field := fmt.Sprintf("files[%d]", i)
		if msg := CheckPath(f.Path); msg != "" {
			p.addf("%s.path: %s", field, msg)
		} else if first, dup := seen[f.Path]; dup {
			p.addf("%s.path: duplicates files[%d]", field, first)
		} else {
			seen[f.Path] = i
		}
		if f.Size != nil {
			if *f.Size < 0 {
				p.addf("%s.size: must not be negative", field)
			} else if listedBytes <= maxInt64-*f.Size {
				listedBytes += *f.Size
			} else {
				p.addf("%s.size: total of listed sizes overflows", field)
			}
		} else {
			allSized = false
		}
		if f.SHA256 != "" && !isSHA256Hex(f.SHA256) {
			p.addf("%s.sha256: must be 64 hexadecimal characters", field)
		}
	}
	// A listed path cannot also be a directory that contains another listed path.
	for i, f := range m.Files {
		// Only clean relative paths reach this loop: path.Dir would never
		// reach "." for an absolute one.
		if first, ok := seen[f.Path]; !ok || first != i {
			continue // invalid or duplicate path, already reported
		}
		for dir := path.Dir(f.Path); dir != "."; dir = path.Dir(dir) {
			if j, ok := seen[dir]; ok {
				p.addf("files[%d].path: is listed as a file but files[%d] is inside it", j, i)
			}
		}
	}

	listed := int64(len(m.Files))
	if fc := m.Expected.FileCount; fc != nil {
		switch {
		case *fc < 0:
			p.add("expected.file_count: must not be negative")
		case !m.Expected.AllowUnlisted && *fc != listed:
			p.addf("expected.file_count: is %d but %d files are listed and allow_unlisted is false", *fc, listed)
		case m.Expected.AllowUnlisted && *fc < listed:
			p.addf("expected.file_count: is %d but %d files are listed", *fc, listed)
		}
	}
	if tb := m.Expected.TotalBytes; tb != nil {
		switch {
		case *tb < 0:
			p.add("expected.total_bytes: must not be negative")
		case !m.Expected.AllowUnlisted && allSized && *tb != listedBytes:
			p.addf("expected.total_bytes: is %d but listed sizes add up to %d and allow_unlisted is false", *tb, listedBytes)
		case *tb < listedBytes:
			p.addf("expected.total_bytes: is %d but listed sizes already add up to %d", *tb, listedBytes)
		}
	}

	if len(m.SuppliedResults) > maxSuppliedNo {
		p.addf("supplied_results: %d entries exceed the limit of %d", len(m.SuppliedResults), maxSuppliedNo)
		return p.err()
	}
	ids := make(map[string]int, len(m.SuppliedResults))
	for i, r := range m.SuppliedResults {
		field := fmt.Sprintf("supplied_results[%d]", i)
		if p.requiredText(field+".id", r.ID, maxShortText) {
			if first, dup := ids[r.ID]; dup {
				p.addf("%s.id: duplicates supplied_results[%d]", field, first)
			} else {
				ids[r.ID] = i
			}
		}
		p.requiredText(field+".description", r.Description, maxLongText)
		switch r.Outcome {
		case OutcomePass, OutcomeFail, OutcomeNotPerformed:
		case "":
			p.addf("%s.outcome: required (pass, fail or not_performed)", field)
		default:
			p.addf("%s.outcome: must be pass, fail or not_performed", field)
		}
		p.optionalText(field+".evidence_ref", r.EvidenceRef, maxShortText)
		p.optionalText(field+".command", r.Command, maxLongText)
		p.optionalText(field+".notes", r.Notes, maxLongText)
	}
	return p.err()
}

const maxInt64 = 1<<63 - 1

// CheckPath reports why name is not an acceptable manifest path, or "" if it
// is. Accepted paths are relative, slash-separated, already clean, and cannot
// name anything outside the restored directory.
func CheckPath(name string) string {
	switch {
	case name == "":
		return "required"
	case len(name) > maxPathBytes:
		return fmt.Sprintf("longer than %d bytes", maxPathBytes)
	case !utf8.ValidString(name):
		return "not valid UTF-8"
	case strings.ContainsRune(name, '\\'):
		return "must use forward slashes"
	case strings.HasPrefix(name, "/"):
		return "must be relative to the restored directory"
	case len(name) >= 2 && name[1] == ':' && isASCIILetter(name[0]):
		return "must not start with a drive letter"
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "must not contain control characters"
		}
	}
	for _, part := range strings.Split(name, "/") {
		switch part {
		case "":
			return "must not contain empty segments or a trailing slash"
		case ".":
			return `must not contain "." segments`
		case "..":
			return `must not contain ".." segments`
		}
	}
	return ""
}

func isASCIILetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func sortFiles(files []File) {
	for i := range files {
		files[i].SHA256 = strings.ToLower(files[i].SHA256)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}

// describeFSError turns a filesystem error into a short message that does not
// repeat the path.
func describeFSError(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "not found"
	case errors.Is(err, os.ErrPermission):
		return "permission denied"
	default:
		return "read error"
	}
}

type problems struct{ list []string }

func (p *problems) add(s string)                    { p.list = append(p.list, s) }
func (p *problems) addf(format string, args ...any) { p.add(fmt.Sprintf(format, args...)) }

func (p *problems) err() error {
	if len(p.list) == 0 {
		return nil
	}
	return &ValidationError{Problems: p.list}
}

func (p *problems) requiredText(field, value string, max int) bool {
	if strings.TrimSpace(value) == "" {
		p.addf("%s: required", field)
		return false
	}
	return p.optionalText(field, value, max)
}

func (p *problems) optionalText(field, value string, max int) bool {
	if utf8.RuneCountInString(value) > max {
		p.addf("%s: longer than %d characters", field, max)
		return false
	}
	return true
}

func (p *problems) timestamp(field, value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		p.addf("%s: must be an RFC 3339 timestamp such as 2026-01-31T14:05:00Z", field)
		return time.Time{}
	}
	return t
}
