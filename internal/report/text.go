package report

import (
	"fmt"
	"io"
	"strconv"
	"unicode"
)

// WriteText writes a short line-oriented summary: the outcome, the counts,
// and every check that was not verified.
func WriteText(w io.Writer, r *Report) error {
	ew := &errWriter{w: w}
	ew.printf("Restore test %s: %s\n", safe(r.Test.ID), r.Outcome)
	ew.printf("Checks: %d verified, %d failed, %d not performed\n", r.Summary.Verified, r.Summary.Failed, r.Summary.NotPerformed)
	if len(r.SuppliedResults) > 0 {
		ew.printf("Supplied results (not verified by this tool): %d pass, %d fail, %d not performed\n",
			r.Summary.SuppliedPass, r.Summary.SuppliedFail, r.Summary.SuppliedNotPerformed)
	}
	for _, c := range r.Checks {
		if c.Status == Verified {
			continue
		}
		line := string(c.Status) + " " + c.Kind
		if c.Subject != "" {
			line += " " + safe(c.Subject)
		}
		if c.Expected != "" || c.Observed != "" {
			line += fmt.Sprintf(" (expected %s, observed %s)", orDash(c.Expected), orDash(c.Observed))
		}
		if c.Reason != "" {
			line += ": " + c.Reason
		}
		ew.printf("%s\n", line)
	}
	for _, s := range r.SuppliedResults {
		if s.Outcome != "pass" {
			ew.printf("supplied %s %s\n", s.Outcome, safe(s.ID))
		}
	}
	return ew.err
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// safe quotes values containing non-printable characters so that crafted
// file names or IDs cannot forge additional lines in terminal or CI output.
func safe(s string) string {
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return strconv.Quote(s)
		}
	}
	return s
}

type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err == nil {
		_, e.err = fmt.Fprintf(e.w, format, args...)
	}
}
