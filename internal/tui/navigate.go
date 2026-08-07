package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
)

// Layout constants, in terminal cells.
const (
	bannerHeight = 1
	statusHeight = 1
	// listMinWidth keeps the date, time and project columns from crushing the
	// title out of existence.
	listMinWidth = 32
	// detailMinWidth keeps a conversation readable rather than a column of
	// single words.
	detailMinWidth = 24
)

func (m *Model) resize() tea.Cmd {
	m.resizePanes()
	return m.renderDetail()
}

func (m *Model) resizePanes() {
	m.detail.SetWidth(m.detailWidth())
	m.detail.SetHeight(m.bodyHeight())
	m.search.input.SetWidth(max(1, m.width-2))
	m.clampView()
}

func (m *Model) bodyHeight() int {
	rows := m.height - bannerHeight - statusHeight - m.footerHeight()
	if m.search.active {
		rows--
	}
	return max(1, rows)
}

func (m *Model) listWidth() int {
	if m.width <= listMinWidth+detailMinWidth+1 {
		return max(1, m.width/2)
	}
	return min(max(m.width/2, listMinWidth), m.width-detailMinWidth-1)
}

func (m *Model) detailWidth() int {
	return max(0, m.width-m.listWidth()-1)
}

func (m *Model) move(delta int) tea.Cmd {
	if m.focus == focusDetail {
		m.scrollDetail(delta)
		return nil
	}
	return m.jumpTo(m.cursor + delta)
}

// scrollDetail moves the conversation pane by delta lines, up when negative.
//
// The direction has to choose the method rather than the sign: the viewport's
// ScrollDown refuses to do anything once the view is at the bottom, whatever
// number it is handed, so scrolling up with a negative step gets stuck there.
func (m *Model) scrollDetail(delta int) {
	if delta < 0 {
		m.detail.ScrollUp(-delta)
		return
	}
	m.detail.ScrollDown(delta)
}

func (m *Model) jumpTo(index int) tea.Cmd {
	if m.focus == focusDetail {
		if index <= 0 {
			m.detail.GotoTop()
		} else {
			m.detail.GotoBottom()
		}
		return nil
	}
	if len(m.rows) == 0 || index == m.cursor {
		return nil
	}
	m.setCursor(index)
	return m.showCurrent()
}

func (m *Model) setCursor(index int) {
	m.cursor = max(0, min(index, len(m.rows)-1))
	m.clampView()
}

// clampView scrolls the list only as far as it must to keep the cursor on
// screen, so a run of j never redraws from the top.
func (m *Model) clampView() {
	height := m.bodyHeight()
	if len(m.rows) <= height {
		m.top = 0
		return
	}
	m.top = min(m.top, len(m.rows)-height)
	m.top = max(0, min(m.top, m.cursor))
	if m.cursor >= m.top+height {
		m.top = m.cursor - height + 1
	}
}

func (m *Model) pick() {
	if !m.agent.Writable() {
		m.warn(i18n.MissingCLIModify, i18n.Args{"agent": m.meta.Label})
		return
	}
	current, ok := m.current()
	if !ok {
		m.warn(i18n.NoCurrentSession)
		return
	}
	m.picked.Toggle(m.forest, current.ID)
}

func (m *Model) click(msg tea.MouseClickMsg) tea.Cmd {
	mouse := msg.Mouse()
	if mouse.Button != tea.MouseLeft {
		return nil
	}
	previous := m.clicked
	m.clicked = -1
	if mouse.Y < bannerHeight || mouse.Y >= bannerHeight+m.bodyHeight() {
		return nil
	}
	if mouse.X >= m.listWidth() {
		// The divider belongs to neither pane, so it does not move focus.
		if mouse.X > m.listWidth() && mouse.X < m.width && m.detailWidth() > 0 {
			m.focus = focusDetail
		}
		return nil
	}
	m.focus = focusList
	row := m.top + mouse.Y - bannerHeight
	if row >= len(m.rows) {
		return nil
	}

	if row == m.cursor && previous == row {
		m.pick()
		return nil
	}
	m.clicked = row
	m.setCursor(row)
	return m.showCurrent()
}

func (m *Model) wheel(msg tea.MouseWheelMsg) tea.Cmd {
	m.clicked = -1
	mouse := msg.Mouse()
	step := 3
	if mouse.Button == tea.MouseWheelUp {
		step = -3
	} else if mouse.Button != tea.MouseWheelDown {
		return nil
	}
	if mouse.X >= m.listWidth() {
		m.scrollDetail(step)
		return nil
	}
	m.setCursor(m.cursor + step)
	return m.showCurrent()
}
