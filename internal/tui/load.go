package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

const detailCacheLimit = 48

type reload struct {
	focusID string
	index   int
	note    statusLine
}

type loadedMsg struct {
	sessions []session.Session
	err      error
	request  reload
}

type messagesMsg struct {
	key      cacheKey
	messages []session.Message
	err      error
}

// cacheKey re-reads a conversation when its file has changed underneath us.
// The session id is part of it because an agent that keeps every session in
// one database would otherwise have them all share an entry.
type cacheKey struct {
	id      string
	path    string
	written int64
	size    int64
}

func keyOf(s session.Session) cacheKey {
	return cacheKey{id: s.ID, path: s.Path, written: s.UpdatedAt.UnixNano(), size: s.Size}
}

func (m *Model) load(request reload) tea.Cmd {
	m.refreshing = true
	target, ctx := m.agent, m.ctx
	return func() tea.Msg {
		found, err := target.Discover(ctx)
		return loadedMsg{sessions: found, err: err, request: request}
	}
}

func (m *Model) loaded(msg loadedMsg) tea.Cmd {
	m.refreshing = false
	if msg.err != nil {
		m.stale = true
		m.status = statusLine{tone: toneError, text: m.print.Err(msg.err)}
		// Keep the previous snapshot on a total failure. A backend may also
		// return partial results with an error; those are useful for browsing,
		// but stale keeps every mutation disabled until a complete refresh.
		if len(msg.sessions) == 0 {
			return nil
		}
	} else {
		m.stale = false
	}

	// Nothing is ever filtered out: a session you cannot see is one you cannot
	// decide about, and an archived row already says what it is.
	m.forest = session.Build(msg.sessions)
	m.rows = m.forest.Rows()
	m.endMouseSequences()
	// Whatever has been deleted since the selection was made is no longer
	// selectable, and would otherwise keep multi-select on with nothing in it.
	m.picked.Retain(m.forest)

	m.cursor = msg.request.index
	if msg.request.focusID != "" {
		for i, row := range m.rows {
			if row.Session.ID == msg.request.focusID {
				m.cursor = i
				break
			}
		}
	}
	m.cursor = max(0, min(m.cursor, len(m.rows)-1))
	m.clampView()

	if msg.err == nil && msg.request.note.text != "" {
		m.status = msg.request.note
	}
	return m.showCurrent()
}

func (m *Model) reload() tea.Cmd {
	// A stale listing is precisely what reload repairs, so it is the one read
	// that remains available in that state.
	if m.busy || m.refreshing {
		m.warn(i18n.PreviousBusy)
		return nil
	}
	current, _ := m.current()
	return m.load(reload{
		focusID: current.ID,
		index:   m.cursor,
		note:    statusLine{text: m.print.T(i18n.Reloaded)},
	})
}

func (m *Model) showCurrent() tea.Cmd {
	current, ok := m.current()
	if !ok {
		m.showing = cacheKey{}
		m.loading = false
		m.detail.GotoTop()
		return m.renderDetail()
	}

	key := keyOf(current)
	if _, held := m.messages[key]; held {
		m.showing = key
		m.loading = false
		m.detail.GotoTop()
		return m.renderDetail()
	}

	m.showing = key
	m.loading = true
	m.detail.GotoTop()
	render := m.renderDetail()
	if m.pending[key] {
		return render
	}
	m.pending[key] = true

	target, ctx := m.agent, m.ctx
	return tea.Batch(render, func() tea.Msg {
		found, err := target.Messages(ctx, current)
		return messagesMsg{key: key, messages: found, err: err}
	})
}

func (m *Model) receive(msg messagesMsg) tea.Cmd {
	delete(m.pending, msg.key)
	if msg.err != nil {
		if msg.key == m.showing {
			m.loading = false
			m.status = statusLine{tone: toneError, text: m.print.Err(msg.err)}
			return m.renderDetail()
		}
		return nil
	}

	m.remember(msg.key, msg.messages)
	// The cursor may have moved on while this was being read.
	if msg.key != m.showing {
		return nil
	}
	m.loading = false
	m.detail.GotoTop()
	return m.renderDetail()
}

func (m *Model) remember(key cacheKey, messages []session.Message) {
	if _, held := m.messages[key]; !held {
		if len(m.order) >= detailCacheLimit {
			dropped := m.order[0]
			delete(m.messages, dropped)
			m.renders.remove(dropped)
			m.order = m.order[1:]
		}
		m.order = append(m.order, key)
	}
	m.messages[key] = messages
}
