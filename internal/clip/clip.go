// Package clip puts text on the system clipboard.
//
// A terminal can be asked to do this over OSC 52, but that escape sequence is
// ignored by a fair number of terminals — macOS Terminal among them — so a
// native helper is tried first and OSC 52 is left to the caller as a fallback.
package clip

import (
	"context"
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
// it. False means the caller should fall back to OSC 52. The caller's context
// cancels helpers promptly when the interface exits.
func Copy(ctx context.Context, text string) bool {
	return copyWith(ctx, helpers, text)
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
