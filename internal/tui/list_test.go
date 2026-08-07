package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
	"github.com/haowang02/agent-session-cleaner/internal/tui/text"
	"github.com/haowang02/agent-session-cleaner/internal/tui/theme"
)

func TestRowsShowTheTree(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))

	// Sub-agents sit beneath the conversation that spawned them, and the
	// trunk runs past the children of a branch that has siblings below it.
	want := []string{"root", "├─", "│ ╰─", "╰─", "other"}
	lines := strings.Split(screen(m), "\n")[1:6]
	for i, marker := range want {
		if !strings.Contains(lines[i], marker) {
			t.Errorf("row %d = %q, want it to show %q", i, lines[i], marker)
		}
	}
}

func TestRowColumns(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	first := row(m, "root")

	// The date is printed once per day rather than once per row.
	if !strings.Contains(first, "12:00") || !strings.Contains(first, "app") {
		t.Errorf("row = %q, want the time and project columns", first)
	}
	if strings.Count(screen(m), "Yesterday") != 1 {
		t.Errorf("the day was repeated on every row:\n%s", screen(m))
	}
	// One client dominates each agent, so only the exceptions get a badge.
	if strings.Contains(first, "cli ·") {
		t.Errorf("row = %q, want the default client left unsaid", first)
	}
	if !strings.Contains(row(m, "first"), "subagent ·") {
		t.Error("a sub-agent row should say so")
	}
}

func TestCursorAndPickedMarkers(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	if !strings.HasPrefix(row(m, "root"), theme.CursorMark) {
		t.Error("the cursor should start on the first row")
	}

	press(t, m, "j")
	if !strings.HasPrefix(row(m, "first"), theme.CursorMark) {
		t.Errorf("j did not move the cursor:\n%s", screen(m))
	}

	press(t, m, "space")
	if !strings.Contains(row(m, "first"), theme.PickedMark) {
		t.Errorf("space did not mark the row:\n%s", screen(m))
	}
}

// The gutter marks repeat what a fill across the whole row already says. The
// fill is the signal people actually read, so it has to be there, it has to
// differ between the cursor and a pick, and it has to reach the edge of the
// pane rather than stopping after the last word.
func TestCursorAndSelectionFillTheWholeRow(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	background := func(title string) string {
		for _, line := range strings.Split(m.View().Content, "\n") {
			// The conversation pane repeats the title of the session under the
			// cursor, so only the list side of the line counts.
			left := ansi.Truncate(line, m.listWidth(), "")
			if !strings.Contains(stripped(left), title) {
				continue
			}
			// The last cell of the pane carries the fill only if it reached
			// the end, which is the half of this that a stray Pad would break.
			return fillOf(ansi.Cut(left, m.listWidth()-1, m.listWidth()))
		}
		return ""
	}

	press(t, m, "G") // "other", the last row, well away from the cursor
	press(t, m, "g")

	cursor := background("root")
	if cursor == "" {
		t.Errorf("the cursor row carries no fill:\n%q", m.View().Content)
	}
	if plainRow := background("other"); plainRow == cursor {
		t.Errorf("an ordinary row is filled like the cursor row (%q)", cursor)
	}

	press(t, m, "j", "space") // pick "first" and everything under it
	nested := background("nested")
	if nested == "" || nested == cursor {
		t.Errorf("a picked row should have a fill of its own, got %q against %q",
			nested, cursor)
	}
	if both := background("first"); both == "" || both == nested {
		t.Errorf("a row that is both picked and under the cursor should say so, got %q", both)
	}
}

// fillOf is the background colour a styled fragment sets, or "" for none.
func fillOf(s string) string {
	for _, part := range strings.Split(s, "\x1b[") {
		if at := strings.Index(part, "48;2;"); at >= 0 {
			return part[at:strings.Index(part, "m")]
		}
	}
	return ""
}

// A sub-agent ran inside the conversation drawn above it, at the same time and
// in the same directory. Repeating those columns on its row buries the tree.
func TestNestedRowsDropTheDateAndProject(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	if parent := row(m, "root"); !strings.Contains(parent, "12:00") ||
		!strings.Contains(parent, "app") {
		t.Fatalf("the parent row lost its columns: %q", parent)
	}
	for _, title := range []string{"first", "nested", "second"} {
		child := row(m, title)
		if strings.Contains(child, ":00") || strings.Contains(child, "app") {
			t.Errorf("sub-agent row %q repeats its parent's columns", child)
		}
	}
	// A sub-agent whose parent is gone stands on its own, so it keeps them.
	orphan := start(t, newFake(session.Session{
		ID: "lost", Title: "lost", Parent: "long-gone", SideThread: true,
		Cwd: "/work/app", CreatedAt: time.Date(2026, 8, 5, 9, 0, 0, 0, time.Local),
	}))
	if line := row(orphan, "lost"); !strings.Contains(line, "09:00") {
		t.Errorf("an orphan row should keep its own date and time, got %q", line)
	}
}

func TestNavigationStopsAtTheEnds(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	press(t, m, "k", "k")
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want it held at the top", m.cursor)
	}
	press(t, m, "G")
	if m.cursor != len(m.rows)-1 {
		t.Errorf("G left the cursor at %d", m.cursor)
	}
	press(t, m, "j")
	if m.cursor != len(m.rows)-1 {
		t.Errorf("cursor = %d, want it held at the bottom", m.cursor)
	}
	press(t, m, "g")
	if m.cursor != 0 {
		t.Errorf("g left the cursor at %d", m.cursor)
	}
}

// The list scrolls only as far as it must, so a run of j does not redraw from
// the top every time.
func TestListScrollsToFollowTheCursor(t *testing.T) {
	t.Parallel()

	var many []session.Session
	for i := range 40 {
		many = append(many, session.Session{
			ID:        string(rune('a' + i%26)),
			Title:     "session " + string(rune('a'+i%26)),
			CreatedAt: time.Now().Add(-time.Duration(i) * time.Hour),
		})
	}
	// Ids must be unique for the forest to hold all of them.
	for i := range many {
		many[i].ID = many[i].ID + string(rune('0'+i/26))
	}

	m := start(t, newFake(many...))
	drive(t, m, send(t, m, tea.WindowSizeMsg{Width: 100, Height: 14}))
	if m.top != 0 {
		t.Fatalf("top = %d before moving", m.top)
	}

	height := m.bodyHeight()
	for range height {
		press(t, m, "j")
	}
	if m.top == 0 {
		t.Error("the list never scrolled")
	}
	if m.cursor < m.top || m.cursor >= m.top+height {
		t.Errorf("cursor %d is outside the visible window [%d,%d)", m.cursor, m.top, m.top+height)
	}
}

func TestClickOutsideTheListBodyDoesNotSelectAnInvisibleRow(t *testing.T) {
	t.Parallel()

	var many []session.Session
	for i := range 30 {
		many = append(many, session.Session{ID: fmt.Sprintf("s-%02d", i), Title: fmt.Sprintf("session %02d", i)})
	}
	m := start(t, newFake(many...))
	drive(t, m, send(t, m, tea.WindowSizeMsg{Width: 80, Height: 12}))
	before := m.cursor
	// This is the status line immediately below the visible list body. Before
	// the boundary check it mapped to a real, but invisible, session index.
	drive(t, m, send(t, m, tea.MouseClickMsg{
		Button: tea.MouseLeft, X: 1, Y: bannerHeight + m.bodyHeight(),
	}))
	if m.cursor != before {
		t.Errorf("clicking the status line moved the cursor from %d to %d", before, m.cursor)
	}
}

func TestDoubleClickRequiresTwoTimelyClicks(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name            string
		elapsed         time.Duration
		initiallyPicked bool
		wantPicked      bool
	}{
		{"within the window", doubleClickWindow / 2, false, true},
		{"timely deselection", doubleClickWindow / 2, true, false},
		{"late selection", doubleClickWindow + time.Second, false, false},
		{"late deselection", doubleClickWindow + time.Second, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m := start(t, newFake(tree()...))
			if test.initiallyPicked {
				m.pick()
			}
			click(t, m, 1, bannerHeight)
			m.clickedAt = m.clickedAt.Add(-test.elapsed)

			click(t, m, 1, bannerHeight)

			if got := !m.picked.Empty(); got != test.wantPicked {
				t.Errorf("picked = %t after %v, want %t", got, test.elapsed, test.wantPicked)
			}
		})
	}
}

func TestDragUsesDynamicRange(t *testing.T) {
	t.Parallel()

	sessions := []session.Session{
		{ID: "one", Title: "one"},
		{ID: "two", Title: "two"},
		{ID: "three", Title: "three"},
		{ID: "four", Title: "four"},
	}

	for _, test := range []struct {
		name              string
		initial           []string
		back              int
		wantFar, wantBack string
	}{
		{"select", []string{"three"}, 1, "two,three,four", "two,three"},
		{"deselect", []string{"one", "two", "three", "four"}, 2, "one", "one,four"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := start(t, newFake(sessions...))
			for _, id := range test.initial {
				m.picked.Toggle(m.forest, id)
			}
			click(t, m, 1, bannerHeight+1)
			dragToRow(t, m, 3)
			if got := titles(m.picked.Picked(m.forest)); got != test.wantFar {
				t.Errorf("drag picked %s, want %s", got, test.wantFar)
			}

			dragToRow(t, m, test.back)
			if got := titles(m.picked.Picked(m.forest)); got != test.wantBack {
				t.Errorf("dragging back picked %s, want %s", got, test.wantBack)
			}

			releaseMouse(t, m)
			dragToRow(t, m, 0)
			if got := titles(m.picked.Picked(m.forest)); got != test.wantBack {
				t.Errorf("motion after release changed the selection to %s", got)
			}
		})
	}
}

func TestReloadEndsDrag(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	click(t, m, 1, bannerHeight+3)
	drive(t, m, send(t, m, loadedMsg{sessions: tree()[:1]}))
	dragToRow(t, m, 0)

	if !m.picked.Empty() {
		t.Error("motion after a reload selected from a stale drag origin")
	}
}

func TestDraggingAwayEndsADoubleClick(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	click(t, m, 1, bannerHeight)
	drive(t, m, send(t, m, tea.MouseMotionMsg{
		Button: tea.MouseLeft, X: m.listWidth() + 1, Y: bannerHeight,
	}))
	releaseMouse(t, m)
	click(t, m, 1, bannerHeight)

	if !m.picked.Empty() {
		t.Error("clicking after a drag away was treated as a double click")
	}
}

func TestBannerCounts(t *testing.T) {
	t.Parallel()

	sessions := tree()
	sessions[4].Archived = true
	sessions = append(sessions, session.Session{
		ID: "lost", Title: "lost", Parent: "long-gone", SideThread: true,
	})

	m := start(t, newFake(sessions...))
	banner := strings.Split(screen(m), "\n")[0]

	for _, want := range []string{"Fake", "6 sessions", "1 archived", "1 orphaned"} {
		if !strings.Contains(banner, want) {
			t.Errorf("banner = %q, want it to mention %q", banner, want)
		}
	}
}

// The banner is the one line that stays put, so a count of zero would be noise.
func TestBannerLeavesOutEmptyCounts(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	banner := strings.Split(screen(m), "\n")[0]
	if strings.Contains(banner, "0 ") {
		t.Errorf("banner = %q, want no zero counts", banner)
	}
}

func TestEmptyListing(t *testing.T) {
	t.Parallel()

	m := New(t.Context(), newFake(), i18n.New(i18n.English))
	drive(t, m, send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30}))
	drive(t, m, m.Init())
	contains(t, m, "No sessions found.")
}

// A key the footer stops mentioning is a key nobody finds, so a group that
// does not fit wraps instead of losing its tail. The widest agent — the one
// that can archive and sweep everything — in a narrow terminal is the test.
func TestFooterWrapsRatherThanClipping(t *testing.T) {
	t.Parallel()

	const narrow = 60
	m := start(t, &archivingFake{newFake(tree()...)})
	drive(t, m, send(t, m, tea.WindowSizeMsg{Width: narrow, Height: 30}))

	// Every key this agent has, whether or not it would do anything just now:
	// this is about the layout, not about what is on offer.
	lines := m.footerLines(false, func(a action) bool { return m.allowsWhile(a, false) })
	for i, line := range lines {
		if width := text.Width(stripped(line)); width > narrow {
			t.Errorf("footer line %d is %d cells wide:\n%s", i, width, stripped(line))
		}
	}

	shown := stripped(strings.Join(lines, "\n"))
	for _, want := range []string{
		"Archive", "Unarchive", "Delete", "Copy session ID", "Copy working directory",
		"Delete archived", "Delete empty", "Delete orphans", "Danger mode",
		"Select sessions", "Search", "Refresh", "Shortcuts", "Quit",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("the footer does not mention %q:\n%s", want, shown)
		}
	}
}

// Picking a session renames most of the keys and hides some of them. The
// footer is sized for the worst case of the two, so the panes above it must
// not shift by a line when a selection starts or ends.
func TestFooterHeightSurvivesASelection(t *testing.T) {
	t.Parallel()

	for _, width := range []int{60, 80, 120} {
		m := start(t, &archivingFake{newFake(tree()...)})
		drive(t, m, send(t, m, tea.WindowSizeMsg{Width: width, Height: 30}))

		before := len(strings.Split(screen(m), "\n"))
		press(t, m, "space")
		if after := len(strings.Split(screen(m), "\n")); after != before {
			t.Errorf("at %d columns the screen went from %d lines to %d after picking",
				width, before, after)
		}
	}
}

func TestDayLabels(t *testing.T) {
	t.Parallel()

	today := time.Date(2026, 8, 6, 10, 0, 0, 0, time.Local)
	print := i18n.New(i18n.English)

	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{"today", today.Add(-2 * time.Hour), "Today"},
		{"yesterday", today.AddDate(0, 0, -1), "Yesterday"},
		{"this year", today.AddDate(0, 0, -30), "07-07"},
		// Include the year so an old session cannot look recent.
		{"another year", today.AddDate(-1, 0, 0), "25-08-06"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := dayLabel(print, test.when, today); got != test.want {
				t.Errorf("dayLabel = %q, want %q", got, test.want)
			}
		})
	}
}

func stripped(s string) string {
	return strings.TrimRight(plain(s), " ")
}

// A daylight-saving change makes one day 23 hours long. Dividing raw hours by
// 24 would round that down to zero and label yesterday "Today", twice a year.
func TestDayLabelsSurviveDaylightSaving(t *testing.T) {
	t.Parallel()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no timezone database here")
	}
	// Clocks go forward at 02:00 on 29 March 2026.
	after := time.Date(2026, 3, 29, 10, 0, 0, 0, berlin)
	before := time.Date(2026, 3, 28, 10, 0, 0, 0, berlin)

	if got := dayLabel(i18n.New(i18n.English), before, after); got != "Yesterday" {
		t.Errorf("dayLabel across a short day = %q, want Yesterday", got)
	}
	if got := dayLabel(i18n.New(i18n.English), after, after); got != "Today" {
		t.Errorf("dayLabel = %q, want Today", got)
	}
}

// The footer offers what would happen now. A key with nothing to act on would
// only report that it had nothing to act on, so it is not worth a place there.
func TestFooterOffersOnlyWhatWouldDoSomething(t *testing.T) {
	t.Parallel()

	t.Run("sweeps appear with something to sweep", func(t *testing.T) {
		t.Parallel()
		m := start(t, &archivingFake{newFake(tree()...)})
		for _, gone := range []string{
			"D Delete archived", "E Delete empty", "O Delete orphans",
		} {
			omits(t, m, gone)
		}

		sessions := tree()
		sessions[4].Archived = true
		sessions = append(sessions,
			session.Session{ID: "empty", Noise: true},
			session.Session{ID: "lost", Title: "lost", Parent: "gone", SideThread: true},
		)
		full := start(t, &archivingFake{newFake(sessions...)})
		for _, want := range []string{
			"D Delete archived", "E Delete empty", "O Delete orphans",
		} {
			contains(t, full, want)
		}
	})

	t.Run("archive and unarchive follow the cursor", func(t *testing.T) {
		t.Parallel()
		m := start(t, &archivingFake{newFake(
			session.Session{ID: "live", Title: "live"},
			session.Session{ID: "kept", Title: "kept", Archived: true},
		)})
		contains(t, m, "a Archive")
		omits(t, m, "u Unarchive")

		press(t, m, "j")
		contains(t, m, "u Unarchive")
		omits(t, m, "a Archive")
	})

	t.Run("a selection of archived sessions cannot be archived", func(t *testing.T) {
		t.Parallel()
		m := start(t, &archivingFake{newFake(
			session.Session{ID: "kept", Title: "kept", Archived: true},
			session.Session{ID: "live", Title: "live"},
		)})
		press(t, m, "space") // the archived one
		contains(t, m, "u Unarchive selected")
		omits(t, m, "a Archive selected")

		press(t, m, "j", "space") // and the live one alongside it
		contains(t, m, "a Archive selected")
	})

	t.Run("nothing to act on at all", func(t *testing.T) {
		t.Parallel()
		m := start(t, newFake())
		for _, gone := range []string{
			"d Delete", "c Copy session ID", "y Copy working directory",
			"␣ Select sessions", "/ Search",
		} {
			omits(t, m, gone)
		}
		for _, kept := range []string{"r Refresh", "h Shortcuts", "q Quit"} {
			contains(t, m, kept)
		}
	})

	// The footer is a prompt, not the list of what the keys accept: a key it
	// stops offering still works, and still says why it did nothing.
	t.Run("a hidden key still explains itself", func(t *testing.T) {
		t.Parallel()
		m := start(t, &archivingFake{newFake(tree()...)})
		omits(t, m, "O Delete orphans")
		press(t, m, "O")
		contains(t, m, "every recorded source session is still available")
	})
}
