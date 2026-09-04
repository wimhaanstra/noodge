package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wimhaanstra/noodge/internal/config"
)

type state int

const (
	stateList state = iota
	stateForm
)

// Model is the browser: commands on the left, their documentation on the
// right, and a parameter form when the chosen command needs one.
type Model struct {
	file   config.File
	styles styles

	list   list.Model
	detail viewport.Model
	form   *form

	state  state
	result Result

	width, height int
	// lastIndex tracks the selection so the detail pane is only re-rendered
	// when it actually changes.
	lastIndex int
	ready     bool
}

// New builds the browser for a loaded config.
func New(file *config.File) Model {
	st := newStyles()
	cmds := visible(file.Config.Commands)

	return Model{
		file:      *file,
		styles:    st,
		list:      newList(cmds, st, 30, 10),
		detail:    viewport.New(viewport.WithWidth(40), viewport.WithHeight(10)),
		lastIndex: -1,
	}
}

// Result is what the user chose, valid once the program has finished.
func (m Model) Result() Result { return m.result }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		m.refreshDetail(true)
		return m, nil

	case formDone:
		if msg.cancelled {
			m.state = stateList
			m.form = nil
			return m, nil
		}
		if cmd, ok := selectedCommand(m.list); ok {
			m.result = Result{Command: cmd.Name, Args: msg.args}
		}
		return m, tea.Quit

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.state == stateForm {
		f, cmd := m.form.Update(msg)
		m.form = f
		return m, cmd
	}

	// While the filter is being typed, every key belongs to the filter.
	if m.list.SettingFilter() {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		m.refreshDetail(false)
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit

	case "enter":
		cmd, ok := selectedCommand(m.list)
		if !ok {
			return m, nil
		}
		if len(cmd.Params) == 0 {
			m.result = Result{Command: cmd.Name}
			return m, tea.Quit
		}
		m.form = newForm(cmd, m.styles, m.detailWidth())
		m.state = stateForm
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	m.refreshDetail(false)
	return m, cmd
}

// layout divides the window between the two panes.
func (m *Model) layout() {
	body := m.bodyHeight()

	left := m.listWidth()
	m.list.SetSize(left, body)
	m.list.SetDelegate(delegate{styles: m.styles, width: left})

	m.detail.SetWidth(m.detailWidth())
	m.detail.SetHeight(body)
}

func (m Model) listWidth() int {
	w := m.width / 3
	if w > 34 {
		w = 34
	}
	if w < 16 {
		w = 16
	}
	return w
}

func (m Model) detailWidth() int {
	// Two spaces of gutter either side of the separator column.
	w := m.width - m.listWidth() - 4
	if w < 20 {
		w = 20
	}
	return w
}

// bodyHeight is the window less the header and footer.
func (m Model) bodyHeight() int {
	h := m.height - 4
	if h < 3 {
		h = 3
	}
	return h
}

// refreshDetail re-renders the right pane when the selection has moved.
func (m *Model) refreshDetail(force bool) {
	if !force && m.list.Index() == m.lastIndex {
		return
	}
	m.lastIndex = m.list.Index()

	cmd, ok := selectedCommand(m.list)
	if !ok {
		m.detail.SetContent(m.styles.muted.Render("No commands to show."))
		return
	}

	m.detail.SetContent(renderDetail(cmd, m.styles, m.detailWidth()))
	m.detail.GotoTop()
}

func (m Model) View() tea.View {
	v := tea.NewView("")
	// Alt-screen is a field on the view in v2 rather than a command, so it is
	// declared rather than toggled.
	v.AltScreen = true

	if !m.ready {
		v.SetContent("")
		return v
	}

	if m.state == stateForm {
		v.SetContent(m.viewForm())
		return v
	}

	v.SetContent(m.viewList())
	return v
}

func (m Model) viewList() string {
	left := m.listWidth()

	rows := make([]string, 0, m.bodyHeight())
	listLines := strings.Split(m.list.View(), "\n")

	// The list reserves its first row for the filter input, so that pressing /
	// does not shove every command down a line. The detail pane starts a row
	// lower to match, which keeps the two panes aligned and leaves the filter
	// sitting above both.
	detailLines := append([]string{""}, strings.Split(m.detail.View(), "\n")...)

	for i := 0; i < m.bodyHeight(); i++ {
		rows = append(rows, pad(lineAt(listLines, i), left)+
			"  "+m.styles.sep.Render(separator)+" "+
			lineAt(detailLines, i))
	}

	return strings.Join([]string{
		m.header(),
		strings.Join(rows, "\n"),
		m.footer("up/down move   enter run   / filter   q quit"),
	}, "\n")
}

func (m Model) viewForm() string {
	body := m.form.View()

	// Keep the form pane the same height as the list pane so the footer does
	// not jump as the user moves between the two.
	lines := strings.Split(body, "\n")
	for len(lines) < m.bodyHeight() {
		lines = append(lines, "")
	}
	lines = lines[:m.bodyHeight()]

	return strings.Join([]string{
		m.header(),
		strings.Join(lines, "\n"),
		m.footer("tab move   enter run   esc back"),
	}, "\n")
}

func (m Model) header() string {
	name := m.file.Config.Name
	if name == "" {
		name = "noodge"
	}

	line := m.styles.title.Render(name) + "  " +
		m.styles.path.Render(truncate(m.file.Path, m.width-lipgloss.Width(name)-4))

	return line + "\n"
}

func (m Model) footer(keys string) string {
	return "\n" + m.styles.muted.Render(truncate(keys, m.width))
}

// lineAt returns a line, or empty when the slice is shorter than the pane.
func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// Run shows the browser and returns what the user chose.
func Run(file *config.File, opts ...tea.ProgramOption) (Result, error) {
	model := New(file)

	final, err := tea.NewProgram(model, opts...).Run()
	if err != nil {
		return Result{}, fmt.Errorf("browser: %w", err)
	}

	m, ok := final.(Model)
	if !ok {
		return Result{}, fmt.Errorf("browser returned an unexpected model")
	}
	return m.Result(), nil
}
