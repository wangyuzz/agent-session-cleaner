package text_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/haowang02/agent-session-cleaner/internal/tui/text"
)

func TestWidthCountsCells(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want int
	}{
		{"ascii", "hello", 5},
		{"wide characters take two cells each", "宽字符", 6},
		{"mixed", "a宽b", 4},
		{"nothing", "", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := text.Width(test.in); got != test.want {
				t.Errorf("Width(%q) = %d, want %d", test.in, got, test.want)
			}
		})
	}
}

func TestPad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		{"pads to width", "ab", 5, "ab   "},
		{"clips to width", "abcdef", 3, "abc"},
		{"already exact", "abc", 3, "abc"},
		{"wide characters", "宽字", 4, "宽字"},
		// A wide character that would straddle the edge is dropped, and the
		// cell it leaves is filled so the next column still starts on time.
		{"a straddling wide character", "宽字", 3, "宽 "},
		{"nothing fits in nothing", "abc", 0, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := text.Pad(test.in, test.width)
			if got != test.want {
				t.Errorf("Pad(%q, %d) = %q, want %q", test.in, test.width, got, test.want)
			}
			if test.width > 0 && text.Width(got) != test.width {
				t.Errorf("Pad(%q, %d) is %d cells wide", test.in, test.width, text.Width(got))
			}
		})
	}
}

func TestLineRenderClips(t *testing.T) {
	t.Parallel()

	var line text.Line
	line.Add("12345", lipgloss.NewStyle()).Add("67890", lipgloss.NewStyle())

	if got := line.Render(20); got != "1234567890" {
		t.Errorf("Render(20) = %q, want the whole line", got)
	}
	if got := line.Render(10); got != "1234567890" {
		t.Errorf("Render(10) = %q, want an exact fit without an ellipsis", got)
	}
	// A long title must not push the next row's columns down a line.
	got := line.Render(7)
	if !strings.HasSuffix(got, text.Ellipsis) {
		t.Errorf("Render(7) = %q, want it clipped with an ellipsis", got)
	}
	if text.Width(got) > 7 {
		t.Errorf("Render(7) produced %d cells", text.Width(got))
	}
}

func TestLineWidthAndPlain(t *testing.T) {
	t.Parallel()

	var line text.Line
	line.Cell("date", 6, lipgloss.NewStyle()).Space(2).Add("宽字", lipgloss.NewStyle())

	if got := line.Plain(); got != "date    宽字" {
		t.Errorf("Plain() = %q", got)
	}
	if got, want := line.Width(), 6+2+4; got != want {
		t.Errorf("Width() = %d, want %d", got, want)
	}
}

func TestHighlight(t *testing.T) {
	t.Parallel()

	mark := lipgloss.NewStyle().Reverse(true)

	t.Run("marks every occurrence", func(t *testing.T) {
		t.Parallel()
		var line text.Line
		line.Add("parser and parser", lipgloss.NewStyle())
		line.Highlight("parser", true, mark)

		// The text is unchanged; only the styling moved.
		if got := line.Plain(); got != "parser and parser" {
			t.Errorf("Plain() = %q", got)
		}
		if got := strings.Count(line.Render(80), "\x1b[7m"); got != 2 {
			t.Errorf("marked %d occurrences, want 2", got)
		}
	})

	t.Run("reaches across columns", func(t *testing.T) {
		t.Parallel()
		// A query can match the project column and the title alike.
		var line text.Line
		line.Cell("app", 6, lipgloss.NewStyle()).Add("fix app", lipgloss.NewStyle())
		line.Highlight("app", true, mark)

		if got := strings.Count(line.Render(80), "\x1b[7m"); got != 2 {
			t.Errorf("marked %d occurrences across spans, want 2", got)
		}
	})

	t.Run("case folding is the caller's choice", func(t *testing.T) {
		t.Parallel()
		var insensitive text.Line
		insensitive.Add("Parser", lipgloss.NewStyle())
		insensitive.Highlight("parser", false, mark)
		if !strings.Contains(insensitive.Render(80), "\x1b[7m") {
			t.Error("a case-insensitive search missed a differently cased match")
		}

		var sensitive text.Line
		sensitive.Add("Parser", lipgloss.NewStyle())
		sensitive.Highlight("parser", true, mark)
		if strings.Contains(sensitive.Render(80), "\x1b[7m") {
			t.Error("a case-sensitive search matched anyway")
		}
	})

	t.Run("case folding preserves Unicode text", func(t *testing.T) {
		t.Parallel()
		// The Kelvin sign folds to an ASCII k, changing its UTF-8 byte length.
		// Byte offsets from the folded string must never slice the original.
		var line text.Line
		line.Add("Kelvin", lipgloss.NewStyle())
		line.Highlight("k", false, mark)
		if got := line.Plain(); got != "Kelvin" {
			t.Errorf("highlight corrupted Unicode text: %q", got)
		}
		if !strings.Contains(line.Render(80), "\x1b[7m") {
			t.Error("the folded Unicode match was not highlighted")
		}
	})

	t.Run("an empty query changes nothing", func(t *testing.T) {
		t.Parallel()
		var line text.Line
		line.Add("untouched", lipgloss.NewStyle())
		line.Highlight("", true, mark)
		if got := line.Render(80); strings.Contains(got, "\x1b[7m") {
			t.Errorf("Render() = %q", got)
		}
	})

	t.Run("what the highlight leaves unset shows through", func(t *testing.T) {
		t.Parallel()
		var line text.Line
		line.Add("bold title", lipgloss.NewStyle().Bold(true))
		line.Highlight("title", true, mark)
		if got := line.Render(80); !strings.Contains(got, "1;7m") &&
			!strings.Contains(got, "7;1m") {
			t.Errorf("Render() = %q, want the bold kept under the highlight", got)
		}
	})
}

func TestFill(t *testing.T) {
	t.Parallel()

	blue := lipgloss.NewStyle().Background(lipgloss.Color("#0000FF"))
	const code = "48;2;0;0;255"

	t.Run("reaches past the last span", func(t *testing.T) {
		t.Parallel()
		var line text.Line
		line.Fill(blue).Add("short", lipgloss.NewStyle())

		rendered := line.Render(20)
		if got := text.Width(ansi.Strip(rendered)); got != 20 {
			t.Errorf("a filled line is %d cells wide, want the full 20", got)
		}
		// Once for the text and once for the run of blank cells after it.
		if got := strings.Count(rendered, code); got != 2 {
			t.Errorf("Render() = %q, want the fill under the padding too", rendered)
		}
	})

	t.Run("a span keeps what it sets for itself", func(t *testing.T) {
		t.Parallel()
		var line text.Line
		line.Fill(blue).Add("match", lipgloss.NewStyle().Background(lipgloss.Color("#FF0000")))
		if got := line.Render(10); strings.Contains(got, code) &&
			!strings.Contains(got, "48;2;255;0;0") {
			t.Errorf("Render() = %q, want the span's own background kept", got)
		}
	})

	t.Run("an unfilled line is not padded", func(t *testing.T) {
		t.Parallel()
		var line text.Line
		line.Add("short", lipgloss.NewStyle())
		if got := line.Render(20); got != lipgloss.NewStyle().Render("short") {
			t.Errorf("Render() = %q, want no padding without a fill", got)
		}
	})
}
