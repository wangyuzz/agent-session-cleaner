// Package execx runs an agent's own command line for the changes it owns.
//
// Where an agent ships a command that archives or deletes sessions, that
// command is the only thing allowed to do it: it knows what else has to move,
// and it reports failure through its exit status instead of leaving half a
// session behind.
package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
)

// DefaultTimeout bounds one invocation. An agent that has not answered by then
// is not going to.
const DefaultTimeout = 60 * time.Second

// ErrNotInstalled reports that the agent's command line is not on PATH.
var ErrNotInstalled = errors.New("command not installed")

// Command is one agent invocation.
type Command struct {
	// Name is the executable to look up on PATH.
	Name string
	Args []string
	// Env is merged over the current environment. Agents use it to pin the
	// command to the same session tree the listing was read from.
	Env map[string]string
	// Label names the agent in anything the user is shown.
	Label string
	// Timeout defaults to [DefaultTimeout].
	Timeout time.Duration
}

// Runner runs one agent command to completion. It is a function so that tests
// can stand in for an agent that is not installed, or one that refuses.
type Runner func(ctx context.Context, cmd Command) error

// Run is the real runner.
func Run(ctx context.Context, cmd Command) error {
	agent := i18n.Args{"agent": cmd.Label}

	timeout := cmd.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	process, err := newProcess(ctx, cmd.Name, cmd.Args)
	if err != nil {
		return i18n.Wrap(ErrNotInstalled, i18n.MissingCLIModify, agent)
	}
	process.Env = environ(process.Env, cmd.Env)
	var stdout, stderr bytes.Buffer
	process.Stdout, process.Stderr = &stdout, &stderr

	switch err := process.Run(); {
	case err == nil:
		// The command's own wording ("Archived session <uuid>.") is written for
		// scripts; the caller phrases success for people.
		return nil
	case parent.Err() != nil:
		// The program is shutting down, which is not the agent's fault.
		return parent.Err()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return i18n.Wrap(ctx.Err(), i18n.AgentCLITimeout, i18n.Args{
			"agent":   cmd.Label,
			"seconds": fmt.Sprintf("%.0f", timeout.Seconds()),
		})
	default:
		var exited *exec.ExitError
		if !errors.As(err, &exited) {
			return i18n.Wrap(err, i18n.AgentCLIStartFailed, i18n.Args{
				"agent": cmd.Label, "error": err,
			})
		}
		if complaint := Complaint(stderr.Bytes()); complaint != "" {
			return i18n.Raw(complaint)
		}
		if complaint := Complaint(stdout.Bytes()); complaint != "" {
			return i18n.Raw(complaint)
		}
		return i18n.Wrap(err, i18n.AgentCLIUnknownFailure, agent)
	}
}

// Available reports whether a command can be found on PATH.
func Available(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Complaint picks one useful, unstyled error line out of command output.
//
// Most commands put the useful explanation last. OpenCode is an exception:
// database failures start with "Error: ..." and end with a bare "params:"
// line, which is neither actionable nor fit for a status bar.
func Complaint(raw []byte) string {
	text := ansi.Strip(string(raw))
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.ToLower(lines[i]), "error:") {
			return lines[i]
		}
	}
	return lines[len(lines)-1]
}

func environ(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	if base == nil {
		base = os.Environ()
	}
	for name, value := range overrides {
		base = append(base, name+"="+value)
	}
	return base
}
