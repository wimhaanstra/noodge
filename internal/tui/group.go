package tui

import (
	"charm.land/bubbles/v2/list"

	"github.com/wimhaanstra/noodge/internal/config"
)

// header is a family heading in the command list. It is a list item so the
// list renders and paginates it like any other row, but it runs nothing.
//
// FilterValue returns an empty string on purpose: the list's fuzzy filter
// matches against it, and an empty target never matches a real query, so every
// header drops out the moment a filter is typed. Browsing is grouped; searching
// is a flat list of matching commands.
type header struct {
	title       string
	description string
	count       int
}

func (h header) FilterValue() string { return "" }

// buildItems turns a config into the rows the list shows: the visible commands,
// bucketed into families by the part of a name before its first colon, each
// family in the order it first appears and its members in file order.
//
// A family gets a heading when it has more than one member, or when the file's
// groups: block names it. A lone command with no declared group is shown as an
// ordinary top-level row, so a single setup or check:scripts is not buried
// under a heading of its own.
func buildItems(file *config.File) []list.Item {
	cmds := visible(file.Config.Commands)

	declared := make(map[string]config.Group, len(file.Config.Groups))
	for _, g := range file.Config.Groups {
		declared[g.Prefix] = g
	}

	var order []string
	members := map[string][]config.NamedCommand{}
	for _, nc := range cmds {
		k := config.GroupKey(nc.Name)
		if _, seen := members[k]; !seen {
			order = append(order, k)
		}
		members[k] = append(members[k], nc)
	}

	var items []list.Item
	for _, k := range order {
		ms := members[k]
		g, isDeclared := declared[k]

		if len(ms) >= 2 || isDeclared {
			title := k
			if g.Title != "" {
				title = g.Title
			}
			items = append(items, header{
				title:       title,
				description: g.Description,
				count:       len(ms),
			})
		}

		for _, nc := range ms {
			items = append(items, item{cmd: nc})
		}
	}

	return items
}

// firstCommandIndex is where the cursor should start: the first real command,
// so the detail pane opens on a command rather than on a family heading.
func firstCommandIndex(items []list.Item) int {
	for i, it := range items {
		if _, ok := it.(item); ok {
			return i
		}
	}
	return 0
}
