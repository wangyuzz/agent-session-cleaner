package session_test

import (
	"testing"

	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// family is a conversation with two sub-agents, one of which spawned a third.
func family() *session.Forest {
	return session.Build([]session.Session{
		node("root", ""),
		node("first", "root"),
		node("nested", "first"),
		node("second", "root"),
		node("elsewhere", ""),
	})
}

func TestSelectionPicksWholeSubtree(t *testing.T) {
	t.Parallel()

	f := family()
	var picked session.Selection
	picked.Toggle(f, "root")

	if got := ids(picked.Picked(f)); got != "root,first,nested,second" {
		t.Errorf("picked %s, want the whole family", got)
	}
	if picked.Len() != 4 {
		t.Errorf("Len() = %d, want 4", picked.Len())
	}
}

func TestSelectionReleasesTheChainButNotSiblings(t *testing.T) {
	t.Parallel()

	f := family()
	var picked session.Selection
	picked.Toggle(f, "root")

	// Releasing a sub-agent has to release what it hangs from as well: an
	// ancestor left standing would take this one along regardless, and the row
	// would be claiming it had been spared when it had not. The sibling was
	// never in question and strands nothing, so it stays.
	picked.Toggle(f, "first")

	if got := ids(picked.Picked(f)); got != "second" {
		t.Errorf("picked %s, want only the untouched sibling", got)
	}
	if picked.Has("nested") {
		t.Error("releasing a sub-agent left its own child behind")
	}
}

func TestSelectionMarksMatchScope(t *testing.T) {
	t.Parallel()

	f := family()
	var picked session.Selection
	picked.Toggle(f, "root")
	picked.Toggle(f, "first")

	// Whatever the rows show is exactly what the keys act on. Anything else is
	// a row claiming to have been spared while it is deleted.
	if got, want := ids(picked.Scope(f)), ids(picked.Picked(f)); got != want {
		t.Errorf("scope %s does not match the marked rows %s", got, want)
	}
}

func TestSelectionScopeDragsSubAgentsAlong(t *testing.T) {
	t.Parallel()

	f := family()
	var picked session.Selection
	picked.Toggle(f, "root")
	// Taken out by hand, but deleting the parent would strand it either way.
	picked.Toggle(f, "nested")
	picked.Toggle(f, "root")

	if got := ids(picked.Picked(f)); got != "root,first,nested,second" {
		t.Fatalf("picked %s, want the whole family again", got)
	}
	if got := ids(picked.Scope(f)); got != "nested,first,second,root" {
		t.Errorf("scope = %s, want sub-agents before the session that spawned them", got)
	}
}

func TestSelectionRetainDropsWhatIsGone(t *testing.T) {
	t.Parallel()

	f := family()
	var picked session.Selection
	picked.Toggle(f, "elsewhere")
	picked.Toggle(f, "second")

	after := session.Build([]session.Session{node("root", ""), node("second", "root")})
	picked.Retain(after)

	if got := ids(picked.Picked(after)); got != "second" {
		t.Errorf("kept %s, want only what still exists", got)
	}
}

func TestSelectionRetainLeavesMultiSelect(t *testing.T) {
	t.Parallel()

	f := family()
	var picked session.Selection
	picked.Toggle(f, "elsewhere")

	// A selection that empties out has to leave the mode with it, or the keys
	// stay renamed with nothing to act on.
	picked.Retain(session.Build([]session.Session{node("root", "")}))
	if !picked.Empty() {
		t.Error("multi-select stayed on with nothing selected")
	}
}

func TestSelectionRemoveAndClear(t *testing.T) {
	t.Parallel()

	f := family()
	var picked session.Selection
	picked.Toggle(f, "root")

	// What an action settled is no longer something there is a decision about.
	picked.Remove("nested", "first")
	if got := ids(picked.Picked(f)); got != "root,second" {
		t.Errorf("after Remove: %s, want root,second", got)
	}

	picked.Clear()
	if !picked.Empty() {
		t.Error("Clear left something behind")
	}
}
