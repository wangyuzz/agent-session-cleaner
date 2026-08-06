package tui

import (
	"math"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
	"github.com/haowang02/agent-session-cleaner/internal/tui/text"
	"github.com/haowang02/agent-session-cleaner/internal/tui/theme"
)

// Column widths, in cells. The date and project columns size themselves to the
// data; the date only grows when sessions from another year are in view.
const (
	dayMinWidth     = 5
	projectMinWidth = 8
	projectMaxWidth = 18
	clockWidth      = 5
)

func (m *Model) banner() string {
	var line text.Line

	// A mode shows in the pill and in the words after the counts. The bar
	// itself stays the colour every other bar is: a whole line of red fights
	// the list under it for attention it does not need to win.
	pill := m.theme.Pill
	switch {
	case m.danger:
		pill = m.theme.PillDanger
	case !m.picked.Empty():
		pill = m.theme.PillSelect
	}
	line.Fill(m.theme.Banner)
	line.Add(" "+m.meta.Label+" ", pill).Space(2)

	counts := []string{
		m.print.N(i18n.BannerSessionsOne, i18n.BannerSessionsMany, m.forest.Len()),
	}
	// Only counts worth showing: the banner is the one line that stays put, so
	// a zero would be noise.
	if archived := m.countIf(func(s session.Session) bool { return s.Archived }); archived > 0 {
		counts = append(counts,
			m.print.N(i18n.BannerArchivedOne, i18n.BannerArchivedMany, archived))
	}
	if stranded := m.forest.Orphans(); m.meta.OrphanLabel != 0 && len(stranded) > 0 {
		counts = append(counts, m.print.N(
			i18n.BannerOrphansOne, i18n.BannerOrphansMany, len(stranded),
			i18n.Args{"what": m.print.Label(m.meta.OrphanLabel, len(stranded))},
		))
	}
	line.Add(strings.Join(counts, " · "), m.theme.Muted)

	// A mode changes what the keys do, which has to be impossible to miss.
	switch {
	case m.danger:
		line.Space(3).Add(m.print.T(i18n.BannerDanger), m.theme.ModeDanger)
	case !m.picked.Empty():
		line.Space(3).Add(m.print.N(
			i18n.BannerSelectedOne, i18n.BannerSelectedMany, m.picked.Len(),
		), m.theme.Mode)
	}

	return line.Render(m.width)
}

func (m *Model) countIf(keep func(session.Session) bool) int {
	found := 0
	for _, row := range m.rows {
		if keep(row.Session) {
			found++
		}
	}
	return found
}

func (m *Model) listLines(width, height int) []string {
	if len(m.rows) == 0 {
		return []string{m.theme.Placeholder.Render(m.print.T(i18n.NoSessionsHere))}
	}

	today := time.Now()
	days := m.dayLabels(today)
	dayWidth := dayMinWidth
	for _, label := range days {
		dayWidth = max(dayWidth, text.Width(label))
	}
	projectWidth := projectMinWidth
	for _, row := range m.rows {
		if row.Nested {
			continue
		}
		projectWidth = max(projectWidth, text.Width(projectOf(row.Session)))
	}
	projectWidth = min(projectWidth, projectMaxWidth)

	lines := make([]string, 0, height)
	for i := m.top; i < len(m.rows) && len(lines) < height; i++ {
		lines = append(lines, m.rowLine(i, days[i], dayWidth, projectWidth, width))
	}
	return lines
}

// dayLabels names the day on the first row of each one, so a run of sessions
// reads as a group rather than spending a column on the same string 20 times.
//
// Nested rows are skipped on both counts. A sub-agent belongs to the
// conversation above it, not to a day of its own, and letting one break the run
// would make the next top-level session repeat a date that never changed.
func (m *Model) dayLabels(today time.Time) []string {
	labels := make([]string, len(m.rows))
	previous := ""
	for i, row := range m.rows {
		if row.Nested {
			continue
		}
		when := row.Session.RecencyAt()
		day := when.Format("2006-01-02")
		if day != previous {
			labels[i] = dayLabel(m.print, when, today)
		}
		previous = day
	}
	return labels
}

func dayLabel(print *i18n.Printer, when, today time.Time) string {
	switch days := daysApart(when, today); {
	case days == 0:
		return print.T(i18n.Today)
	case days == 1:
		return print.T(i18n.Yesterday)
	case when.Year() != today.Year():
		// Include the year so an old session cannot look recent.
		return when.Format("06-01-02")
	default:
		return when.Format("01-02")
	}
}

// daysApart counts calendar days between two moments.
//
// Both are pinned to midday before subtracting: a day that a daylight-saving
// change makes 23 hours long would otherwise round down to zero, and yesterday
// would be labelled "Today" twice a year.
func daysApart(when, today time.Time) int {
	noon := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, t.Location())
	}
	return int(math.Round(noon(today).Sub(noon(when)).Hours() / 24))
}

func (m *Model) rowLine(index int, day string, dayWidth, projectWidth, width int) string {
	row := m.rows[index]
	s := row.Session
	cursor, picked := index == m.cursor, m.picked.Has(s.ID)

	var line text.Line
	line.Fill(m.rowFill(cursor, picked))

	// Two gutter cells repeating what the fill says: where the cursor is, and
	// what has been picked out.
	if cursor {
		line.Add(theme.CursorMark, m.theme.Cursor)
	} else {
		line.Add(theme.Blank, m.theme.Cursor)
	}
	if picked {
		line.Add(theme.PickedMark, m.theme.Picked)
	} else {
		line.Add(theme.Blank, m.theme.Picked)
	}

	// Archived rows were set aside deliberately; orphaned ones were left
	// behind by accident. One colour covers the whole line either way.
	muted := m.theme.Archived
	switch {
	case row.Orphan:
		muted = m.theme.Orphan
	case !s.Archived:
		muted = lipgloss.Style{}
	}
	shaded := func(own lipgloss.Style) lipgloss.Style {
		if muted.String() != "" {
			return muted
		}
		return own
	}

	// A sub-agent ran inside the conversation drawn above it, in the same
	// directory and within the same minute or two. Repeating all three columns
	// on its row says nothing and buries the tree; the space goes to the title.
	if row.Nested {
		line.Space(dayWidth + 1 + clockWidth + 2 + projectWidth + 2)
	} else {
		line.Cell(day, dayWidth, shaded(m.theme.Day)).Space(1)
		line.Cell(s.RecencyAt().Format("15:04"), clockWidth, shaded(m.theme.Clock)).Space(2)
		line.Cell(projectOf(s), projectWidth, shaded(m.theme.Project)).Space(2)
	}

	for _, guide := range row.Guides {
		line.Add(theme.Guide(guide), shaded(m.theme.Guide))
	}
	// One client dominates each agent, so naming it on every row is noise;
	// only the exceptions get a badge.
	if s.Client != "" && s.Client != m.meta.DefaultClient {
		line.Add(s.Client+" · ", shaded(m.theme.Client))
	}

	line.Add(titleOf(m.print, s), shaded(m.theme.Title))

	line.Highlight(m.search.query, caseSensitive(m.search.query), m.theme.Highlight)
	return line.Render(width)
}

// rowFill is the background a row is drawn on. A selection outranks the
// cursor, because the rows an action is about to touch matter more than the
// one the cursor happens to rest on.
//
// The cursor dims while the conversation pane has the keys, which is half of
// how the interface says where they are going; the other half is the rule
// between the two panes.
//
// Every other row is plain screen. Rows already carry a tree, a date column
// and a title; banding them as well gives the eye a fourth pattern to sort
// through for no more information.
func (m *Model) rowFill(cursor, picked bool) lipgloss.Style {
	switch {
	case cursor && picked:
		return m.theme.RowPickedCursor
	case picked:
		return m.theme.RowPicked
	case cursor && m.focus == focusDetail:
		return m.theme.RowCursorIdle
	case cursor:
		return m.theme.RowCursor
	}
	return m.theme.Screen
}

// projectOf is the last segment of a session's working directory, which is
// enough to tell projects apart without spending the width on a full path.
func projectOf(s session.Session) string {
	if s.Cwd == "" {
		return ""
	}
	return filepath.Base(s.Cwd)
}
