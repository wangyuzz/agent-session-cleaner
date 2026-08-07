package tui

import (
	"strings"
	"testing"

	"github.com/haowang02/agent-session-cleaner/internal/tui/theme"
)

// marked lists the rows showing the picked mark, which is the only claim the
// interface makes about what a key will act on.
func marked(m *Model) []string {
	var found []string
	for i, r := range m.rows {
		if strings.Contains(row(m, r.Session.Title), theme.PickedMark) {
			found = append(found, m.rows[i].Session.Title)
		}
	}
	return found
}

// Whatever the rows show is exactly what the keys act on. Anything else is a
// row claiming to have been spared while it is deleted.
func TestMarkedRowsMatchWhatWouldBeActedOn(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	press(t, m, "space", "j", "space")

	if got, want := titles(m.picked.Scope(m.forest)), strings.Join(marked(m), ","); got != want {
		t.Errorf("would act on %s, but the rows show %s", got, want)
	}
}

func TestEscapeLeavesMultiSelect(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	press(t, m, "space")
	press(t, m, "esc")

	if !m.picked.Empty() {
		t.Error("Esc did not clear the selection")
	}
	contains(t, m, "Selection cleared.")
	omits(t, m, "selected")
}

// The keys do not change while a selection stands, but what they act on does,
// and the footer is the only place that says so.
func TestFooterRenamesKeysForTheSelection(t *testing.T) {
	t.Parallel()

	m := start(t, &archivingFake{newFake(tree()...)})
	contains(t, m, "d Delete")
	contains(t, m, "c Copy session ID")
	contains(t, m, "y Copy working directory")

	press(t, m, "space")
	contains(t, m, "d Delete selected")
	contains(t, m, "a Archive selected")
	// Copying acts on the row under the cursor, which is no longer what the keys
	// are about; the sweeps act on a different set from the one on screen.
	omits(t, m, "c Copy session ID")
	omits(t, m, "y Copy working directory")
	omits(t, m, "D Delete archived")
	omits(t, m, "! Danger mode")
}

// Esc backs out of the most alarming state first.
func TestEscapeUnwindsInOrder(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	press(t, m, "/", "r", "enter")
	// Danger mode goes on before anything is picked out: a standing selection
	// takes the key off the table.
	press(t, m, "!", "y")
	press(t, m, "space")

	if !m.danger {
		t.Fatal("danger mode did not come on")
	}
	press(t, m, "esc")
	if m.danger {
		t.Error("Esc should have turned danger mode off first")
	}
	if m.picked.Empty() {
		t.Error("Esc cleared the selection before danger mode")
	}

	press(t, m, "esc")
	if !m.picked.Empty() {
		t.Error("Esc should have cleared the selection next")
	}
	if m.search.query == "" {
		t.Error("Esc cleared the search before the selection")
	}

	press(t, m, "esc")
	if m.search.query != "" {
		t.Error("Esc should have cleared the search last")
	}
}
