// Package output writes report files without overwriting anything by
// accident.
//
// Without Force, a report is created with O_EXCL, so an existing file or
// symbolic link at the target path is never replaced or followed. With Force,
// an existing regular file is replaced atomically through a temporary file and
// a rename; directories, symbolic links and other special files are still
// refused. Reports are never written inside a protected directory (the
// restored directory) or over a protected file (the manifest).
package output

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Options controls Write.
type Options struct {
	Force          bool
	ProtectedDirs  []string
	ProtectedFiles []string
}

// Write creates the file name and fills it by calling render. Error messages
// name the flag-supplied path only as the caller passes it in label.
func Write(name, label string, opts Options, render func(io.Writer) error) error {
	abs, err := filepath.Abs(name)
	if err != nil {
		return fmt.Errorf("%s: cannot resolve path", label)
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return fmt.Errorf("%s: the output directory does not exist", label)
	}
	target := filepath.Join(dir, filepath.Base(abs))

	for _, p := range opts.ProtectedDirs {
		// Make the path absolute before resolving it, exactly as for the
		// target. Resolving a relative path first would leave the working
		// directory unresolved (for example /var instead of /private/var on
		// macOS, or an 8.3 short name on Windows) and the check would miss.
		resolved, err := filepath.Abs(p)
		if err == nil {
			resolved, err = filepath.EvalSymlinks(resolved)
		}
		if err != nil {
			return fmt.Errorf("%s: cannot resolve the restored directory to check the output location", label)
		}
		if Within(target, resolved) {
			return fmt.Errorf("%s: refusing to write inside the restored directory", label)
		}
	}

	existing, err := os.Lstat(target)
	switch {
	case err == nil:
		if !opts.Force {
			return fmt.Errorf("%s: file already exists (use -force to replace it)", label)
		}
		if !existing.Mode().IsRegular() {
			return fmt.Errorf("%s: refusing to replace something that is not a regular file", label)
		}
		for _, p := range opts.ProtectedFiles {
			if info, err := os.Stat(p); err == nil && os.SameFile(info, existing) {
				return fmt.Errorf("%s: refusing to replace the manifest", label)
			}
		}
		return replace(target, label, render)
	case errors.Is(err, fs.ErrNotExist):
		return create(target, label, render)
	default:
		return fmt.Errorf("%s: cannot inspect the output path", label)
	}
}

func create(target, label string, render func(io.Writer) error) (err error) {
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s: file already exists (use -force to replace it)", label)
		}
		return fmt.Errorf("%s: cannot create the file", label)
	}
	defer func() {
		if err != nil {
			os.Remove(target)
		}
	}()
	return finish(f, label, render)
}

func replace(target, label string, render func(io.Writer) error) (err error) {
	f, err := os.CreateTemp(filepath.Dir(target), ".restore-evidence-*.tmp")
	if err != nil {
		return fmt.Errorf("%s: cannot create a temporary file next to the output", label)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			os.Remove(tmp)
		}
	}()
	if err := finish(f, label, render); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("%s: cannot replace the existing file", label)
	}
	return nil
}

func finish(f *os.File, label string, render func(io.Writer) error) error {
	bw := bufio.NewWriterSize(f, 1<<16)
	if err := render(bw); err != nil {
		f.Close()
		return fmt.Errorf("%s: cannot write the report", label)
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		return fmt.Errorf("%s: cannot write the report", label)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("%s: cannot write the report", label)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%s: cannot write the report", label)
	}
	return nil
}

// Within reports whether path is dir itself or inside it. Both must be
// absolute and already resolved.
func Within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// SamePath reports whether two output paths refer to the same location, as
// far as can be told before either exists.
func SamePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(aa, bb)
	}
	return aa == bb
}
