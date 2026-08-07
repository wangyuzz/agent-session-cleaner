// Command agent-session-cleaner browses, resumes and cleans up Codex,
// Claude Code, OpenCode, and Pi session history.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/buildinfo"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/tui"
	"github.com/haowang02/agent-session-cleaner/internal/tui/picker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	print := i18n.New(i18n.Detect(os.Getenv))
	if err := run(ctx, os.Args[1:], print, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, print.T(i18n.CLIError, i18n.Args{
			"prog":    buildinfo.Name,
			"message": print.Err(err),
		}))
		os.Exit(2)
	}
}

func run(ctx context.Context, args []string, print *i18n.Printer, out, _ io.Writer) error {
	opts, err := parse(args)
	if err != nil {
		return err
	}
	switch {
	case opts.help:
		usage(out, print)
		return nil
	case opts.version:
		fmt.Fprintf(out, "%s %s\n", buildinfo.Name, buildinfo.Version)
		return nil
	}

	chosen := opts.agent
	if chosen == "" {
		if chosen, err = choose(ctx, opts.homes, print); err != nil || chosen == "" {
			return err
		}
	}

	target, ok := build(chosen, opts.homes)
	if !ok {
		return i18n.Errorf(i18n.CLIAgentChoices, i18n.Args{
			"value": chosen, "choices": strings.Join(ids(), ", "),
		})
	}
	if err := ready(target); err != nil {
		return err
	}

	_, err = tea.NewProgram(tui.New(ctx, target, print), tea.WithContext(ctx)).Run()
	return err
}

// choose opens the chooser and reports which agent the user settled on, or an
// empty string if they quit.
func choose(ctx context.Context, homes map[string]string, print *i18n.Printer) (string, error) {
	chooser := picker.New(ctx, buildAll(homes), print)
	if _, err := tea.NewProgram(chooser, tea.WithContext(ctx)).Run(); err != nil {
		return "", err
	}
	return chooser.Choice, nil
}

// ready refuses a session tree that cannot be worked with, before anything is
// listed. Saying so once a deletion is already under way would be too late.
func ready(target agent.Agent) error {
	meta := target.Meta()
	info, err := os.Stat(meta.Home)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return i18n.Wrap(err, i18n.UnexpectedError, i18n.Args{"error": err})
		}
		return i18n.Errorf(i18n.CLIHomeUnavailable, i18n.Args{
			"agent": meta.Label, "path": meta.Home,
		})
	}
	if !info.IsDir() {
		return i18n.Errorf(i18n.CLIHomeUnavailable, i18n.Args{
			"agent": meta.Label, "path": meta.Home,
		})
	}
	return target.Preflight()
}
