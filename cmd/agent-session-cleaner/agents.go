package main

import (
	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/agent/claude"
	"github.com/haowang02/agent-session-cleaner/internal/agent/codex"
	"github.com/haowang02/agent-session-cleaner/internal/agent/grok"
	"github.com/haowang02/agent-session-cleaner/internal/agent/opencode"
	"github.com/haowang02/agent-session-cleaner/internal/agent/pi"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
)

// known is every agent this program manages, in the order the chooser lists
// them. Adding one here is the only registration there is.
var known = []struct {
	id string
	// homeHelp describes this agent's --<id>-home flag.
	homeHelp i18n.Key
	build    func(options) agent.Agent
}{
	{
		id:       codex.ID,
		homeHelp: i18n.CLICodexHome,
		build: func(opts options) agent.Agent {
			var configured []codex.Option
			if opts.codexBin != "" {
				configured = append(configured, codex.WithBinary(opts.codexBin))
			}
			if opts.codexConcurrency > 0 {
				configured = append(configured, codex.WithBulkConcurrency(opts.codexConcurrency))
			}
			return codex.New(opts.homes[codex.ID], configured...)
		},
	},
	{
		id:       claude.ID,
		homeHelp: i18n.CLIClaudeHome,
		build:    func(opts options) agent.Agent { return claude.New(opts.homes[claude.ID]) },
	},
	{
		id:       opencode.ID,
		homeHelp: i18n.CLIOpenCodeHome,
		build:    func(opts options) agent.Agent { return opencode.New(opts.homes[opencode.ID]) },
	},
	{
		id:       pi.ID,
		homeHelp: i18n.CLIPiHome,
		build:    func(opts options) agent.Agent { return pi.New(opts.homes[pi.ID]) },
	},
	{
		id:       grok.ID,
		homeHelp: i18n.CLIGrokHome,
		build:    func(opts options) agent.Agent { return grok.New(opts.homes[grok.ID]) },
	},
}

// ids lists the agent names accepted on the command line.
func ids() []string {
	names := make([]string, len(known))
	for i, entry := range known {
		names[i] = entry.id
	}
	return names
}

// buildAll constructs every agent, applying whichever home overrides were given.
func buildAll(opts options) []agent.Agent {
	agents := make([]agent.Agent, len(known))
	for i, entry := range known {
		agents[i] = entry.build(opts)
	}
	return agents
}

// build constructs one agent by id.
func build(id string, opts options) (agent.Agent, bool) {
	for _, entry := range known {
		if entry.id == id {
			return entry.build(opts), true
		}
	}
	return nil, false
}
