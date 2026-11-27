package output

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeString(s string) func(io.Writer) error {
	return func(w io.Writer) error { _, err := io.WriteString(w, s); return err }
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCreateNew(t *testing.T) {
	p := filepath.Join(t.TempDir(), "r.json")
	if err := Write(p, "-json", Options{}, writeString("new")); err != nil {
		t.Fatal(err)
	}
	if read(t, p) != "new" {
		t.Fatal("content mismatch")
	}
}

func TestNoOverwriteWithoutForce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "r.json")
	os.WriteFile(p, []byte("old"), 0o644)
	err := Write(p, "-json", Options{}, writeString("new"))
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
	if read(t, p) != "old" {
		t.Fatal("existing file was modified")
	}
}

func TestForceReplacesRegularFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "r.json")
	os.WriteFile(p, []byte("old"), 0o644)
	if err := Write(p, "-json", Options{Force: true}, writeString("new")); err != nil {
		t.Fatal(err)
	}
	if read(t, p) != "new" {
		t.Fatal("not replaced")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestForceRefusesDirectory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub")
	os.Mkdir(p, 0o755)
	if err := Write(p, "-json", Options{Force: true}, writeString("x")); err == nil {
		t.Fatal("replaced a directory")
	}
}

func TestFailedRenderLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "r.json")
	fail := func(io.Writer) error { return errors.New("boom") }
	if err := Write(p, "-json", Options{}, fail); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial file left behind")
	}
	os.WriteFile(p, []byte("old"), 0o644)
	if err := Write(p, "-json", Options{Force: true}, fail); err == nil {
		t.Fatal("expected error")
	}
	if read(t, p) != "old" {
		t.Fatal("failed forced write damaged the existing file")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestSymlinkTargetsAreNeverFollowed(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim.txt")
	os.WriteFile(victim, []byte("precious"), 0o644)
	link := filepath.Join(dir, "r.json")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("cannot create symbolic links here: %v", err)
	}
	if err := Write(link, "-json", Options{}, writeString("x")); err == nil {
		t.Error("wrote through a symbolic link without -force")
	}
	if err := Write(link, "-json", Options{Force: true}, writeString("x")); err == nil {
		t.Error("replaced a symbolic link with -force")
	}
	dangling := filepath.Join(dir, "dangling.json")
	if err := os.Symlink(filepath.Join(dir, "created-through-link"), dangling); err != nil {
		t.Fatal(err)
	}
	if err := Write(dangling, "-json", Options{}, writeString("x")); err == nil {
		t.Error("wrote through a dangling symbolic link")
	}
	if _, err := os.Lstat(filepath.Join(dir, "created-through-link")); err == nil {
		t.Error("file was created at the symbolic link's target")
	}
	if read(t, victim) != "precious" {
		t.Fatal("link target was modified")
	}
}

func TestRefusesInsideProtectedDirectory(t *testing.T) {
	restored := t.TempDir()
	os.Mkdir(filepath.Join(restored, "sub"), 0o755)
	for _, p := range []string{filepath.Join(restored, "r.json"), filepath.Join(restored, "sub", "r.json")} {
		err := Write(p, "-json", Options{ProtectedDirs: []string{restored}}, writeString("x"))
		if err == nil || !strings.Contains(err.Error(), "inside the restored directory") {
			t.Errorf("%s: err = %v", p, err)
		}
	}
	// A relative spelling of the protected directory is resolved too.
	wd, _ := os.Getwd()
	t.Chdir(restored)
	err := Write("r.json", "-json", Options{ProtectedDirs: []string{"."}}, writeString("x"))
	t.Chdir(wd)
	if err == nil {
		t.Error("relative protected directory not enforced")
	}
	if entries, _ := os.ReadDir(restored); len(entries) != 1 {
		t.Fatalf("restored directory was written to: %v", entries)
	}
	// A sibling directory with a shared prefix is fine.
	sibling := restored + "-reports"
	os.Mkdir(sibling, 0o755)
	t.Cleanup(func() { os.RemoveAll(sibling) })
	if err := Write(filepath.Join(sibling, "r.json"), "-json", Options{ProtectedDirs: []string{restored}}, writeString("x")); err != nil {
		t.Errorf("sibling directory refused: %v", err)
	}
}

func TestRefusesInsideProtectedDirectoryThroughSymlink(t *testing.T) {
	base := t.TempDir()
	restored := filepath.Join(base, "restored")
	os.Mkdir(restored, 0o755)
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(restored, alias); err != nil {
		t.Skipf("cannot create symbolic links here: %v", err)
	}
	if err := Write(filepath.Join(alias, "r.json"), "-json", Options{ProtectedDirs: []string{restored}}, writeString("x")); err == nil {
		t.Error("wrote into the restored directory through a symbolic link")
	}
}

func TestForceRefusesManifest(t *testing.T) {
	p := filepath.Join(t.TempDir(), "manifest.json")
	os.WriteFile(p, []byte("{}"), 0o644)
	err := Write(p, "-json", Options{Force: true, ProtectedFiles: []string{p}}, writeString("x"))
	if err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("err = %v", err)
	}
	if read(t, p) != "{}" {
		t.Fatal("manifest modified")
	}
}

func TestMissingDirectoryAndPrivacy(t *testing.T) {
	p := filepath.Join(t.TempDir(), "private-client-name", "r.json")
	err := Write(p, "-json", Options{}, writeString("x"))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "private-client-name") {
		t.Errorf("error leaks the path: %v", err)
	}
}

func TestWithin(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "data", "restored")
	if filepath.VolumeName(os.TempDir()) != "" {
		root = filepath.VolumeName(os.TempDir()) + root
	}
	cases := map[string]bool{
		root:                                   true,
		filepath.Join(root, "a"):               true,
		filepath.Join(root, "a", "b"):          true,
		filepath.Join(root, "..", "x"):         false,
		root + "-other":                        false,
		filepath.Join(root, "..restored", "x"): true, // a child literally named "..restored"
	}
	for p, want := range cases {
		if got := Within(p, root); got != want {
			t.Errorf("Within(%q) = %v, want %v", p, got, want)
		}
	}
}
