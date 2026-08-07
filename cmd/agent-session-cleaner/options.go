package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/buildinfo"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/tui/text"
)

// options is the command line, once it has been read.
//
// The parser is written out rather than taken from the standard library
// because the standard one stops at the first positional argument, which would
// make `agent-session-cleaner codex --codex-home DIR` silently ignore the
// directory. Every message here is ours to translate, too.
type options struct {
	agent   string
	homes   map[string]string
	help    bool
	version bool
}

func parse(args []string) (options, error) {
	opts := options{homes: map[string]string{}}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if opts.agent != "" {
				return opts, i18n.Errorf(i18n.CLIUnexpectedArgument, i18n.Args{"value": arg})
			}
			opts.agent = arg
			continue
		}

		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		switch name {
		case "h", "help":
			if hasValue {
				return opts, i18n.Errorf(i18n.CLIUnknownOption, i18n.Args{"name": arg})
			}
			opts.help = true
			continue
		case "version":
			if hasValue {
				return opts, i18n.Errorf(i18n.CLIUnknownOption, i18n.Args{"name": arg})
			}
			opts.version = true
			continue
		}

		home, ok := strings.CutSuffix(name, "-home")
		if !ok || !isAgent(home) {
			return opts, i18n.Errorf(i18n.CLIUnknownOption, i18n.Args{"name": arg})
		}
		if !hasValue {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return opts, i18n.Errorf(i18n.CLIOptionNeedsValue, i18n.Args{"name": arg})
			}
			i++
			value = args[i]
		}
		opts.homes[home] = agent.ExpandHome(value)
	}

	if opts.agent != "" && !isAgent(opts.agent) {
		return opts, i18n.Errorf(i18n.CLIAgentChoices, i18n.Args{
			"value":   opts.agent,
			"choices": strings.Join(ids(), ", "),
		})
	}
	return opts, nil
}

func isAgent(name string) bool {
	for _, id := range ids() {
		if id == name {
			return true
		}
	}
	return false
}

// usage prints what this program takes, in the user's language.
func usage(out io.Writer, print *i18n.Printer) {
	type entry struct{ flag, what string }
	entries := []entry{{"-h, --help", print.T(i18n.CLIHelp)}}
	for _, known := range known {
		entries = append(entries, entry{
			flag: fmt.Sprintf("    --%s-home %s", known.id, print.T(i18n.CLIDirectory)),
			what: print.T(known.homeHelp),
		})
	}
	entries = append(entries, entry{"    --version", print.T(i18n.CLIVersion)})

	// text.Pad clips its input, so include the agent list in the shared column width.
	agents := strings.Join(ids(), "|")
	width := text.Width(agents)
	for _, e := range entries {
		width = max(width, text.Width(e.flag))
	}

	fmt.Fprintln(out, print.T(i18n.CLIDescription))
	fmt.Fprintln(out)
	fmt.Fprintln(out, print.T(i18n.CLIUsage))
	fmt.Fprintf(out, "  %s [%s] [options]\n\n", buildinfo.Name, agents)
	fmt.Fprintln(out, print.T(i18n.CLIArguments))
	fmt.Fprintf(out, "  %s  %s\n\n", text.Pad(agents, width), print.T(i18n.CLIAgent))
	fmt.Fprintln(out, print.T(i18n.CLIOptions))
	for _, e := range entries {
		fmt.Fprintf(out, "  %s  %s\n", text.Pad(e.flag, width), e.what)
	}
}
