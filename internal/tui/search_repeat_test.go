package tui

import (
	"testing"

	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// Repeated search must run the fetch command returned by the cursor jump.
func TestRepeatingASearchReadsTheSessionItLandsOn(t *testing.T) {
	t.Parallel()

	f := newFake(
		session.Session{ID: "a", Title: "target one"},
		session.Session{ID: "b", Title: "plain"},
		session.Session{ID: "c", Title: "target two"},
	)
	for _, id := range []string{"a", "b", "c"} {
		f.talk[id] = []session.Message{{Role: session.User, Text: "conversation " + id}}
	}

	m := start(t, f)
	press(t, m, "/", "t", "a", "r", "enter")

	press(t, m, "n")
	contains(t, m, "conversation c")

	press(t, m, "N")
	contains(t, m, "conversation a")
	press(t, m, "n")
	omits(t, m, "Loading")
}
