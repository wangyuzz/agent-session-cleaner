package agent_test

import (
	"path/filepath"
	"testing"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/agent/claude"
	"github.com/haowang02/agent-session-cleaner/internal/agent/codex"
	"github.com/haowang02/agent-session-cleaner/internal/agent/opencode"
	"github.com/haowang02/agent-session-cleaner/internal/agent/pi"
)

// every agent this program knows about, so the next one cannot half-land.
func every() []agent.Agent {
	return []agent.Agent{
		codex.New("/tmp/codex"),
		claude.New("/tmp/claude"),
		opencode.New("/tmp/data/opencode"),
		pi.New("/tmp/pi/agent"),
	}
}

func TestEveryAgentDescribesItself(t *testing.T) {
	t.Parallel()

	ids := map[string]bool{}
	shortcuts := map[rune]bool{}

	for _, a := range every() {
		meta := a.Meta()
		t.Run(meta.ID, func(t *testing.T) {
			switch {
			case meta.ID == "":
				t.Error("no id")
			case meta.Label == "":
				t.Error("no label")
			case meta.Reply == "":
				t.Error("no name to sign replies with")
			case meta.Home == "":
				t.Error("no home directory")
			case meta.DefaultClient == "":
				t.Error("no default client, so every row would carry a badge")
			case meta.BulkConcurrency < 1:
				t.Error("a batch could never make progress")
			case meta.Shortcut == 0:
				t.Error("no chooser shortcut")
			}
		})

		if ids[meta.ID] {
			t.Errorf("two agents answer to %q", meta.ID)
		}
		if shortcuts[meta.Shortcut] {
			t.Errorf("two agents answer to %q in the chooser", string(meta.Shortcut))
		}
		ids[meta.ID], shortcuts[meta.Shortcut] = true, true
	}
}

func TestExpandHome(t *testing.T) {
	// Not parallel: the home directory is read from the environment.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if got := agent.ExpandHome("~/.codex"); got != filepath.Join(home, ".codex") {
		t.Errorf("ExpandHome(~/.codex) = %q", got)
	}
	if got := agent.ExpandHome(`~\.codex`); got != filepath.Join(home, ".codex") {
		t.Errorf(`ExpandHome(~\.codex) = %q`, got)
	}
	// A tilde in the middle of a path is just a character.
	middle := filepath.Join(home, "~", "x")
	if got := agent.ExpandHome(middle); got != middle {
		t.Errorf("ExpandHome() rewrote a path it should not have: %q", got)
	}
	if got := agent.UnderHome(filepath.Join(home, ".codex")); got != filepath.Join("~", ".codex") {
		t.Errorf("UnderHome() = %q", got)
	}
	outside := filepath.Join(filepath.Dir(home), "somewhere-else", "hosts")
	if got := agent.UnderHome(outside); got != outside {
		t.Errorf("UnderHome() = %q", got)
	}
	// A lexical prefix is not a directory boundary: /home/me-too is not under
	// /home/me and must not be rendered as a bogus tilde path.
	sibling := home + "-other/session"
	if got := agent.UnderHome(sibling); got != sibling {
		t.Errorf("UnderHome(%q) = %q", sibling, got)
	}
}

func TestResolvePrefersAnOverride(t *testing.T) {
	t.Parallel()

	fallback := func() string { return "/default" }
	if got := agent.Resolve("", fallback); got != "/default" {
		t.Errorf("Resolve(\"\") = %q", got)
	}
	if got := agent.Resolve("/elsewhere", fallback); got != "/elsewhere" {
		t.Errorf("Resolve() = %q", got)
	}
}
