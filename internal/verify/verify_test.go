package verify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/JSP-IT/restore-evidence/internal/manifest"
	"github.com/JSP-IT/restore-evidence/internal/report"
)

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func size(n int) *int64 { v := int64(n); return &v }

// writeTree creates files (slash-separated name -> content) under a new
// temporary directory and returns it.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// entryFor returns a manifest file entry that matches content exactly.
func entryFor(name, content string) manifest.File {
	return manifest.File{Path: name, Size: size(len(content)), SHA256: digest(content)}
}

func build(t *testing.T, m manifest.Manifest) *manifest.Loaded {
	t.Helper()
	if m.SchemaVersion == 0 {
		m.SchemaVersion = 1
	}
	if m.Test.ID == "" {
		m.Test = manifest.Test{ID: "T-1", Title: "test"}
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	l, err := manifest.Load(bytes.NewReader(data), manifest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func run(t *testing.T, l *manifest.Loaded, opts Options) *report.Report {
	t.Helper()
	if opts.ToolName == "" {
		opts.ToolName, opts.ToolVersion = "restore-evidence", "test"
	}
	r, err := Run(l, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func find(t *testing.T, r *report.Report, kind, subject string) report.Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Kind == kind && c.Subject == subject {
			return c
		}
	}
	t.Fatalf("no %s check for %q in %+v", kind, subject, r.Checks)
	return report.Check{}
}

func want(t *testing.T, c report.Check, status report.Status) {
	t.Helper()
	if c.Status != status {
		t.Errorf("%s %s: status %s (reason %q), want %s", c.Kind, c.Subject, c.Status, c.Reason, status)
	}
}

func TestAllVerified(t *testing.T) {
	files := map[string]string{"a.txt": "alpha", "sub/b.txt": "bravo"}
	dir := writeTree(t, files)
	l := build(t, manifest.Manifest{
		Expected: manifest.Expected{FileCount: size(2), TotalBytes: size(10)},
		Files:    []manifest.File{entryFor("a.txt", "alpha"), entryFor("sub/b.txt", "bravo")},
		SuppliedResults: []manifest.SuppliedResult{
			{ID: "s", Description: "service started", Outcome: "pass"},
		},
	})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	if r.Outcome != report.OutcomePass {
		t.Fatalf("outcome %s, checks %+v", r.Outcome, r.Checks)
	}
	if r.Summary.Verified != 9 || r.Summary.Failed != 0 || r.Summary.NotPerformed != 0 {
		t.Fatalf("summary %+v", r.Summary)
	}
	if r.Observed.RegularFiles != 2 || r.Observed.TotalBytes != 10 {
		t.Fatalf("observed %+v", r.Observed)
	}
}

func TestMissingFile(t *testing.T) {
	dir := writeTree(t, map[string]string{"a.txt": "alpha"})
	l := build(t, manifest.Manifest{Files: []manifest.File{entryFor("a.txt", "alpha"), entryFor("gone.txt", "x")}})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	p := find(t, r, report.KindPresence, "gone.txt")
	want(t, p, report.Failed)
	if p.Observed != "not found" {
		t.Errorf("observed %q", p.Observed)
	}
	want(t, find(t, r, report.KindSize, "gone.txt"), report.NotPerformed)
	want(t, find(t, r, report.KindSHA256, "gone.txt"), report.NotPerformed)
	if r.Outcome != report.OutcomeFail {
		t.Fatalf("outcome %s", r.Outcome)
	}
}

func TestHashMismatch(t *testing.T) {
	dir := writeTree(t, map[string]string{"a.txt": "tampered"})
	e := entryFor("a.txt", "original")
	e.Size = size(len("tampered"))
	l := build(t, manifest.Manifest{Files: []manifest.File{e}})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	c := find(t, r, report.KindSHA256, "a.txt")
	want(t, c, report.Failed)
	if c.Expected != digest("original") || c.Observed != digest("tampered") {
		t.Errorf("expected/observed = %s / %s", c.Expected, c.Observed)
	}
	want(t, find(t, r, report.KindSize, "a.txt"), report.Verified)
	if r.Outcome != report.OutcomeFail {
		t.Fatalf("outcome %s", r.Outcome)
	}
}

func TestSizeMismatch(t *testing.T) {
	dir := writeTree(t, map[string]string{"a.txt": "abc"})
	l := build(t, manifest.Manifest{Files: []manifest.File{{Path: "a.txt", Size: size(4)}}})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	c := find(t, r, report.KindSize, "a.txt")
	want(t, c, report.Failed)
	if c.Expected != "4" || c.Observed != "3" {
		t.Errorf("expected/observed = %s / %s", c.Expected, c.Observed)
	}
}

func TestCountAndTotalMismatch(t *testing.T) {
	dir := writeTree(t, map[string]string{"a": "1", "b": "22", "c": "333"})
	l := build(t, manifest.Manifest{
		Expected: manifest.Expected{FileCount: size(4), TotalBytes: size(10), AllowUnlisted: true},
		Files:    []manifest.File{entryFor("a", "1")},
	})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	fc := find(t, r, report.KindFileCount, "")
	want(t, fc, report.Failed)
	if fc.Expected != "4" || fc.Observed != "3" {
		t.Errorf("file_count %+v", fc)
	}
	tb := find(t, r, report.KindTotalBytes, "")
	want(t, tb, report.Failed)
	if tb.Observed != "6" {
		t.Errorf("total_bytes %+v", tb)
	}
	for _, c := range r.Checks {
		if c.Kind == report.KindUnlistedFiles {
			t.Fatalf("unlisted check should not run when allow_unlisted is true")
		}
	}
	if r.Observed.UnlistedFiles != 2 {
		t.Errorf("unlisted %d", r.Observed.UnlistedFiles)
	}
}

func TestUnlistedFiles(t *testing.T) {
	dir := writeTree(t, map[string]string{"a": "1", "extra/z": "2", "extra/y": "3"})
	l := build(t, manifest.Manifest{Files: []manifest.File{entryFor("a", "1")}})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	want(t, find(t, r, report.KindUnlistedFiles, ""), report.Failed)
	if got := strings.Join(r.Observed.UnlistedSample, ","); got != "extra/y,extra/z" {
		t.Errorf("sample %q (must be lexical)", got)
	}

	r = run(t, l, Options{RestoredDir: dir, Hash: true, RedactPaths: true})
	if len(r.Observed.UnlistedSample) != 0 {
		t.Errorf("redacted report lists unlisted names: %v", r.Observed.UnlistedSample)
	}
}

func TestNoRestoredDirectory(t *testing.T) {
	l := build(t, manifest.Manifest{
		Expected:        manifest.Expected{FileCount: size(1)},
		Files:           []manifest.File{entryFor("a", "1")},
		SuppliedResults: []manifest.SuppliedResult{{ID: "s", Description: "d", Outcome: "pass"}},
	})
	r := run(t, l, Options{Hash: true})
	if r.Outcome != report.OutcomeIncomplete {
		t.Fatalf("outcome %s", r.Outcome)
	}
	for _, c := range r.Checks {
		want(t, c, report.NotPerformed)
		if c.Reason == "" {
			t.Errorf("%s check has no reason", c.Kind)
		}
	}
	if r.Observed != nil {
		t.Errorf("observed should be absent")
	}
	if r.Inputs.RestoredDirectorySupplied {
		t.Errorf("inputs claim a directory was supplied")
	}
}

func TestHashingDisabledAndMissingDigest(t *testing.T) {
	dir := writeTree(t, map[string]string{"a": "1", "b": "2"})
	l := build(t, manifest.Manifest{Files: []manifest.File{entryFor("a", "1"), {Path: "b", Size: size(1)}}})
	r := run(t, l, Options{RestoredDir: dir, Hash: false})
	c := find(t, r, report.KindSHA256, "a")
	want(t, c, report.NotPerformed)
	if !strings.Contains(c.Reason, "disabled") {
		t.Errorf("reason %q", c.Reason)
	}
	r = run(t, l, Options{RestoredDir: dir, Hash: true})
	c = find(t, r, report.KindSHA256, "b")
	want(t, c, report.NotPerformed)
	if !strings.Contains(c.Reason, "no SHA-256") {
		t.Errorf("reason %q", c.Reason)
	}
	if r.Outcome != report.OutcomeIncomplete {
		t.Errorf("outcome %s", r.Outcome)
	}
}

func TestSuppliedResultsAffectOutcomeButAreNotVerified(t *testing.T) {
	dir := writeTree(t, map[string]string{"a": "1"})
	base := manifest.Manifest{Files: []manifest.File{entryFor("a", "1")}}

	base.SuppliedResults = []manifest.SuppliedResult{{ID: "s", Description: "d", Outcome: "fail"}}
	r := run(t, build(t, base), Options{RestoredDir: dir, Hash: true})
	if r.Outcome != report.OutcomeFail || r.Summary.Failed != 0 || r.Summary.SuppliedFail != 1 {
		t.Errorf("supplied fail: outcome %s summary %+v", r.Outcome, r.Summary)
	}

	base.SuppliedResults = []manifest.SuppliedResult{{ID: "s", Description: "d", Outcome: "not_performed"}}
	r = run(t, build(t, base), Options{RestoredDir: dir, Hash: true})
	if r.Outcome != report.OutcomeIncomplete {
		t.Errorf("supplied not_performed: outcome %s", r.Outcome)
	}
}

func TestDirectoryWhereFileExpected(t *testing.T) {
	dir := writeTree(t, map[string]string{"a/inner": "x"})
	l := build(t, manifest.Manifest{
		Expected: manifest.Expected{AllowUnlisted: true},
		Files:    []manifest.File{{Path: "a", Size: size(1)}},
	})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	c := find(t, r, report.KindPresence, "a")
	want(t, c, report.Failed)
	if c.Observed != "directory" {
		t.Errorf("observed %q", c.Observed)
	}
}

func TestParentIsAFile(t *testing.T) {
	dir := writeTree(t, map[string]string{"a": "x"})
	l := build(t, manifest.Manifest{
		Expected: manifest.Expected{AllowUnlisted: true},
		Files:    []manifest.File{{Path: "a/b"}},
	})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	want(t, find(t, r, report.KindPresence, "a/b"), report.Failed)
}

func TestRedaction(t *testing.T) {
	dir := writeTree(t, map[string]string{"hr/salaries.csv": "s", "public.txt": "p"})
	secret := entryFor("hr/salaries.csv", "s")
	secret.Sensitive = true
	l := build(t, manifest.Manifest{Files: []manifest.File{secret, entryFor("public.txt", "p")}})

	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	assertNotInReport(t, r, "salaries")
	find(t, r, report.KindPresence, "[redacted file #1]")
	find(t, r, report.KindPresence, "public.txt")

	r = run(t, l, Options{RestoredDir: dir, Hash: true, RedactPaths: true})
	assertNotInReport(t, r, "salaries")
	assertNotInReport(t, r, "public.txt")
	find(t, r, report.KindPresence, "[redacted file #2]")
	if !r.Inputs.PathsRedacted {
		t.Error("inputs.paths_redacted not set")
	}
}

func TestReportOmitsHostPathsAndContent(t *testing.T) {
	const content = "UNIQUE-CONTENT-MARKER-1234"
	dir := writeTree(t, map[string]string{"a.txt": content})
	l := build(t, manifest.Manifest{Files: []manifest.File{entryFor("a.txt", content)}})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	assertNotInReport(t, r, content)
	assertNotInReport(t, r, dir)
	assertNotInReport(t, r, filepath.ToSlash(dir))
}

func assertNotInReport(t *testing.T, r *report.Report, s string) {
	t.Helper()
	var j, h bytes.Buffer
	if err := report.WriteJSON(&j, r); err != nil {
		t.Fatal(err)
	}
	if err := report.WriteHTML(&h, r); err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{"JSON": j.Bytes(), "HTML": h.Bytes()} {
		if bytes.Contains(b, []byte(s)) || bytes.Contains(b, []byte(strings.ReplaceAll(s, `\`, `\\`))) {
			t.Errorf("%s report contains %q", name, s)
		}
	}
}

func TestDeterministic(t *testing.T) {
	files := map[string]string{}
	var entries []manifest.File
	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("d%d/f%02d.txt", i%5, i)
		files[name] = strings.Repeat("x", i)
		entries = append(entries, entryFor(name, files[name]))
	}
	dir := writeTree(t, files)
	l := build(t, manifest.Manifest{Files: entries})
	var first []byte
	for i := 0; i < 3; i++ {
		var j, h bytes.Buffer
		r := run(t, l, Options{RestoredDir: dir, Hash: true})
		if err := report.WriteJSON(&j, r); err != nil {
			t.Fatal(err)
		}
		if err := report.WriteHTML(&h, r); err != nil {
			t.Fatal(err)
		}
		out := append(j.Bytes(), h.Bytes()...)
		if first == nil {
			first = out
		} else if !bytes.Equal(first, out) {
			t.Fatal("report output differs between runs")
		}
	}
}

// snapshot records every entry under dir with its size, mode, mtime and
// content digest.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		v := fmt.Sprintf("%v %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			v += " " + digest(string(b))
		}
		out[p] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRestoredDirectoryIsNotModified(t *testing.T) {
	dir := writeTree(t, map[string]string{"a": "1", "b/c": "2", "b/d": "3"})
	before := snapshot(t, dir)
	l := build(t, manifest.Manifest{Files: []manifest.File{entryFor("a", "1"), entryFor("b/c", "wrong"), entryFor("missing", "x")}})
	run(t, l, Options{RestoredDir: dir, Hash: true})
	after := snapshot(t, dir)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("restored directory changed:\nbefore %v\nafter  %v", before, after)
	}
}

func TestRestoredDirectoryErrors(t *testing.T) {
	l := build(t, manifest.Manifest{Files: []manifest.File{{Path: "a"}}})
	dir := t.TempDir()
	if _, err := Run(l, Options{RestoredDir: filepath.Join(dir, "secret-name-nope")}); err == nil || strings.Contains(err.Error(), "secret-name-nope") {
		t.Errorf("missing dir: %v", err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(l, Options{RestoredDir: file}); err == nil {
		t.Error("a regular file was accepted as the restored directory")
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symbolic links here: %v", err)
	}
}

func TestSymlinkToFileOutsideIsNotFollowed(t *testing.T) {
	const secret = "OUTSIDE-SECRET"
	outside := writeTree(t, map[string]string{"secret.txt": secret})
	dir := writeTree(t, map[string]string{"a": "1"})
	symlinkOrSkip(t, filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt"))

	l := build(t, manifest.Manifest{Files: []manifest.File{entryFor("a", "1"), entryFor("link.txt", secret)}})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	c := find(t, r, report.KindPresence, "link.txt")
	want(t, c, report.Failed)
	if c.Observed != "symbolic link" {
		t.Errorf("observed %q", c.Observed)
	}
	h := find(t, r, report.KindSHA256, "link.txt")
	want(t, h, report.NotPerformed)
	if h.Observed != "" {
		t.Errorf("a digest was computed through the link: %s", h.Observed)
	}
	if r.Observed.SymlinksNotFollowed != 1 || r.Observed.RegularFiles != 1 {
		t.Errorf("observed %+v", r.Observed)
	}
}

func TestSymlinkedDirectoryIsNotFollowed(t *testing.T) {
	outside := writeTree(t, map[string]string{"etc/passwd": "root:x:0:0", "etc/shadow": "nope"})
	dir := writeTree(t, map[string]string{"a": "1"})
	symlinkOrSkip(t, filepath.Join(outside, "etc"), filepath.Join(dir, "etc"))

	l := build(t, manifest.Manifest{Files: []manifest.File{entryFor("a", "1"), entryFor("etc/passwd", "root:x:0:0")}})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	c := find(t, r, report.KindPresence, "etc/passwd")
	want(t, c, report.Failed)
	if !strings.Contains(c.Reason, "symbolic link") {
		t.Errorf("reason %q", c.Reason)
	}
	want(t, find(t, r, report.KindSHA256, "etc/passwd"), report.NotPerformed)
	if r.Observed.RegularFiles != 1 {
		t.Errorf("files outside the root were counted: %+v", r.Observed)
	}
}

func TestRelativeSymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	if err := os.WriteFile(filepath.Join(parent, "secret.txt"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(parent, "restored")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, filepath.Join("..", "secret.txt"), filepath.Join(dir, "up"))
	l := build(t, manifest.Manifest{
		Expected: manifest.Expected{AllowUnlisted: true},
		Files:    []manifest.File{entryFor("up", "s")},
	})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	want(t, find(t, r, report.KindPresence, "up"), report.Failed)
	want(t, find(t, r, report.KindSHA256, "up"), report.NotPerformed)
}

func TestUnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits are not enforced here")
	}
	dir := writeTree(t, map[string]string{"ok": "1", "locked/a": "2"})
	locked := filepath.Join(dir, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	l := build(t, manifest.Manifest{
		Expected: manifest.Expected{FileCount: size(2)},
		Files:    []manifest.File{entryFor("ok", "1"), entryFor("locked/a", "2")},
	})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	want(t, find(t, r, report.KindPresence, "locked/a"), report.NotPerformed)
	want(t, find(t, r, report.KindFileCount, ""), report.NotPerformed)
	want(t, find(t, r, report.KindUnlistedFiles, ""), report.NotPerformed)
	if r.Outcome != report.OutcomeIncomplete {
		t.Errorf("outcome %s", r.Outcome)
	}
}

func TestLargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("large input")
	}
	dir := t.TempDir()
	const n = 48 << 20 // spans many read buffers
	f, err := os.Create(filepath.Join(dir, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	block := bytes.Repeat([]byte("0123456789abcdef"), 4096)
	for written := 0; written < n; written += len(block) {
		block[0] = byte(written)
		f.Write(block)
		h.Write(block)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	l := build(t, manifest.Manifest{Files: []manifest.File{{Path: "big.bin", Size: size(n), SHA256: hex.EncodeToString(h.Sum(nil))}}})
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	want(t, find(t, r, report.KindSHA256, "big.bin"), report.Verified)
	want(t, find(t, r, report.KindSize, "big.bin"), report.Verified)
}

func TestManyFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("large input")
	}
	const n = 3000
	files := make(map[string]string, n)
	entries := make([]manifest.File, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("d%02d/f%05d", i%40, i)
		files[name] = fmt.Sprint(i)
		entries = append(entries, entryFor(name, files[name]))
	}
	dir := writeTree(t, files)
	l := build(t, manifest.Manifest{Expected: manifest.Expected{FileCount: size(n)}, Files: entries})
	start := time.Now()
	r := run(t, l, Options{RestoredDir: dir, Hash: true})
	if r.Outcome != report.OutcomePass {
		t.Fatalf("outcome %s summary %+v", r.Outcome, r.Summary)
	}
	if d := time.Since(start); d > 60*time.Second {
		t.Fatalf("verifying %d files took %s", n, d)
	}
}
