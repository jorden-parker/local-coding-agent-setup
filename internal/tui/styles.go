package tui

import (
	"image/color"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// palette follows the shape of claude-config-cli's styles.go with a smaller
// set of semantic colours that adapt to light and dark backgrounds.
type palette struct {
	ink, dim, faint, accent, ok, warn, err color.Color
}

func newPalette(isDark bool) palette {
	ld := lipgloss.LightDark(isDark)
	return palette{
		ink:    lipgloss.NoColor{},
		dim:    ld(lipgloss.Color("#5B6470"), lipgloss.Color("#9AA5B1")),
		faint:  ld(lipgloss.Color("#D5DBE1"), lipgloss.Color("#3A4350")),
		accent: ld(lipgloss.Color("#0F6E8C"), lipgloss.Color("#7FD1EA")),
		ok:     ld(lipgloss.Color("#14765E"), lipgloss.Color("#88DDC0")),
		warn:   ld(lipgloss.Color("#916000"), lipgloss.Color("#F2CE79")),
		err:    ld(lipgloss.Color("#B42318"), lipgloss.Color("#FF7070")),
	}
}

type styles struct {
	p                                      palette
	title, subtle, label, ok, warn, err    lipgloss.Style
	tab, tabOn, pane, header, selected, kv lipgloss.Style
}

func newStyles(isDark bool) styles {
	p := newPalette(isDark)
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	return styles{
		p:        p,
		title:    fg(p.accent).Bold(true),
		subtle:   fg(p.dim),
		label:    fg(p.ink).Bold(true),
		ok:       fg(p.ok),
		warn:     fg(p.warn),
		err:      fg(p.err),
		tab:      fg(p.dim).Padding(0, 1),
		tabOn:    fg(p.accent).Bold(true).Reverse(true).Padding(0, 1),
		pane:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.faint).Padding(0, 1),
		header:   fg(p.accent).Bold(true).Padding(0, 1),
		selected: fg(p.accent).Bold(true).Reverse(true),
		kv:       fg(p.ink).Padding(0, 1),
	}
}

// formTheme themes the Huh form with the same palette.
func formTheme(isDark bool) huh.Theme {
	return huh.ThemeFunc(func(bool) *huh.Styles {
		p := newPalette(isDark)
		t := huh.ThemeCharm(isDark)
		for _, f := range []*huh.FieldStyles{&t.Focused, &t.Blurred} {
			f.Title = f.Title.Foreground(p.accent)
			f.Description = f.Description.Foreground(p.dim)
			f.SelectSelector = f.SelectSelector.Foreground(p.accent)
			f.SelectedOption = f.SelectedOption.Foreground(p.accent)
			f.ErrorIndicator = f.ErrorIndicator.Foreground(p.err)
			f.ErrorMessage = f.ErrorMessage.Foreground(p.err)
			f.TextInput.Prompt = f.TextInput.Prompt.Foreground(p.accent)
			f.TextInput.Cursor = f.TextInput.Cursor.Foreground(p.accent)
			f.FocusedButton = lipgloss.NewStyle().Padding(0, 2).MarginRight(1).Bold(true).Foreground(p.accent).Reverse(true)
			f.Next = f.FocusedButton
		}
		t.Focused.Base = t.Focused.Base.BorderForeground(p.accent)
		t.Focused.Card = t.Focused.Base
		return t
	})
}
