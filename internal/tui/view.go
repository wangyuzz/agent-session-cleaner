package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/tui/text"
)

func (m *Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}

	rows := []string{m.banner(), m.body(), m.statusBar()}
	if m.search.active {
		rows = append(rows, m.searchBar())
	}
	rows = append(rows, m.footer())
	screen := strings.Join(rows, "\n")

	switch {
	case m.help:
		screen = m.overlay(screen, m.helpBox())
	case m.dialog != nil:
		screen = m.overlay(screen, m.dialogBox())
	}

	view := tea.NewView(m.paint(screen))
	view.AltScreen = true
	// Clicking a row moves the cursor to it, clicking it again picks it out,
	// and dragging across rows applies one selection state to the range.
	view.MouseMode = tea.MouseModeCellMotion
	view.WindowTitle = m.print.T(i18n.AppTitle, i18n.Args{"agent": m.meta.Label})
	if m.search.active {
		view.Cursor = m.search.input.Cursor()
	}
	return view
}

func (m *Model) body() string {
	height := m.bodyHeight()
	listWidth, detailWidth := m.listWidth(), m.detailWidth()

	list := m.listLines(listWidth, height)
	if detailWidth == 0 {
		lines := make([]string, height)
		for i := range lines {
			left := ""
			if i < len(list) {
				left = list[i]
			}
			lines[i] = text.Fit(left, listWidth, m.theme.Screen)
		}
		return strings.Join(lines, "\n")
	}
	detail := strings.Split(m.detail.View(), "\n")

	// The rule between the panes says which one the keys are talking to. Only
	// its colour changes: the same hairline, lit up. A heavier glyph would
	// redraw the whole column and read as a structural change rather than as
	// an indicator.
	rule := m.theme.Divider
	if m.focus == focusDetail {
		rule = m.theme.DividerFocus
	}
	divider := rule.Render("│")
	lines := make([]string, height)
	for i := range lines {
		left, right := "", ""
		if i < len(list) {
			left = list[i]
		}
		if i < len(detail) {
			// The viewport squares its own content off with bare spaces, which
			// would show as a hole in a painted screen. Take them back off and
			// pad it again in a colour.
			right = strings.TrimRight(detail[i], " ")
		}
		// Both sides are squared off against the screen's own background: a
		// pane that stops where its text does has a ragged edge.
		lines[i] = text.Fit(left, listWidth, m.theme.Screen) +
			divider +
			text.Fit(right, detailWidth, m.theme.Screen)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) statusBar() string {
	style := m.theme.Status
	switch m.status.tone {
	case toneOK:
		style = m.theme.StatusOK
	case toneError:
		style = m.theme.StatusError
	}
	// The whole bar takes the tone, not just the words on it, so a failure is
	// visible from across the room rather than needing to be read.
	var line text.Line
	line.Fill(style).Space(1).Add(m.status.text, style)
	return line.Render(m.width)
}

func (m *Model) searchBar() string {
	sigil := "/"
	if m.search.direction < 0 {
		sigil = "?"
	}
	// The input draws itself, so the bar is painted either side of it rather
	// than under it: one cell of margin and the sigil, then whatever room the
	// input does not use.
	var head text.Line
	head.Fill(m.theme.SearchBar).Space(1).Add(sigil, m.theme.Sigil)
	prefix := head.Render(min(2, m.width))

	input := m.search.input.View()
	tail := max(0, m.width-text.Width(prefix)-text.Width(input))
	return prefix + input + m.theme.SearchBar.Render(strings.Repeat(" ", tail))
}

func (m *Model) footer() string {
	// What is on offer right now: a key with nothing to act on is a key that
	// would only report that it had nothing to act on.
	lines := m.footerLines(!m.picked.Empty(), func(a action) bool {
		return m.allows(a) && m.useful(a)
	})
	// The height is measured against everything this agent could ever show, so
	// that a key coming or going cannot shift the panes above by a line. The
	// spare lines are still bar.
	for len(lines) < m.footerHeight() {
		var blank text.Line
		lines = append(lines, blank.Fill(m.theme.Footer).Render(m.width))
	}
	return strings.Join(lines, "\n")
}

// footerHeight is how many lines the footer occupies. It is the worst case
// over both modes and over everything the agent can do, rather than the state
// of the moment, which is what keeps it still.
func (m *Model) footerHeight() int {
	room := func(picking bool) int {
		return len(m.footerLines(picking, func(a action) bool {
			return m.allowsWhile(a, picking)
		}))
	}
	return max(room(false), room(true))
}

// footerLines lays the two groups of keys out, wrapping a group that does not
// fit onto further lines. Wrapping rather than clipping keeps the grouping
// meaningful in a narrow terminal: a key the footer stops mentioning is a key
// nobody finds, and the ones that would fall off the end here are the
// destructive ones people go looking for.
func (m *Model) footerLines(picking bool, show func(action) bool) []string {
	var lines []string
	for _, group := range [][]action{footerTop, footerBottom} {
		lines = append(lines, m.footerRows(group, picking, show)...)
	}
	return lines
}

func (m *Model) footerRows(actions []action, picking bool, show func(action) bool) []string {
	// Every row stands one cell in from the edge, in step with the banner, the
	// status line and the search bar.
	fresh := func() text.Line {
		var line text.Line
		line.Fill(m.theme.Footer).Space(1)
		return line
	}

	var lines []string
	line := fresh()

	for _, a := range actions {
		b, ok := m.binding(a)
		if !ok || !show(a) {
			continue
		}
		display := b.display
		if display == "" {
			display = b.keys[0]
		}
		key, what := display+" ", m.describe(b, picking)

		gap := 0
		if line.Width() > 1 {
			// Two spaces rather than a bulleted separator, which buys back a
			// cell per key without making the entries run together.
			gap = 2
		}
		// Before the first window size arrives there is no width to wrap to,
		// and one group per line is the right guess.
		wrap := m.width > 0 && line.Width()+gap+text.Width(key+what) > m.width
		if wrap && line.Width() > 1 {
			lines = append(lines, line.Render(m.width))
			line = fresh()
			gap = 0
		}
		line.Space(gap).Add(key, m.theme.Key).Add(what, m.theme.KeyDesc)
	}
	return append(lines, line.Render(m.width))
}

func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}
