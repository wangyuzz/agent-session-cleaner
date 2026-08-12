package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

type exportedMsg struct {
	path  string
	count int
	err   error
}

// exportMarkdown writes the current session, or every selected session, as
// a Markdown file in the working directory.
func (m *Model) exportMarkdown() tea.Cmd {
	targets := m.exportTargets()
	if len(targets) == 0 {
		m.warn(i18n.NoCurrentSession)
		return nil
	}

	cached := make(map[string][]session.Message, len(targets))
	for _, s := range targets {
		if held, ok := m.messages[keyOf(s)]; ok {
			cached[s.ID] = held
		}
	}

	m.say(i18n.Exporting)
	target, ctx := m.agent, m.ctx
	meta := m.meta
	you := m.print.T(i18n.You)
	now := time.Now()
	return func() tea.Msg {
		var parts []string
		for _, s := range targets {
			messages, ok := cached[s.ID]
			if !ok {
				found, err := target.Messages(ctx, s)
				if err != nil {
					return exportedMsg{err: err}
				}
				messages = found
			}
			parts = append(parts, sessionMarkdown(s, messages, meta.ResumeCommand(s.ID), you, meta.Reply))
		}

		dir, err := os.Getwd()
		if err != nil {
			return exportedMsg{err: err}
		}
		name := exportFileName(targets, now)
		path := uniquePath(dir, name)
		body := strings.Join(parts, "\n---\n\n")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return exportedMsg{err: err}
		}
		return exportedMsg{path: path, count: len(targets)}
	}
}

func (m *Model) exported(msg exportedMsg) tea.Cmd {
	if msg.err != nil {
		m.status = statusLine{tone: toneError, text: m.print.T(i18n.ExportFailed, i18n.Args{"error": msg.err})}
		return nil
	}
	m.status = statusLine{tone: toneOK, text: m.print.T(i18n.ExportSuccess, i18n.Args{
		"count": strconv.Itoa(msg.count),
		"path":  msg.path,
	})}
	return nil
}

func (m *Model) exportTargets() []session.Session {
	if !m.picked.Empty() {
		return m.picked.Picked(m.forest)
	}
	if current, ok := m.current(); ok {
		return []session.Session{current}
	}
	return nil
}

func sessionMarkdown(s session.Session, messages []session.Message, resume, you, reply string) string {
	var b strings.Builder
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = s.ID
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "- Agent: %s\n", s.Agent)
	fmt.Fprintf(&b, "- Session: `%s`\n", s.ID)
	if resume != "" {
		fmt.Fprintf(&b, "- Resume: `%s`\n", resume)
	}
	if s.Cwd != "" {
		fmt.Fprintf(&b, "- Directory: `%s`\n", s.Cwd)
	}
	if !s.CreatedAt.IsZero() {
		fmt.Fprintf(&b, "- Created: %s\n", s.CreatedAt.Format("2006-01-02 15:04"))
	}
	b.WriteByte('\n')

	if len(messages) == 0 {
		b.WriteString("*(No messages.)*\n")
		return b.String()
	}

	for _, message := range messages {
		who := you
		if message.Role == session.Assistant {
			who = reply
		}
		fmt.Fprintf(&b, "## %s\n\n", who)
		text := strings.TrimSpace(message.Text)
		if text != "" {
			b.WriteString(text)
			b.WriteString("\n")
		}
		if message.Images > 0 {
			fmt.Fprintf(&b, "\n*%d image(s)*\n", message.Images)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func exportFileName(sessions []session.Session, now time.Time) string {
	if len(sessions) == 1 {
		return safeFileName(sessions[0].Title, sessions[0].ID) + ".md"
	}
	return "asc-export-" + now.Format("20060102-150405") + ".md"
}

func safeFileName(title, id string) string {
	title = strings.TrimSpace(session.Condense(title, 40))
	var b strings.Builder
	for _, r := range title {
		switch {
		case r < 32 || r == 0x7f:
			continue
		case strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteByte('-')
		case unicode.IsSpace(r):
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	name := strings.Trim(b.String(), ".-")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	if name == "" {
		name = "session"
	}
	if id != "" {
		name += "-" + id
	}
	return name
}

func uniquePath(dir, name string) string {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		return path
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; i < 1000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, time.Now().UnixNano(), ext))
}
