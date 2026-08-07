package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// Transcript text is untrusted and panes can be only one cell wide.
func TestHostileMessagesInANarrowPane(t *testing.T) {
	t.Parallel()

	texts := map[string]string{
		"empty":          "",
		"newlines":       "\n\n\n",
		"invalid utf8":   "a\xff\xfeb",
		"control chars":  "a\x00\x07\r\tb",
		"ansi injection": "\x1b[31mred\x1b[0m and \x1b]8;;http://x\x07link\x1b]8;;\x07",
		"unclosed fence": "```go\nfunc main() {",
		"huge word":      strings.Repeat("x", 5000),
		"combining":      "é́́ combining",
		"emoji":          "👨‍👩‍👧‍👦 family and 🇯🇵 flag",
		"rtl":            "شكرا لك على المساعدة",
	}

	for _, width := range []int{1, 2, 10, 80} {
		for name, text := range texts {
			f := newFake(session.Session{ID: "abc", Title: "t"})
			f.talk["abc"] = []session.Message{
				{Role: session.User, Text: text},
				{Role: session.Assistant, Text: text, Images: 1, Truncated: 5},
			}
			m := New(t.Context(), f, i18n.New(i18n.English))
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("width %d, %s: panic %v", width, name, r)
					}
				}()
				drive(t, m, m.Init())
				drive(t, m, send(t, m, tea.WindowSizeMsg{Width: width, Height: 10}))
				checkScreenWidth(t, m, width, name)
				press(t, m, "tab", "j", "G", "g")
				checkScreenWidth(t, m, width, name)
			}()
		}
	}
}

func checkScreenWidth(t *testing.T, m *Model, width int, name string) {
	t.Helper()
	for i, line := range strings.Split(m.View().Content, "\n") {
		if got := ansi.StringWidth(line); got != width {
			t.Errorf("width %d, %s: screen line %d is %d cells: %q",
				width, name, i, got, ansi.Strip(line))
			return
		}
	}
}
