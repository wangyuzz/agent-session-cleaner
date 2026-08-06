package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

type emphasis uint8

const (
	plainText emphasis = iota
	strongText
	faintText
)

type dialogLine struct {
	text     string
	emphasis emphasis
}

// dialog is a confirmation. Cancel holds the focus when it opens, so Enter is
// never the destructive answer.
type dialog struct {
	title   string
	lines   []dialogLine
	confirm string
	yes     bool
	// plan is what to do if the answer is yes. An empty plan with danger set
	// means the dialog is asking about danger mode instead.
	plan   plan
	danger bool
}

func deleteDialog(print *i18n.Printer, current session.Session, extra int, next plan) *dialog {
	body := print.T(i18n.DeleteIrreversible)
	if extra > 0 {
		// State the cascade explicitly: the key affects more than the row
		// under the cursor, even though taking the children prevents orphans.
		body = print.N(i18n.DeleteDescendantOne, i18n.DeleteDescendantMany, extra,
			i18n.Args{"warning": body})
	}
	return &dialog{
		title: print.T(i18n.ConfirmDeleteTitle),
		lines: append([]dialogLine{
			{text: titleOf(print, current), emphasis: strongText},
			{text: current.ID, emphasis: faintText},
			{},
		}, wrapLines(body, faintText)...),
		confirm: print.T(i18n.Delete),
		plan:    next,
	}
}

type bulkAsk struct {
	title string
	what  i18n.Key
	note  string
}

func bulkDialog(
	print *i18n.Printer, targets []session.Session, ask bulkAsk, next plan,
) *dialog {
	lines := []dialogLine{{
		text: print.T(i18n.BulkCount, i18n.Args{
			"count": len(targets),
			"what":  print.Label(ask.what, len(targets)),
		}),
		emphasis: strongText,
	}}
	for _, target := range targets[:min(len(targets), bulkPreviewLimit)] {
		lines = append(lines, dialogLine{
			text: "  · " + titleOf(print, target), emphasis: faintText,
		})
	}
	if remaining := len(targets) - bulkPreviewLimit; remaining > 0 {
		lines = append(lines, dialogLine{
			text: print.T(i18n.BulkRemaining, i18n.Args{"count": remaining}), emphasis: faintText,
		})
	}

	lines = append(lines, dialogLine{})
	if ask.note != "" {
		lines = append(lines, wrapLines(ask.note, faintText)...)
	}
	lines = append(lines, wrapLines(print.T(i18n.DeleteIrreversible), faintText)...)

	return &dialog{
		title:   ask.title,
		lines:   lines,
		confirm: print.T(i18n.ConfirmBulkButton, i18n.Args{"count": len(targets)}),
		plan:    next,
	}
}

func dangerDialog(print *i18n.Printer) *dialog {
	return &dialog{
		title: print.T(i18n.ConfirmDangerTitle),
		lines: []dialogLine{
			{text: print.T(i18n.DangerLineOne), emphasis: strongText},
			{text: print.T(i18n.DangerLineTwo), emphasis: faintText},
		},
		confirm: print.T(i18n.Enable),
		danger:  true,
	}
}

// dialogKey answers the dialog. y and n work wherever the focus is, because
// they are the only two things it is asking.
func (m *Model) dialogKey(msg tea.KeyPressMsg) tea.Cmd {
	switch keyName(msg) {
	case "y":
		return m.answer(true)
	case "n", "esc":
		return m.answer(false)
	case "tab", "left", "right", "h", "l":
		m.dialog.yes = !m.dialog.yes
	case "enter":
		return m.answer(m.dialog.yes)
	}
	return nil
}

func (m *Model) answer(yes bool) tea.Cmd {
	asked := m.dialog
	m.dialog = nil
	if !yes {
		return nil
	}
	if asked.danger {
		m.danger = true
		m.status = statusLine{tone: toneError, text: m.print.T(i18n.DangerOn)}
		return nil
	}
	return m.start(asked.plan)
}

func titleOf(print *i18n.Printer, s session.Session) string {
	if s.Title == "" {
		return print.T(i18n.EmptySession)
	}
	return s.Title
}

func wrapLines(text string, emphasis emphasis) []dialogLine {
	var lines []dialogLine
	for _, line := range splitLines(text) {
		lines = append(lines, dialogLine{text: line, emphasis: emphasis})
	}
	return lines
}
