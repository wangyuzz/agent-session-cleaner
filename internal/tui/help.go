package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
)

// helpEntry is one line of the help screen. A gated entry appears only where
// its action does, so the help can never claim a key this agent, this
// installation or this mode does not have.
type helpEntry struct {
	keys  string
	what  i18n.Key
	gated action
}

type helpSection struct {
	name    i18n.Key
	entries []helpEntry
}

var helpSections = []helpSection{
	{name: i18n.HelpBrowse, entries: []helpEntry{
		{keys: "↑ ↓ / j k", what: i18n.HelpSelect},
		{keys: "g / G", what: i18n.HelpTopBottom},
		{keys: "Tab", what: i18n.HelpFocus},
	}},
	{name: i18n.HelpSearch, entries: []helpEntry{
		{keys: "/", what: i18n.HelpSearchFields},
		{keys: "?", what: i18n.HelpSearchReverse},
		{keys: "n / N", what: i18n.HelpSearchMatch},
	}},
	{name: i18n.HelpManage, entries: []helpEntry{
		{keys: "␣", what: i18n.HelpSelectToggle, gated: pickAction},
		{keys: "c", what: i18n.HelpCopySessionID, gated: copySessionIDAction},
		{keys: "y", what: i18n.HelpCopyCwd, gated: copyWorkingDirectoryAction},
		{keys: "d", what: i18n.HelpDelete, gated: deleteAction},
		{keys: "a", what: i18n.HelpArchive, gated: archiveAction},
		{keys: "u", what: i18n.HelpUnarchive, gated: unarchiveAction},
		{keys: "D", what: i18n.HelpDeleteArchived, gated: sweepArchivedAction},
		{keys: "O", what: i18n.HelpDeleteOrphans, gated: sweepOrphansAction},
		{keys: "E", what: i18n.HelpDeleteEmpty, gated: sweepEmptyAction},
		{keys: "!", what: i18n.HelpDanger, gated: dangerAction},
	}},
	{name: i18n.HelpOther, entries: []helpEntry{
		{keys: "Esc", what: i18n.HelpEscape},
		{keys: "r", what: i18n.HelpReload},
		{keys: "h", what: i18n.HelpOpen},
		{keys: "q", what: i18n.HelpQuit},
	}},
}

func (m *Model) helpText() []helpSection {
	var shown []helpSection
	for _, section := range helpSections {
		var entries []helpEntry
		for _, entry := range section.entries {
			if entry.gated == noAction || m.allows(entry.gated) {
				entries = append(entries, entry)
			}
		}
		if len(entries) > 0 {
			shown = append(shown, helpSection{name: section.name, entries: entries})
		}
	}
	return shown
}

func (m *Model) helpKey(msg tea.KeyPressMsg) tea.Cmd {
	switch keyName(msg) {
	case "esc", "q", "h", "enter":
		m.help, m.helpTop = false, 0
	case "j", "down":
		m.scrollHelp(1)
	case "k", "up":
		m.scrollHelp(-1)
	case "g":
		m.helpTop = 0
	}
	return nil
}

func (m *Model) helpWheel(msg tea.MouseWheelMsg) {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		m.scrollHelp(-3)
	case tea.MouseWheelDown:
		m.scrollHelp(3)
	}
}

func (m *Model) scrollHelp(delta int) {
	lines := m.helpLines(m.helpInnerWidth())
	last := max(0, len(lines)-m.helpRoom())
	m.helpTop = max(0, min(m.helpTop, last)+delta)
	m.helpTop = min(m.helpTop, last)
}
