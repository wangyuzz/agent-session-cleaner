package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
	"github.com/haowang02/agent-session-cleaner/internal/tui/markdown"
	"github.com/haowang02/agent-session-cleaner/internal/tui/text"
)

// bodyKey identifies one rendering of one conversation. Markdown has to be
// laid out again whenever the pane's width or the terminal's colour scheme
// changes, so both belong to the identity of what was drawn.
type bodyKey struct {
	session cacheKey
	width   int
	dark    bool
}

type renderedMsg struct {
	key   bodyKey
	lines []string
}

const (
	// Larger conversations render in a command so they cannot stall input.
	syncRenderBytes = 16 << 10
	// Rendered output can be much larger than its Markdown source.
	renderCacheBytes = 24 << 20
)

func (m *Model) want() bodyKey {
	return bodyKey{session: m.showing, width: m.detailWidth(), dark: m.theme.Dark}
}

type renderCache struct {
	held  map[bodyKey][]string
	order []bodyKey
	bytes int
}

func (c *renderCache) get(key bodyKey) ([]string, bool) {
	lines, ok := c.held[key]
	return lines, ok
}

// put keeps a rendering, dropping the oldest until the total fits. The newest
// is always kept even when it alone is over the bound: it is what is on screen.
func (c *renderCache) put(key bodyKey, lines []string) {
	if c.held == nil {
		c.held = map[bodyKey][]string{}
	}
	if _, held := c.held[key]; held {
		return
	}
	c.held[key] = lines
	c.order = append(c.order, key)
	c.bytes += weigh(lines)

	for len(c.order) > 1 && c.bytes > renderCacheBytes {
		oldest := c.order[0]
		c.bytes -= weigh(c.held[oldest])
		delete(c.held, oldest)
		c.order = c.order[1:]
	}
}

func (c *renderCache) remove(session cacheKey) {
	kept := c.order[:0]
	for _, key := range c.order {
		if key.session != session {
			kept = append(kept, key)
			continue
		}
		c.bytes -= weigh(c.held[key])
		delete(c.held, key)
	}
	c.order = kept
}

func (c *renderCache) clear() {
	c.held, c.order, c.bytes = nil, nil, 0
}

func weigh(lines []string) int {
	total := 0
	for _, line := range lines {
		total += len(line)
	}
	return total
}

func conversationBytes(messages []session.Message) int {
	total := 0
	for _, message := range messages {
		total += len(message.Text)
	}
	return total
}

// renderDetail rebuilds the pane. Large conversations show plain text first
// and return a command that replaces it with Markdown.
func (m *Model) renderDetail() tea.Cmd {
	width := m.detailWidth()
	if width <= 0 {
		return nil
	}
	current, ok := m.current()
	if !ok {
		m.detail.SetContent(m.theme.Placeholder.Render(m.print.T(i18n.NoSessionsHere)))
		return nil
	}

	messages := m.messages[m.showing]
	lines := m.detailHeader(current, messages, width)

	var cmd tea.Cmd
	switch {
	case m.loading:
		lines = append(lines, m.theme.Placeholder.Render(m.print.T(i18n.PickerLoading)))
	case len(messages) == 0:
		lines = append(lines, m.theme.Placeholder.Render(m.print.T(i18n.NoConversation)))
	default:
		want := m.want()
		marked, held := m.renders.get(want)
		large := conversationBytes(messages) > syncRenderBytes
		if !held && !large {
			if marked = m.markupFor(want)(messages); marked != nil {
				m.renders.put(want, marked)
				held = true
			}
		}
		if held {
			lines = append(lines, marked...)
			break
		}
		// Plain text is both the immediate view for large conversations and the
		// fallback when no renderer can be built.
		lines = append(lines, m.plainBody(messages, width)...)
		if large {
			cmd = m.renderBody(want, messages)
		}
	}

	// The offset survives being handed shorter content and then longer content
	// again, which is what the swap from plain text to Markdown amounts to.
	offset := m.detail.YOffset()
	m.detail.SetContent(strings.Join(lines, "\n"))
	m.detail.SetYOffset(offset)
	return cmd
}

// markupFor captures immutable styles so the returned work is safe in a Cmd.
func (m *Model) markupFor(key bodyKey) func([]session.Message) []string {
	decorate := m.decorator(key.width)
	colors, dark := m.theme.Colors, m.theme.Dark
	width := max(1, key.width-gutterWidth)

	return func(messages []session.Message) []string {
		render, err := markdown.New(colors, dark, width)
		if err != nil {
			return nil
		}
		var lines []string
		for _, message := range messages {
			lines = append(lines, decorate(message, render.Lines(message.Text))...)
		}
		return lines
	}
}

// renderBody serializes expensive work. When this result arrives, rendered
// starts the newest requested layout and skips any intermediate states.
func (m *Model) renderBody(key bodyKey, messages []session.Message) tea.Cmd {
	if m.rendering != nil {
		return nil
	}
	m.rendering = &key

	lay := m.markupFor(key)
	return func() tea.Msg {
		return renderedMsg{key: key, lines: lay(messages)}
	}
}

func (m *Model) rendered(msg renderedMsg) tea.Cmd {
	if m.rendering == nil || *m.rendering != msg.key {
		return nil
	}
	m.rendering = nil
	current := msg.key == m.want()
	if current && msg.lines != nil {
		m.renders.put(msg.key, msg.lines)
	}
	if current && msg.lines == nil {
		return nil
	}
	// A stale result is discarded, then the latest requested layout begins.
	return m.renderDetail()
}

// plainBody is the conversation with nothing interpreted, which is what the
// pane shows until the marked-up one arrives and what it keeps if Glamour
// cannot make one.
func (m *Model) plainBody(messages []session.Message, width int) []string {
	decorate := m.decorator(width)
	var lines []string
	for _, message := range messages {
		lines = append(lines, decorate(message, markdown.Plain(
			message.Text, max(1, width-gutterWidth)))...)
	}
	return lines
}

func (m *Model) detailHeader(s session.Session, messages []session.Message, width int) []string {
	// Every field on its own line, clipped rather than wrapped: the pane may
	// be narrow, and a long title must not push the next field down.
	clip := func(line *text.Line) string { return line.Fill(m.theme.Screen).Render(width) }

	var head text.Line
	if s.Archived {
		head.Add(m.print.T(i18n.ArchivedMarker)+"  ", m.theme.ArchivedTag)
	}
	head.Add(titleOf(m.print, s), m.theme.DetailTitle)

	var id text.Line
	id.Add(s.ID, m.theme.DetailMeta)

	meta := make([]string, 0, 4)
	if !s.CreatedAt.IsZero() {
		meta = append(meta, s.CreatedAt.Format("2006-01-02 15:04"))
	}
	meta = append(meta, s.Client,
		m.print.N(i18n.MessagesOne, i18n.MessagesMany, len(messages)))
	if s.Version != "" {
		meta = append(meta, "v"+s.Version)
	}
	var facts text.Line
	facts.Add(strings.Join(meta, " · "), m.theme.DetailMeta)

	var where text.Line
	if s.Cwd == "" {
		where.Add(m.print.T(i18n.CwdUnknown), m.theme.DetailMeta)
	} else {
		where.Add(agent.UnderHome(s.Cwd), m.theme.DetailMeta)
	}

	lines := []string{clip(&head), clip(&id), clip(&facts), clip(&where)}

	// Where a side thread came from, and whether that conversation is still
	// around — the one thing that decides whether it is worth keeping.
	var origin text.Line
	switch {
	case s.Parent != "" && m.forest.Stranded(s.ID):
		origin.Add(m.print.T(i18n.ParentDeleted), m.theme.OrphanTag)
	case s.Parent != "":
		origin.Add(m.print.T(i18n.SpawnedBy, i18n.Args{"session_id": s.Parent}), m.theme.DetailMeta)
	case s.SideThread:
		// Otherwise this row just looks like a top-level conversation that
		// forgot to be one. Say why it stands alone, and why it is safe.
		origin.Add(m.print.T(i18n.SourceUnrecorded), m.theme.DetailMeta)
	}
	if origin.Width() > 0 {
		lines = append(lines, clip(&origin))
	}

	return append(lines,
		m.theme.Divider.Render(strings.Repeat("─", width)),
		m.theme.Screen.Render(strings.Repeat(" ", width)),
	)
}

const gutterWidth = 2

func (m *Model) decorator(width int) func(session.Message, []string) []string {
	you, reply := m.print.T(i18n.You), m.meta.Reply
	images := func(n int) string { return m.print.N(i18n.ImagesOne, i18n.ImagesMany, n) }
	truncated := func(n int) string {
		return m.print.N(i18n.MessageTruncatedOne, i18n.MessageTruncatedMany, n)
	}
	style := m.theme

	return func(message session.Message, body []string) []string {
		tag, bar, tagStyle, mark := you, style.UserBar, style.UserTag, "▶ "
		if message.Role == session.Assistant {
			tag, bar, tagStyle, mark = reply, style.AgentBar, style.AgentTag, "◀ "
		}

		lines := []string{tagStyle.Render(mark + tag)}
		gutter := bar.Render("▎") + style.Screen.Render(" ")
		for _, line := range body {
			lines = append(lines, gutter+style.Body.Render(line))
		}
		// The interface's own remarks about a message are not part of it, so
		// they are never marked up.
		if message.Images > 0 {
			lines = append(lines, gutter+style.Body.Render(images(message.Images)))
		}
		if message.Truncated > 0 {
			lines = append(lines, gutter+style.Truncated.Render(truncated(message.Truncated)))
		}
		return append(lines, style.Screen.Render(strings.Repeat(" ", width)))
	}
}
