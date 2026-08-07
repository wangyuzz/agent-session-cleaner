package main

import (
	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/agent/claude"
	"github.com/haowang02/agent-session-cleaner/internal/agent/codex"
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
	build    func(home string) agent.Agent
}{
	{
		id:       codex.ID,
		homeHelp: i18n.CLICodexHome,
		build:    func(home string) agent.Agent { return codex.New(home) },
	},
	{
		id:       claude.ID,
		homeHelp: i18n.CLIClaudeHome,
		build:    func(home string) agent.Agent { return claude.New(home) },
	},
	{
		id:       opencode.ID,
		homeHelp: i18n.CLIOpenCodeHome,
		build:    func(home string) agent.Agent { return opencode.New(home) },
	},
	{
		id:       pi.ID,
		homeHelp: i18n.CLIPiHome,
		build:    func(home string) agent.Agent { return pi.New(home) },
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
func buildAll(homes map[string]string) []agent.Agent {
	agents := make([]agent.Agent, len(known))
	for i, entry := range known {
		agents[i] = entry.build(homes[entry.id])
	}
	return agents
}

// build constructs one agent by id.
func build(id string, homes map[string]string) (agent.Agent, bool) {
	for _, entry := range known {
		if entry.id == id {
			return entry.build(homes[id]), true
		}
	}
	return nil, false
}
