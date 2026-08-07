package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/haowang02/agent-session-cleaner/internal/session"
)

func TestConversationPane(t *testing.T) {
	t.Parallel()

	f := newFake(session.Session{
		ID: "abc", Title: "Refactor the parser", Cwd: "/work/app", Client: "cli",
		Version: "1.2.0", CreatedAt: time.Date(2026, 8, 5, 9, 30, 0, 0, time.Local),
	})
	f.talk["abc"] = []session.Message{
		{Role: session.User, Text: "make it faster"},
		{Role: session.Assistant, Text: "done"},
	}

	m := start(t, f)
	for _, want := range []string{
		"Refactor the parser", "abc", "2026-08-05 09:30", "cli", "2 messages",
		"v1.2.0", "/work/app", "▶ You", "make it faster", "◀ Fake", "done",
	} {
		contains(t, m, want)
	}
}

func TestConversationPaneWithNothingInIt(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(session.Session{ID: "abc", Title: "quiet"}))
	contains(t, m, "No messages in this session.")
	contains(t, m, "Working directory not recorded")
}

func TestArchivedAndOrphanedSessionsSayWhatTheyAre(t *testing.T) {
	t.Parallel()

	m := start(t, &archivingFake{newFake(
		session.Session{ID: "kept", Title: "kept", Archived: true},
		session.Session{ID: "lost", Title: "lost", Parent: "long-gone", SideThread: true},
		session.Session{ID: "loose", Title: "loose", SideThread: true},
	)})

	contains(t, m, "◆ Archived")

	press(t, m, "j")
	// The pane clips rather than wraps, so only the opening of the line is
	// guaranteed to be on screen.
	contains(t, m, "Source session deleted")

	press(t, m, "j")
	// Otherwise this row just looks like a top-level conversation that forgot
	// to be one. Say why it stands alone, and why it is safe.
	contains(t, m, "Source session not recorded")
}

func TestSubAgentNamesTheConversationThatSpawnedIt(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	press(t, m, "j")
	contains(t, m, "Source session: root")
}

func TestTruncatedAndImageMessages(t *testing.T) {
	t.Parallel()

	f := newFake(session.Session{ID: "abc", Title: "talk"})
	f.talk["abc"] = []session.Message{
		{Role: session.User, Text: "look", Images: 2},
		{Role: session.Assistant, Text: "long", Truncated: 1200},
	}

	m := start(t, f)
	contains(t, m, "[2 images]")
	contains(t, m, "Message truncated: 1200 trailing characters omitted")
}

func TestTabMovesFocusToTheConversation(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	f.talk["root"] = []session.Message{{Role: session.User, Text: strings.Repeat("line\n", 60)}}

	m := start(t, f)
	press(t, m, "tab")
	if m.focus != focusDetail {
		t.Fatal("Tab did not move focus to the conversation")
	}

	before := m.cursor
	press(t, m, "j")
	if m.cursor != before {
		t.Error("j moved the session cursor while the conversation had focus")
	}

	press(t, m, "tab")
	press(t, m, "j")
	if m.cursor == before {
		t.Error("Tab did not hand focus back to the list")
	}
}

func TestClickingTheConversationMovesFocusToIt(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))

	before := m.cursor
	click(t, m, m.listWidth()+2, bannerHeight+1)
	if m.focus != focusDetail {
		t.Fatal("clicking the conversation did not move focus to it")
	}
	if m.cursor != before {
		t.Errorf("the click moved the session cursor from %d to %d", before, m.cursor)
	}

	click(t, m, m.listWidth(), bannerHeight+1)
	if m.focus != focusDetail {
		t.Error("clicking the rule between the panes moved focus")
	}

	click(t, m, 1, bannerHeight+len(m.rows)+1)
	if m.focus != focusList {
		t.Error("clicking the list's empty space did not hand focus back to it")
	}
}

func TestClickingAwayEndsADoubleClick(t *testing.T) {
	t.Parallel()

	for name, away := range map[string]func(*Model) (int, int){
		"conversation": func(m *Model) (int, int) {
			return m.listWidth() + 2, bannerHeight + 1
		},
		"divider": func(m *Model) (int, int) {
			return m.listWidth(), bannerHeight + 1
		},
		"empty list space": func(m *Model) (int, int) {
			return 1, bannerHeight + len(m.rows) + 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := start(t, newFake(tree()...))
			click(t, m, 1, bannerHeight+1)
			x, y := away(m)
			click(t, m, x, y)
			click(t, m, 1, bannerHeight+1)
			if !m.picked.Empty() {
				t.Error("returning to a row after a detour selected it")
			}
		})
	}
}

func click(t *testing.T, m *Model, x, y int) {
	t.Helper()
	drive(t, m, send(t, m, tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y}))
}

// Tab changes what every key does, so which pane holds it cannot be a guess.
// Two things say so at once: the rule between the panes, and how strongly the
// row under the cursor is filled.
func TestFocusIsVisibleOnScreen(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	rules := strings.Count(screen(m), "│")
	if rules == 0 {
		t.Fatalf("no rule between the panes:\n%s", screen(m))
	}
	onTheList, listRule := cursorFill(m), ruleColour(m)
	if onTheList == "" || listRule == "" {
		t.Fatalf("the list holds focus but nothing says so: fill %q rule %q",
			onTheList, listRule)
	}

	press(t, m, "tab")

	// The rule lights up rather than thickening: the same glyph in every
	// state, so the column cannot look like it changed shape.
	if got := strings.Count(screen(m), "│"); got != rules {
		t.Errorf("the rule was redrawn with a different glyph: %d of │, want %d",
			got, rules)
	}
	if got := ruleColour(m); got == "" || got == listRule {
		t.Errorf("the rule is %q either way, so it says nothing about focus", got)
	}
	if got := cursorFill(m); got == "" || got == onTheList {
		t.Errorf("the cursor fill is %q either way, so it says nothing about focus", got)
	}
}

// cursorFill is the background colour of the row the cursor stands on.
func cursorFill(m *Model) string {
	line := strings.Split(m.View().Content, "\n")[1+m.cursor-m.top]
	return fillOf(ansi.Cut(line, m.listWidth()-1, m.listWidth()))
}

// ruleColour is the colour of the rule drawn between the two panes.
func ruleColour(m *Model) string {
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(lipgloss.NewLayer(m.View().Content))
	cell := canvas.CellAt(m.listWidth(), bannerHeight)
	if cell == nil || cell.Content != "│" || cell.Style.Fg == nil {
		return ""
	}
	return fmt.Sprint(cell.Style.Fg)
}

// The interface paints its own background rather than letting the terminal's
// show through, so a pane that stops where its text does would leave a hole.
// Every cell of every state has to carry a colour.
func TestTheWholeScreenIsPainted(t *testing.T) {
	t.Parallel()

	states := map[string]func(m *Model){
		"listing":     func(*Model) {},
		"searching":   func(m *Model) { press(t, m, "/", "a") },
		"selecting":   func(m *Model) { press(t, m, "space", "j") },
		"on the pane": func(m *Model) { press(t, m, "tab") },
		"confirming":  func(m *Model) { press(t, m, "d") },
		"helping":     func(m *Model) { press(t, m, "h") },
		"empty":       func(m *Model) {},
		// Wide characters cover two cells, only the first of which carries a
		// colour, so a screen full of them must not read as full of holes.
		"in Chinese": func(*Model) {},
		// Chroma's full reset must not leave holes in the pane background.
		"marked up": func(*Model) {},
		"marked up in daylight": func(m *Model) {
			drive(t, m, send(t, m, tea.BackgroundColorMsg{Color: lipgloss.White}))
		},
	}
	for name, reach := range states {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			target := newFake(tree()...)
			switch name {
			case "empty":
				target = newFake()
			case "in Chinese":
				target = newFake(session.Session{
					ID: "宽", Title: "递归删除所有缓存目录", Cwd: "/work/项目",
				})
				target.talk["宽"] = []session.Message{
					{Role: session.User, Text: "把这个项目里的宽字符都对齐"},
				}
			case "marked up", "marked up in daylight":
				target = newFake(session.Session{ID: "md", Title: "a reply"})
				target.talk["md"] = []session.Message{{
					Role: session.Assistant,
					Text: "## Heading\n\nSome **bold** and `code`:\n\n" +
						"```go\nfunc main() { println(1) }\n```\n\n" +
						"| a | b |\n|---|---|\n| 1 | 2 |\n\n" +
						"> quoted\n\n- one\n- two\n\n[a link](https://example.com)\n",
				}}
			}
			m := start(t, target)
			reach(m)

			for _, at := range bare(m.View().Content, m.width, m.height) {
				t.Errorf("cell %v is not painted:\n%s", at, screen(m))
				break
			}
		})
	}
}

// bare lists the cells of a rendered screen that set no background colour.
func bare(content string, width, height int) [][2]int {
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewLayer(content))

	var found [][2]int
	for y := range height {
		for x := range width {
			// A zero-width cell is the second half of a wide character,
			// which the first half already colours.
			cell := canvas.CellAt(x, y)
			if cell == nil || (cell.Width > 0 && cell.Style.Bg == nil) {
				found = append(found, [2]int{x, y})
			}
		}
	}
	return found
}

// The viewport's ScrollDown does nothing once the view is at the bottom,
// whatever number it is handed, so a negative step gets stuck there. Both the
// keys and the wheel have to be able to come back up.
func TestTheConversationScrollsBackUpFromTheBottom(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	f.talk["root"] = []session.Message{{Role: session.User, Text: strings.Repeat("line\n", 200)}}

	for _, test := range []struct {
		name string
		up   func(m *Model)
	}{
		{"keys", func(m *Model) { press(t, m, "k") }},
		{"wheel", func(m *Model) {
			drive(t, m, send(t, m, wheel(tea.MouseWheelUp, m.listWidth()+2)))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := start(t, f)
			press(t, m, "tab", "G")
			if !m.detail.AtBottom() {
				t.Fatal("G did not reach the bottom of the conversation")
			}

			bottom := m.detail.YOffset()
			test.up(m)
			if m.detail.YOffset() >= bottom {
				t.Errorf("still at offset %d after scrolling up from %d",
					m.detail.YOffset(), bottom)
			}
		})
	}
}

func TestResizePreservesConversationScrollPosition(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	f.talk["root"] = []session.Message{{Role: session.User, Text: strings.Repeat("line of text\n", 160)}}
	m := start(t, f)
	press(t, m, "tab")
	for range 20 {
		press(t, m, "j")
	}
	before := m.detail.YOffset()
	if before == 0 {
		t.Fatal("test did not scroll the conversation")
	}
	drive(t, m, send(t, m, tea.WindowSizeMsg{Width: 100, Height: 26}))
	if got := m.detail.YOffset(); got == 0 {
		t.Errorf("resize reset conversation offset %d to the top", before)
	}
}

// wheel builds the message the runtime delivers for one notch of the wheel.
func wheel(button tea.MouseButton, x int) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{Button: button, X: x, Y: 5}
}

func TestEmptySessionsAreNamedByTheInterface(t *testing.T) {
	t.Parallel()

	// The agent leaves the title empty; what to call a session with nothing in
	// it is this layer's decision.
	m := start(t, newFake(session.Session{ID: "abc", Noise: true}))
	contains(t, m, "(Empty session)")
	press(t, m, "d", "y")
	contains(t, m, "Deleted: (Empty session)")
}
