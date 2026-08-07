package tui

import (
	"testing"

	"github.com/haowang02/agent-session-cleaner/internal/session"
)

func TestCopySessionID(t *testing.T) {
	t.Parallel()

	f := newFake(session.Session{ID: "abc", Title: "work", Cwd: "/work/my app", Archived: true})
	f.readonly = true
	m := start(t, f)
	press(t, m, "c")

	contains(t, m, "Session ID copied to clipboard: abc")
}

func TestCopyWorkingDirectory(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(session.Session{
		ID: "abc", Title: "work", Path: "/sessions/abc.jsonl", Cwd: "/work/my app",
	}))
	cmd := send(t, m, keyPress("y"))
	if cmd == nil {
		t.Fatal("y produced no copy command")
	}
	raw := cmd()
	msg, ok := raw.(copiedClipboardMsg)
	if !ok {
		t.Fatalf("copy command returned %T", raw)
	}
	if want := "/work/my app"; msg.text != want {
		t.Errorf("clipboard text = %q, want %q", msg.text, want)
	}
	drive(t, m, send(t, m, msg))

	contains(t, m, "Working directory copied to clipboard: /work/my app")
}

func TestCopyWithNothingUnderTheCursor(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"c", "y"} {
		t.Run(key, func(t *testing.T) {
			m := start(t, newFake())
			press(t, m, key)
			contains(t, m, "No session is available.")
		})
	}
}

func TestCopyResultsCannotOverwriteNewerState(t *testing.T) {
	t.Parallel()

	const wantStatus = "Working directory not recorded"
	m := start(t, newFake(session.Session{ID: "current", Title: "current"}))
	m.copySeq = 1
	cancelled := false
	m.copyStop = func() { cancelled = true }
	press(t, m, "y")
	if !cancelled {
		t.Error("a rejected copy did not cancel the older clipboard request")
	}
	if m.status.text != wantStatus {
		t.Fatalf("status = %q, want %q", m.status.text, wantStatus)
	}
	if cmd := m.copiedToClipboard(copiedClipboardMsg{seq: 1, text: "old", native: false}); cmd != nil {
		t.Error("a stale copy attempted an OSC 52 fallback")
	}
	if m.status.text != wantStatus {
		t.Errorf("stale copy replaced status with %q", m.status.text)
	}

	m.busy = true
	m.copiedToClipboard(copiedClipboardMsg{seq: m.copySeq, text: "current", native: true})
	if m.status.text != wantStatus {
		t.Errorf("copy completion hid operation progress with %q", m.status.text)
	}
}

func TestReload(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	m := start(t, f)

	f.mu.Lock()
	f.list = append(f.list, session.Session{ID: "new", Title: "arrived later"})
	f.mu.Unlock()

	press(t, m, "r")
	contains(t, m, "Session list refreshed.")
	contains(t, m, "arrived later")
}

// Until the list is rebuilt, the next keypress would be deciding about rows
// that are already gone.
func TestKeysAreRefusedWhileAnOperationRuns(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	m.busy = true

	press(t, m, "d")
	contains(t, m, "An operation is already in progress.")
	press(t, m, "r")
	contains(t, m, "An operation is already in progress.")
	if m.dialog != nil {
		t.Error("a dialog opened while an operation was running")
	}
}
