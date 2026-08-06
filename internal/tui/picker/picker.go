// Package picker is the opening screen, shown when no agent is named on the
// command line: a wordmark, one line per agent with how many sessions it has,
// and the key that opens it.
package picker

import (
	"context"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/tui/text"
	"github.com/haowang02/agent-session-cleaner/internal/tui/theme"
)

// wordmark is the tool's name in a block font, drawn in two weights: the solid
// cells lead, the line the font draws around them sits a shade back.
var wordmark = []string{
	`███████╗███████╗███████╗███████╗██╗ ██████╗ ███╗   ██╗███████╗`,
	`██╔════╝██╔════╝██╔════╝██╔════╝██║██╔═══██╗████╗  ██║██╔════╝`,
	`███████╗█████╗  ███████╗███████╗██║██║   ██║██╔██╗ ██║███████╗`,
	`╚════██║██╔══╝  ╚════██║╚════██║██║██║   ██║██║╚██╗██║╚════██║`,
	`███████║███████╗███████║███████║██║╚██████╔╝██║ ╚████║███████║`,
	`╚══════╝╚══════╝╚══════╝╚══════╝╚═╝ ╚═════╝ ╚═╝  ╚═══╝╚══════╝`,
}

const (
	// blockWidth is the wordmark's width, which the rows below it line up with.
	blockWidth = 62
	// labelWidth lines the counts up under one another.
	labelWidth = 14
	// solid is the wordmark cell that takes the leading colour.
	solid = '█'
	// spark opens the closing line, the way a dashboard signs off.
	spark = "⚡ "
)

type countedMsg struct {
	id    string
	count int
	err   error
}

type row struct {
	target agent.Agent
	meta   agent.Meta
	// usable is false when this agent's directory is not there, which is what
	// "not installed, or never used" looks like from here.
	usable bool
	count  int
	known  bool
	failed bool
}

var _ tea.Model = (*Model)(nil)

// Model is the chooser. Choice reports what the user picked once it exits.
type Model struct {
	ctx    context.Context
	print  *i18n.Printer
	theme  theme.Theme
	rows   []row
	cursor int
	hint   string
	// Choice is the agent id the user settled on, empty if they quit.
	Choice string

	width, height int
}

func New(ctx context.Context, agents []agent.Agent, print *i18n.Printer) *Model {
	rows := make([]row, 0, len(agents))
	hasUsable := false
	for _, target := range agents {
		meta := target.Meta()
		info, err := os.Stat(meta.Home)
		usable := err == nil && info.IsDir()
		hasUsable = hasUsable || usable
		rows = append(rows, row{target: target, meta: meta, usable: usable})
	}

	m := &Model{ctx: ctx, print: print, theme: theme.New(true), rows: rows}
	m.hint = print.T(i18n.PickerHint)
	if !hasUsable {
		m.hint = print.T(i18n.PickerNone)
	}
	// Start on something that can actually be opened.
	for i, r := range rows {
		if r.usable {
			m.cursor = i
			break
		}
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.RequestBackgroundColor}
	// One count per agent, each on its own. An unreadable store must not leave
	// every row after it stuck on "Loading…".
	for _, r := range m.rows {
		if !r.usable {
			continue
		}
		target, id, ctx := r.target, r.meta.ID, m.ctx
		cmds = append(cmds, func() tea.Msg {
			found, err := target.Discover(ctx)
			return countedMsg{id: id, count: len(found), err: err}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case tea.BackgroundColorMsg:
		m.theme = theme.New(msg.IsDark())

	case countedMsg:
		for i := range m.rows {
			if m.rows[i].meta.ID == msg.id {
				m.rows[i].count, m.rows[i].known = msg.count, true
				m.rows[i].failed = msg.err != nil
			}
		}

	case tea.KeyPressMsg:
		return m, m.press(msg)
	}
	return m, nil
}

func (m *Model) press(msg tea.KeyPressMsg) tea.Cmd {
	switch key := msg.String(); key {
	case "q", "esc", "ctrl+c":
		return tea.Quit
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.rows)-1, m.cursor+1)
	case "enter":
		return m.open(m.cursor)
	default:
		for i, r := range m.rows {
			if key == string(r.meta.Shortcut) {
				return m.open(i)
			}
		}
	}
	return nil
}

func (m *Model) open(index int) tea.Cmd {
	if index < 0 || index >= len(m.rows) {
		return nil
	}
	chosen := m.rows[index]
	if !chosen.usable {
		// Explain why the key did nothing rather than leaving it ambiguous.
		m.hint = m.print.T(i18n.PickerAgentNone, i18n.Args{"agent": chosen.meta.Label})
		return nil
	}
	m.Choice = chosen.meta.ID
	return tea.Quit
}

func (m *Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}

	block := m.lines()
	view := tea.NewView(lipgloss.Place(
		m.width, m.height, lipgloss.Center, lipgloss.Center,
		strings.Join(block, "\n"),
		lipgloss.WithWhitespaceStyle(m.theme.Screen),
	))
	view.AltScreen = true
	view.WindowTitle = m.print.T(i18n.PickerTitle)
	return view
}

// lines is the whole block, every line the same width so that centring the
// block does not centre each of its lines separately.
func (m *Model) lines() []string {
	width := min(blockWidth, max(1, m.width-4))

	var block []string
	// The wordmark is the first thing to go: a terminal too small for it still
	// has to be able to choose an agent.
	if width >= blockWidth && m.height >= len(wordmark)+len(m.rows)+6 {
		for _, art := range wordmark {
			block = append(block, m.logoLine(art).Render(width))
		}
		block = append(block, "",
			m.centred(m.print.T(i18n.PickerChoose), m.theme.Muted, width), "")
	} else {
		block = append(block, m.centred(m.print.T(i18n.PickerChoose), m.theme.Text.Bold(true), width), "")
	}

	for i, r := range m.rows {
		block = append(block, m.rowLine(i, r, width))
	}
	block = append(block, "", m.line().Add(spark, m.theme.Spark).
		Add(m.hint, m.theme.Placeholder).Render(width))

	// A blank line is still background.
	for i, line := range block {
		if line == "" {
			block[i] = m.line().Render(width)
		}
	}
	return block
}

// line starts a line on the screen's own background, which every line has to
// carry: the background is set per line, not once around the block.
func (m *Model) line() *text.Line {
	line := new(text.Line)
	return line.Fill(m.theme.Screen)
}

func (m *Model) centred(s string, style lipgloss.Style, width int) string {
	line := m.line()
	return line.Space((width-text.Width(s))/2).Add(s, style).Render(width)
}

// logoLine splits one line of the wordmark into its solid cells and the line
// the font draws around them, so the two can take different weights.
func (m *Model) logoLine(art string) *text.Line {
	line := m.line().Space((blockWidth - text.Width(art)) / 2)

	run := []rune(art)
	for start := 0; start < len(run); {
		end := start
		filled := run[start] == solid
		for end < len(run) && (run[end] == solid) == filled {
			end++
		}
		style := m.theme.LogoShadow
		if filled {
			style = m.theme.Logo
		}
		line.Add(string(run[start:end]), style)
		start = end
	}
	return line
}

func (m *Model) rowLine(index int, r row, width int) string {
	line := m.line()
	if index == m.cursor {
		line.Add(theme.CursorMark+" ", m.theme.Cursor)
	} else {
		line.Space(2)
	}
	line.Cell(r.meta.Label, labelWidth, m.theme.Title).Space(2)

	switch {
	case !r.usable:
		line.Add(m.print.T(i18n.PickerUnavailable), m.theme.Placeholder)
	case !r.known:
		line.Add(m.print.T(i18n.PickerLoading), m.theme.Placeholder)
	case r.failed && r.count == 0:
		line.Add(m.print.T(i18n.PickerUnreadable), m.theme.Placeholder)
	default:
		line.Add(m.print.N(i18n.PickerCountOne, i18n.PickerCountMany, r.count), m.theme.Count)
		if r.failed {
			line.Space(2).Add(m.print.T(i18n.PickerIncomplete), m.theme.Placeholder)
		}
		if !r.target.Writable() {
			line.Space(2).Add(m.print.T(i18n.PickerBrowseOnly), m.theme.Placeholder)
		}
	}

	// The key that opens this agent sits against the right-hand edge, the way
	// a dashboard lists its shortcuts.
	key := string(r.meta.Shortcut)
	shortcut := m.theme.Shortcut
	if !r.usable {
		shortcut = m.theme.Placeholder
	}
	line.Space(max(1, width-line.Width()-text.Width(key))).Add(key, shortcut)
	return line.Render(width)
}
