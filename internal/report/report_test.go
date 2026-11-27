package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func hostileReport() *Report {
	r := &Report{
		Tool:     Tool{Name: "restore-evidence", Version: "test"},
		Manifest: ManifestInfo{SchemaVersion: 1, SHA256: strings.Repeat("a", 64), FileEntries: 1},
		Test: Test{
			ID:    `T-1"><script>alert(1)</script>`,
			Title: "<script>alert('title')</script>",
			Notes: "</dd><img src=x onerror=alert(1)>",
		},
		Inputs: Inputs{RestoredDirectorySupplied: true, HashingEnabled: true},
		Checks: []Check{
			{Kind: KindPresence, Subject: `x"><img src=x onerror=alert(2)>.txt`, Status: Failed, Expected: "regular file", Observed: "not found"},
			{Kind: KindSHA256, Subject: "<b>bold</b>", Status: NotPerformed, Reason: "the file was not found"},
		},
		SuppliedResults: []SuppliedResult{{
			ID:          "<i>id</i>",
			Description: "<iframe src=//example.invalid></iframe>",
			Outcome:     "pass",
			EvidenceRef: "javascript:alert(3)",
			Command:     "</pre><script>fetch('//example.invalid')</script> && rm -rf /",
		}},
		Observed: &Observed{UnlistedFiles: 1, UnlistedSample: []string{"<svg onload=alert(4)>"}},
	}
	r.Finalize()
	return r
}

func TestHTMLEscaping(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteHTML(&buf, hostileReport()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	lower := strings.ToLower(out)
	// Attribute-like text such as "onerror=" may appear, but only as escaped
	// text: no element other than the template's own may be opened.
	for _, bad := range []string{"<script", "<img", "<iframe", "<svg", "<b>", "<i>", "<a ", "href="} {
		if strings.Contains(lower, bad) {
			t.Errorf("HTML contains unescaped %q", bad)
		}
	}
	for _, good := range []string{"&lt;script&gt;", "&lt;img src=x", "&lt;/pre&gt;", "&lt;svg onload=alert(4)&gt;"} {
		if !strings.Contains(out, good) {
			t.Errorf("HTML does not contain escaped %q", good)
		}
	}
	if !strings.Contains(out, "Content-Security-Policy") || !strings.Contains(out, "default-src 'none'") {
		t.Error("HTML lacks the restrictive Content-Security-Policy")
	}
}

func TestJSONEscapesMarkup(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, hostileReport()); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("<script")) {
		t.Error("JSON contains a raw <script tag")
	}
	var back Report
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if back.Test.Title != "<script>alert('title')</script>" {
		t.Errorf("title did not round-trip: %q", back.Test.Title)
	}
}

func TestTextQuotesControlCharacters(t *testing.T) {
	r := &Report{
		Test:   Test{ID: "T\nfake: pass"},
		Checks: []Check{{Kind: KindPresence, Subject: "a\nverified presence b", Status: Failed}},
	}
	r.Finalize()
	var buf bytes.Buffer
	if err := WriteText(&buf, r); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, "fake") || strings.HasPrefix(line, "verified") {
			t.Errorf("crafted value forged a line: %q", line)
		}
	}
}

func TestFinalizeOutcome(t *testing.T) {
	cases := []struct {
		name     string
		checks   []Status
		supplied []string
		want     Outcome
	}{
		{"all verified", []Status{Verified, Verified}, []string{"pass"}, OutcomePass},
		{"one failed", []Status{Verified, Failed, NotPerformed}, nil, OutcomeFail},
		{"not performed", []Status{Verified, NotPerformed}, nil, OutcomeIncomplete},
		{"supplied fail", []Status{Verified}, []string{"pass", "fail"}, OutcomeFail},
		{"supplied not performed", []Status{Verified}, []string{"not_performed"}, OutcomeIncomplete},
		{"supplied only", nil, []string{"pass"}, OutcomePass},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Report{}
			for _, s := range tc.checks {
				r.Checks = append(r.Checks, Check{Kind: KindPresence, Status: s})
			}
			for _, o := range tc.supplied {
				r.SuppliedResults = append(r.SuppliedResults, SuppliedResult{ID: o, Outcome: o})
			}
			r.Finalize()
			if r.Outcome != tc.want {
				t.Errorf("outcome %s, want %s", r.Outcome, tc.want)
			}
		})
	}
}

func TestEmptyReportHasArrays(t *testing.T) {
	r := &Report{}
	r.Finalize()
	var buf bytes.Buffer
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`"checks": []`, `"supplied_results": []`, `"report_schema_version": 1`} {
		if !strings.Contains(buf.String(), s) {
			t.Errorf("JSON lacks %s", s)
		}
	}
}

// The report must not overstate what it shows.
func TestNoOverclaiming(t *testing.T) {
	r := hostileReport()
	r.Test = Test{ID: "T", Title: "T"}
	r.Checks, r.SuppliedResults, r.Observed = nil, nil, nil
	r.Finalize()
	var j, h, txt bytes.Buffer
	WriteJSON(&j, r)
	WriteHTML(&h, r)
	WriteText(&txt, r)
	all := strings.ToLower(j.String() + h.String() + txt.String())
	for _, claim := range []string{"guarantee", "tamper-proof", "tamperproof", "certified", "is compliant", "fully recoverable", "proves"} {
		if strings.Contains(all, claim) {
			t.Errorf("report output contains %q", claim)
		}
	}
}
