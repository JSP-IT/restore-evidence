// Package verify compares a validated manifest with a restored directory and
// builds a report.
//
// The restored directory is only read. It is opened as an os.Root, so no path
// can resolve outside it, and the directory walk does not follow symbolic
// links. A manifest entry is only opened for hashing when the walk found it as
// a regular file at exactly that path, so a manifest cannot direct the tool to
// a symlink, a device, or anything outside the directory.
package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/JSP-IT/restore-evidence/internal/manifest"
	"github.com/JSP-IT/restore-evidence/internal/report"
)

// DefaultUnlistedSample is how many unlisted file names a report lists.
const DefaultUnlistedSample = 20

// Options configures a run.
type Options struct {
	// RestoredDir is the directory to inspect. Empty means none was supplied:
	// every check that needs it is reported as not performed.
	RestoredDir string
	// Hash enables SHA-256 verification of listed files that have a digest.
	Hash bool
	// RedactPaths replaces every file path in the report with a label and
	// leaves out the names of unlisted files.
	RedactPaths bool
	// ToolName and ToolVersion are recorded in the report.
	ToolName    string
	ToolVersion string
	// UnlistedSample overrides DefaultUnlistedSample when positive.
	UnlistedSample int
}

// Run verifies the manifest and returns the finished report. It returns an
// error only when the restored directory cannot be opened at all.
func Run(loaded *manifest.Loaded, opts Options) (*report.Report, error) {
	m := &loaded.Manifest
	r := &report.Report{
		Tool: report.Tool{Name: opts.ToolName, Version: opts.ToolVersion},
		Manifest: report.ManifestInfo{
			SchemaVersion: m.SchemaVersion,
			SHA256:        loaded.Digest,
			FileEntries:   len(m.Files),
		},
		Test: report.Test(m.Test),
		Inputs: report.Inputs{
			RestoredDirectorySupplied: opts.RestoredDir != "",
			HashingEnabled:            opts.Hash,
			PathsRedacted:             opts.RedactPaths,
		},
	}
	for _, s := range m.SuppliedResults {
		r.SuppliedResults = append(r.SuppliedResults, report.SuppliedResult{
			ID: s.ID, Description: s.Description, Outcome: s.Outcome,
			EvidenceRef: s.EvidenceRef, Command: s.Command, Notes: s.Notes,
		})
	}

	labels := make([]string, len(m.Files))
	for i, f := range m.Files {
		if opts.RedactPaths || f.Sensitive {
			labels[i] = fmt.Sprintf("[redacted file #%d]", i+1)
		} else {
			labels[i] = f.Path
		}
	}

	if opts.RestoredDir == "" {
		notSupplied(r, m, labels)
		r.Finalize()
		return r, nil
	}

	root, err := os.OpenRoot(opts.RestoredDir)
	if err != nil {
		return nil, fmt.Errorf("cannot open restored directory: %s", describe(err))
	}
	defer root.Close()
	if info, err := root.Stat("."); err != nil {
		return nil, fmt.Errorf("cannot open restored directory: %s", describe(err))
	} else if !info.IsDir() {
		return nil, errors.New("restored directory is not a directory")
	}

	sample := opts.UnlistedSample
	if sample <= 0 {
		sample = DefaultUnlistedSample
	}
	if opts.RedactPaths {
		sample = 0
	}
	w, err := walk(root, m.Files, sample)
	if err != nil {
		return nil, err
	}
	r.Observed = &w.observed

	directoryChecks(r, m, w)
	for i, f := range m.Files {
		fileChecks(r, root, f, labels[i], w, opts.Hash)
	}
	r.Finalize()
	return r, nil
}

func notSupplied(r *report.Report, m *manifest.Manifest, labels []string) {
	const reason = "no restored directory was supplied"
	add := func(kind, subject, expected string) {
		r.Checks = append(r.Checks, report.Check{Kind: kind, Subject: subject, Status: report.NotPerformed, Expected: expected, Reason: reason})
	}
	if m.Expected.FileCount != nil {
		add(report.KindFileCount, "", itoa(*m.Expected.FileCount))
	}
	if m.Expected.TotalBytes != nil {
		add(report.KindTotalBytes, "", itoa(*m.Expected.TotalBytes))
	}
	if !m.Expected.AllowUnlisted && len(m.Files) > 0 {
		add(report.KindUnlistedFiles, "", "0")
	}
	for i, f := range m.Files {
		add(report.KindPresence, labels[i], "regular file")
		add(report.KindSize, labels[i], optInt(f.Size))
		add(report.KindSHA256, labels[i], f.SHA256)
	}
}

type entryKind int

const (
	missing entryKind = iota
	regular
	symlink
	directory
	other
	unreadable
)

type entry struct {
	kind entryKind
	size int64
}

type walkResult struct {
	observed report.Observed
	entries  map[string]*entry
	// ancestors maps every parent directory of a listed path to what the walk
	// found there, when that was not a plain readable directory.
	blockedDirs map[string]entryKind
}

func walk(root *os.Root, files []manifest.File, sample int) (*walkResult, error) {
	w := &walkResult{
		entries:     make(map[string]*entry, len(files)),
		blockedDirs: map[string]entryKind{},
	}
	ancestors := map[string]bool{}
	for _, f := range files {
		w.entries[f.Path] = &entry{kind: missing}
		for dir := path.Dir(f.Path); dir != "."; dir = path.Dir(dir) {
			if ancestors[dir] {
				break
			}
			ancestors[dir] = true
		}
	}
	o := &w.observed

	err := fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return fmt.Errorf("cannot read restored directory: %s", describe(err))
			}
			o.UnreadableEntries++
			if ancestors[p] {
				w.blockedDirs[p] = unreadable
			}
			if e := w.entries[p]; e != nil {
				e.kind = unreadable
			}
			return nil // keep walking the rest of the tree
		}
		if p == "." {
			return nil
		}
		e := w.entries[p]
		t := d.Type()
		switch {
		case t&fs.ModeSymlink != 0:
			o.SymlinksNotFollowed++
			if e != nil {
				e.kind = symlink
			}
			if ancestors[p] {
				w.blockedDirs[p] = symlink
			}
		case t.IsDir():
			if e != nil {
				e.kind = directory
			}
		case t.IsRegular():
			info, err := d.Info()
			if err != nil {
				o.UnreadableEntries++
				if e != nil {
					e.kind = unreadable
				}
				return nil
			}
			o.RegularFiles++
			o.TotalBytes += info.Size()
			if e != nil {
				e.kind = regular
				e.size = info.Size()
			} else {
				o.UnlistedFiles++
				if len(o.UnlistedSample) < sample {
					o.UnlistedSample = append(o.UnlistedSample, p)
				}
			}
		default:
			o.OtherNonRegular++
			if e != nil {
				e.kind = other
			}
			if ancestors[p] {
				w.blockedDirs[p] = other
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

func directoryChecks(r *report.Report, m *manifest.Manifest, w *walkResult) {
	o := w.observed
	incomplete := ""
	if o.UnreadableEntries > 0 {
		incomplete = fmt.Sprintf("%d entries in the restored directory could not be read, so the totals are incomplete", o.UnreadableEntries)
	}
	compare := func(kind string, expected, observed int64) {
		c := report.Check{Kind: kind, Expected: itoa(expected), Observed: itoa(observed)}
		switch {
		case incomplete != "":
			c.Status, c.Reason = report.NotPerformed, incomplete
		case expected == observed:
			c.Status = report.Verified
		default:
			c.Status = report.Failed
		}
		r.Checks = append(r.Checks, c)
	}
	if m.Expected.FileCount != nil {
		compare(report.KindFileCount, *m.Expected.FileCount, o.RegularFiles)
	}
	if m.Expected.TotalBytes != nil {
		compare(report.KindTotalBytes, *m.Expected.TotalBytes, o.TotalBytes)
	}
	if !m.Expected.AllowUnlisted && len(m.Files) > 0 {
		compare(report.KindUnlistedFiles, 0, o.UnlistedFiles)
		if c := &r.Checks[len(r.Checks)-1]; c.Status == report.Failed {
			c.Reason = "the restored directory contains regular files that the manifest does not list"
		}
	}
}

func fileChecks(r *report.Report, root *os.Root, f manifest.File, label string, w *walkResult, hash bool) {
	e := w.entries[f.Path]
	presence := report.Check{Kind: report.KindPresence, Subject: label, Expected: "regular file"}
	size := report.Check{Kind: report.KindSize, Subject: label, Expected: optInt(f.Size), Status: report.NotPerformed}
	digest := report.Check{Kind: report.KindSHA256, Subject: label, Expected: f.SHA256, Status: report.NotPerformed}
	defer func() { r.Checks = append(r.Checks, presence, size, digest) }()

	skip := func(reason string) {
		size.Reason, digest.Reason = reason, reason
	}

	switch e.kind {
	case regular:
		presence.Status, presence.Observed = report.Verified, "regular file"
	case missing:
		blocked, kind := blockedAncestor(f.Path, w.blockedDirs)
		switch {
		case kind == symlink:
			presence.Status = report.Failed
			presence.Observed = "not found"
			presence.Reason = "a parent directory is a symbolic link, which is not followed"
		case kind == other:
			presence.Status = report.Failed
			presence.Observed = "not found"
			presence.Reason = "a parent path is not a plain directory (for example a mount point, junction or special file), so it was not entered"
		case blocked:
			presence.Status = report.NotPerformed
			presence.Reason = "a parent directory could not be read"
		default:
			presence.Status, presence.Observed = report.Failed, "not found"
		}
		skip("the file was not found")
		return
	case symlink:
		presence.Status, presence.Observed = report.Failed, "symbolic link"
		presence.Reason = "symbolic links are not followed"
		skip("the path is not a regular file")
		return
	case directory:
		presence.Status, presence.Observed = report.Failed, "directory"
		skip("the path is not a regular file")
		return
	case other:
		presence.Status, presence.Observed = report.Failed, "not a regular file"
		presence.Reason = "devices, pipes and sockets are not read"
		skip("the path is not a regular file")
		return
	default: // unreadable
		presence.Status = report.NotPerformed
		presence.Reason = "the entry could not be inspected"
		skip("the entry could not be inspected")
		return
	}

	if f.Size == nil {
		size.Reason = "the manifest gives no size for this file"
	} else {
		size.Observed = itoa(e.size)
		if *f.Size == e.size {
			size.Status = report.Verified
		} else {
			size.Status = report.Failed
		}
	}

	switch {
	case f.SHA256 == "":
		digest.Reason = "the manifest gives no SHA-256 digest for this file"
	case !hash:
		digest.Reason = "hashing was disabled"
	default:
		sum, n, err := hashFile(root, f.Path)
		switch {
		case err != nil:
			digest.Reason = err.Error()
		case n != e.size:
			digest.Reason = "the file changed size while it was being read"
		default:
			digest.Observed = sum
			if sum == f.SHA256 {
				digest.Status = report.Verified
			} else {
				digest.Status = report.Failed
			}
		}
	}
}

func blockedAncestor(p string, blocked map[string]entryKind) (bool, entryKind) {
	if len(blocked) == 0 {
		return false, missing
	}
	// Report the outermost blocked ancestor: that is where the walk stopped.
	found, kind := false, missing
	for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
		if k, ok := blocked[dir]; ok {
			found, kind = true, k
		}
	}
	return found, kind
}

func hashFile(root *os.Root, name string) (string, int64, error) {
	f, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return "", 0, fmt.Errorf("the file could not be opened: %s", describe(err))
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", 0, errors.New("the file changed while it was being verified")
	}
	h := sha256.New()
	buf := make([]byte, 1<<20)
	n, err := io.CopyBuffer(h, f, buf)
	if err != nil {
		return "", n, errors.New("the file could not be read completely")
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func describe(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "not found"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	default:
		return "read error"
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func optInt(n *int64) string {
	if n == nil {
		return ""
	}
	return itoa(*n)
}
