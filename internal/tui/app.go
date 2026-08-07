// Package tui is the interactive session browser: a list of sessions on the
// left, the conversation on the right.
//
// The model holds no shared mutable state. Everything slow — reading a session
// tree, running an agent's command line — happens in a command and comes back
// as a message, so what is on screen is always a rendering of one settled
// state rather than of something half-changed.
package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
	"github.com/haowang02/agent-session-cleaner/internal/tui/theme"
)

type tone uint8

const (
	toneNone tone = iota
	toneOK
	toneError
)

type statusLine struct {
	text string
	tone tone
}

type focusTarget uint8

const (
	focusList focusTarget = iota
	focusDetail
)

var _ tea.Model = (*Model)(nil)

type Model struct {
	// ctx bounds every agent call. Bubble Tea gives commands no context of
	// their own, so the program's one lives here.
	ctx      context.Context
	agent    agent.Agent
	archiver agent.Archiver // nil where this agent cannot set sessions aside
	meta     agent.Meta
	print    *i18n.Printer
	theme    theme.Theme

	width, height int

	// The listing, arranged as a forest, and where the cursor stands in it.
	forest *session.Forest
	rows   []session.Row
	cursor int
	top    int // first visible row, so the cursor stays on screen
	picked session.Selection

	focus focusTarget
	// clicked remembers the last row a click landed on, so a timely second
	// click can pick it, the way Space does.
	clicked   int
	clickedAt time.Time
	// A drag restores dragInitial on every move, then gives the range from
	// dragStart to the pointer the opposite of the anchor's initial state.
	// A start of -1 means no drag.
	dragStart   int
	dragInitial session.Selection

	detail    viewport.Model
	messages  map[cacheKey][]session.Message
	order     []cacheKey // insertion order, for evicting the oldest
	pending   map[cacheKey]bool
	showing   cacheKey // the conversation the detail pane currently holds
	loading   bool
	renders   renderCache
	rendering *bodyKey
	copySeq   uint64
	copyStop  context.CancelFunc

	search search
	status statusLine
	danger bool
	busy   bool // a destructive operation is running
	// refreshing keeps mutations from acting on a listing that is being
	// replaced. Quitting is still safe while a read-only refresh is in flight.
	refreshing bool
	stale      bool // the last mutation could not be reconciled with disk
	// updates carries progress from the operation in flight, if any.
	updates chan opUpdate

	dialog *dialog
	help   bool
	// helpTop scrolls the help screen, which is taller than a short terminal.
	helpTop int
}

func New(ctx context.Context, target agent.Agent, print *i18n.Printer) *Model {
	input := textinput.New()
	input.Prompt = ""
	// The terminal's own cursor is reported in the view, so the drawn stand-in
	// would be a second one — and it draws an unstyled cell into a bar that is
	// otherwise painted.
	input.SetVirtualCursor(false)
	archiver, _ := target.(agent.Archiver)

	m := &Model{
		ctx:      ctx,
		agent:    target,
		archiver: archiver,
		meta:     target.Meta(),
		print:    print,
		forest:   session.Build(nil),
		messages: make(map[cacheKey][]session.Message),
		pending:  make(map[cacheKey]bool),
		clicked:  -1,
		detail:   viewport.New(),
		search:   search{input: input, direction: 1},

		dragStart: -1,
	}
	m.restyle(true)
	return m
}

// restyle resolves the style sheet for a dark or light terminal. The search
// input draws itself, so it has to be handed the bar it sits on rather than
// leaving a gap in the middle of it.
func (m *Model) restyle(dark bool) {
	if m.theme.Dark != dark {
		// Rendered lines contain the previous palette's ANSI colours.
		m.renders.clear()
	}
	m.theme = theme.New(dark)
	styles := textinput.DefaultStyles(dark)
	for _, state := range []*textinput.StyleState{&styles.Focused, &styles.Blurred} {
		state.Text = m.theme.SearchBar
		state.Prompt = m.theme.SearchBar
		state.Placeholder = m.theme.SearchBar.Foreground(m.theme.Faded)
		state.Suggestion = m.theme.SearchBar.Foreground(m.theme.Faded)
	}
	m.search.input.SetStyles(styles)
}

func (m *Model) Init() tea.Cmd {
	first := statusLine{text: agent.UnderHome(m.meta.Home)}
	if !m.agent.Writable() {
		first = statusLine{
			tone: toneError,
			text: m.print.T(i18n.MissingCLIBrowse, i18n.Args{"agent": m.meta.Label}),
		}
	}
	m.status = first

	return tea.Batch(
		// Lip Gloss no longer guesses; Bubble Tea asks the terminal and tells
		// us, in step with the rest of the program.
		tea.RequestBackgroundColor,
		m.load(reload{}),
	)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.resize()

	case tea.BackgroundColorMsg:
		m.restyle(msg.IsDark())
		return m, m.renderDetail()

	case loadedMsg:
		return m, m.loaded(msg)

	case messagesMsg:
		return m, m.receive(msg)

	case renderedMsg:
		return m, m.rendered(msg)

	case opUpdate:
		return m, m.progressed(msg)

	case copiedClipboardMsg:
		return m, m.copiedToClipboard(msg)

	case tea.KeyPressMsg:
		return m, m.press(msg)

	case tea.MouseClickMsg:
		if m.help || m.dialog != nil || m.search.active {
			return m, nil
		}
		return m, m.click(msg)

	case tea.MouseReleaseMsg:
		m.endDrag()
		return m, nil

	case tea.MouseMotionMsg:
		if m.help || m.dialog != nil || m.search.active {
			return m, nil
		}
		return m, m.drag(msg)

	case tea.MouseWheelMsg:
		if m.dialog != nil || m.search.active {
			return m, nil
		}
		if m.help {
			m.helpWheel(msg)
			return m, nil
		}
		return m, m.wheel(msg)
	}
	return m, nil
}

func (m *Model) press(msg tea.KeyPressMsg) tea.Cmd {
	// A keyboard action breaks mouse click and drag sequences.
	m.endMouseSequences()
	// Ctrl+C is the terminal-wide way out, including while a help screen,
	// search field or confirmation has focus. An active mutation still gets
	// the same partial-operation protection as q.
	if keyName(msg) == "ctrl+c" {
		return m.quit()
	}
	switch {
	case m.help:
		return m.helpKey(msg)
	case m.dialog != nil:
		return m.dialogKey(msg)
	case m.search.active:
		return m.searchKey(msg)
	}
	return m.command(resolve(keyName(msg)))
}

func (m *Model) command(a action) tea.Cmd {
	if a != noAction && a != quitAction && !m.allows(a) {
		return nil
	}
	switch a {
	case upAction:
		return m.move(-1)
	case downAction:
		return m.move(1)
	case topAction:
		return m.jumpTo(0)
	case bottomAction:
		return m.jumpTo(len(m.rows) - 1)
	case focusAction:
		if m.focus == focusList {
			m.focus = focusDetail
		} else {
			m.focus = focusList
		}
	case escapeAction:
		return m.escape()
	case helpAction:
		m.help = true
	case reloadAction:
		return m.reload()
	case quitAction:
		return m.quit()
	case searchAction:
		return m.beginSearch(1)
	case searchBackAction:
		return m.beginSearch(-1)
	case nextMatchAction:
		return m.repeatSearch(m.search.direction)
	case prevMatchAction:
		return m.repeatSearch(-m.search.direction)
	case pickAction:
		m.pick()
		return nil
	case copySessionIDAction:
		return m.copySessionID()
	case copyWorkingDirectoryAction:
		return m.copyWorkingDirectory()
	case archiveAction:
		return m.archive()
	case unarchiveAction:
		return m.unarchive()
	case deleteAction:
		return m.delete()
	case sweepArchivedAction:
		return m.sweepArchived()
	case sweepEmptyAction:
		return m.sweepEmpty()
	case sweepOrphansAction:
		return m.sweepOrphans()
	case dangerAction:
		return m.toggleDanger()
	}
	return nil
}

func (m *Model) escape() tea.Cmd {
	switch {
	case m.danger:
		m.danger = false
		m.say(i18n.DangerOff)
	case !m.picked.Empty():
		m.picked.Clear()
		m.say(i18n.SelectionCleared)
	case m.search.query != "":
		m.search.query = ""
		m.status = statusLine{}
	}
	return nil
}

// quit refuses while an operation is in flight: stopping between two sessions
// would leave a sweep half done.
func (m *Model) quit() tea.Cmd {
	if m.busy {
		m.warn(i18n.BusyCannotQuit)
		return nil
	}
	if m.copyStop != nil {
		m.copyStop()
		m.copyStop = nil
	}
	return tea.Quit
}

func (m *Model) toggleDanger() tea.Cmd {
	if m.danger {
		m.danger = false
		m.say(i18n.DangerOff)
		return nil
	}
	if m.rejectWhileBusy() {
		return nil
	}
	m.dialog = dangerDialog(m.print)
	return nil
}

func (m *Model) current() (session.Session, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return session.Session{}, false
	}
	return m.rows[m.cursor].Session, true
}

func (m *Model) require() (session.Session, bool) {
	if !m.agent.Writable() {
		m.warn(i18n.MissingCLIModify, i18n.Args{"agent": m.meta.Label})
		return session.Session{}, false
	}
	if m.rejectWhileBusy() {
		return session.Session{}, false
	}
	current, ok := m.current()
	if !ok {
		m.warn(i18n.NoCurrentSession)
	}
	return current, ok
}

func (m *Model) rejectWhileBusy() bool {
	if m.busy || m.refreshing {
		m.warn(i18n.PreviousBusy)
		return true
	}
	if m.stale {
		m.warn(i18n.ListingStale)
		return true
	}
	return false
}

func (m *Model) say(key i18n.Key, args ...i18n.Args) {
	m.status = statusLine{text: m.print.T(key, args...)}
}

func (m *Model) report(key i18n.Key, args ...i18n.Args) {
	m.status = statusLine{tone: toneOK, text: m.print.T(key, args...)}
}

func (m *Model) warn(key i18n.Key, args ...i18n.Args) {
	m.status = statusLine{tone: toneError, text: m.print.T(key, args...)}
}
