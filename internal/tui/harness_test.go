package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// fake stands in for an agent. Because capability is expressed as an
// interface, a test agent that cannot archive is simply this one; a test agent
// that can is [archivingFake].
type fake struct {
	mu          sync.Mutex
	meta        agent.Meta
	list        []session.Session
	talk        map[string][]session.Message
	refuse      map[string]error
	acted       []string
	readonly    bool
	slow        time.Duration
	discoverErr error
}

func newFake(sessions ...session.Session) *fake {
	return &fake{
		meta: agent.Meta{
			ID: "fake", Label: "Fake", Reply: "Fake", Shortcut: 'f',
			Home: "/tmp/fake", DefaultClient: "cli",
			EmptyLabel: i18n.EmptySessions, OrphanLabel: i18n.OrphanSessions,
			BulkConcurrency: 4,
		},
		// A copy: deleting from one fake rewrites its listing in place, and
		// callers routinely build several fakes from one slice of sessions.
		list: append([]session.Session(nil), sessions...),
		talk: map[string][]session.Message{},
	}
}

func (f *fake) Meta() agent.Meta { return f.meta }
func (f *fake) Preflight() error { return nil }
func (f *fake) Writable() bool   { return !f.readonly }

func (f *fake) Discover(context.Context) ([]session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]session.Session(nil), f.list...), f.discoverErr
}

func (f *fake) Messages(_ context.Context, s session.Session) ([]session.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.talk[s.ID], nil
}

func (f *fake) Delete(_ context.Context, s session.Session) error {
	return f.act(s, func(int) { f.drop(s.ID) })
}

// act records the call, honours a staged refusal, and applies the change.
func (f *fake) act(s session.Session, change func(int)) error {
	if f.slow > 0 {
		time.Sleep(f.slow)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acted = append(f.acted, s.ID)
	if err := f.refuse[s.ID]; err != nil {
		return err
	}
	for i := range f.list {
		if f.list[i].ID == s.ID {
			change(i)
			break
		}
	}
	return nil
}

// drop removes a session, along with the sub-agents it took with it, the way
// an agent's own delete command cascades.
func (f *fake) drop(id string) {
	kept := f.list[:0]
	for _, s := range f.list {
		if s.ID != id {
			kept = append(kept, s)
		}
	}
	f.list = kept
}

func (f *fake) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.acted...)
}

// archivingFake is an agent that can also set sessions aside.
type archivingFake struct{ *fake }

func (f *archivingFake) Archive(_ context.Context, s session.Session) error {
	return f.act(s, func(i int) { f.list[i].Archived = true })
}

func (f *archivingFake) Unarchive(_ context.Context, s session.Session) error {
	return f.act(s, func(i int) { f.list[i].Archived = false })
}

// start builds a model, sizes it and lets its first load settle, so a test
// begins where a user would.
func start(t *testing.T, target agent.Agent) *Model {
	t.Helper()
	m := New(t.Context(), target, i18n.New(i18n.English))
	drive(t, m, m.Init())
	drive(t, m, send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30}))
	return m
}

// drive runs a command to completion, feeding whatever it produces back into
// the model, exactly as the runtime would. Update is a pure function, so this
// needs no terminal and nothing to wait for.
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

func dragToRow(t *testing.T, m *Model, row int) {
	t.Helper()
	drive(t, m, send(t, m, tea.MouseMotionMsg{
		Button: tea.MouseLeft, X: 1, Y: bannerHeight + row,
	}))
}

func releaseMouse(t *testing.T, m *Model) {
	t.Helper()
	drive(t, m, send(t, m, tea.MouseReleaseMsg{Button: tea.MouseLeft}))
}

// press feeds one key by name and settles whatever it starts.
func press(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, key := range keys {
		drive(t, m, send(t, m, keyPress(key)))
	}
}

// keyPress builds the message the runtime would deliver for a key.
func keyPress(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	runes := []rune(name)
	key := tea.KeyPressMsg{Code: runes[0], Text: name}
	if len(runes) == 1 && runes[0] >= 'A' && runes[0] <= 'Z' {
		key.Mod = tea.ModShift
	}
	return key
}

// plain removes styling, leaving what a person would read off the terminal.
func plain(s string) string { return ansi.Strip(s) }

// screen is the whole rendered interface, unstyled.
func screen(m *Model) string { return plain(m.View().Content) }

// row returns the list side of the line showing this title. The conversation
// pane repeats the title of the session under the cursor, so a search across
// the whole screen would find the wrong line.
func row(m *Model, title string) string {
	for _, l := range strings.Split(screen(m), "\n") {
		if left := ansi.Truncate(l, m.listWidth(), ""); strings.Contains(left, title) {
			return left
		}
	}
	return ""
}

func contains(t *testing.T, m *Model, want string) {
	t.Helper()
	if !strings.Contains(screen(m), want) {
		t.Errorf("the screen does not mention %q:\n%s", want, screen(m))
	}
}

func omits(t *testing.T, m *Model, unwanted string) {
	t.Helper()
	if strings.Contains(screen(m), unwanted) {
		t.Errorf("the screen mentions %q and should not:\n%s", unwanted, screen(m))
	}
}

// titles lists what is picked out, in display order.
func titles(sessions []session.Session) string {
	names := make([]string, len(sessions))
	for i, s := range sessions {
		names[i] = s.Title
	}
	return strings.Join(names, ",")
}

// tree is a small family: a conversation with two sub-agents, one of which
// spawned a third, plus an unrelated conversation.
func tree() []session.Session {
	// List labels are relative to the current date.
	day := time.Now().AddDate(0, 0, -1)
	when := func(hour int) time.Time {
		return time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, time.Local)
	}
	return []session.Session{
		{ID: "root", Title: "root", Cwd: "/work/app", Client: "cli", CreatedAt: when(12)},
		{ID: "first", Title: "first", Parent: "root", SideThread: true,
			Client: "subagent", Cwd: "/work/app", CreatedAt: when(11)},
		{ID: "nested", Title: "nested", Parent: "first", SideThread: true,
			Client: "subagent", Cwd: "/work/app", CreatedAt: when(10)},
		{ID: "second", Title: "second", Parent: "root", SideThread: true,
			Client: "subagent", Cwd: "/work/app", CreatedAt: when(9)},
		{ID: "other", Title: "other", Cwd: "/work/lib", Client: "cli", CreatedAt: when(8)},
	}
}
