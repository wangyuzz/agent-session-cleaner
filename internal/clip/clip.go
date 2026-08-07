// Package clip puts text on the system clipboard.
//
// Over SSH, OSC 52 lets the client terminal handle the clipboard. Elsewhere a
// native helper is tried first because some terminals ignore that sequence.
package clip

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

const timeout = 5 * time.Second

type helper struct {
	name string
	args []string
}

// The first helper that exists wins.
var helpers = []helper{
	{name: "pbcopy"},  // macOS
	{name: "wl-copy"}, // Wayland
	{name: "xclip", args: []string{"-selection", "clipboard"}},
	{name: "xsel", args: []string{"--clipboard", "--input"}},
}

// Copy puts text on the clipboard, reporting whether a native helper managed
// it. SSH sessions always report false so the caller uses OSC 52 on the client.
// Otherwise, false means no helper worked. The caller's context cancels helpers
// promptly when the interface exits.
func Copy(ctx context.Context, text string) bool {
	return copyFor(ctx, helpers, text, os.Getenv)
}

func copyFor(ctx context.Context, candidates []helper, text string, getenv func(string) string) bool {
	if sshSession(getenv) {
		return false
	}
	return copyWith(ctx, candidates, text)
}

func sshSession(getenv func(string) string) bool {
	return getenv("SSH_CONNECTION") != "" || getenv("SSH_CLIENT") != "" || getenv("SSH_TTY") != ""
}

func copyWith(ctx context.Context, candidates []helper, text string) bool {
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return false
		}
		path, err := exec.LookPath(candidate.name)
		if err != nil {
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, timeout)
		cmd := exec.CommandContext(attempt, path, candidate.args...)
		cmd.Stdin = strings.NewReader(text)
		err = cmd.Run()
		cancel()
		if err == nil {
			return true
		}
	}
	return false
}
