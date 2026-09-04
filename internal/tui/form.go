package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/wimhaanstra/noodge/internal/config"
)

// The form is why selecting a command in the browser is useful at all. Without
// it, choosing anything that takes a required parameter would exit the browser
// and fail immediately on the missing value — which is most of the commands
// worth having documentation for.

// inputWidth is how wide a text field renders. Left at its zero value the
// input truncates its own contents to a single character, which looks like a
// rendering bug rather than an unset width.
const inputWidth = 32

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldBool
	fieldEnum
)

// field is one parameter being filled in.
type field struct {
	param config.Param
	kind  fieldKind

	input textinput.Model
	on    bool
	// choice indexes param.Values, or is -1 when an optional enum is unset.
	choice int
}

// form collects a command's parameters before it runs.
type form struct {
	cmd    config.NamedCommand
	fields []field
	focus  int
	err    string
	styles styles
	width  int
}

func newForm(nc config.NamedCommand, st styles, width int) *form {
	f := &form{cmd: nc, styles: st, width: width}

	// Required parameters first: they are the ones that stop the command
	// running, so they should be the ones under the cursor when the form opens.
	for _, p := range nc.Params {
		if p.Required {
			f.fields = append(f.fields, newField(p))
		}
	}
	for _, p := range nc.Params {
		if !p.Required {
			f.fields = append(f.fields, newField(p))
		}
	}

	f.focusField(0)
	return f
}

func newField(p config.Param) field {
	fl := field{param: p, choice: -1}

	switch p.ResolvedType() {
	case config.TypeBool:
		fl.kind = fieldBool
		if b, ok := p.Default.(bool); ok {
			fl.on = b
		}

	case config.TypeEnum:
		fl.kind = fieldEnum
		if s, ok := p.Default.(string); ok {
			for i, v := range p.Values {
				if v == s {
					fl.choice = i
				}
			}
		}
		// A required enum always has a value, so start on the first one.
		if fl.choice < 0 && p.Required && len(p.Values) > 0 {
			fl.choice = 0
		}

	default:
		fl.kind = fieldText
		ti := textinput.New()
		// The field already carries its flag as a label, so the input's own
		// prompt would be a second cursor competing with the form's.
		ti.Prompt = ""
		ti.SetWidth(inputWidth)
		ti.Placeholder = placeholderFor(p)
		if p.Default != nil {
			ti.SetValue(fmt.Sprint(p.Default))
		}
		fl.input = ti
	}

	return fl
}

func placeholderFor(p config.Param) string {
	if p.Required {
		return "required"
	}
	return "optional"
}

// focusField moves the cursor, focusing the text input when there is one.
func (f *form) focusField(i int) {
	if len(f.fields) == 0 {
		return
	}
	if i < 0 {
		i = len(f.fields) - 1
	}
	if i >= len(f.fields) {
		i = 0
	}

	for j := range f.fields {
		if f.fields[j].kind != fieldText {
			continue
		}
		if j == i {
			f.fields[j].input.Focus()
		} else {
			f.fields[j].input.Blur()
		}
	}
	f.focus = i
}

// formDone is emitted when the form is submitted or abandoned.
type formDone struct {
	// cancelled means go back to the list rather than run anything.
	cancelled bool
	args      []string
}

func (f *form) Update(msg tea.Msg) (*form, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return f, nil
	}

	cur := &f.fields[f.focus]

	switch key.String() {
	case "esc":
		return f, func() tea.Msg { return formDone{cancelled: true} }

	case "tab", "down":
		f.focusField(f.focus + 1)
		return f, nil

	case "shift+tab", "up":
		f.focusField(f.focus - 1)
		return f, nil

	case "enter":
		args, err := f.args()
		if err != nil {
			f.err = err.Error()
			return f, nil
		}
		return f, func() tea.Msg { return formDone{args: args} }

	case " ", "space":
		if cur.kind == fieldBool {
			cur.on = !cur.on
			return f, nil
		}

	case "left":
		if cur.kind == fieldEnum {
			cur.choice = cycle(cur.choice-1, cur.param)
			return f, nil
		}

	case "right":
		if cur.kind == fieldEnum {
			cur.choice = cycle(cur.choice+1, cur.param)
			return f, nil
		}
	}

	if cur.kind == fieldText {
		var cmd tea.Cmd
		cur.input, cmd = cur.input.Update(msg)
		f.err = ""
		return f, cmd
	}

	return f, nil
}

// cycle moves through an enum's values, including the unset position when the
// parameter is optional.
func cycle(next int, p config.Param) int {
	low := 0
	if !p.Required {
		low = -1
	}
	high := len(p.Values) - 1

	switch {
	case next > high:
		return low
	case next < low:
		return high
	default:
		return next
	}
}

// args renders the form as the command line that would produce it.
func (f *form) args() ([]string, error) {
	var out []string

	for _, fl := range f.fields {
		switch fl.kind {
		case fieldBool:
			if fl.on {
				out = append(out, fl.param.Flag)
			}

		case fieldEnum:
			if fl.choice < 0 {
				if fl.param.Required {
					return nil, fmt.Errorf("%s is required", fl.param.Flag)
				}
				continue
			}
			out = append(out, fl.param.Flag, fl.param.Values[fl.choice])

		default:
			v := strings.TrimSpace(fl.input.Value())
			if v == "" {
				if fl.param.Required {
					return nil, fmt.Errorf("%s is required", fl.param.Flag)
				}
				continue
			}
			out = append(out, fl.param.Flag, v)
		}
	}

	return out, nil
}

func (f *form) View() string {
	var b strings.Builder

	b.WriteString(f.styles.title.Render(f.cmd.Name))
	b.WriteString("\n")
	b.WriteString(f.styles.muted.Render(truncate(firstLine(f.cmd.Description), f.width)))
	b.WriteString("\n\n")

	for i, fl := range f.fields {
		marker := noCursor
		labelStyle := f.styles.label
		if i == f.focus {
			marker = cursor
			labelStyle = f.styles.focus
		}

		b.WriteString(marker)
		b.WriteString(labelStyle.Render(fl.param.Flag))
		if fl.param.Required {
			b.WriteString(f.styles.required.Render(" *"))
		}
		b.WriteString("  ")
		b.WriteString(f.value(fl))
		b.WriteString("\n")

		if desc := strings.TrimSpace(fl.param.Description); desc != "" {
			b.WriteString(indent(wrap(desc, f.width-4), 4))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if f.err != "" {
		b.WriteString(f.styles.warn.Render(f.err))
		b.WriteString("\n")
	}

	return b.String()
}

func (f *form) value(fl field) string {
	switch fl.kind {
	case fieldBool:
		if fl.on {
			return checked + " " + f.styles.muted.Render("(space toggles)")
		}
		return unchecked + " " + f.styles.muted.Render("(space toggles)")

	case fieldEnum:
		if fl.choice < 0 {
			return f.styles.muted.Render("(unset - left/right to choose)")
		}
		return fl.param.Values[fl.choice] + " " + f.styles.muted.Render("(left/right)")

	default:
		return fl.input.View()
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
