package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"

	"github.com/jorden-parker/local-coding-agent-setup/internal/app"
	"github.com/jorden-parker/local-coding-agent-setup/internal/config"
	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
)

type configScreen struct {
	form   *huh.Form
	file   *config.File
	values map[string]*string
	save   bool
	err    error
}

// newConfigScreen builds one Huh form with a field per catalogue key. Values
// are edited in place and written only when the final Confirm says save.
func newConfigScreen(isDark bool, width, height int) configScreen {
	c := configScreen{values: map[string]*string{}}
	f, err := app.LoadConfig()
	if err != nil {
		c.err = err
		return c
	}
	c.file = f
	var fields []huh.Field
	for _, k := range config.Keys {
		v, ok := f.Get(k.Name)
		if !ok {
			v = k.Default
		}
		p := new(string)
		*p = v
		c.values[k.Name] = p
		name := k.Name
		desc := k.Help
		if k.Flag != "" {
			desc += " (" + k.Flag + ")"
		}
		switch k.Kind {
		case config.KindBool:
			fields = append(fields, huh.NewSelect[string]().Key(name).Title(name).Description(desc).
				Options(huh.NewOption("false", "false"), huh.NewOption("true", "true")).Value(p))
		default:
			fields = append(fields, huh.NewInput().Key(name).Title(name).Description(desc).Value(p).
				Validate(func(s string) error { _, err := config.Validate(name, s); return err }))
		}
	}
	fields = append(fields, huh.NewConfirm().Key("save").Title("Write "+paths.Tildify(f.Path)+"?").
		Description("ALIAS, PORT, CTX and THINKING are also merged into the harness's own configuration.").
		Affirmative("Save").Negative("Discard").Value(&c.save))
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))
	c.form = huh.NewForm(huh.NewGroup(fields...)).WithTheme(formTheme(isDark)).WithKeyMap(km).
		WithWidth(max(20, width-4)).WithHeight(max(5, height-6)).WithShowHelp(true)
	return c
}

// commit writes every changed key and returns a status line.
func (c *configScreen) commit() (string, bool) {
	if !c.save {
		return "Discarded; nothing written.", false
	}
	var changed, hints []string
	for _, k := range config.Keys {
		nv := strings.TrimSpace(*c.values[k.Name])
		old, _ := c.file.Get(k.Name)
		if nv == old {
			continue
		}
		_, hint, err := app.Set(c.file, k.Name, nv)
		if err != nil {
			return fmt.Sprintf("%s: %v", k.Name, err), true
		}
		changed = append(changed, k.Name+"="+nv)
		hints = append(hints, hint)
	}
	if len(changed) == 0 {
		return "No changes.", false
	}
	return "Saved " + strings.Join(changed, " ") + ". " + hints[len(hints)-1], false
}
