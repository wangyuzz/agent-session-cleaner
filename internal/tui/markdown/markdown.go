// Package markdown renders transcript messages with the application's palette.
// It preserves ordinary newlines and falls back to plain text when Glamour
// would discard raw HTML.
package markdown

import (
	"regexp"
	"strings"
	"unicode"

	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/haowang02/agent-session-cleaner/internal/tui/theme"
)

// Renderer turns Markdown into styled lines of a fixed width. A renderer is
// confined to the goroutine that created it because Glamour owns its buffer.
type Renderer struct {
	term  *glamour.TermRenderer
	width int
}

var rawHTMLTag = regexp.MustCompile(`</?[A-Za-z][A-Za-z0-9.-]*(?:\s[^<>]*)?/?>`)

// New builds a renderer for a pane of the given width in cells.
func New(p theme.Palette, dark bool, width int) (*Renderer, error) {
	width = max(1, width)
	term, err := glamour.NewTermRenderer(
		glamour.WithStyles(styleSheet(p, dark)),
		glamour.WithWordWrap(width),
		// Most messages are prose, not Markdown. Without this, every single
		// newline a person typed is thrown away as paragraph filling.
		glamour.WithPreservedNewLines(),
		// The default is 256 colours, which would leave code blocks a shade
		// off from everything around them.
		glamour.WithChromaFormatter("terminal16m"),
	)
	if err != nil {
		return nil, err
	}
	return &Renderer{term: term, width: width}, nil
}

// Lines renders one message, falling back to wrapped text on failure.
func (r *Renderer) Lines(text string) []string {
	text = clean(text)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	out, err := r.term.Render(text)
	if err != nil || losesRawHTML(text, out) {
		return Plain(text, r.width)
	}
	return split(out)
}

// Plain wraps text without interpreting it.
func Plain(text string, width int) []string {
	text = clean(text)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	// " -" lets a long unbroken token — a path, a URL, a hash — break rather
	// than run off the edge of the pane.
	return split(ansi.Wrap(text, max(1, width), " -"))
}

func losesRawHTML(source, rendered string) bool {
	tags := rawHTMLTag.FindAllString(source, -1)
	if len(tags) == 0 {
		return false
	}
	visible := ansi.Strip(rendered)
	for _, tag := range tags {
		if !strings.Contains(visible, tag) {
			return true
		}
	}
	return false
}

// clean removes terminal commands and controls from transcript text before it
// is passed to either renderer. Tabs become spaces so their width is stable.
func clean(text string) string {
	text = strings.ToValidUTF8(ansi.Strip(text), "�")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}

func split(s string) []string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
