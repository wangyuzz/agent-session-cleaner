package tui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

type search struct {
	// query is what rows are matched and highlighted against, which outlives
	// the input bar being open.
	query  string
	input  textinput.Model
	active bool
	// direction is 1 for / and -1 for ?, and is what n repeats.
	direction int
	// origin is where the cursor stood when the search began, so abandoning it
	// puts the cursor back.
	origin int
	before string
}

func (m *Model) beginSearch(direction int) tea.Cmd {
	if len(m.rows) == 0 {
		m.warn(i18n.SearchEmptyList)
		return nil
	}
	m.search.direction = direction
	m.search.origin = m.cursor
	m.search.before = m.search.query
	m.search.active = true
	m.search.input.SetValue("")
	m.search.input.Focus()
	// The search bar changes the viewport height.
	return m.resize()
}

// searchKey drives the input bar. Everything except the two ways out goes to
// the text field, and every keystroke moves the cursor to the nearest match.
func (m *Model) searchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch keyName(msg) {
	case "esc":
		m.search.query = m.search.before
		m.status = statusLine{}
		return m.endSearch(true)
	case "enter":
		return m.endSearch(false)
	}

	var cmd tea.Cmd
	m.search.input, cmd = m.search.input.Update(msg)
	m.search.query = m.search.input.Value()
	if m.search.query == "" {
		return cmd
	}
	// Incremental search starts at the cursor, so a row already under it
	// counts as a match; n afterwards is what moves past it.
	return tea.Batch(cmd, m.jump(m.search.origin, m.search.direction, true))
}

func (m *Model) endSearch(restore bool) tea.Cmd {
	m.search.active = false
	m.search.input.Blur()
	if restore && len(m.rows) > 0 {
		m.setCursor(m.search.origin)
	}
	m.resizePanes()
	if restore {
		return m.showCurrent()
	}
	return m.renderDetail()
}

func (m *Model) repeatSearch(direction int) tea.Cmd {
	if m.search.query == "" {
		m.warn(i18n.SearchNotStarted)
		return nil
	}
	return m.jump(m.cursor, direction, false)
}

// caseSensitive is smart case, as in vim: an uppercase letter makes the search
// exact.
func caseSensitive(query string) bool { return query != strings.ToLower(query) }

func (m *Model) haystack(s session.Session) string {
	return strings.Join([]string{s.Title, s.Client, s.ID, s.Cwd}, " ")
}

func (m *Model) matches() []int {
	if m.search.query == "" {
		return nil
	}
	exact := caseSensitive(m.search.query)
	needle := m.search.query
	if !exact {
		needle = strings.ToLower(needle)
	}

	var found []int
	for i, row := range m.rows {
		hay := m.haystack(row.Session)
		if !exact {
			hay = strings.ToLower(hay)
		}
		if strings.Contains(hay, needle) {
			found = append(found, i)
		}
	}
	return found
}

func (m *Model) jump(start, direction int, inclusive bool) tea.Cmd {
	found := m.matches()
	if len(found) == 0 {
		m.warn(i18n.SearchNotFound, i18n.Args{"query": m.search.query})
		return nil
	}

	// "Wrapped" means the search ran off the end and started over, which is
	// exactly "nothing left in the direction we were going". Inferring it from
	// where the target landed instead gets a backwards search that stays put
	// wrong.
	var target int
	wrapped := true
	if direction > 0 {
		target = found[0]
		for _, at := range found {
			if at > start || (inclusive && at == start) {
				target, wrapped = at, false
				break
			}
		}
	} else {
		target = found[len(found)-1]
		for i := len(found) - 1; i >= 0; i-- {
			if at := found[i]; at < start || (inclusive && at == start) {
				target, wrapped = at, false
				break
			}
		}
	}

	m.setCursor(target)

	note := ""
	if wrapped {
		if direction > 0 {
			note = m.print.T(i18n.SearchWrappedForward)
		} else {
			note = m.print.T(i18n.SearchWrappedBackward)
		}
	}
	sigil := "/"
	if m.search.direction < 0 {
		sigil = "?"
	}
	m.status = statusLine{text: m.print.T(i18n.SearchStatus, i18n.Args{
		"sigil":   sigil,
		"query":   m.search.query,
		"current": slices.Index(found, target) + 1,
		"total":   len(found),
		"note":    note,
	})}
	return m.showCurrent()
}
