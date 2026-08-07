package picker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

type stub struct {
	meta     agent.Meta
	count    int
	readonly bool
	err      error
}

func (s *stub) Meta() agent.Meta { return s.meta }
func (s *stub) Preflight() error { return nil }
func (s *stub) Writable() bool   { return !s.readonly }

func (s *stub) Delete(context.Context, session.Session) error { return nil }

func (s *stub) Messages(context.Context, session.Session) ([]session.Message, error) {
	return nil, nil
}

func (s *stub) Discover(context.Context) ([]session.Session, error) {
	found := make([]session.Session, s.count)
	for i := range found {
		found[i] = session.Session{ID: string(rune('a' + i))}
	}
	return found, s.err
}

// present builds an agent whose data directory exists.
func present(t *testing.T, id, label string, shortcut rune, count int) *stub {
	t.Helper()
	home := filepath.Join(t.TempDir(), id)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return &stub{meta: agent.Meta{ID: id, Label: label, Shortcut: shortcut, Home: home}, count: count}
}

func absent(id, label string, shortcut rune) *stub {
	return &stub{meta: agent.Meta{
		ID: id, Label: label, Shortcut: shortcut, Home: "/nowhere/" + id,
	}}
}

func open(t *testing.T, agents ...agent.Agent) *Model {
	t.Helper()
	m := New(t.Context(), agents, i18n.New(i18n.English))
	drive(t, m, m.Init())
	drive(t, m, send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24}))
	return m
}

func drive(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, next := range msg {
			drive(t, m, next)
		}
	default:
		drive(t, m, send(t, m, msg))
	}
}

func send(t *testing.T, m *Model, msg tea.Msg) tea.Cmd {
	t.Helper()
	_, cmd := m.Update(msg)
	return cmd
}

func press(t *testing.T, m *Model, key string) {
	t.Helper()
	runes := []rune(key)
	msg := tea.KeyPressMsg{Code: runes[0], Text: key}
	switch key {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	}
	drive(t, m, send(t, m, msg))
}

func screen(m *Model) string { return ansi.Strip(m.View().Content) }

func TestPickerListsAgentsWithTheirCounts(t *testing.T) {
	t.Parallel()

	m := open(t,
		present(t, "codex", "Codex", 'x', 3),
		absent("claude", "Claude Code", 'c'),
	)

	view := screen(m)
	for _, want := range []string{"Choose an agent", "Codex", "3 sessions", "Claude Code",
		"Data directory unavailable"} {
		if !strings.Contains(view, want) {
			t.Errorf("the chooser does not mention %q:\n%s", want, view)
		}
	}
}

func TestPickerOpensWithEnter(t *testing.T) {
	t.Parallel()

	m := open(t, present(t, "codex", "Codex", 'x', 1), present(t, "claude", "Claude", 'c', 2))
	press(t, m, "down")
	press(t, m, "enter")

	if m.Choice != "claude" {
		t.Errorf("Choice = %q, want claude", m.Choice)
	}
}

func TestPickerOpensWithAShortcut(t *testing.T) {
	t.Parallel()

	m := open(t, present(t, "codex", "Codex", 'x', 1), present(t, "claude", "Claude", 'c', 2))
	press(t, m, "c")

	if m.Choice != "claude" {
		t.Errorf("Choice = %q, want claude", m.Choice)
	}
}

// Explain why the key did nothing rather than leaving it ambiguous.
func TestPickerExplainsAnAgentItCannotOpen(t *testing.T) {
	t.Parallel()

	m := open(t, present(t, "codex", "Codex", 'x', 1), absent("claude", "Claude Code", 'c'))
	press(t, m, "c")

	if m.Choice != "" {
		t.Errorf("Choice = %q, want nothing chosen", m.Choice)
	}
	if !strings.Contains(screen(m), "Claude Code: data directory unavailable") {
		t.Errorf("the chooser did not say why:\n%s", screen(m))
	}
}

func TestPickerStartsOnSomethingOpenable(t *testing.T) {
	t.Parallel()

	m := open(t, absent("codex", "Codex", 'x'), present(t, "claude", "Claude", 'c', 1))
	press(t, m, "enter")

	if m.Choice != "claude" {
		t.Errorf("Choice = %q, want the cursor to have started on a usable agent", m.Choice)
	}
}

func TestPickerWithNothingInstalled(t *testing.T) {
	t.Parallel()

	m := open(t, absent("codex", "Codex", 'x'), absent("claude", "Claude", 'c'))
	if !strings.Contains(screen(m), "No usable session data directories found.") {
		t.Errorf("the chooser did not say the tree was empty:\n%s", screen(m))
	}
}

func TestPickerMarksBrowseOnlyAgents(t *testing.T) {
	t.Parallel()

	browsable := present(t, "codex", "Codex", 'x', 2)
	browsable.readonly = true

	m := open(t, browsable)
	if !strings.Contains(screen(m), "CLI unavailable; browse only") {
		t.Errorf("the chooser did not flag a browse-only agent:\n%s", screen(m))
	}
}

func TestPickerDoesNotTurnReadErrorsIntoZeroSessions(t *testing.T) {
	t.Parallel()

	broken := present(t, "codex", "Codex", 'x', 0)
	broken.err = errors.New("permission denied")
	m := open(t, broken)
	if !strings.Contains(screen(m), "Could not read session data") {
		t.Errorf("the chooser hid a discovery error as an empty store:\n%s", screen(m))
	}

	partial := present(t, "claude", "Claude", 'c', 2)
	partial.err = errors.New("one project unreadable")
	m = open(t, partial)
	for _, want := range []string{"2 sessions", "partial"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("the chooser lost %q from a partial result:\n%s", want, screen(m))
		}
	}
}

func TestPickerQuits(t *testing.T) {
	t.Parallel()

	m := open(t, present(t, "codex", "Codex", 'x', 1))
	press(t, m, "q")
	if m.Choice != "" {
		t.Errorf("Choice = %q, want nothing chosen", m.Choice)
	}
}

// The wordmark is the first thing to go: a terminal too small for it still has
// to be able to choose an agent.
func TestWordmarkGivesWayToTheAgents(t *testing.T) {
	t.Parallel()

	m := open(t, present(t, "codex", "Codex", 'x', 101),
		absent("opencode", "OpenCode", 'o'))
	drive(t, m, send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}))

	roomy := ansi.Strip(m.View().Content)
	if !strings.Contains(roomy, wordmark[0]) {
		t.Errorf("no wordmark on a 100x30 screen:\n%s", roomy)
	}
	for _, want := range []string{"Codex", "101 sessions", "x", "OpenCode"} {
		if !strings.Contains(roomy, want) {
			t.Errorf("the opening screen does not mention %q:\n%s", want, roomy)
		}
	}

	drive(t, m, send(t, m, tea.WindowSizeMsg{Width: 44, Height: 12}))
	cramped := ansi.Strip(m.View().Content)
	if strings.Contains(cramped, wordmark[0]) {
		t.Errorf("the wordmark does not fit but was drawn anyway:\n%s", cramped)
	}
	for _, want := range []string{"Codex", "Choose an agent"} {
		if !strings.Contains(cramped, want) {
			t.Errorf("a cramped screen dropped %q:\n%s", want, cramped)
		}
	}
}

// The opening screen paints its own background, like the interface it opens.
func TestOpeningScreenIsPainted(t *testing.T) {
	t.Parallel()

	m := open(t, present(t, "codex", "Codex", 'x', 101),
		absent("opencode", "OpenCode", 'o'))

	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(lipgloss.NewLayer(m.View().Content))
	for y := range m.height {
		for x := range m.width {
			// A zero-width cell is the second half of a wide character,
			// which the first half already colours.
			cell := canvas.CellAt(x, y)
			if cell == nil || (cell.Width > 0 && cell.Style.Bg == nil) {
				t.Fatalf("cell [%d %d] is not painted:\n%s", x, y, ansi.Strip(m.View().Content))
			}
		}
	}
}
