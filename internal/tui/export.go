package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
// a Markdown file in the configured export directory.
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
	dir := m.exportDir
	return func() tea.Msg {
		path, err := writeExport(ctx, dir, exportFileName(targets, now), func(out io.Writer) error {
			for i, s := range targets {
				if err := ctx.Err(); err != nil {
					return err
				}
				messages, ok := cached[s.ID]
				if !ok {
					found, err := target.Messages(ctx, s)
					if err != nil {
						return err
					}
					messages = found
				}
				if i > 0 {
					if _, err := io.WriteString(out, "\n---\n\n"); err != nil {
						return err
					}
				}
				if _, err := io.WriteString(out, sessionMarkdown(s, messages, meta.ResumeCommand(s.ID), you, meta.Reply)); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
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
	title := strings.Join(strings.Fields(s.Title), " ")
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
		if message.Truncated > 0 {
			fmt.Fprintf(&b, "\n*Preview truncated: %d character(s) omitted.*\n", message.Truncated)
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
	name := safeFilePart(session.Condense(title, 40), 120)
	if name == "" {
		name = "session"
	}
	if suffix := safeFilePart(id, 80); suffix != "" {
		name += "-" + suffix
	}
	// Windows reserves these names even when followed by an extension.
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		name = "session-" + name
	default:
		if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
			number := base[3:]
			if utf8.RuneCountInString(number) == 1 && strings.ContainsAny(number, "123456789¹²³") {
				name = "session-" + name
			}
		}
	}
	return name
}

func safeFilePart(value string, maxBytes int) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r < 32 || r == 0x7f:
			continue
		case strings.ContainsRune(`<>:"/\|?*`, r):
			r = '-'
		case unicode.IsSpace(r):
			r = '-'
		}
		if b.Len()+utf8.RuneLen(r) > maxBytes {
			break
		}
		b.WriteRune(r)
	}
	name := strings.Trim(b.String(), ".-")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return name
}

// writeExport claims a filename before writing, so concurrent exports cannot
// overwrite one another. Failed or cancelled exports leave no partial file.
func writeExport(ctx context.Context, dir, name string, write func(io.Writer) error) (path string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if dir == "" {
		dir = "."
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	file, err := createExport(ctx, dir, name)
	if err != nil {
		return "", err
	}
	path = file.Name()
	defer func() {
		closeErr := file.Close()
		err = errors.Join(err, closeErr)
		if err != nil {
			err = errors.Join(err, os.Remove(path))
			path = ""
		}
	}()
	buffer := bufio.NewWriterSize(exportWriter{ctx: ctx, out: file}, 64<<10)
	if err = write(buffer); err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = buffer.Flush()
	}
	return path, err
}

type exportWriter struct {
	ctx context.Context
	out io.Writer
}

func (w exportWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.out.Write(p)
}

func createExport(ctx context.Context, dir, name string) (*os.File, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; i <= 10000; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		candidate := name
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		file, err := os.OpenFile(filepath.Join(dir, candidate), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no available export filename for %q", name)
}
