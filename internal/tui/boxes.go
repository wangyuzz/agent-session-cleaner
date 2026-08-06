package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/tui/text"
)

const dialogMaxWidth = 68

// overlay draws a box centred over the screen behind it, with that screen
// faded so the box is plainly the thing being asked about.
func (m *Model) overlay(base, box string) string {
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(lipgloss.NewLayer(base))
	fade(canvas, m.theme.Faded, m.theme.Screen.GetBackground())

	// A Layer's own X, Y and Z only mean anything to a Compositor: composing a
	// Layer straight onto a Canvas draws its content at the origin, across the
	// canvas's whole area, which wipes out everything already there.
	canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(box).
			X(max(0, (m.width-lipgloss.Width(box))/2)).
			Y(max(0, (m.height-lipgloss.Height(box))/2)),
	))
	return canvas.Render()
}

// paint gives the screen's background to any cell that has none.
//
// Everything drawn here carries its own colour, but the text input does not:
// it emits one bare cell where the cursor stands, which would show as a hole
// in a bar that is otherwise painted. Rather than reach into somebody else's
// widget, the finished screen is swept once.
func (m *Model) paint(screen string) string {
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(lipgloss.NewLayer(screen))

	background := m.theme.Screen.GetBackground()
	for y := range m.height {
		for x := range m.width {
			// A zero-width cell is the second half of a wide character, which
			// the first half already colours.
			if cell := canvas.CellAt(x, y); cell != nil &&
				cell.Width > 0 && cell.Style.Bg == nil {
				cell.Style.Bg = background
			}
		}
	}
	return canvas.Render()
}

func fade(canvas *lipgloss.Canvas, text, background color.Color) {
	for y := range canvas.Height() {
		for x := range canvas.Width() {
			cell := canvas.CellAt(x, y)
			if cell == nil {
				continue
			}
			cell.Style.Fg = text
			cell.Style.Bg = background
			cell.Style.UnderlineColor = nil
			cell.Style.Underline = 0
			cell.Style.Attrs = 0
		}
	}
}

func (m *Model) dialogBox() string {
	// The box: two border columns, then two padding columns either side.
	modal, horizontalFrame, verticalFrame := m.modalStyle(true)
	available := m.width - 4
	if horizontalFrame == 0 || m.width < 28 {
		available = m.width
	}
	box := min(dialogMaxWidth, max(24, available))
	box = min(max(1, m.width), box)
	inner := max(1, box-horizontalFrame)

	lines := []string{
		m.floatLine().Add(m.dialog.title, m.theme.ModalTitle).Render(inner),
		m.floatLine().Render(inner),
	}
	for _, line := range m.dialog.lines {
		lines = append(lines,
			m.floatLine().Add(line.text, m.emphasisStyle(line.emphasis)).Render(inner))
	}

	confirm, cancel := m.theme.Button, m.theme.Button
	if m.dialog.yes {
		confirm = m.theme.ButtonDanger
	} else {
		cancel = m.theme.ButtonFocus
	}
	confirmButton := confirm.Render(m.dialog.confirm + " (y)")
	cancelButton := cancel.Render(m.print.T(i18n.Cancel) + " (n)")
	buttons := lipgloss.JoinHorizontal(lipgloss.Top,
		confirmButton, m.theme.Float.Render("  "), cancelButton)
	lines = append(lines, m.floatLine().Render(inner))
	controlLines := 1
	if lipgloss.Width(buttons) <= inner {
		lines = append(lines, lipgloss.PlaceHorizontal(
			inner, lipgloss.Right, buttons, lipgloss.WithWhitespaceStyle(m.theme.Float),
		))
	} else {
		// On a narrow terminal both choices remain visible instead of the safe
		// answer falling beyond the right edge.
		controlLines = 2
		lines = append(lines,
			text.Fit(confirm.Padding(0).Render("(y) "+m.dialog.confirm), inner, m.theme.Float),
			text.Fit(cancel.Padding(0).Render("(n) "+m.print.T(i18n.Cancel)), inner, m.theme.Float),
		)
	}

	// Keep the controls on screen in a short terminal. The full question is
	// still available after a resize; at this size an ellipsis is more honest
	// than silently clipping the answer buttons off the bottom.
	lines = fitDialogHeight(lines, max(1, m.height-verticalFrame), controlLines,
		m.floatLine().Add("…", m.theme.ModalWarning).Render(inner))

	return modal.Render(strings.Join(lines, "\n"))
}

func fitDialogHeight(lines []string, room, controls int, ellipsis string) []string {
	if len(lines) <= room {
		return lines
	}
	if room <= controls {
		return lines[len(lines)-room:]
	}
	head := min(room-controls-1, len(lines)-controls)
	shown := append([]string(nil), lines[:head]...)
	shown = append(shown, ellipsis)
	return append(shown, lines[len(lines)-controls:]...)
}

// floatLine starts a line on a box's own background. Every line of a box has
// to carry it: a background set on the box itself stops at the first reset
// sequence inside a line, leaving the rest of that line bare.
func (m *Model) floatLine() *text.Line {
	line := new(text.Line)
	return line.Fill(m.theme.Float)
}

func (m *Model) emphasisStyle(e emphasis) lipgloss.Style {
	switch e {
	case strongText:
		return m.theme.Text.Bold(true)
	case faintText:
		return m.theme.ModalWarning
	default:
		return m.theme.Text
	}
}

func (m *Model) helpBox() string {
	modal, _, _ := m.modalStyle(false)
	// Wide enough for its longest line, but never wider than the terminal.
	// Clipping a description would leave the help unable to say what a key
	// does, which is its whole job.
	inner := m.helpInnerWidth()
	lines := m.helpLines(inner)

	// The help is taller than a short terminal. Show a window of it rather
	// than a box whose bottom half is off the screen.
	if room := m.helpRoom(); len(lines) > room {
		top := min(max(0, m.helpTop), len(lines)-room)
		lines = lines[top : top+room]
	}
	return modal.Render(strings.Join(lines, "\n"))
}

func (m *Model) helpInnerWidth() int {
	_, horizontalFrame, _ := m.modalStyle(false)
	return min(m.helpNaturalWidth(), max(1, m.width-horizontalFrame-2))
}

func (m *Model) helpRoom() int {
	_, _, verticalFrame := m.modalStyle(false)
	return max(1, m.height-verticalFrame)
}

// modalStyle drops decoration only when the terminal cannot physically hold
// its six horizontal or four vertical frame cells. Content and controls are
// more important than a border in a window that small.
func (m *Model) modalStyle(danger bool) (lipgloss.Style, int, int) {
	if m.width < 7 || m.height < 5 {
		return m.theme.Float, 0, 0
	}
	if danger {
		return m.theme.ModalDanger, 6, 4
	}
	return m.theme.Modal, 6, 4
}

func (m *Model) helpNaturalWidth() int {
	width := max(text.Width(m.print.T(i18n.HelpTitle)), text.Width(m.print.T(i18n.HelpClose)))
	keyWidth := m.helpKeyWidth()
	for _, section := range m.helpText() {
		width = max(width, text.Width(m.print.T(section.name)))
		for _, entry := range section.entries {
			width = max(width, 2+keyWidth+3+text.Width(m.print.T(entry.what)))
		}
	}
	return width
}

func (m *Model) helpKeyWidth() int {
	keyWidth := 0
	for _, section := range m.helpText() {
		for _, entry := range section.entries {
			keyWidth = max(keyWidth, text.Width(entry.keys))
		}
	}
	return keyWidth
}

func (m *Model) helpLines(width int) []string {
	sections := m.helpText()
	keyWidth := m.helpKeyWidth()

	lines := []string{
		m.floatLine().Add(m.print.T(i18n.HelpTitle), m.theme.Text.Bold(true)).Render(width),
		m.floatLine().Render(width),
	}
	for _, section := range sections {
		lines = append(lines,
			m.floatLine().Add(m.print.T(section.name), m.theme.Muted).Render(width))
		for _, entry := range section.entries {
			line := m.floatLine()
			line.Space(2).Add(text.Pad(entry.keys, keyWidth), m.theme.Shortcut).Space(3)
			line.Add(m.print.T(entry.what), m.theme.Text)
			lines = append(lines, line.Render(width))
		}
		lines = append(lines, m.floatLine().Render(width))
	}
	return append(lines,
		m.floatLine().Add(m.print.T(i18n.HelpClose), m.theme.Faint).Render(width))
}
