package tui

import (
	"testing"

	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// typing runs a search from the bar, as a person would.
func typing(t *testing.T, m *Model, sigil string, query string) {
	t.Helper()
	press(t, m, sigil)
	for _, r := range query {
		press(t, m, string(r))
	}
}

func searchable() []session.Session {
	return []session.Session{
		{ID: "a", Title: "Refactor the Parser", Cwd: "/work/app", Client: "cli"},
		{ID: "b", Title: "unrelated work", Cwd: "/work/lib", Client: "cli"},
		{ID: "c", Title: "parser tests", Cwd: "/work/app", Client: "cli"},
	}
}

func TestSearchJumpsToTheFirstMatch(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(searchable()...))
	typing(t, m, "/", "parser")

	// Smart case: an all-lowercase query matches either way.
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want the first match", m.cursor)
	}
	contains(t, m, "/parser   match 1/2")
}

// Smart case, as in vim: an uppercase letter makes the search exact.
func TestSearchSmartCase(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(searchable()...))
	typing(t, m, "/", "Parser")
	contains(t, m, "match 1/1")
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want the capitalised match", m.cursor)
	}
}

func TestSearchRepeatsAndWraps(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(searchable()...))
	typing(t, m, "/", "parser")
	press(t, m, "enter")

	press(t, m, "n")
	if m.cursor != 2 {
		t.Errorf("n left the cursor at %d, want the second match", m.cursor)
	}
	contains(t, m, "match 2/2")

	// Running off the end starts over, and says so.
	press(t, m, "n")
	if m.cursor != 0 {
		t.Errorf("n left the cursor at %d, want it wrapped to the first match", m.cursor)
	}
	contains(t, m, "wrapped to top")

	press(t, m, "N")
	contains(t, m, "wrapped to bottom")
}

func TestSearchBackwards(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(searchable()...))
	press(t, m, "G") // start at the bottom
	typing(t, m, "?", "parser")
	press(t, m, "enter")

	if m.cursor != 2 {
		t.Errorf("cursor = %d, want the match at or above the start", m.cursor)
	}
	press(t, m, "n")
	if m.cursor != 0 {
		t.Errorf("n after ? left the cursor at %d, want it to keep going up", m.cursor)
	}
}

func TestSearchReadsMoreThanTitles(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(searchable()...))
	// Directories, session ids and clients are searchable too.
	typing(t, m, "/", "lib")
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want the session in /work/lib", m.cursor)
	}
}

func TestSearchWithNoMatch(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(searchable()...))
	typing(t, m, "/", "zzz")
	contains(t, m, "No matches for “zzz”")
	if m.cursor != 0 {
		t.Errorf("a failed search moved the cursor to %d", m.cursor)
	}
}

// Abandoning a search restores both the previous query and the row the cursor
// came from.
func TestSearchCanBeAbandoned(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(searchable()...))
	press(t, m, "j") // stand on the second row
	typing(t, m, "/", "parser")
	press(t, m, "esc")

	if m.cursor != 1 {
		t.Errorf("cursor = %d, want it back where the search started", m.cursor)
	}
	if m.search.query != "" {
		t.Errorf("query = %q, want it dropped", m.search.query)
	}
	if m.search.active {
		t.Error("the search bar stayed open")
	}
}

func TestRepeatWithNoSearchSaysSo(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(searchable()...))
	press(t, m, "n")
	contains(t, m, "No active search.")
}

func TestSearchOnAnEmptyListSaysSo(t *testing.T) {
	t.Parallel()

	m := start(t, newFake())
	press(t, m, "/")
	contains(t, m, "No sessions to search.")
	if m.search.active {
		t.Error("the search bar opened with nothing to search")
	}
}
