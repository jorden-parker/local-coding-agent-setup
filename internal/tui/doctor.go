package tui

import (
	"strings"

	"github.com/jorden-parker/local-coding-agent-setup/internal/app"
)

type doctorScreen struct {
	checks []app.Check
	ran    bool
}

func (d *doctorScreen) run() { d.checks, d.ran = app.Doctor(), true }

func (d *doctorScreen) view(st styles) string {
	if !d.ran {
		return st.subtle.Render("running checks…") + "\n"
	}
	var b strings.Builder
	for _, c := range d.checks {
		mark := st.ok.Render("ok  ")
		switch {
		case c.Warn:
			mark = st.warn.Render("warn")
		case !c.OK:
			mark = st.err.Render("FAIL")
		}
		b.WriteString(mark + "  " + st.label.Render(padRight(c.Name, 18)) + " " + c.Detail + "\n")
	}
	if app.Failed(d.checks) {
		b.WriteString("\n" + st.err.Render("Some checks failed.") + "\n")
	} else {
		b.WriteString("\n" + st.ok.Render("All checks passed.") + "\n")
	}
	return b.String()
}

func padRight(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}
