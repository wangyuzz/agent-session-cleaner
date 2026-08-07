package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/clip"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// bulkPreviewLimit is how many titles a confirmation lists before collapsing
// the rest into a count.
const bulkPreviewLimit = 6

type copiedClipboardMsg struct {
	text   string
	status i18n.Key
	args   i18n.Args
	native bool
	seq    uint64
}

func (m *Model) archive() tea.Cmd {
	if !m.picked.Empty() {
		return m.batch(opArchive)
	}
	current, ok := m.require()
	if !ok {
		return nil
	}
	if current.Archived {
		m.warn(i18n.AlreadyArchived)
		return nil
	}
	// Archiving what is already archived is an error the agent's own command
	// line would rightly complain about.
	return m.cascade(opArchive, current, func(s session.Session) bool { return !s.Archived })
}

func (m *Model) unarchive() tea.Cmd {
	if !m.picked.Empty() {
		return m.batch(opUnarchive)
	}
	current, ok := m.require()
	if !ok {
		return nil
	}
	if !current.Archived {
		m.warn(i18n.NotArchived)
		return nil
	}
	return m.cascade(opUnarchive, current, func(s session.Session) bool { return s.Archived })
}

func (m *Model) delete() tea.Cmd {
	if !m.picked.Empty() {
		return m.batch(opDelete)
	}
	current, ok := m.require()
	if !ok {
		return nil
	}
	targets := m.forest.Cascade(current, nil)
	next := plan{kind: opDelete, targets: targets, single: true, subject: current}

	// Danger mode drops the confirmation for the one session under the cursor.
	// A batch always confirms, however the mode is set.
	if m.danger {
		return m.start(next)
	}
	m.dialog = deleteDialog(m.print, current, len(targets)-1, next)
	return nil
}

func (m *Model) cascade(kind opKind, current session.Session, keep func(session.Session) bool) tea.Cmd {
	return m.start(plan{
		kind:    kind,
		targets: m.forest.Cascade(current, keep),
		single:  true,
		subject: current,
	})
}

func (m *Model) batch(kind opKind) tea.Cmd {
	if !m.agent.Writable() {
		m.warn(i18n.MissingCLIModify, i18n.Args{"agent": m.meta.Label})
		return nil
	}
	if m.rejectWhileBusy() {
		return nil
	}

	picked := m.picked.Picked(m.forest)
	targets := m.picked.Scope(m.forest)
	switch kind {
	case opArchive:
		targets = keepIf(targets, func(s session.Session) bool { return !s.Archived })
	case opUnarchive:
		targets = keepIf(targets, func(s session.Session) bool { return s.Archived })
	}
	if len(targets) == 0 {
		m.warn(nothingToDo[kind])
		return nil
	}

	next := plan{kind: kind, targets: targets, what: i18n.SessionsWord}
	if kind != opDelete {
		return m.start(next)
	}
	// A picked session drags its sub-agents along even when they were taken
	// out of the selection by hand, so the dialog says how many that is.
	m.dialog = bulkDialog(m.print, targets, bulkAsk{
		title: m.print.T(i18n.ConfirmSelectionTitle),
		what:  i18n.SessionsWord,
		note:  m.cascadeNote(len(targets) - len(picked)),
	}, next)
	return nil
}

// nothingToDo says why a key did nothing: the selection holds nothing this
// action can act on.
var nothingToDo = map[opKind]i18n.Key{
	opArchive:   i18n.SelectedAllArchived,
	opUnarchive: i18n.SelectedNoneArchived,
	opDelete:    i18n.SelectedNone,
}

func (m *Model) sweepArchived() tea.Cmd {
	if m.rejectWhileBusy() {
		return nil
	}
	archived := keepIf(m.sessions(), func(s session.Session) bool { return s.Archived })
	if len(archived) == 0 {
		m.warn(i18n.NoArchived)
		return nil
	}
	targets := m.forest.WithDescendants(archived)
	return m.confirmSweep(targets, i18n.ArchivedSessions, m.cascadeNote(len(targets)-len(archived)))
}

func (m *Model) sweepEmpty() tea.Cmd {
	if m.rejectWhileBusy() {
		return nil
	}
	empty := keepIf(m.sessions(), func(s session.Session) bool { return s.Noise })
	if len(empty) == 0 {
		m.warn(i18n.NoneOf, i18n.Args{"what": m.print.T(m.meta.EmptyLabel)})
		return nil
	}
	return m.confirmSweep(m.forest.WithDescendants(empty), m.meta.EmptyLabel, "")
}

func (m *Model) sweepOrphans() tea.Cmd {
	if m.rejectWhileBusy() {
		return nil
	}
	stranded := m.forest.Orphans()
	if len(stranded) == 0 {
		m.warn(i18n.NoOrphans, i18n.Args{"what": m.print.T(m.meta.OrphanLabel)})
		return nil
	}
	return m.confirmSweep(
		m.forest.WithDescendants(stranded),
		m.meta.OrphanLabel,
		m.print.T(i18n.OrphanNote),
	)
}

func (m *Model) confirmSweep(targets []session.Session, what i18n.Key, note string) tea.Cmd {
	m.dialog = bulkDialog(m.print, targets, bulkAsk{
		title: m.print.T(i18n.ConfirmBulkTitle, i18n.Args{"what": m.print.T(what)}),
		what:  what,
		note:  note,
	}, plan{kind: opDelete, targets: targets, what: what})
	return nil
}

// cascadeNote explains the sub-agents a sweep drags along with it.
func (m *Model) cascadeNote(extra int) string {
	if extra <= 0 {
		return ""
	}
	return m.print.N(i18n.CascadeOrphansOne, i18n.CascadeOrphansMany, extra)
}

func (m *Model) copySessionID() tea.Cmd {
	current, ok := m.current()
	if !ok {
		m.supersedeCopy()
		m.warn(i18n.NoCurrentSession)
		return nil
	}
	return m.copyToClipboard(
		current.ID,
		i18n.CopyingSessionID,
		i18n.CopySessionIDSuccess,
		i18n.Args{"session_id": current.ID},
	)
}

func (m *Model) copyWorkingDirectory() tea.Cmd {
	current, ok := m.current()
	if !ok {
		m.supersedeCopy()
		m.warn(i18n.NoCurrentSession)
		return nil
	}
	if current.Cwd == "" {
		m.supersedeCopy()
		m.warn(i18n.CwdUnknown)
		return nil
	}
	return m.copyToClipboard(
		current.Cwd,
		i18n.CopyingCwd,
		i18n.CopyCwdSuccess,
		i18n.Args{"cwd": current.Cwd},
	)
}

func (m *Model) copyToClipboard(text string, copying, copied i18n.Key, args i18n.Args) tea.Cmd {
	m.say(copying)
	m.supersedeCopy()
	seq := m.copySeq
	ctx, cancel := context.WithCancel(m.ctx)
	m.copyStop = cancel
	return func() tea.Msg {
		return copiedClipboardMsg{
			text: text, status: copied, args: args,
			native: clip.Copy(ctx, text), seq: seq,
		}
	}
}

// supersedeCopy cancels a clipboard request when possible and makes any result
// it still returns stale.
func (m *Model) supersedeCopy() {
	if m.copyStop != nil {
		m.copyStop()
		m.copyStop = nil
	}
	m.copySeq++
}

func (m *Model) copiedToClipboard(msg copiedClipboardMsg) tea.Cmd {
	// A later copy supersedes an earlier attempt. The earlier completion must
	// not overwrite the terminal clipboard or newer status.
	if msg.seq != m.copySeq {
		return nil
	}
	if m.copyStop != nil {
		m.copyStop()
		m.copyStop = nil
	}
	if !m.busy && !m.refreshing {
		m.report(msg.status, msg.args)
	}
	if msg.native {
		return nil
	}
	// No platform clipboard integration. OSC 52 works in terminals that
	// support it.
	return tea.SetClipboard(msg.text)
}

func (m *Model) progressed(update opUpdate) tea.Cmd {
	if !update.done {
		m.status = statusLine{text: update.progress}
		return waitFor(m.updates)
	}

	m.busy = false
	m.stale = true
	m.endMouseSequences()
	// What this settled is no longer what a selection was made for. What it
	// could not settle still is, and so is anything picked out while it ran.
	m.picked.Remove(update.result.settled...)

	focusID := ""
	if update.result.plan.kind != opDelete && update.result.plan.single {
		// Archiving moves the file, so the row survives under its own id.
		focusID = update.result.plan.subject.ID
	}
	return m.load(reload{
		focusID: focusID,
		index:   m.cursor,
		note:    m.summarise(update.result),
	})
}

func (m *Model) sessions() []session.Session {
	found := make([]session.Session, 0, len(m.rows))
	for _, row := range m.rows {
		found = append(found, row.Session)
	}
	return found
}

func keepIf(sessions []session.Session, keep func(session.Session) bool) []session.Session {
	kept := make([]session.Session, 0, len(sessions))
	for _, s := range sessions {
		if keep(s) {
			kept = append(kept, s)
		}
	}
	return kept
}
