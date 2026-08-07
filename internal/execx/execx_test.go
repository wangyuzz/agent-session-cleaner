//go:build !windows

package execx_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/haowang02/agent-session-cleaner/internal/execx"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
)

func shell(script string) execx.Command {
	return execx.Command{Name: "sh", Args: []string{"-c", script}, Label: "Codex"}
}

func TestRunSucceeds(t *testing.T) {
	t.Parallel()

	if err := execx.Run(t.Context(), shell("exit 0")); err != nil {
		t.Errorf("Run() = %v, want success", err)
	}
}

func TestRunReportsAMissingCommand(t *testing.T) {
	t.Parallel()

	err := execx.Run(t.Context(), execx.Command{
		Name: "definitely-not-installed-anywhere", Label: "Codex",
	})
	if !errors.Is(err, execx.ErrNotInstalled) {
		t.Fatalf("Run() = %v, want ErrNotInstalled", err)
	}
	if got := i18n.New(i18n.English).Err(err); !strings.Contains(got, "Codex CLI is unavailable") {
		t.Errorf("message = %q, want it to name the agent", got)
	}
}

// The agent's own complaint is what a person can act on, so it reaches the
// status line in its own words.
func TestRunSurfacesTheAgentsComplaint(t *testing.T) {
	t.Parallel()

	err := execx.Run(t.Context(), shell(`echo "Error: session not found" >&2; exit 1`))
	if got := i18n.New(i18n.English).Err(err); got != "Error: session not found" {
		t.Errorf("message = %q, want the agent's own line", got)
	}
}

func TestRunFallsBackToStdout(t *testing.T) {
	t.Parallel()

	err := execx.Run(t.Context(), shell(`echo "nothing to delete"; exit 2`))
	if got := i18n.New(i18n.English).Err(err); got != "nothing to delete" {
		t.Errorf("message = %q, want the line printed to stdout", got)
	}
}

func TestRunReportsSilentFailure(t *testing.T) {
	t.Parallel()

	err := execx.Run(t.Context(), shell("exit 7"))
	got := i18n.New(i18n.English).Err(err)
	if !strings.Contains(got, "did not report why") {
		t.Errorf("message = %q, want the unexplained-failure wording", got)
	}
}

func TestRunTimesOut(t *testing.T) {
	t.Parallel()

	cmd := shell("sleep 30")
	cmd.Timeout = 50 * time.Millisecond
	err := execx.Run(t.Context(), cmd)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() = %v, want a deadline", err)
	}
	if got := i18n.New(i18n.English).Err(err); !strings.Contains(got, "within 0 seconds") {
		t.Errorf("message = %q, want the timeout wording", got)
	}
}

// Shutting down is not the agent misbehaving, and must not be reported as it.
func TestRunPassesCancellationThrough(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := execx.Run(ctx, shell("sleep 30"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() = %v, want context.Canceled", err)
	}
}

func TestRunPassesEnvironment(t *testing.T) {
	t.Parallel()

	cmd := shell(`test "$CODEX_HOME" = /tmp/pinned`)
	cmd.Env = map[string]string{"CODEX_HOME": "/tmp/pinned"}
	if err := execx.Run(t.Context(), cmd); err != nil {
		t.Errorf("Run() = %v, want the pinned home to reach the command", err)
	}
}

func TestComplaint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"nothing", "", ""},
		{"one line", "boom\n", "boom"},
		{"the last line usually explains", "warming up\nreally boom\n", "really boom"},
		{
			// OpenCode ends database failures with a bare "params:" line,
			// which is neither actionable nor fit for a status bar.
			name: "an error line beats a trailing scrap",
			raw:  "Error: database is locked\nparams:\n",
			want: "Error: database is locked",
		},
		{"styling is stripped", "\x1b[31mError: nope\x1b[0m\n", "Error: nope"},
		{"blank lines are ignored", "\n\n  \nlast\n\n", "last"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := execx.Complaint([]byte(test.raw)); got != test.want {
				t.Errorf("Complaint(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestAvailable(t *testing.T) {
	t.Parallel()

	if !execx.Available("sh") {
		t.Error("Available(sh) = false")
	}
	if execx.Available("definitely-not-installed-anywhere") {
		t.Error("Available reported a command that does not exist")
	}
}
