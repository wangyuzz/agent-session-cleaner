package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// action is something a key does. Naming actions rather than keys lets the
// footer, the help screen and the key handler agree on what exists without
// repeating the list three times.
type action uint8

const (
	noAction action = iota

	archiveAction
	unarchiveAction
	deleteAction
	copySessionIDAction
	copyWorkingDirectoryAction
	pickAction
	sweepArchivedAction
	sweepEmptyAction
	sweepOrphansAction
	dangerAction
	searchAction
	reloadAction
	helpAction
	quitAction

	// Keys that need no footer entry.
	searchBackAction
	nextMatchAction
	prevMatchAction
	escapeAction
	focusAction
	upAction
	downAction
	topAction
	bottomAction
)

type binding struct {
	keys    []string
	display string
	action  action
	label   i18n.Key
	// picked is what the footer calls this action while a selection stands,
	// because the keys do not change but what they act on does.
	picked i18n.Key
}

var bindings = []binding{
	{keys: []string{"a"}, action: archiveAction, label: i18n.BindingArchive, picked: i18n.BindingArchiveSelected},
	{keys: []string{"u"}, action: unarchiveAction, label: i18n.BindingUnarchive, picked: i18n.BindingUnarchiveSelected},
	{keys: []string{"d"}, action: deleteAction, label: i18n.BindingDelete, picked: i18n.BindingDeleteSelected},
	{keys: []string{"c"}, action: copySessionIDAction, label: i18n.BindingCopySessionID},
	{keys: []string{"y"}, action: copyWorkingDirectoryAction, label: i18n.BindingCopyCwd},
	// Shown as the open-box glyph: "space" spelled out is wider than the label
	// it introduces, and reads as a word rather than a key.
	{keys: []string{"space"}, display: "␣", action: pickAction, label: i18n.BindingSelect, picked: i18n.BindingSelectMore},
	{keys: []string{"D"}, action: sweepArchivedAction, label: i18n.BindingDeleteArchived},
	{keys: []string{"E"}, action: sweepEmptyAction, label: i18n.BindingDeleteEmpty},
	{keys: []string{"O"}, action: sweepOrphansAction, label: i18n.BindingDeleteOrphans},
	{keys: []string{"!"}, action: dangerAction, label: i18n.BindingDanger},
	{keys: []string{"/"}, action: searchAction, label: i18n.BindingSearch},
	{keys: []string{"r"}, action: reloadAction, label: i18n.BindingReload},
	{keys: []string{"h"}, action: helpAction, label: i18n.BindingHelp},
	{keys: []string{"q", "ctrl+c"}, action: quitAction, label: i18n.BindingQuit},

	{keys: []string{"?"}, action: searchBackAction},
	{keys: []string{"n"}, action: nextMatchAction},
	{keys: []string{"N"}, action: prevMatchAction},
	{keys: []string{"esc"}, action: escapeAction},
	{keys: []string{"tab"}, action: focusAction, label: i18n.BindingFocus},
	{keys: []string{"k", "up"}, action: upAction},
	{keys: []string{"j", "down"}, action: downAction},
	{keys: []string{"g"}, action: topAction},
	{keys: []string{"G"}, action: bottomAction},
}

// The footer is two rows, split by what the keys are for rather than by how
// they happen to fit: above, everything that acts on the session under the
// cursor or on what has been picked out; below, getting around and the way out.
// One row would clip its trailing keys in any ordinary terminal, and the keys
// that clip first are the ones people go looking for.
var (
	footerTop = []action{
		archiveAction, unarchiveAction, deleteAction, copySessionIDAction, copyWorkingDirectoryAction,
		sweepArchivedAction, sweepEmptyAction, sweepOrphansAction, dangerAction,
	}
	footerBottom = []action{
		pickAction, searchAction, reloadAction, helpAction, quitAction,
	}
)

// writes lists everything that changes a session, which is hidden when the
// agent's own command line is not installed.
var writes = map[action]bool{
	archiveAction:       true,
	unarchiveAction:     true,
	deleteAction:        true,
	sweepArchivedAction: true,
	sweepEmptyAction:    true,
	sweepOrphansAction:  true,
	pickAction:          true,
	dangerAction:        true,
}

// needsArchiver lists what only means anything where sessions can be set
// aside instead of destroyed.
var needsArchiver = map[action]bool{
	archiveAction:       true,
	unarchiveAction:     true,
	sweepArchivedAction: true,
}

// hiddenWhilePicking lists what a standing selection takes off the table.
// Either the action works on the row under the cursor, which is no longer what
// the keys are about, or it sweeps the whole list, which is a different set
// from the one on screen.
var hiddenWhilePicking = map[action]bool{
	copySessionIDAction:        true,
	copyWorkingDirectoryAction: true,
	sweepArchivedAction:        true,
	sweepEmptyAction:           true,
	sweepOrphansAction:         true,
	dangerAction:               true,
}

// allows reports whether an action exists right now, for this agent, this
// installation and this mode. The footer and the help screen both ask, so
// neither can drift from what the keys actually do.
func (m *Model) allows(a action) bool {
	return m.allowsWhile(a, !m.picked.Empty())
}

// allowsWhile answers the same question for a mode the model is not in, which
// is how the footer sizes itself for the worst case rather than the moment.
func (m *Model) allowsWhile(a action, picking bool) bool {
	switch {
	case needsArchiver[a] && m.archiver == nil:
		return false
	case a == sweepEmptyAction && m.meta.EmptyLabel == 0:
		return false
	case a == sweepOrphansAction && m.meta.OrphanLabel == 0:
		return false
	case writes[a] && !m.agent.Writable():
		return false
	case picking && hiddenWhilePicking[a]:
		return false
	}
	return true
}

// useful reports whether an action would change anything as things stand:
// whether there is an archived session to sweep, whether the row under the
// cursor can still be archived, and so on.
//
// It is deliberately narrower than allows, and only the footer asks. The key
// itself still works and still says why it did nothing, and the help screen
// still lists it — a reference that changes as the cursor moves is not a
// reference. What the footer offers is what would happen now.
//
// Each case mirrors the condition its action refuses on, so the two cannot
// drift into a key the footer offers and the action then declines.
func (m *Model) useful(a action) bool {
	switch a {
	case archiveAction:
		return m.wouldTouch(func(s session.Session) bool { return !s.Archived })
	case unarchiveAction:
		return m.wouldTouch(func(s session.Session) bool { return s.Archived })
	case sweepArchivedAction:
		return m.countIf(func(s session.Session) bool { return s.Archived }) > 0
	case sweepEmptyAction:
		return m.countIf(func(s session.Session) bool { return s.Noise }) > 0
	case sweepOrphansAction:
		return len(m.forest.Orphans()) > 0
	case copyWorkingDirectoryAction:
		current, ok := m.current()
		return ok && current.Cwd != ""
	case deleteAction, copySessionIDAction, pickAction, searchAction:
		// Nothing to delete, copy, pick out or search through.
		return len(m.rows) > 0
	}
	return true
}

// wouldTouch reports whether archiving or unarchiving has anything to work on.
//
// A selection is filtered down to what the action applies to, so one session
// in the right state is enough; a single session under the cursor is refused
// outright when it is already in that state.
func (m *Model) wouldTouch(applies func(session.Session) bool) bool {
	if !m.picked.Empty() {
		for _, s := range m.picked.Scope(m.forest) {
			if applies(s) {
				return true
			}
		}
		return false
	}
	current, ok := m.current()
	return ok && applies(current)
}

func (m *Model) describe(b binding, picking bool) string {
	if picking && b.picked != 0 {
		return m.print.T(b.picked)
	}
	return m.print.T(b.label)
}

func (m *Model) binding(a action) (binding, bool) {
	for _, b := range bindings {
		if b.action == a {
			return b, true
		}
	}
	return binding{}, false
}

func resolve(name string) action {
	for _, b := range bindings {
		for _, key := range b.keys {
			if key == name {
				return b.action
			}
		}
	}
	return noAction
}

// keyName reduces a key press to the name the binding table uses.
//
// Text is matched rather than the key's own string form because that is what
// distinguishes D from d, and the sweep keys are deliberately the shifted ones.
func keyName(msg tea.KeyPressMsg) string {
	key := tea.Key(msg)
	switch key.Code {
	case tea.KeyEnter:
		return "enter"
	case tea.KeyEscape:
		return "esc"
	case tea.KeyTab:
		return "tab"
	case tea.KeySpace:
		return "space"
	case tea.KeyUp:
		return "up"
	case tea.KeyDown:
		return "down"
	}
	// Anything held down with ctrl or alt is not one of ours.
	if key.Mod&^tea.ModShift != 0 {
		return msg.String()
	}
	if key.Text != "" {
		return key.Text
	}
	return msg.String()
}
