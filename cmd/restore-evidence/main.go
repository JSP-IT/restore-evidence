// Command restore-evidence documents and verifies the results of a backup
// restore test that has already been carried out.
//
// It reads a manifest and, optionally, a restored directory, and writes a
// report. It never starts a backup or restore, never runs commands from the
// manifest, and never writes to the restored directory.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/JSP-IT/restore-evidence/internal/manifest"
	"github.com/JSP-IT/restore-evidence/internal/output"
	"github.com/JSP-IT/restore-evidence/internal/report"
	"github.com/JSP-IT/restore-evidence/internal/verify"
)

const toolName = "restore-evidence"

var version = "dev"

// Exit codes.
const (
	exitPass       = 0
	exitFail       = 1
	exitError      = 2
	exitIncomplete = 3
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitError
	}
	switch args[0] {
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "version", "-version", "--version":
		fmt.Fprintf(stdout, "%s %s\n", toolName, versionString())
		return exitPass
	case "help", "-h", "-help", "--help":
		usage(stdout)
		return exitPass
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n\n", args[0])
		usage(stderr)
		return exitError
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  restore-evidence validate -manifest FILE
  restore-evidence verify   -manifest FILE [-restored DIR] [-json FILE|-] [-html FILE|-]
                            [-no-hash] [-redact-paths] [-force]
  restore-evidence version

Documents and verifies the results of a restore test that has already been
carried out. It never starts a backup or restore, never runs commands from the
manifest, and never writes to the restored directory.

Exit codes for verify: 0 pass, 1 fail, 3 incomplete (some checks not
performed), 2 usage or input error. Run "restore-evidence verify -h" for flags.
`)
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "path to the restore-test manifest (JSON)")
	if code, ok := parse(flags, args); !ok {
		return code
	}
	if *manifestPath == "" {
		fmt.Fprintln(stderr, "error: -manifest is required")
		return exitError
	}
	loaded, err := manifest.LoadFile(*manifestPath, manifest.Options{})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitError
	}
	m := loaded.Manifest
	fmt.Fprintf(stdout, "Manifest is valid: schema_version %d, %d files, %d supplied results, sha256 %s\n",
		m.SchemaVersion, len(m.Files), len(m.SuppliedResults), loaded.Digest)
	return exitPass
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "path to the restore-test manifest (JSON)")
	restored := flags.String("restored", "", "restored directory to inspect read-only (optional; without it, directory checks are reported as not performed)")
	jsonOut := flags.String("json", "", "write the JSON report to this new file, or - for standard output")
	htmlOut := flags.String("html", "", "write the HTML report to this new file, or - for standard output")
	noHash := flags.Bool("no-hash", false, "do not compute SHA-256 digests (digest checks are reported as not performed)")
	redact := flags.Bool("redact-paths", false, "replace every file path in the reports with a numbered label and omit unlisted file names")
	force := flags.Bool("force", false, "allow replacing existing report files (never directories, symbolic links or the manifest)")
	if code, ok := parse(flags, args); !ok {
		return code
	}
	if *manifestPath == "" {
		fmt.Fprintln(stderr, "error: -manifest is required")
		return exitError
	}
	if *jsonOut == "-" && *htmlOut == "-" {
		fmt.Fprintln(stderr, "error: only one of -json and -html can write to standard output")
		return exitError
	}
	if *jsonOut != "" && *htmlOut != "" && *jsonOut != "-" && *htmlOut != "-" && output.SamePath(*jsonOut, *htmlOut) {
		fmt.Fprintln(stderr, "error: -json and -html must name different files")
		return exitError
	}

	loaded, err := manifest.LoadFile(*manifestPath, manifest.Options{})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitError
	}
	rep, err := verify.Run(loaded, verify.Options{
		RestoredDir: *restored,
		Hash:        !*noHash,
		RedactPaths: *redact,
		ToolName:    toolName,
		ToolVersion: versionString(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitError
	}

	opts := output.Options{Force: *force, ProtectedFiles: []string{*manifestPath}}
	if *restored != "" {
		opts.ProtectedDirs = []string{*restored}
	}
	summaryTo := stdout
	for _, out := range []struct {
		flag, path string
		render     func(io.Writer, *report.Report) error
	}{
		{"-json", *jsonOut, report.WriteJSON},
		{"-html", *htmlOut, report.WriteHTML},
	} {
		switch out.path {
		case "":
			continue
		case "-":
			summaryTo = stderr
			if err := out.render(stdout, rep); err != nil {
				fmt.Fprintf(stderr, "error: %s: cannot write the report\n", out.flag)
				return exitError
			}
		default:
			err := output.Write(out.path, out.flag, opts, func(w io.Writer) error { return out.render(w, rep) })
			if err != nil {
				fmt.Fprintf(stderr, "error: %v\n", err)
				return exitError
			}
		}
	}
	if err := report.WriteText(summaryTo, rep); err != nil {
		return exitError
	}

	switch rep.Outcome {
	case report.OutcomePass:
		return exitPass
	case report.OutcomeFail:
		return exitFail
	default:
		return exitIncomplete
	}
}

func parse(flags *flag.FlagSet, args []string) (int, bool) {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitPass, false
		}
		return exitError, false
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(flags.Output(), "error: positional arguments are not supported")
		return exitError, false
	}
	return 0, true
}

func versionString() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}
