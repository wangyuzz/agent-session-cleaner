package markdown_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/haowang02/agent-session-cleaner/internal/tui/markdown"
	"github.com/haowang02/agent-session-cleaner/internal/tui/theme"
)

func renderer(t *testing.T, width int) *markdown.Renderer {
	t.Helper()
	r, err := markdown.New(theme.Colors(true), true, width)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	return r
}

// read is what a person would see on the terminal, one line per line.
func read(lines []string) []string {
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = strings.TrimRight(ansi.Strip(line), " ")
	}
	return plain
}

// A line wider than the pane is not clipped by anything downstream — it pushes
// the divider and the list beside it out of true.
func TestEveryLineFitsTheWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
	}{
		{"prose", "A reply of the ordinary kind, long enough to need wrapping somewhere."},
		{
			// No spaces to break at, so this can only be broken by width.
			name: "a Chinese paragraph",
			text: "好的，我来解释一下这个问题。这里是一段很长的中文段落，中间完全没有空格。",
		},
		{"one unbreakable token", "See https://example.com/" + strings.Repeat("a", 200) + "/end."},
		{"a table", "| column one | column two |\n|---|---|\n| a value | another value |"},
		{"a code block", "```go\nfunc main() { fmt.Println(\"a fairly long line of source\") }\n```"},
		{"a quotation", "> " + strings.Repeat("quoted words ", 20)},
		{"a nested list", "- one\n  - two\n    - three with rather a lot of text after it\n"},
		{"a heading", "# " + strings.Repeat("long ", 30)},
		{"a rule", "before\n\n---\n\nafter"},
	}

	for _, width := range []int{12, 40, 79} {
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()
				for i, line := range renderer(t, width).Lines(test.text) {
					if got := ansi.StringWidth(line); got > width {
						t.Errorf("width %d: line %d is %d cells wide:\n%s",
							width, i, got, ansi.Strip(line))
					}
				}
			})
		}
	}
}

func TestMarkupIsReplacedByStyling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		want    string
		unwant  string
		styling bool
	}{
		{name: "bold", text: "a **strong** word", want: "a strong word", unwant: "**", styling: true},
		{name: "emphasis", text: "an *emphatic* word", want: "an emphatic word", unwant: "*", styling: true},
		{name: "inline code", text: "the `symbol` here", want: "the symbol here", unwant: "`", styling: true},
		{name: "a heading", text: "## Heading", want: "## Heading", styling: true},
		{name: "a bullet", text: "- item", want: "• item", unwant: "- item"},
		{name: "a numbered item", text: "1. item", want: "1. item"},
		{name: "a task", text: "- [x] done", want: "[✓] done", unwant: "[x]"},
		{name: "plain text is left alone", text: "nothing to do here", want: "nothing to do here"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lines := renderer(t, 40).Lines(test.text)
			got := strings.Join(read(lines), "\n")
			if !strings.Contains(got, test.want) {
				t.Errorf("rendered %q as %q, want it to contain %q", test.text, got, test.want)
			}
			if test.unwant != "" && strings.Contains(got, test.unwant) {
				t.Errorf("rendered %q as %q, want the %q gone", test.text, got, test.unwant)
			}
			if test.styling && strings.Join(lines, "") == got {
				t.Errorf("rendered %q with no styling at all", test.text)
			}
		})
	}
}

// Most of a transcript is not Markdown, and a message somebody laid out by
// hand has to survive being shown. Without preserved newlines every one of
// these lines is swept into a single paragraph.
func TestSingleNewlinesAreKept(t *testing.T) {
	t.Parallel()

	lines := read(renderer(t, 40).Lines("Line one\nLine two\nLine three"))
	if want := []string{"Line one", "Line two", "Line three"}; !slices.Equal(lines, want) {
		t.Errorf("rendered %q, want %q", lines, want)
	}
}

// A message is clipped to a fixed number of characters before it ever gets
// here, so the fence it opened may well have no end.
func TestAnUnclosedCodeFenceKeepsItsContents(t *testing.T) {
	t.Parallel()

	got := strings.Join(read(renderer(t, 40).Lines("Here:\n\n```go\nfunc main() {\n\tprintln(1)")), "\n")
	for _, want := range []string{"Here:", "func main()", "println(1)"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered:\n%s\nwant it to contain %q", got, want)
		}
	}
}

func TestRawHTMLFallsBackWithoutLosingText(t *testing.T) {
	t.Parallel()

	lines := renderer(t, 40).Lines("Use the <Button> component with **care**.")
	got := strings.Join(read(lines), "\n")
	if !strings.Contains(got, "<Button>") || !strings.Contains(got, "**care**") {
		t.Errorf("raw HTML message was not preserved: %q", got)
	}
}

func TestTerminalControlsAreRemoved(t *testing.T) {
	t.Parallel()

	source := "\x1b[31mred\x1b[0m\tand\x00\x07 \x1b]8;;https://x\x07link\x1b]8;;\x07"
	got := strings.Join(read(renderer(t, 80).Lines(source)), "\n")
	if got != "red    and link" {
		t.Errorf("rendered hostile controls as %q", got)
	}
}

func TestNothingToSayRendersNothing(t *testing.T) {
	t.Parallel()

	r := renderer(t, 40)
	for _, text := range []string{"", "   ", "\n\n\t\n"} {
		if lines := r.Lines(text); len(lines) != 0 {
			t.Errorf("Lines(%q) = %q, want none", text, lines)
		}
		if lines := markdown.Plain(text, 40); len(lines) != 0 {
			t.Errorf("Plain(%q) = %q, want none", text, lines)
		}
	}
}

// Chroma keeps its styles in one registry for the whole program, so the two
// palettes have to be able to coexist: whichever terminal answered first must
// not decide the colour of code for the other.
func TestTheTwoPalettesColourCodeDifferently(t *testing.T) {
	t.Parallel()

	const code = "```go\nvar x = 1\n```"
	rendered := func(dark bool) string {
		r, err := markdown.New(theme.Colors(dark), dark, 40)
		if err != nil {
			t.Fatalf("New(dark=%v) = %v", dark, err)
		}
		return strings.Join(r.Lines(code), "\n")
	}

	dark, light := rendered(true), rendered(false)
	if dark == light {
		t.Error("a code block looks the same on a dark and a light terminal")
	}
	for name, out := range map[string]string{"dark": dark, "light": light} {
		want := "var"
		if !strings.Contains(ansi.Strip(out), want) {
			t.Errorf("%s rendering lost the code: %q", name, ansi.Strip(out))
		}
		if !strings.Contains(out, "\x1b[") {
			t.Errorf("%s rendering has no syntax colouring", name)
		}
	}
}

func TestPlainInterpretsNothing(t *testing.T) {
	t.Parallel()

	lines := markdown.Plain("a **strong** word and a `symbol`", 40)
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, "**strong**") || !strings.Contains(got, "`symbol`") {
		t.Errorf("Plain() interpreted its input: %q", got)
	}
	for i, line := range markdown.Plain(strings.Repeat("word ", 40), 20) {
		if got := ansi.StringWidth(line); got > 20 {
			t.Errorf("line %d is %d cells wide", i, got)
		}
	}
}

// A pane can be asked for at no width at all while the terminal is being
// resized, and wrapping to zero cells would be a division by nothing.
func TestAWidthBelowOneIsTakenAsOne(t *testing.T) {
	t.Parallel()

	for _, width := range []int{0, -1, -80} {
		r, err := markdown.New(theme.Colors(true), true, width)
		if err != nil {
			t.Fatalf("New(%d) = %v", width, err)
		}
		lines := r.Lines("hello")
		if len(lines) == 0 {
			t.Errorf("New(%d) rendered nothing", width)
		}
		for i, line := range lines {
			if got := ansi.StringWidth(line); got != 1 {
				t.Errorf("New(%d): line %d is %d cells, want 1", width, i, got)
			}
		}
		if got := markdown.Plain("hello", width); len(got) == 0 {
			t.Errorf("Plain(%d) returned nothing", width)
		}
	}
}
