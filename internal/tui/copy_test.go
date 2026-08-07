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

func TestCopyWithNothingUnderTheCursor(t *testing.T) {
	t.Parallel()

	m := start(t, newFake())
	press(t, m, "c")
	contains(t, m, "No session is available.")
}

func TestLateCopyResultCannotOverwriteNewerState(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	m.copySeq = 2
	m.status = statusLine{text: "newer status"}
	if cmd := m.copiedSessionID(copiedSessionIDMsg{seq: 1, sessionID: "old", native: false}); cmd != nil {
		t.Error("a stale copy attempted an OSC 52 fallback")
	}
	if m.status.text != "newer status" {
		t.Errorf("stale copy replaced status with %q", m.status.text)
	}

	m.busy = true
	m.copiedSessionID(copiedSessionIDMsg{seq: 2, sessionID: "current", native: true})
	if m.status.text != "newer status" {
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
