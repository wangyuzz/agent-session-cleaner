package tui

import (
	"testing"

	"github.com/haowang02/agent-session-cleaner/internal/session"
)

func TestCopyResumeCommand(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(session.Session{ID: "abc", Title: "work", Cwd: "/work/my app"}))
	press(t, m, "c")

	// The directory is quoted so a shell reads it as one argument.
	contains(t, m, "cd '/work/my app' && fake --resume abc")
	contains(t, m, "Resume command copied to clipboard")
}

func TestCopyResumeWithoutADirectory(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(session.Session{ID: "abc", Title: "work"}))
	press(t, m, "c")
	contains(t, m, "clipboard: fake --resume abc")
}

// Only worth saying where there is an unarchive key to say it about.
func TestCopyMentionsUnarchivingOnlyWhereItExists(t *testing.T) {
	t.Parallel()

	archived := session.Session{ID: "abc", Title: "work", Archived: true}

	canArchive := start(t, &archivingFake{newFake(archived)})
	press(t, canArchive, "c")
	contains(t, canArchive, "unarchive this session with u")

	cannot := start(t, newFake(archived))
	press(t, cannot, "c")
	omits(t, cannot, "unarchive this session with u")
}

// The command is still worth having: it will work on a machine where the agent
// is installed.
func TestCopyWarnsWhenTheAgentIsNotInstalled(t *testing.T) {
	t.Parallel()

	f := newFake(session.Session{ID: "abc", Title: "work"})
	f.readonly = true
	m := start(t, f)
	press(t, m, "c")

	contains(t, m, "Fake CLI is unavailable on this machine")
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
	if cmd := m.copied(copiedMsg{seq: 1, command: "old", native: false}); cmd != nil {
		t.Error("a stale copy attempted an OSC 52 fallback")
	}
	if m.status.text != "newer status" {
		t.Errorf("stale copy replaced status with %q", m.status.text)
	}

	m.busy = true
	m.copied(copiedMsg{seq: 2, command: "current", native: true})
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
