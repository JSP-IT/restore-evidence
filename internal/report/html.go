package report

import (
	_ "embed"
	"html/template"
	"io"
)

//go:embed report.html.tmpl
var htmlSource string

// html/template escapes every value for its context, so operator-supplied
// text and file names cannot inject markup. The page loads nothing external
// and contains no script; the Content-Security-Policy forbids both anyway.
var htmlTemplate = template.Must(template.New("report").Funcs(template.FuncMap{
	"statusLabel": func(s Status) string {
		switch s {
		case Verified:
			return "Verified"
		case Failed:
			return "Failed"
		default:
			return "Not performed"
		}
	},
	"outcomeLabel": func(o Outcome) string {
		switch o {
		case OutcomePass:
			return "Pass: all checks verified"
		case OutcomeFail:
			return "Fail: at least one check failed"
		default:
			return "Incomplete: some checks were not performed"
		}
	},
	"suppliedLabel": func(o string) string {
		switch o {
		case "pass":
			return "Reported pass"
		case "fail":
			return "Reported fail"
		default:
			return "Reported not performed"
		}
	},
}).Parse(htmlSource))

// WriteHTML writes the report as a self-contained HTML page.
func WriteHTML(w io.Writer, r *Report) error {
	return htmlTemplate.Execute(w, r)
}
