package session_test

import (
	"strings"
	"testing"

	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// node builds a session with just the fields the tree cares about.
func node(id, parent string) session.Session {
	return session.Session{ID: id, Title: id, Parent: parent, SideThread: parent != ""}
}

// shape renders the forest as one line per row, so a layout can be asserted
// as a picture rather than as a slice of enum values.
func shape(f *session.Forest) string {
	glyph := map[session.Guide]string{
		session.GuideGap:     "  ",
		session.GuideTrunk:   "| ",
		session.GuideBranch:  "+-",
		session.GuideLast:    "L-",
		session.GuideSevered: "~~",
		session.GuideUnknown: "??",
	}
	var lines []string
	for _, row := range f.Rows() {
		var prefix strings.Builder
		for _, guide := range row.Guides {
			prefix.WriteString(glyph[guide])
		}
		lines = append(lines, prefix.String()+row.Session.ID)
	}
	return strings.Join(lines, "\n")
}

func TestForestLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sessions []session.Session
		want     string
	}{
		{
			name:     "a flat listing has no tree at all",
			sessions: []session.Session{node("a", ""), node("b", "")},
			want:     "a\nb",
		},
		{
			name: "children of a conversation start at the left edge",
			sessions: []session.Session{
				node("parent", ""), node("first", "parent"), node("second", "parent"),
			},
			want: "parent\n+-first\nL-second",
		},
		{
			name: "a trunk runs past the children of a branch that has siblings",
			sessions: []session.Session{
				node("root", ""),
				node("branch", "root"),
				node("nested", "branch"),
				node("last", "root"),
			},
			want: "root\n+-branch\n| L-nested\nL-last",
		},
		{
			name: "the last branch leaves a gap under it",
			sessions: []session.Session{
				node("root", ""), node("only", "root"), node("nested", "only"),
			},
			want: "root\nL-only\n  L-nested",
		},
		{
			name: "a sub-agent with no parent recorded says so",
			sessions: []session.Session{
				{ID: "loose", Title: "loose", SideThread: true},
			},
			want: "??loose",
		},
		{
			name: "a sub-agent whose parent is gone is severed, not nested",
			sessions: []session.Session{
				node("kept", ""), node("stranded", "deleted-parent"),
			},
			want: "kept\n~~stranded",
		},
		{
			name: "a severed row still carries its own children",
			sessions: []session.Session{
				node("stranded", "gone"), node("below", "stranded"),
			},
			want: "~~stranded\n  L-below",
		},
		{
			name: "a session claiming itself as its parent stays a root",
			sessions: []session.Session{
				{ID: "self", Title: "self", Parent: "self"},
			},
			want: "self",
		},
		{
			name: "a parent cycle stays visible",
			sessions: []session.Session{
				node("first", "second"), node("second", "first"),
			},
			want: "first\nsecond",
		},
		{
			name: "sibling order follows the listing order",
			sessions: []session.Session{
				node("root", ""), node("newest", "root"), node("oldest", "root"),
			},
			want: "root\n+-newest\nL-oldest",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := shape(session.Build(test.sessions))
			if got != test.want {
				t.Errorf("layout:\n%s\nwant:\n%s", got, test.want)
			}
		})
	}
}

// Nested says a row is drawn underneath the session that spawned it, which is
// what lets the display leave off columns the parent already carries. A session
// whose parent is gone stands on its own and is not nested, however it got
// there.
func TestForestRowsSayWhichAreNested(t *testing.T) {
	t.Parallel()

	f := session.Build([]session.Session{
		node("root", ""),
		node("child", "root"),
		node("grandchild", "child"),
		node("stranded", "long-gone"),
		node("self", "self"),
	})

	want := map[string]bool{
		"root": false, "child": true, "grandchild": true,
		"stranded": false, "self": false,
	}
	for _, row := range f.Rows() {
		if got := row.Nested; got != want[row.Session.ID] {
			t.Errorf("%s: Nested = %v, want %v", row.Session.ID, got, want[row.Session.ID])
		}
	}
}

func TestForestDropsDuplicateSessionIDs(t *testing.T) {
	t.Parallel()

	f := session.Build([]session.Session{
		{ID: "same", Title: "first", Path: "/first"},
		{ID: "same", Title: "second", Path: "/second"},
		{ID: "other", Title: "other"},
	})
	if f.Len() != 2 || len(f.Rows()) != 2 {
		t.Fatalf("forest kept duplicate rows: %+v", f.Rows())
	}
	got, ok := f.Get("same")
	if !ok || got.Path != "/first" {
		t.Errorf("Get(same) = %+v, %v; want the first occurrence", got, ok)
	}
}

func TestForestOrphans(t *testing.T) {
	t.Parallel()

	// An archived parent still exists, so its children are not orphans.
	archived := node("parent", "")
	archived.Archived = true
	f := session.Build([]session.Session{
		archived,
		node("child", "parent"),
		node("stranded", "long-gone"),
		node("root", ""),
	})

	if got := ids(f.Orphans()); got != "stranded" {
		t.Errorf("Orphans() = %s, want stranded", got)
	}
	if !f.Stranded("stranded") || f.Stranded("child") {
		t.Error("Stranded disagrees with Orphans")
	}
}

func TestForestDescendantsAndAncestors(t *testing.T) {
	t.Parallel()

	f := session.Build([]session.Session{
		node("root", ""),
		node("mid", "root"),
		node("leaf", "mid"),
		node("sibling", "root"),
		node("elsewhere", ""),
	})

	// Deepest first, so a cascade never steps over a session whose parent it
	// has already removed.
	if got := ids(f.Descendants("root")); got != "leaf,mid,sibling" {
		t.Errorf("Descendants(root) = %s, want leaf,mid,sibling", got)
	}
	if got := ids(f.Descendants("leaf")); got != "" {
		t.Errorf("Descendants(leaf) = %s, want nothing", got)
	}
	if got := ids(f.Ancestors("leaf")); got != "mid,root" {
		t.Errorf("Ancestors(leaf) = %s, want mid,root", got)
	}
	if got := ids(f.Ancestors("elsewhere")); got != "" {
		t.Errorf("Ancestors(elsewhere) = %s, want nothing", got)
	}
}

func TestForestWalksTerminateOnCycles(t *testing.T) {
	t.Parallel()

	f := session.Build([]session.Session{
		node("first", "second"),
		node("second", "first"),
	})

	// Nothing here is correct data; the only requirement is that asking
	// returns at all.
	if got := ids(f.Descendants("first")); got != "second" {
		t.Errorf("Descendants(first) = %s, want second", got)
	}
	if got := ids(f.Ancestors("first")); got != "second" {
		t.Errorf("Ancestors(first) = %s, want second", got)
	}
}

func TestForestCascade(t *testing.T) {
	t.Parallel()

	kept := node("kept", "root")
	archived := node("archived", "root")
	archived.Archived = true
	root := node("root", "")
	f := session.Build([]session.Session{root, kept, archived})

	// Sub-agents first, then the session under the cursor.
	if got := ids(f.Cascade(root, nil)); got != "kept,archived,root" {
		t.Errorf("Cascade = %s, want kept,archived,root", got)
	}

	// Archiving what is already archived is an error the agent's own command
	// line would rightly complain about.
	unarchived := f.Cascade(root, func(s session.Session) bool { return !s.Archived })
	if got := ids(unarchived); got != "kept,root" {
		t.Errorf("filtered Cascade = %s, want kept,root", got)
	}
}

func TestForestWithDescendants(t *testing.T) {
	t.Parallel()

	f := session.Build([]session.Session{
		node("root", ""),
		node("mid", "root"),
		node("leaf", "mid"),
		node("other", ""),
	})

	// Overlapping families are listed once each, deepest first.
	got := ids(f.WithDescendants([]session.Session{
		mustGet(t, f, "root"), mustGet(t, f, "mid"), mustGet(t, f, "other"),
	}))
	if got != "leaf,mid,root,other" {
		t.Errorf("WithDescendants = %s, want leaf,mid,root,other", got)
	}
}

func TestForestBranches(t *testing.T) {
	t.Parallel()

	build := func(sessions ...session.Session) *session.Forest { return session.Build(sessions) }

	t.Run("unrelated sessions run side by side", func(t *testing.T) {
		t.Parallel()
		f := build(node("a", ""), node("b", ""))
		branches := f.Branches([]session.Session{mustGet(t, f, "a"), mustGet(t, f, "b")})
		if got := describe(branches); got != "a|b" {
			t.Errorf("Branches = %s, want a|b", got)
		}
	})

	t.Run("one family runs in one sequence, deepest first", func(t *testing.T) {
		t.Parallel()
		f := build(node("root", ""), node("mid", "root"), node("leaf", "mid"))
		branches := f.Branches([]session.Session{
			mustGet(t, f, "root"), mustGet(t, f, "leaf"), mustGet(t, f, "mid"),
		})
		if got := describe(branches); got != "leaf,mid,root" {
			t.Errorf("Branches = %s, want leaf,mid,root", got)
		}
	})

	t.Run("a generation left out of the batch does not split the family", func(t *testing.T) {
		t.Parallel()
		// The middle session is filtered out of the batch — already archived,
		// say. Grandparent and grandchild are still one branch, and running
		// them at the same time would be exactly the race this prevents.
		f := build(node("grand", ""), node("mid", "grand"), node("leaf", "mid"))
		branches := f.Branches([]session.Session{
			mustGet(t, f, "grand"), mustGet(t, f, "leaf"),
		})
		if got := describe(branches); got != "leaf,grand" {
			t.Errorf("Branches = %s, want one branch leaf,grand", got)
		}
	})

	t.Run("a parent cycle becomes one sequential group", func(t *testing.T) {
		t.Parallel()
		f := build(node("first", "second"), node("second", "first"))
		branches := f.Branches([]session.Session{
			mustGet(t, f, "first"), mustGet(t, f, "second"),
		})
		if len(branches) != 1 {
			t.Errorf("Branches = %s, want a single group", describe(branches))
		}
	})

	t.Run("an empty batch has no branches", func(t *testing.T) {
		t.Parallel()
		if got := build().Branches(nil); len(got) != 0 {
			t.Errorf("Branches(nil) = %v, want nothing", got)
		}
	})
}

func describe(branches [][]session.Session) string {
	parts := make([]string, len(branches))
	for i, branch := range branches {
		parts[i] = ids(branch)
	}
	return strings.Join(parts, "|")
}

func mustGet(t *testing.T, f *session.Forest, id string) session.Session {
	t.Helper()
	s, ok := f.Get(id)
	if !ok {
		t.Fatalf("no session %q in the forest", id)
	}
	return s
}
