package markdown

import (
	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"

	"charm.land/glamour/v2/ansi"

	"github.com/haowang02/agent-session-cleaner/internal/tui/theme"
)

// Glamour registers generated Chroma styles under one global name. Registering
// both palettes ourselves prevents the first render from deciding all later
// code-block colours.
const (
	darkCode  = "asc-dark"
	lightCode = "asc-light"
)

func init() {
	chromastyles.Register(codeStyle(darkCode, theme.Colors(true)))
	chromastyles.Register(codeStyle(lightCode, theme.Colors(false)))
}

func codeStyle(name string, p theme.Palette) *chroma.Style {
	return chroma.MustNewStyle(name, chroma.StyleEntries{
		// Foregrounds only. A code block sits on the pane's own background, the
		// way every other kind of block does, and a band of a second colour
		// down one half of a two-pane screen reads as a third pane.
		chroma.Background:          p.Fg,
		chroma.Text:                p.Fg,
		chroma.Error:               p.Red,
		chroma.Comment:             p.Comment + " italic",
		chroma.CommentPreproc:      p.Yellow,
		chroma.Keyword:             p.Blue,
		chroma.KeywordType:         p.Cyan,
		chroma.Operator:            p.Blue,
		chroma.Punctuation:         p.FgDark,
		chroma.Name:                p.Fg,
		chroma.NameBuiltin:         p.Cyan,
		chroma.NameTag:             p.Blue,
		chroma.NameAttribute:       p.Cyan,
		chroma.NameClass:           p.Cyan,
		chroma.NameConstant:        p.Orange,
		chroma.NameDecorator:       p.Yellow,
		chroma.NameException:       p.Red,
		chroma.NameFunction:        p.Cyan,
		chroma.Literal:             p.Orange,
		chroma.LiteralString:       p.Green,
		chroma.LiteralStringEscape: p.Yellow,
		chroma.GenericDeleted:      p.Red,
		chroma.GenericInserted:     p.Green,
		chroma.GenericEmph:         p.Fg + " italic",
		chroma.GenericStrong:       p.Fg + " bold",
		chroma.GenericSubheading:   p.FgDark,
	})
}

func styleSheet(p theme.Palette, dark bool) ansi.StyleConfig {
	code := darkCode
	if !dark {
		code = lightCode
	}
	hue := func(hex string) *string { return &hex }
	yes := func() *bool { b := true; return &b }
	size := func(n uint) *uint { return &n }
	token := func(s string) *string { return &s }

	heading := ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{
		BlockSuffix: "\n",
		Color:       hue(p.Fg),
		Bold:        yes(),
	}}
	prefixed := func(prefix string) ansi.StyleBlock {
		return ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: prefix}}
	}

	return ansi.StyleConfig{
		// No margin and no surrounding blank lines: the pane supplies its own
		// gutter and spacing, and a document that indents itself would leave
		// the conversation's left edge ragged.
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: hue(p.Fg)},
			Margin:         size(0),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: hue(p.FgDark), Italic: yes()},
			Indent:         size(1),
			IndentToken:    token("│ "),
		},
		Paragraph: ansi.StyleBlock{},
		List: ansi.StyleList{
			StyleBlock:  ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: hue(p.Fg)}},
			LevelIndent: 2,
		},

		Heading: heading,
		H1:      prefixed("# "),
		H2:      prefixed("## "),
		H3:      prefixed("### "),
		H4:      prefixed("#### "),
		H5:      prefixed("##### "),
		H6:      prefixed("###### "),

		Text:           ansi.StylePrimitive{},
		Strikethrough:  ansi.StylePrimitive{CrossedOut: yes()},
		Emph:           ansi.StylePrimitive{Italic: yes()},
		Strong:         ansi.StylePrimitive{Bold: yes()},
		HorizontalRule: ansi.StylePrimitive{Color: hue(p.Gutter), Format: "\n────────\n"},

		Item:        ansi.StylePrimitive{BlockPrefix: "• ", Color: hue(p.FgDark)},
		Enumeration: ansi.StylePrimitive{BlockPrefix: ". ", Color: hue(p.FgDark)},
		Task: ansi.StyleTask{
			StylePrimitive: ansi.StylePrimitive{Color: hue(p.Green)},
			Ticked:         "[✓] ",
			Unticked:       "[ ] ",
		},

		Link:     ansi.StylePrimitive{Color: hue(p.Comment), Underline: yes()},
		LinkText: ansi.StylePrimitive{Color: hue(p.FgDark)},

		Image:     ansi.StylePrimitive{Color: hue(p.Comment), Underline: yes()},
		ImageText: ansi.StylePrimitive{Color: hue(p.FgDark), Format: "Image: {{.text}} →"},

		Code: ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: hue(p.Cyan)}},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: hue(p.Fg)},
				Margin:         size(0),
			},
			// Named, not built from Chroma: see the note on darkCode.
			Theme: code,
		},

		Table: ansi.StyleTable{
			StyleBlock:      ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: hue(p.Fg)}},
			CenterSeparator: token("┼"),
			ColumnSeparator: token("│"),
			RowSeparator:    token("─"),
		},

		DefinitionList:        ansi.StyleBlock{},
		DefinitionTerm:        ansi.StylePrimitive{Bold: yes()},
		DefinitionDescription: ansi.StylePrimitive{BlockPrefix: "\n ", Color: hue(p.FgDark)},

		HTMLBlock: ansi.StyleBlock{},
		HTMLSpan:  ansi.StyleBlock{},
	}
}
