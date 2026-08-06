// Package text builds one line of styled terminal output.
//
// Lip Gloss styles whole strings; a session row is a run of differently styled
// columns that has to be measured, clipped and searched as a single line. This
// package is that line: spans go in, and one string that fits a given number of
// terminal cells comes out.
//
// Width is counted in cells throughout, never in bytes or runes, so a CJK
// title lines its columns up with an ASCII one.
package text

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Ellipsis marks a line that did not fit.
const Ellipsis = "…"

// Width is how many terminal cells a string occupies.
func Width(s string) int { return ansi.StringWidth(s) }

// Pad clips or pads a string to exactly width cells. A wide character that
// would straddle the edge is dropped, and the gap it leaves is filled.
func Pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if Width(s) > width {
		s = ansi.Truncate(s, width, "")
	}
	if gap := width - Width(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// Fit clips or pads an already-styled string to exactly width cells, painting
// whatever it has to add. [Pad] leaves its padding bare, which shows as a hole
// in a screen that is otherwise painted.
func Fit(s string, width int, style lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	if Width(s) > width {
		s = ansi.Truncate(s, width, "")
	}
	if gap := width - Width(s); gap > 0 {
		s += style.Render(strings.Repeat(" ", gap))
	}
	return s
}

type span struct {
	text  string
	style lipgloss.Style
}

// Line is a sequence of styled spans that renders as one terminal line.
// The zero value is ready to use.
type Line struct {
	spans []span
	fill  lipgloss.Style
	// filled distinguishes "no fill" from "a fill that happens to be the zero
	// style", and is what makes a filled line pad itself out to full width.
	filled bool
}

// Fill paints the whole line, including the empty cells after its last span,
// in one style. Spans keep whatever they set for themselves; the fill only
// supplies what they leave unset, so a search highlight still shows through.
func (l *Line) Fill(style lipgloss.Style) *Line {
	l.fill, l.filled = style, true
	return l
}

// Add appends styled text.
func (l *Line) Add(s string, style lipgloss.Style) *Line {
	if s != "" {
		l.spans = append(l.spans, span{text: s, style: style})
	}
	return l
}

// Cell appends text occupying exactly width cells, which is how columns stay
// aligned across rows.
func (l *Line) Cell(s string, width int, style lipgloss.Style) *Line {
	return l.Add(Pad(s, width), style)
}

// Space appends n blank cells.
func (l *Line) Space(n int) *Line {
	if n > 0 {
		l.spans = append(l.spans, span{text: strings.Repeat(" ", n)})
	}
	return l
}

// Width is how many cells the whole line occupies.
func (l *Line) Width() int {
	total := 0
	for _, s := range l.spans {
		total += Width(s.text)
	}
	return total
}

// Plain is the line without any styling, for searching and for tests.
func (l *Line) Plain() string {
	var out strings.Builder
	for _, s := range l.spans {
		out.WriteString(s.text)
	}
	return out.String()
}

// Highlight restyles every occurrence of query, wherever it falls across the
// spans. The highlight's own settings win; anything it leaves unset — a bold
// title, say — shows through.
func (l *Line) Highlight(query string, caseSensitive bool, style lipgloss.Style) {
	if query == "" {
		return
	}
	needle := foldedRunes(query, caseSensitive)

	marked := make([]span, 0, len(l.spans))
	for _, s := range l.spans {
		original := []rune(s.text)
		hay := foldedRunes(s.text, caseSensitive)
		from := 0
		for at := runeIndex(hay, needle, from); at >= 0; at = runeIndex(hay, needle, from) {
			if at > from {
				marked = append(marked, span{text: string(original[from:at]), style: s.style})
			}
			end := at + len(needle)
			marked = append(marked, span{text: string(original[at:end]), style: style.Inherit(s.style)})
			from = end
		}
		if from < len(original) {
			marked = append(marked, span{text: string(original[from:]), style: s.style})
		}
	}
	l.spans = marked
}

func foldedRunes(s string, caseSensitive bool) []rune {
	runes := []rune(s)
	if !caseSensitive {
		for i := range runes {
			runes[i] = unicode.ToLower(runes[i])
		}
	}
	return runes
}

func runeIndex(haystack, needle []rune, start int) int {
	for at := start; at+len(needle) <= len(haystack); at++ {
		match := true
		for i := range needle {
			if haystack[at+i] != needle[i] {
				match = false
				break
			}
		}
		if match {
			return at
		}
	}
	return -1
}

// Render draws the line, clipping it to max cells with an ellipsis when it
// does not fit. Lines never wrap: a long title must not push the next row's
// columns down a line. A filled line is padded out to max, because a row's
// background has to reach the edge of the pane to read as one band.
func (l *Line) Render(max int) string {
	if max <= 0 {
		return ""
	}
	var out strings.Builder
	used := 0
	for _, s := range l.spans {
		style := s.style
		if l.filled {
			style = style.Inherit(l.fill)
		}
		width := Width(s.text)
		if used+width <= max {
			out.WriteString(style.Render(s.text))
			used += width
			continue
		}
		if room := max - used; room > 0 {
			clipped := ansi.Truncate(s.text, room, Ellipsis)
			out.WriteString(style.Render(clipped))
			used += Width(clipped)
		}
		break
	}
	if l.filled && used < max {
		out.WriteString(l.fill.Render(strings.Repeat(" ", max-used)))
	}
	return out.String()
}
