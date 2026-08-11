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
	// caseSensitive is explicit rather than hidden smart-case: the same query
	// always means the same thing until Alt+C changes the mode.
	caseSensitive bool
	beforeCase    bool
}

func (m *Model) beginSearch(direction int) tea.Cmd {
	if len(m.rows) == 0 {
		m.warn(i18n.SearchEmptyList)
		return nil
	}
	m.search.direction = direction
	m.search.origin = m.cursor
	m.search.before = m.search.query
	m.search.beforeCase = m.search.caseSensitive
	m.search.active = true
	// Reopening search keeps the visible query editable instead of showing an
	// empty input while the old query is still highlighting rows underneath.
	m.search.input.SetValue(m.search.query)
	m.search.input.Focus()
	// The search bar changes the viewport height.
	return m.resize()
}

// searchKey drives the input bar. Exit, match navigation and mode keys stay
// commands; text-editing keys go to the field and move to the nearest match.
func (m *Model) searchKey(msg tea.KeyPressMsg) tea.Cmd {
	name := keyName(msg)
	switch name {
	case "esc":
		m.search.query = m.search.before
		m.search.caseSensitive = m.search.beforeCase
		m.status = statusLine{}
		return m.endSearch(true)
	case "enter":
		return m.endSearch(false)
	}
	switch m.resolve(name) {
	case matchCaseAction:
		return m.toggleMatchCase()
	case nextMatchAction:
		return m.repeatSearch(m.search.direction)
	case prevMatchAction:
		return m.repeatSearch(-m.search.direction)
	}

	var cmd tea.Cmd
	m.search.input, cmd = m.search.input.Update(msg)
	m.search.query = m.search.input.Value()
	if m.search.query == "" {
		m.status = statusLine{}
		return cmd
	}
	// Incremental search starts at the cursor, so a row already under it
	// counts as a match; n afterwards is what moves past it.
	return tea.Batch(cmd, m.jump(m.search.origin, m.search.direction, true))
}

func (m *Model) toggleMatchCase() tea.Cmd {
	m.search.caseSensitive = !m.search.caseSensitive
	if m.search.query != "" {
		return m.jump(m.cursor, m.search.direction, true)
	}
	if m.search.caseSensitive {
		m.say(i18n.SearchCaseOn)
	} else {
		m.say(i18n.SearchCaseOff)
	}
	return nil
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

func (m *Model) haystack(s session.Session) string {
	return strings.Join([]string{s.Title, s.Client, s.ID, s.Cwd}, " ")
}

func (m *Model) matches() []int {
	if m.search.query == "" {
		return nil
	}
	exact := m.search.caseSensitive
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
		"mode":    m.searchCaseLabel(),
	})}
	return m.showCurrent()
}

func (m *Model) searchCaseLabel() string {
	if m.search.caseSensitive {
		return m.print.T(i18n.SearchCaseSensitive)
	}
	return m.print.T(i18n.SearchCaseInsensitive)
}
