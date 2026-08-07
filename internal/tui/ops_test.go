package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/haowang02/agent-session-cleaner/internal/session"
	"github.com/haowang02/agent-session-cleaner/internal/tui/text"
)

// A side thread exists only because of the conversation that spawned it, so it
// goes wherever that conversation goes — and it goes first, so a failure
// part-way through leaves the parent standing rather than a fresh orphan.
func TestDeleteTakesSubAgentsFirst(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	m := start(t, f)
	press(t, m, "d")

	contains(t, m, "Delete this session?")
	contains(t, m, "This also deletes 3 descendant sub-agent sessions.")
	press(t, m, "y")

	if got := strings.Join(f.calls(), ","); got != "nested,first,second,root" {
		t.Errorf("acted on %s, want sub-agents before the session that spawned them", got)
	}
	contains(t, m, "Deleted: root (including 3 sub-agent sessions)")
}

func TestDeleteCanBeCancelled(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	m := start(t, f)
	press(t, m, "d", "n")

	if len(f.calls()) != 0 {
		t.Errorf("cancelling still ran %v", f.calls())
	}
	if m.dialog != nil {
		t.Error("the dialog stayed open")
	}
}

// A confirmation is a question about what is on the screen, so the screen has
// to stay on it: the box sits in the middle, faded but legible either side.
func TestDialogSitsOverTheScreenRatherThanReplacingIt(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	press(t, m, "d")

	lines := strings.Split(screen(m), "\n")
	var top, bottom, left int
	for i, line := range lines {
		at := strings.Index(line, "╭")
		if at < 0 {
			continue
		}
		top, left = i, at
	}
	for i, line := range lines {
		if strings.Contains(line, "╰") {
			bottom = i
		}
	}
	if top == 0 || left == 0 {
		t.Fatalf("no dialog on the screen:\n%s", screen(m))
	}

	// Centred: the margins above and below, and to the left and right, match
	// to within the rounding of an odd number of spare lines.
	below := len(lines) - 1 - bottom
	if diff := top - below; diff < -1 || diff > 1 {
		t.Errorf("the box has %d lines above it and %d below", top, below)
	}
	right := m.width - text.Width(strings.TrimRight(lines[top], " "))
	if diff := left - right; diff < -1 || diff > 1 {
		t.Errorf("the box has %d columns to its left and %d to its right", left, right)
	}

	// And the interface it is asking about is still there around it.
	for _, want := range []string{"Fake", "5 sessions", "other", "/tmp/fake"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("the dialog blanked out %q:\n%s", want, screen(m))
		}
	}
}

// Enter is safe: the dialog opens with Cancel focused.
func TestDialogOpensOnCancel(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	m := start(t, f)
	press(t, m, "d", "enter")

	if len(f.calls()) != 0 {
		t.Errorf("Enter on a fresh dialog deleted %v", f.calls())
	}
}

func TestDialogBlocksMouseInputToTheListing(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	press(t, m, "d")
	before := m.cursor
	click := tea.MouseClickMsg{Button: tea.MouseLeft, X: 1, Y: bannerHeight + 2}
	drive(t, m, send(t, m, click))
	drive(t, m, send(t, m, click))

	if m.cursor != before || !m.picked.Empty() {
		t.Errorf("mouse input passed through the dialog: cursor=%d picked=%d", m.cursor, m.picked.Len())
	}
	if m.dialog == nil {
		t.Error("the dialog unexpectedly closed")
	}
}

func TestDialogFitsSmallWindowsAndKeepsItsControls(t *testing.T) {
	t.Parallel()

	for _, size := range []struct{ width, height int }{{5, 3}, {10, 6}, {20, 8}} {
		m := start(t, newFake(tree()...))
		drive(t, m, send(t, m, tea.WindowSizeMsg{Width: size.width, Height: size.height}))
		press(t, m, "d")
		box := m.dialogBox()
		if got := lipgloss.Width(box); got > size.width {
			t.Errorf("%dx%d dialog is %d columns wide", size.width, size.height, got)
		}
		if got := len(strings.Split(box, "\n")); got > size.height {
			t.Errorf("%dx%d dialog is %d lines tall", size.width, size.height, got)
		}
		if size.width >= 10 && (!strings.Contains(plain(box), "(y)") || !strings.Contains(plain(box), "(n)")) {
			t.Errorf("%dx%d dialog lost its controls:\n%s", size.width, size.height, plain(box))
		}
	}
}

// Danger mode drops the confirmation for the one session under the cursor. A
// batch always confirms, however the mode is set.
func TestDangerModeSkipsOnlyTheSingleDeletion(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	m := start(t, f)

	press(t, m, "!")
	contains(t, m, "Enable danger mode?")
	press(t, m, "y")
	contains(t, m, "Danger mode on.")

	press(t, m, "G") // "other", which has no sub-agents
	press(t, m, "d")
	if m.dialog != nil {
		t.Error("danger mode still asked about a single deletion")
	}
	if got := strings.Join(f.calls(), ","); got != "other" {
		t.Errorf("acted on %s", got)
	}

	press(t, m, "g", "space", "d")
	if m.dialog == nil {
		t.Error("a batch deletion must confirm even in danger mode")
	}
}

func TestArchiveAndUnarchive(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	m := start(t, &archivingFake{f})

	press(t, m, "a")
	contains(t, m, "Archived: root (including 3 sub-agent sessions)")
	if !strings.Contains(row(m, "root"), "root") {
		t.Error("the row went missing after archiving")
	}

	// Archiving what is already archived is an error the agent's own command
	// line would rightly complain about, so it is left out of the cascade.
	f.acted = nil
	press(t, m, "a")
	contains(t, m, "This session is already archived.")

	press(t, m, "u")
	contains(t, m, "Unarchived: root")
}

func TestArchiveKeysAreHiddenWhereTheyMeanNothing(t *testing.T) {
	t.Parallel()

	// This agent has nowhere to put a session it is not deleting.
	m := start(t, newFake(tree()...))
	omits(t, m, "a Archive")
	omits(t, m, "u Unarchive")
	omits(t, m, "D Delete archived")
	contains(t, m, "d Delete")

	// The key does nothing rather than reporting a failure from the agent.
	press(t, m, "a")
	omits(t, m, "already archived")
}

func TestWritingKeysVanishWithoutTheAgentsCommand(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	f.readonly = true
	m := start(t, f)

	contains(t, m, "Fake CLI is unavailable. You can browse sessions")
	omits(t, m, "d Delete")
	omits(t, m, "␣ Select sessions")
	omits(t, m, "! Danger mode")
	// Browsing and copying session metadata need no agent CLI.
	contains(t, m, "c Copy session ID")
	contains(t, m, "y Copy working directory")
	contains(t, m, "/ Search")
}

// A refusal says nothing about a session unrelated to it. What a batch holds
// back is only the chain the refusal hangs from.
func TestBatchHoldsBackTheChainButNotSiblings(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	f.refuse = map[string]error{"first": errors.New("refused")}
	m := start(t, f)

	press(t, m, "space") // root and its three sub-agents
	press(t, m, "d", "y")

	acted := strings.Join(f.calls(), ",")
	if !strings.Contains(acted, "second") {
		t.Errorf("acted on %s, want the untouched sibling deleted", acted)
	}
	// "root" would have been left with a sub-agent behind it, which is exactly
	// what strands one.
	if strings.Contains(acted, "root") {
		t.Errorf("acted on %s, want the parent held back", acted)
	}
}

// The numbers have to add up to what the list still shows.
func TestBatchFailureTallyMatchesWhatIsLeft(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	f.refuse = map[string]error{"first": errors.New("refused")}
	m := start(t, f)

	press(t, m, "space", "d", "y")

	contains(t, m, "Deleted 2/4; 2 not completed")
	if got := m.forest.Len(); got != 3 {
		t.Errorf("%d sessions remain, want 3", got)
	}
	// What could not be settled is still something there is a decision about,
	// so it stays selected.
	if got := titles(m.picked.Picked(m.forest)); got != "root,first" {
		t.Errorf("still selected: %s, want what did not go through", got)
	}
}

func TestSuccessfulBatchLeavesMultiSelect(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	m := start(t, f)
	press(t, m, "space", "d", "y")

	contains(t, m, "Deleted 4 sessions")
	if !m.picked.Empty() {
		t.Error("multi-select stayed on after everything settled")
	}
}

func TestOperationCompletionEndsDrag(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(
		session.Session{ID: "one", Title: "one"},
		session.Session{ID: "two", Title: "two"},
		session.Session{ID: "three", Title: "three"},
	))
	press(t, m, "space", "j", "space", "j", "space")
	m.busy = true
	click(t, m, 1, bannerHeight+1)
	dragToRow(t, m, 2)

	_ = m.progressed(opUpdate{done: true, result: opResult{
		plan: plan{
			kind: opDelete, single: true, subject: m.rows[0].Session,
		},
		total: 1, settled: []string{"one"},
	}})
	dragToRow(t, m, 1)
	if !m.picked.Empty() {
		t.Error("motion restored a selection snapshot after an operation completed")
	}
}

// A cascade gives up at the first refusal, and says why the session under the
// cursor was left alone.
func TestCascadeStopsAtARefusingSubAgent(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	f.refuse = map[string]error{"nested": errors.New("still running")}
	m := start(t, f)

	press(t, m, "d", "y")

	if got := strings.Join(f.calls(), ","); got != "nested" {
		t.Errorf("acted on %s, want it to stop at the refusal", got)
	}
	contains(t, m, "Could not delete a sub-agent session.")
	contains(t, m, "The source session was left unchanged")
}

func TestSweepsActOnTheRightSets(t *testing.T) {
	t.Parallel()

	sessions := tree()
	sessions[4].Archived = true // "other"
	sessions = append(sessions,
		session.Session{ID: "empty", Title: "", Noise: true},
		session.Session{ID: "lost", Title: "lost", Parent: "long-gone", SideThread: true},
	)

	t.Run("archived", func(t *testing.T) {
		t.Parallel()
		f := newFake(sessions...)
		m := start(t, &archivingFake{f})
		press(t, m, "D", "y")
		if got := strings.Join(f.calls(), ","); got != "other" {
			t.Errorf("swept %s, want only the archived session", got)
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		f := newFake(sessions...)
		m := start(t, f)
		press(t, m, "E", "y")
		if got := strings.Join(f.calls(), ","); got != "empty" {
			t.Errorf("swept %s, want only the empty session", got)
		}
	})

	t.Run("orphans", func(t *testing.T) {
		t.Parallel()
		f := newFake(sessions...)
		m := start(t, f)
		press(t, m, "O", "y")
		if got := strings.Join(f.calls(), ","); got != "lost" {
			t.Errorf("swept %s, want only the stranded sub-agent", got)
		}
	})
}

func TestSweepsSayWhenThereIsNothingToDo(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	m := start(t, &archivingFake{f})

	press(t, m, "D")
	contains(t, m, "No archived sessions found.")
	press(t, m, "E")
	contains(t, m, "No empty sessions found.")
	press(t, m, "O")
	contains(t, m, "every recorded source session is still available")
	if len(f.calls()) != 0 {
		t.Errorf("a sweep with nothing to do still ran %v", f.calls())
	}
}

// Sweeping a set of parents without their sub-agents would manufacture exactly
// the orphans the next key along has to clean up.
func TestSweepDragsSubAgentsAlongAndSaysSo(t *testing.T) {
	t.Parallel()

	sessions := tree()
	sessions[0].Archived = true // the root of the family
	f := newFake(sessions...)
	m := start(t, &archivingFake{f})

	press(t, m, "D")
	contains(t, m, "Also deletes 3 sub-agent sessions to avoid leaving orphans.")
	press(t, m, "y")

	if got := strings.Join(f.calls(), ","); got != "nested,first,second,root" {
		t.Errorf("swept %s, want the whole family deepest first", got)
	}
}

func TestQuitWaitsForAnOperation(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	m.busy = true
	press(t, m, "q")
	contains(t, m, "Wait for the current operation to finish")
}

func TestControlCQuitsFromModalStates(t *testing.T) {
	t.Parallel()

	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	for _, reach := range []func(*Model){
		func(m *Model) { press(t, m, "h") },
		func(m *Model) { press(t, m, "/") },
		func(m *Model) { press(t, m, "d") },
	} {
		m := start(t, newFake(tree()...))
		reach(m)
		cmd := m.press(ctrlC)
		if cmd == nil {
			t.Fatal("Ctrl+C produced no quit command")
		}
		msg := cmd()
		if _, ok := msg.(tea.QuitMsg); !ok {
			t.Errorf("Ctrl+C command returned %T, want tea.QuitMsg", msg)
		}
	}
}

func TestRefreshBlocksMutationsUntilItsSnapshotArrives(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	m := start(t, f)
	refresh := m.reload()
	if !m.refreshing {
		t.Fatal("reload did not mark the listing as refreshing")
	}
	press(t, m, "d")
	if m.dialog != nil || len(f.calls()) != 0 {
		t.Error("deletion started against a listing being refreshed")
	}
	contains(t, m, "An operation is already in progress")
	drive(t, m, refresh)
	if m.refreshing {
		t.Error("the refresh stayed in flight after its response")
	}
}

func TestFailedRefreshMakesTheOldSnapshotReadOnly(t *testing.T) {
	t.Parallel()

	f := newFake(tree()...)
	m := start(t, f)
	f.discoverErr = errors.New("cannot read store")
	drive(t, m, m.reload())
	if !m.stale {
		t.Fatal("a failed refresh left the old snapshot writable")
	}

	press(t, m, "d")
	if m.dialog != nil || len(f.calls()) != 0 {
		t.Error("deletion started against a stale snapshot")
	}
	contains(t, m, "Press r before changing anything else")

	f.discoverErr = nil
	press(t, m, "r")
	if m.stale {
		t.Error("a successful refresh did not make the snapshot usable again")
	}
}
