package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validHash = "ca51ad1a436bbadf1b97ae3b92d7706cc43feee822a86c185f63aceb084b15dc"

func load(t *testing.T, s string) (*Loaded, error) {
	t.Helper()
	return Load(strings.NewReader(s), Options{})
}

func mustProblems(t *testing.T, err error, wants ...string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected a ValidationError, got %v", err)
	}
	joined := strings.Join(ve.Problems, "\n")
	for _, w := range wants {
		if !strings.Contains(joined, w) {
			t.Errorf("problems do not mention %q:\n%s", w, joined)
		}
	}
}

func TestLoadExample(t *testing.T) {
	l, err := LoadFile(filepath.Join("..", "..", "examples", "manifest.json"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(l.Manifest.Files); got != 4 {
		t.Fatalf("files = %d, want 4", got)
	}
	if len(l.Digest) != 64 {
		t.Fatalf("digest %q is not a SHA-256 hex digest", l.Digest)
	}
}

func TestFilesAreSortedAndDigestsLowercased(t *testing.T) {
	l, err := load(t, `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[
		{"path":"z.txt"},{"path":"a/b.txt","sha256":"`+strings.ToUpper(validHash)+`"},{"path":"m.txt"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range l.Manifest.Files {
		got = append(got, f.Path)
	}
	if strings.Join(got, ",") != "a/b.txt,m.txt,z.txt" {
		t.Fatalf("order = %v", got)
	}
	if l.Manifest.Files[0].SHA256 != validHash {
		t.Fatalf("digest not lowercased: %s", l.Manifest.Files[0].SHA256)
	}
}

func TestByteOrderMarkAccepted(t *testing.T) {
	if _, err := load(t, "\xef\xbb\xbf"+`{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"x"}]}`); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedManifests(t *testing.T) {
	cases := []struct {
		name, input string
		want        []string
	}{
		{"empty input", ``, []string{"malformed JSON"}},
		{"not json", `hello`, []string{"malformed JSON at line 1"}},
		{"truncated", `{"schema_version":1,"test":{`, []string{"malformed JSON"}},
		{"trailing comma", "{\"schema_version\":1,\n\"files\":[{\"path\":\"a\"},]}", []string{"line 2"}},
		{"array root", `[1,2]`, []string{"(root)"}},
		{"trailing data", `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"x"}]} {}`, []string{"after top-level value"}},
		{"missing schema", `{"test":{"id":"a","title":"b"}}`, []string{"schema_version: required"}},
		{"string schema", `{"schema_version":"1"}`, []string{"schema_version: must be an integer"}},
		{"future schema", `{"schema_version":2,"new_field":true}`, []string{"schema_version: 2 is not supported (this build supports 1)"}},
		{"unknown field", `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"x","mode":"0644"}]}`, []string{`unknown field "mode"`}},
		{"wrong type", `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"x","size":"12"}]}`, []string{"size: expected int64, found JSON string"}},
		{"invalid utf8", "{\"schema_version\":1,\"test\":{\"id\":\"\xff\",\"title\":\"b\"}}", []string{"not valid UTF-8"}},
		{"nothing to check", `{"schema_version":1,"test":{"id":"a","title":"b"}}`, []string{"nothing to check"}},
		{"missing test fields", `{"schema_version":1,"files":[{"path":"x"}]}`, []string{"test.id: required", "test.title: required"}},
		{"bad hash", `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"x","sha256":"abc"}]}`, []string{"files[0].sha256: must be 64"}},
		{"non-hex hash", `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"x","sha256":"` + strings.Repeat("g", 64) + `"}]}`, []string{"files[0].sha256"}},
		{"negative size", `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"x","size":-1}]}`, []string{"files[0].size: must not be negative"}},
		{"bad timestamp", `{"schema_version":1,"test":{"id":"a","title":"b","restore_started_at":"yesterday"},"files":[{"path":"x"}]}`, []string{"test.restore_started_at: must be an RFC 3339"}},
		{"completed before started", `{"schema_version":1,"test":{"id":"a","title":"b","restore_started_at":"2026-01-02T00:00:00Z","restore_completed_at":"2026-01-01T00:00:00Z"},"files":[{"path":"x"}]}`, []string{"earlier than test.restore_started_at"}},
		{"duplicate path", `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"x"},{"path":"x"}]}`, []string{"files[1].path: duplicates files[0]"}},
		{"file and dir", `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"a"},{"path":"a/b/c"}]}`, []string{"files[0].path: is listed as a file but files[1] is inside it"}},
		{"file count mismatch", `{"schema_version":1,"test":{"id":"a","title":"b"},"expected":{"file_count":3},"files":[{"path":"x"}]}`, []string{"expected.file_count: is 3 but 1 files are listed"}},
		{"file count below listed", `{"schema_version":1,"test":{"id":"a","title":"b"},"expected":{"file_count":0,"allow_unlisted":true},"files":[{"path":"x"}]}`, []string{"expected.file_count: is 0 but 1 files are listed"}},
		{"total bytes mismatch", `{"schema_version":1,"test":{"id":"a","title":"b"},"expected":{"total_bytes":5},"files":[{"path":"x","size":3}]}`, []string{"expected.total_bytes: is 5 but listed sizes add up to 3"}},
		{"size overflow", `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"x","size":9223372036854775807},{"path":"y","size":1}]}`, []string{"overflows"}},
		{"bad outcome", `{"schema_version":1,"test":{"id":"a","title":"b"},"supplied_results":[{"id":"r","description":"d","outcome":"ok"}]}`, []string{"outcome: must be pass, fail or not_performed"}},
		{"missing outcome", `{"schema_version":1,"test":{"id":"a","title":"b"},"supplied_results":[{"id":"r","description":"d"}]}`, []string{"outcome: required"}},
		{"duplicate result id", `{"schema_version":1,"test":{"id":"a","title":"b"},"supplied_results":[{"id":"r","description":"d","outcome":"pass"},{"id":"r","description":"d","outcome":"pass"}]}`, []string{"supplied_results[1].id: duplicates"}},
		{"traversal", `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"../etc/passwd"}]}`, []string{`files[0].path: must not contain ".." segments`}},
		{"absolute", `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"/etc/passwd"}]}`, []string{"files[0].path: must be relative"}},
		{"too long text", `{"schema_version":1,"test":{"id":"a","title":"` + strings.Repeat("t", 600) + `"},"files":[{"path":"x"}]}`, []string{"test.title: longer than 512"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.input)
			mustProblems(t, err, tc.want...)
		})
	}
}

func TestAllProblemsReported(t *testing.T) {
	_, err := load(t, `{"schema_version":1,"test":{},"files":[{"path":"../a"},{"path":"b","size":-2}]}`)
	var ve *ValidationError
	if !errors.As(err, &ve) || len(ve.Problems) < 4 {
		t.Fatalf("expected at least 4 problems, got %v", err)
	}
}

func TestCheckPath(t *testing.T) {
	good := []string{"a", "a/b.txt", "dir with space/file", "café/naïve.txt", "a..b", "...", ".hidden/x", "backup-12:00.tar", "con"}
	for _, p := range good {
		if msg := CheckPath(p); msg != "" {
			t.Errorf("CheckPath(%q) = %q, want accepted", p, msg)
		}
	}
	bad := map[string]string{
		"":                        "required",
		"/etc/passwd":             "relative",
		"../x":                    `".."`,
		"a/../../x":               `".."`,
		"a/..":                    `".."`,
		"./a":                     `"."`,
		"a/./b":                   `"."`,
		"a//b":                    "empty segments",
		"a/":                      "trailing slash",
		`a\b`:                     "forward slashes",
		`..\..\windows\win.ini`:   "forward slashes",
		"C:/Windows/win.ini":      "drive letter",
		"c:x":                     "drive letter",
		"a\x00b":                  "control characters",
		"a\nb":                    "control characters",
		"\xff":                    "UTF-8",
		strings.Repeat("a", 5000): "longer than",
	}
	for p, want := range bad {
		if msg := CheckPath(p); !strings.Contains(msg, want) {
			t.Errorf("CheckPath(%q) = %q, want message containing %q", p, msg, want)
		}
	}
}

func TestSizeLimit(t *testing.T) {
	big := `{"schema_version":1,"test":{"id":"a","title":"b"},"files":[{"path":"x"}],"pad":"` + strings.Repeat("x", 4096) + `"}`
	_, err := Load(strings.NewReader(big), Options{MaxBytes: 1024})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestFileCountLimit(t *testing.T) {
	m := buildManifest(11)
	_, err := Load(strings.NewReader(m), Options{MaxFiles: 10})
	mustProblems(t, err, "files: 11 entries exceed the limit of 10")
}

func TestLargeManifest(t *testing.T) {
	if testing.Short() {
		t.Skip("large input")
	}
	const n = 100_000
	m := buildManifest(n)
	start := time.Now()
	l, err := Load(strings.NewReader(m), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Manifest.Files) != n {
		t.Fatalf("files = %d", len(l.Manifest.Files))
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("loading %d entries took %s", n, d)
	}
}

func buildManifest(n int) string {
	var b strings.Builder
	b.WriteString(`{"schema_version":1,"test":{"id":"large","title":"large"},"files":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"path":"d%03d/f%07d.bin","size":%d,"sha256":"%s"}`, i%1000, i, i, validHash)
	}
	b.WriteString(`]}`)
	return b.String()
}

func TestLoadFileErrorsOmitPath(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret-project-name", "m.json")
	_, err := LoadFile(secret, Options{})
	if err == nil || strings.Contains(err.Error(), "secret-project-name") {
		t.Fatalf("error should not contain the path: %v", err)
	}
	_, err = LoadFile(dir, Options{})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory as manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "m.json"), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadFile(filepath.Join(dir, "m.json"), Options{})
	mustProblems(t, err, "malformed JSON")
}
