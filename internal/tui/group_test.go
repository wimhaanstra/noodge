package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// groupedConfig has a two-member family (db:*), a lone command with a colon
// (check:scripts) that must not get a heading of its own, and a groups: block
// that titles and describes the db family.
const groupedConfig = `version: 1
name: sample
groups:
  - prefix: db
    title: Database
    description: Local SQL Server lifecycle.
commands:
  setup:
    description: First run on a fresh clone.
    steps:
      - echo setup
  db:stop:
    description: Stops the container.
    steps:
      - echo stop
  db:reset:
    description: Rebuilds the database from scratch.
    steps:
      - echo reset
  check:scripts:
    description: Type-checks the scripts.
    steps:
      - echo check
`

func TestMultiMemberFamilyShowsAHeading(t *testing.T) {
	m := sized(t, groupedConfig)
	view := plain(m.View().Content)

	// The declared title, not the raw prefix, heads the family.
	if !strings.Contains(view, "Database") {
		t.Errorf("the db family should be headed by its title:\n%s", view)
	}
	for _, want := range []string{"db:stop", "db:reset"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q:\n%s", want, view)
		}
	}
}

func TestLoneCommandsGetNoHeading(t *testing.T) {
	m := sized(t, groupedConfig)
	items := buildItems(&m.file)

	// setup and check:scripts are families of one and undeclared, so they are
	// plain rows: exactly one heading (Database) in the whole list.
	headings := 0
	for _, it := range items {
		if _, ok := it.(header); ok {
			headings++
		}
	}
	if headings != 1 {
		t.Errorf("want exactly one heading, got %d", headings)
	}
}

func TestGroupHeadingShowsItsDescription(t *testing.T) {
	m := sized(t, groupedConfig)

	// Walk down to the Database heading and confirm the detail pane describes
	// the family rather than a command.
	found := false
	for i := 0; i < 8; i++ {
		if _, ok := m.list.SelectedItem().(header); ok {
			found = true
			break
		}
		m, _ = press(t, m, special(tea.KeyDown))
	}
	if !found {
		t.Fatal("never landed on a heading")
	}

	view := plain(m.View().Content)
	if !strings.Contains(view, "Local SQL Server lifecycle.") {
		t.Errorf("the detail pane should describe the family:\n%s", view)
	}
}

func TestEnterOnAHeadingChoosesNothing(t *testing.T) {
	m := sized(t, groupedConfig)

	// Move onto the first heading.
	for i := 0; i < 8; i++ {
		if _, ok := m.list.SelectedItem().(header); ok {
			break
		}
		m, _ = press(t, m, special(tea.KeyDown))
	}
	if _, ok := m.list.SelectedItem().(header); !ok {
		t.Fatal("expected to be on a heading")
	}

	m, quit := press(t, m, special(tea.KeyEnter))
	if quit {
		t.Error("pressing enter on a heading must not choose or run anything")
	}
	if m.Result().Chosen() {
		t.Errorf("a heading is not a command: %+v", m.Result())
	}
}

func TestFilteringHidesHeadingsAndFindsByDescription(t *testing.T) {
	m := sized(t, groupedConfig)

	// Filter on a word that appears only in db:reset's description. Driven
	// through the list's own filter so the assertion is about what survives a
	// filter, not about the cursor-blink timer the interactive path starts.
	m.list.SetFilterText("Rebuilds")

	var names []string
	for _, it := range m.list.VisibleItems() {
		if _, ok := it.(header); ok {
			t.Errorf("headings must drop out while filtering")
		}
		if ci, ok := it.(item); ok {
			names = append(names, ci.cmd.Name)
		}
	}

	if !slices.Contains(names, "db:reset") {
		t.Errorf("a description word should find its command; visible: %v", names)
	}
}

func TestInitialSelectionIsACommand(t *testing.T) {
	m := sized(t, groupedConfig)
	if _, ok := m.list.SelectedItem().(item); !ok {
		t.Errorf("the cursor should start on a command, not a heading")
	}
}
