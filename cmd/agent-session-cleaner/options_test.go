package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/buildinfo"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		args  []string
		agent string
		homes map[string]string
	}{
		{"nothing at all", nil, "", map[string]string{}},
		{"an agent", []string{"codex"}, "codex", map[string]string{}},
		{
			// The standard library's parser stops at the first positional
			// argument, which would silently ignore the directory here.
			name:  "an agent before its options",
			args:  []string{"codex", "--codex-home", "/tmp/x"},
			agent: "codex",
			homes: map[string]string{"codex": "/tmp/x"},
		},
		{
			name:  "options before the agent",
			args:  []string{"--claude-home=/tmp/y", "claude"},
			agent: "claude",
			homes: map[string]string{"claude": "/tmp/y"},
		},
		{
			name:  "every home at once",
			args:  []string{"--codex-home", "/a", "--claude-home", "/b", "--opencode-home", "/c", "--pi-home", "/d"},
			homes: map[string]string{"codex": "/a", "claude": "/b", "opencode": "/c", "pi": "/d"},
		},
		{
			name:  "a single dash is short form",
			args:  []string{"-codex-home", "/a"},
			homes: map[string]string{"codex": "/a"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			opts, err := parse(test.args)
			if err != nil {
				t.Fatalf("parse(%v) = %v", test.args, err)
			}
			if opts.agent != test.agent {
				t.Errorf("agent = %q, want %q", opts.agent, test.agent)
			}
			for id, want := range test.homes {
				if opts.homes[id] != want {
					t.Errorf("%s home = %q, want %q", id, opts.homes[id], want)
				}
			}
			if len(opts.homes) != len(test.homes) {
				t.Errorf("homes = %v, want %v", opts.homes, test.homes)
			}
		})
	}
}

func TestParseHelpAndVersion(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"-h", "--help"} {
		if opts, err := parse([]string{flag}); err != nil || !opts.help {
			t.Errorf("parse(%q) = %+v, %v", flag, opts, err)
		}
	}
	if opts, err := parse([]string{"--version"}); err != nil || !opts.version {
		t.Errorf("parse(--version) = %+v, %v", opts, err)
	}
}

func TestParseRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"an agent nobody has heard of", []string{"gemini"}, "unknown agent “gemini”"},
		{"an option nobody has heard of", []string{"--colour"}, "unknown option “--colour”"},
		{"a home flag for no agent", []string{"--gemini-home", "/x"}, "unknown option"},
		{"a home flag with nothing after it", []string{"--codex-home"}, "needs a value"},
		{"a home flag followed by another option", []string{"--codex-home", "--help"}, "needs a value"},
		{"a help flag with a value", []string{"--help=yes"}, "unknown option"},
		{"a version flag with a value", []string{"--version=1"}, "unknown option"},
		{"two agents", []string{"codex", "claude"}, "unexpected argument “claude”"},
	}

	print := i18n.New(i18n.English)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parse(test.args)
			if err == nil {
				t.Fatalf("parse(%v) succeeded", test.args)
			}
			if got := print.Err(err); !strings.Contains(got, test.want) {
				t.Errorf("message = %q, want it to mention %q", got, test.want)
			}
		})
	}
}

func TestParseExpandsHome(t *testing.T) {
	// Not parallel: the home directory is read from the environment.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	opts, err := parse([]string{"--codex-home", "~/.codex"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".codex"); opts.homes["codex"] != want {
		t.Errorf("home = %q, want %q", opts.homes["codex"], want)
	}
}

func TestUsageNamesEveryAgent(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	usage(&out, i18n.New(i18n.English))
	text := out.String()

	for _, want := range []string{
		buildinfo.Name, "Usage:", "Options:", "-h, --help", "--version",
		"--codex-home", "--claude-home", "--opencode-home", "--pi-home",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("usage does not mention %q:\n%s", want, text)
		}
	}
}

func TestUsageDoesNotClipTheListOfAgents(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	usage(&out, i18n.New(i18n.English))
	agents := strings.Join(ids(), "|")
	if got := strings.Count(out.String(), agents); got != 2 {
		t.Errorf("%q appears %d times, want it whole in both the usage line and the arguments:\n%s",
			agents, got, out.String())
	}
}

func TestUsageIsTranslated(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	usage(&out, i18n.New(i18n.Chinese))
	if !strings.Contains(out.String(), "用法：") {
		t.Errorf("Chinese usage was not translated:\n%s", out.String())
	}
}

func TestRegistryIsComplete(t *testing.T) {
	t.Parallel()

	if got := strings.Join(ids(), ","); got != "codex,claude,opencode,pi" {
		t.Errorf("ids() = %s", got)
	}
	agents := buildAll(map[string]string{"codex": "/tmp/codex"})
	if len(agents) != len(known) {
		t.Fatalf("built %d agents, want %d", len(agents), len(known))
	}
	if got := agents[0].Meta().Home; got != "/tmp/codex" {
		t.Errorf("the override did not reach the agent: %q", got)
	}
	for _, entry := range known {
		if _, ok := build(entry.id, nil); !ok {
			t.Errorf("cannot build %q", entry.id)
		}
		if entry.homeHelp == 0 {
			t.Errorf("%q has no help for its --home flag", entry.id)
		}
	}
	if _, ok := build("gemini", nil); ok {
		t.Error("built an agent that does not exist")
	}
}

// Refusing a session tree that cannot be worked with happens before anything
// is listed. Saying so once a deletion is under way would be too late.
func TestReady(t *testing.T) {
	t.Parallel()

	t.Run("a missing directory", func(t *testing.T) {
		t.Parallel()
		target, _ := build("codex", map[string]string{"codex": "/nowhere/at/all"})
		err := ready(target)
		if err == nil {
			t.Fatal("ready() accepted a directory that does not exist")
		}
		if got := i18n.New(i18n.English).Err(err); !strings.Contains(got, "missing or is not a directory") {
			t.Errorf("message = %q", got)
		}
	})

	t.Run("a directory the agent could never be pointed at", func(t *testing.T) {
		t.Parallel()
		home := filepath.Join(t.TempDir(), "my-sessions")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		target, _ := build("opencode", map[string]string{"opencode": home})
		if err := ready(target); !errors.Is(err, agent.ErrUnusableHome) {
			t.Errorf("ready() = %v, want ErrUnusableHome", err)
		}
	})

	t.Run("a directory that is fine", func(t *testing.T) {
		t.Parallel()
		home := filepath.Join(t.TempDir(), "opencode")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		target, _ := build("opencode", map[string]string{"opencode": home})
		if err := ready(target); err != nil {
			t.Errorf("ready() = %v", err)
		}
	})
}

func TestRunPrintsHelpAndVersion(t *testing.T) {
	t.Parallel()

	print := i18n.New(i18n.English)
	var out bytes.Buffer
	if err := run(t.Context(), []string{"--help"}, print, &out, &out); err != nil {
		t.Fatalf("run(--help) = %v", err)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("run(--help) printed:\n%s", out.String())
	}

	out.Reset()
	if err := run(t.Context(), []string{"--version"}, print, &out, &out); err != nil {
		t.Fatalf("run(--version) = %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != buildinfo.Name+" "+buildinfo.Version {
		t.Errorf("run(--version) printed %q", got)
	}
}
