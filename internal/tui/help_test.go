package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
)

func TestHelpOpensAndCloses(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	press(t, m, "h")
	contains(t, m, "Keyboard shortcuts")
	contains(t, m, "Move to the previous / next session")

	press(t, m, "esc")
	omits(t, m, "Keyboard shortcuts")
}

func TestHelpFitsSmallWindows(t *testing.T) {
	t.Parallel()

	for _, size := range []struct{ width, height int }{{5, 3}, {10, 6}, {20, 8}} {
		m := start(t, newFake(tree()...))
		drive(t, m, send(t, m, tea.WindowSizeMsg{Width: size.width, Height: size.height}))
		press(t, m, "h")
		box := m.helpBox()
		if got := lipgloss.Width(box); got > size.width {
			t.Errorf("%dx%d help is %d columns wide", size.width, size.height, got)
		}
		if got := lipgloss.Height(box); got > size.height {
			t.Errorf("%dx%d help is %d lines tall", size.width, size.height, got)
		}
	}
}

func TestHelpScrollDoesNotOvershootTheBottom(t *testing.T) {
	t.Parallel()

	m := start(t, newFake(tree()...))
	drive(t, m, send(t, m, tea.WindowSizeMsg{Width: 50, Height: 8}))
	press(t, m, "h")
	for range 100 {
		press(t, m, "j")
	}
	bottom := m.helpTop
	press(t, m, "k")
	if m.helpTop != bottom-1 {
		t.Errorf("one step up from the bottom moved %d to %d", bottom, m.helpTop)
	}
}

// The help lists what the keys accept, gated on the same question the key
// handler asks, so the two can never drift. It is deliberately not gated on
// what the footer is offering at the moment — see
// [TestFooterOffersOnlyWhatWouldDoSomething].
func TestHelpAndFooterAgree(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		build func() *Model
	}{
		{"an agent that can archive", func() *Model {
			return start(t, &archivingFake{newFake(tree()...)})
		}},
		{"an agent that cannot", func() *Model {
			return start(t, newFake(tree()...))
		}},
		{"an agent whose command line is missing", func() *Model {
			f := newFake(tree()...)
			f.readonly = true
			return start(t, f)
		}},
	}

	gated := []struct {
		a    action
		what i18n.Key
	}{
		{archiveAction, i18n.HelpArchive},
		{unarchiveAction, i18n.HelpUnarchive},
		{sweepArchivedAction, i18n.HelpDeleteArchived},
		{sweepEmptyAction, i18n.HelpDeleteEmpty},
		{sweepOrphansAction, i18n.HelpDeleteOrphans},
		{pickAction, i18n.HelpSelectToggle},
		{dangerAction, i18n.HelpDanger},
		{deleteAction, i18n.HelpDelete},
		{copySessionIDAction, i18n.HelpCopySessionID},
		{copyWorkingDirectoryAction, i18n.HelpCopyCwd},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m := test.build()
			press(t, m, "h")
			help := screen(m)

			for _, entry := range gated {
				want := m.allows(entry.a)
				got := strings.Contains(help, m.print.T(entry.what))
				if got != want {
					t.Errorf("help shows %q: %v, but the key exists: %v",
						m.print.T(entry.what), got, want)
				}
			}
		})
	}
}

// Every action the footer can name has a binding, and every binding resolves
// from the key it claims.
func TestBindingsAreConsistent(t *testing.T) {
	t.Parallel()

	seen := map[string]action{}
	for _, b := range bindings {
		if len(b.keys) == 0 {
			t.Errorf("action %d has no key", b.action)
		}
		for _, key := range b.keys {
			if other, clash := seen[key]; clash {
				t.Errorf("key %q is claimed by actions %d and %d", key, other, b.action)
			}
			seen[key] = b.action
			if resolve(key) != b.action {
				t.Errorf("key %q resolves to the wrong action", key)
			}
		}
	}

	for _, a := range append(append([]action{}, footerTop...), footerBottom...) {
		b, ok := (&Model{}).binding(a)
		if !ok {
			t.Errorf("footer names action %d, which has no binding", a)
		}
		if b.label == 0 {
			t.Errorf("footer names action %d, which has no label", a)
		}
	}
}
