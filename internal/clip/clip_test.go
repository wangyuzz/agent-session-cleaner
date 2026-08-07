//go:build !windows

package clip

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// script writes an executable that copies stdin to sink.
func script(t *testing.T, sink string, exit int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "helper")
	body := "#!/bin/sh\ncat > " + sink + "\nexit " + itoa(exit) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return string(rune('0' + n))
}

func TestCopyUsesTheFirstHelperThatWorks(t *testing.T) {
	t.Parallel()

	sink := filepath.Join(t.TempDir(), "clipboard")
	candidates := []helper{
		{name: "definitely-not-installed-anywhere"},
		{name: script(t, sink, 0)},
	}

	if !copyWith(context.Background(), candidates, "session-abc") {
		t.Fatal("copyWith reported failure")
	}
	got, err := os.ReadFile(sink)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "session-abc" {
		t.Errorf("helper received %q", got)
	}
}

func TestCopyMovesOnFromAHelperThatRefuses(t *testing.T) {
	t.Parallel()

	broken := filepath.Join(t.TempDir(), "broken")
	working := filepath.Join(t.TempDir(), "working")
	candidates := []helper{
		{name: script(t, broken, 1)},
		{name: script(t, working, 0)},
	}

	if !copyWith(context.Background(), candidates, "text") {
		t.Fatal("copyWith gave up after the first refusal")
	}
	if _, err := os.Stat(working); err != nil {
		t.Errorf("the second helper never ran: %v", err)
	}
}

func TestCopyReportsWhenNothingIsAvailable(t *testing.T) {
	t.Parallel()

	if copyWith(context.Background(), []helper{{name: "definitely-not-installed-anywhere"}}, "text") {
		t.Error("copyWith claimed success with no helper installed")
	}
}

func TestSSHSessionUsesTerminalClipboard(t *testing.T) {
	t.Parallel()

	for _, variable := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		variable := variable
		t.Run(variable, func(t *testing.T) {
			t.Parallel()
			getenv := func(name string) string {
				if name == variable {
					return "set"
				}
				return ""
			}
			sink := filepath.Join(t.TempDir(), "clipboard")
			if copyFor(context.Background(), []helper{{name: script(t, sink, 0)}}, "text", getenv) {
				t.Error("native clipboard helper ran over SSH")
			}
			if _, err := os.Stat(sink); !os.IsNotExist(err) {
				t.Errorf("native clipboard helper wrote over SSH: %v", err)
			}
		})
	}
}
