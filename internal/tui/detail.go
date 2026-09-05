package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/wimhaanstra/noodge/internal/config"
	"github.com/wimhaanstra/noodge/internal/runner"
)

// lipglossWidth is aliased so the width calculation used for layout is
// obviously the same one used for rendering.
var lipglossWidth = lipgloss.Width

// renderDetail is the right-hand pane: everything the config says about one
// command. This is the whole reason the tool exists, so nothing is abbreviated
// away — the pane scrolls instead.
func renderDetail(nc config.NamedCommand, st styles, width int) string {
	var b strings.Builder

	b.WriteString(st.title.Render(nc.Name))
	b.WriteString("\n\n")

	if desc := strings.TrimSpace(nc.Description); desc != "" {
		b.WriteString(wrap(desc, width))
	} else {
		b.WriteString(st.muted.Render("(no description)"))
	}
	b.WriteString("\n")

	if len(nc.Params) > 0 {
		b.WriteString("\n")
		b.WriteString(st.heading.Render("Parameters"))
		b.WriteString("\n")
		b.WriteString(renderParams(nc.Params, st, width))
	}

	if out := strings.TrimSpace(nc.Output); out != "" {
		b.WriteString("\n")
		b.WriteString(st.heading.Render("Output"))
		b.WriteString("\n")
		b.WriteString(wrap(out, width))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(st.heading.Render("Steps"))
	b.WriteString("\n")
	for i, s := range nc.Steps {
		line := fmt.Sprintf("%d. %s", i+1, s.String())
		b.WriteString(st.muted.Render(truncate(line, width)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if runner.UsesArgs(nc.Steps) {
		b.WriteString(st.muted.Render(wrap("Arguments after -- are substituted where {{args}} appears.", width)))
	} else {
		b.WriteString(st.muted.Render(wrap("Arguments after -- are appended to the last step.", width)))
	}
	b.WriteString("\n")

	return b.String()
}

// renderGroupDetail is the right-hand pane for a family heading: its title,
// what it is for, and how many commands it holds. A family is a navigation aid,
// so this stays short — the commands under it carry the real documentation.
func renderGroupDetail(h header, st styles, width int) string {
	var b strings.Builder

	b.WriteString(st.title.Render(h.title))
	b.WriteString("\n\n")

	if desc := strings.TrimSpace(h.description); desc != "" {
		b.WriteString(wrap(desc, width))
		b.WriteString("\n\n")
	}

	commands := "commands"
	if h.count == 1 {
		commands = "command"
	}
	b.WriteString(st.muted.Render(fmt.Sprintf("%d %s in this group.", h.count, commands)))
	b.WriteString("\n")

	return b.String()
}

func renderParams(params []config.Param, st styles, width int) string {
	var b strings.Builder

	for _, p := range params {
		b.WriteString("  ")
		b.WriteString(st.label.Render(p.Flag))

		var notes []string
		notes = append(notes, string(p.ResolvedType()))
		if p.Required {
			notes = append(notes, "required")
		}
		if p.Default != nil {
			notes = append(notes, fmt.Sprintf("default %v", p.Default))
		}
		if p.ResolvedType() == config.TypeEnum && len(p.Values) > 0 {
			notes = append(notes, strings.Join(p.Values, "|"))
		}

		b.WriteString(" ")
		b.WriteString(st.muted.Render("(" + strings.Join(notes, ", ") + ")"))
		b.WriteString("\n")

		if desc := strings.TrimSpace(p.Description); desc != "" {
			b.WriteString(indent(wrap(desc, width-6), 6))
			b.WriteString("\n")
		}
	}

	return b.String()
}

// wrap breaks text at word boundaries to fit width, preserving the blank lines
// that separate paragraphs in a description.
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}

	var out []string
	for _, para := range strings.Split(s, "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapLine(para, width)...)
	}
	return strings.Join(out, "\n")
}

func wrapLine(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}

	var (
		lines []string
		line  strings.Builder
	)

	for _, w := range words {
		switch {
		case line.Len() == 0:
			line.WriteString(w)
		case lipglossWidth(line.String())+1+lipglossWidth(w) <= width:
			line.WriteString(" ")
			line.WriteString(w)
		default:
			lines = append(lines, line.String())
			line.Reset()
			line.WriteString(w)
		}
	}
	if line.Len() > 0 {
		lines = append(lines, line.String())
	}

	return lines
}

func indent(s string, n int) string {
	prefix := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
