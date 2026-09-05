package tui

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wimhaanstra/noodge/internal/config"
)

// item is one command in the left-hand pane.
type item struct {
	cmd config.NamedCommand
	// indent is set when the command sits under a family heading, so it can be
	// shown stepped in beneath it. A lone command with no heading is not.
	indent bool
}

// FilterValue is what the list's built-in filter matches against. Including
// the description means typing a word you remember from the docs finds the
// command, which is most of what a command palette is for.
func (i item) FilterValue() string {
	return i.cmd.Name + " " + i.cmd.Description
}

// delegate renders a command as a single line.
//
// The stock two-line delegate would show four or five commands on a short
// terminal. A command list is a navigation aid, and the description already
// has a whole pane of its own.
type delegate struct {
	styles styles
	width  int
}

func (d delegate) Height() int  { return 1 }
func (d delegate) Spacing() int { return 0 }

func (d delegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d delegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	prefix := noCursor
	focused := index == m.Index()
	if focused {
		prefix = cursor
	}

	switch v := listItem.(type) {
	case header:
		// A heading stays legible when the cursor is on it, but reads as a
		// heading rather than a runnable row the rest of the time.
		style := d.styles.group
		if focused {
			style = d.styles.selected
		}
		title := truncate(v.title, d.width-len(prefix))
		fmt.Fprint(w, style.Render(prefix+title))

	case item:
		style := d.styles.item
		if focused {
			style = d.styles.selected
		}
		// Step commands in under their heading — but not while filtering, when
		// the headings are gone and the matches read best as a flat list.
		pad := ""
		if v.indent && m.FilterState() == list.Unfiltered {
			pad = "  "
		}
		name := truncate(v.cmd.Name, d.width-len(prefix)-len(pad))
		fmt.Fprint(w, style.Render(prefix+pad+name))
	}
}

// newList builds the command list from rows already grouped by buildItems.
func newList(items []list.Item, st styles, width, height int) list.Model {
	l := list.New(items, delegate{styles: st, width: width}, width, height)
	l.SetShowTitle(false)
	// Hiding the title leaves the title bar's own padding behind, which puts a
	// blank row above the first command and knocks the two panes out of
	// alignment with each other.
	l.Styles.TitleBar = lipgloss.NewStyle()
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.SetShowPagination(true)

	return l
}

// selectedCommand returns the command under the cursor.
func selectedCommand(l list.Model) (config.NamedCommand, bool) {
	it, ok := l.SelectedItem().(item)
	if !ok {
		return config.NamedCommand{}, false
	}
	return it.cmd, true
}

// pad right-pads s to width, so two panes joined side by side keep their edge
// straight even when a line is short.
func pad(s string, width int) string {
	gap := width - lipglossWidth(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}
