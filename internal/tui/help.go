package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
)

// helpEntry is one line of the help screen. A gated entry appears only where
// its action does, so the help can never claim a key this agent, this
// installation or this mode does not have.
type helpEntry struct {
	actions []action
	keys    string
	what    i18n.Key
	gated   action
}

type helpSection struct {
	name    i18n.Key
	entries []helpEntry
}

var helpSections = []helpSection{
	{name: i18n.HelpBrowse, entries: []helpEntry{
		{actions: []action{upAction, downAction}, what: i18n.HelpSelect},
		{actions: []action{topAction, bottomAction}, what: i18n.HelpTopBottom},
		{actions: []action{focusAction}, what: i18n.HelpFocus},
	}},
	{name: i18n.HelpSearch, entries: []helpEntry{
		{actions: []action{searchAction}, what: i18n.HelpSearchFields},
		{actions: []action{searchBackAction}, what: i18n.HelpSearchReverse},
		{actions: []action{nextMatchAction, prevMatchAction}, what: i18n.HelpSearchMatch},
		{actions: []action{matchCaseAction}, what: i18n.HelpMatchCase},
	}},
	{name: i18n.HelpManage, entries: []helpEntry{
		{actions: []action{pickAction}, what: i18n.HelpSelectToggle, gated: pickAction},
		{actions: []action{copySessionIDAction}, what: i18n.HelpCopySessionID, gated: copySessionIDAction},
		{actions: []action{copyWorkingDirectoryAction}, what: i18n.HelpCopyCwd, gated: copyWorkingDirectoryAction},
		{actions: []action{exportAction}, what: i18n.HelpExport, gated: exportAction},
		{actions: []action{deleteAction}, what: i18n.HelpDelete, gated: deleteAction},
		{actions: []action{archiveAction}, what: i18n.HelpArchive, gated: archiveAction},
		{actions: []action{unarchiveAction}, what: i18n.HelpUnarchive, gated: unarchiveAction},
		{actions: []action{sweepArchivedAction}, what: i18n.HelpDeleteArchived, gated: sweepArchivedAction},
		{actions: []action{sweepOrphansAction}, what: i18n.HelpDeleteOrphans, gated: sweepOrphansAction},
		{actions: []action{sweepEmptyAction}, what: i18n.HelpDeleteEmpty, gated: sweepEmptyAction},
		{actions: []action{dangerAction}, what: i18n.HelpDanger, gated: dangerAction},
	}},
	{name: i18n.HelpOther, entries: []helpEntry{
		{actions: []action{escapeAction}, what: i18n.HelpEscape},
		{actions: []action{reloadAction}, what: i18n.HelpReload},
		{actions: []action{helpAction}, what: i18n.HelpOpen},
		{actions: []action{quitAction}, what: i18n.HelpQuit},
	}},
}

func (m *Model) helpText() []helpSection {
	var shown []helpSection
	for _, section := range helpSections {
		var entries []helpEntry
		for _, entry := range section.entries {
			if entry.gated == noAction || m.allows(entry.gated) {
				entry.keys = m.helpKeys(entry.actions...)
				entries = append(entries, entry)
			}
		}
		if len(entries) > 0 {
			shown = append(shown, helpSection{name: section.name, entries: entries})
		}
	}
	return shown
}

func (m *Model) helpKeys(actions ...action) string {
	groups := make([]string, 0, len(actions))
	for _, action := range actions {
		binding, ok := m.binding(action)
		if !ok {
			continue
		}
		labels := make([]string, len(binding.keys))
		for i, key := range binding.keys {
			labels[i] = keyLabel(key)
		}
		groups = append(groups, strings.Join(labels, ", "))
	}
	return strings.Join(groups, " / ")
}

func (m *Model) helpKey(msg tea.KeyPressMsg) tea.Cmd {
	name := keyName(msg)
	if name == "esc" || name == "enter" {
		m.help, m.helpTop = false, 0
		return nil
	}
	switch m.resolve(name) {
	case helpAction, quitAction:
		m.help, m.helpTop = false, 0
	case downAction:
		m.scrollHelp(1)
	case upAction:
		m.scrollHelp(-1)
	case topAction:
		m.helpTop = 0
	case bottomAction:
		m.helpTop = max(0, len(m.helpLines(m.helpInnerWidth()))-m.helpRoom())
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
