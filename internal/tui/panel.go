package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
	"github.com/haowang02/agent-session-cleaner/internal/tui/text"
)

// renderDetail rebuilds the conversation pane, which has to happen whenever
// the session, its messages, the pane's width or the colour profile changes.
// SetContent clamps the existing offset to the new content, so a resize does
// not unexpectedly throw a reader back to the first message.
func (m *Model) renderDetail() {
	width := m.detailWidth()
	if width <= 0 {
		return
	}
	current, ok := m.current()
	if !ok {
		m.detail.SetContent(m.theme.Placeholder.Render(m.print.T(i18n.NoSessionsHere)))
		return
	}

	messages := m.messages[m.showing]
	lines := m.detailHeader(current, messages, width)

	switch {
	case m.loading:
		lines = append(lines, m.theme.Placeholder.Render(m.print.T(i18n.PickerLoading)))
	case len(messages) == 0:
		lines = append(lines, m.theme.Placeholder.Render(m.print.T(i18n.NoConversation)))
	default:
		for _, message := range messages {
			lines = append(lines, m.messageLines(message, width)...)
		}
	}

	m.detail.SetContent(strings.Join(lines, "\n"))
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

func (m *Model) messageLines(message session.Message, width int) []string {
	tag, bar := m.print.T(i18n.You), m.theme.UserBar
	tagStyle := m.theme.UserTag
	mark := "▶ "
	if message.Role == session.Assistant {
		tag, bar, tagStyle, mark = m.meta.Reply, m.theme.AgentBar, m.theme.AgentTag, "◀ "
	}

	lines := []string{tagStyle.Render(mark + tag)}
	gutter := bar.Render("▎") + m.theme.Screen.Render(" ")
	body := message.Text
	if message.Images > 0 {
		note := m.print.N(i18n.ImagesOne, i18n.ImagesMany, message.Images)
		if body == "" {
			body = note
		} else {
			body += "\n" + note
		}
	}
	for _, line := range splitLines(ansi.Wrap(body, max(1, width-2), " -")) {
		lines = append(lines, gutter+m.theme.Body.Render(line))
	}
	if message.Truncated > 0 {
		lines = append(lines, gutter+m.theme.Truncated.Render(m.print.N(
			i18n.MessageTruncatedOne, i18n.MessageTruncatedMany, message.Truncated,
		)))
	}
	return append(lines, m.theme.Screen.Render(strings.Repeat(" ", width)))
}
