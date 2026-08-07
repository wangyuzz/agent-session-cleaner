// Package theme is the style sheet.
//
// Every colour and glyph the interface uses is named here, once, so the look
// can be read in one place and changed without hunting through render code.
//
// The palette is Tokyo Night — moon for a dark terminal, day for a light one —
// which is what LazyVim ships with and what a lot of terminals are already
// wearing. Unlike the rest of the interface's history, the screen is painted
// rather than left transparent: a two-pane list needs its panes to have edges.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// Palette is one Tokyo Night variant, named the way the upstream theme names
// its colours so the two can be compared.
//
// Hex strings also let non-Lip Gloss renderers use the same colours.
//
// What each accent is allowed to mean, and nothing else:
//
//	blue    where you are — the cursor, the keys of the interface, the wordmark
//	green   affirmative — what you picked out, and what went through
//	red     what cannot be undone, and what failed
//	yellow  left behind by accident, or set aside
//	orange  a key you can press, and the match a search found
//	cyan    a count or an identifier
//
// The row fills are the one place the exact numbers matter, because a fill is
// read against the background rather than on its own. Both ramps hold their
// hue at roughly twice and four times the background's luminance: dark enough
// to stay a background, saturated enough not to silt up into grey.
type Palette struct {
	Bg, BgDark, BgFooter        string
	BgCursor, BgCursorIdle      string
	BgPicked, BgPickedCursor    string
	Fg, FgDark, Comment, Gutter string
	Blue, Cyan                  string
	Orange, Yellow, Green       string
	Red, RedFill, OnRedFill     string
}

// Colors is the palette a dark or light terminal is dressed in.
func Colors(dark bool) Palette {
	if dark {
		return moon
	}
	return day
}

// moon and day are the two variants, kept side by side so a colour can never
// be changed in one and forgotten in the other.
var (
	moon = Palette{
		Bg: "#222436", BgDark: "#1e2030", BgFooter: "#1b1d2b",
		BgCursor: "#2d3f76", BgCursorIdle: "#26314f",
		BgPicked: "#244a2d", BgPickedCursor: "#2e6039",
		Fg: "#c8d3f5", FgDark: "#828bb8", Comment: "#636da6", Gutter: "#545c7e",
		Blue: "#82aaff", Cyan: "#86e1fc",
		Orange: "#ff966c", Yellow: "#ffc777", Green: "#c3e88d",
		Red: "#ff757f", RedFill: "#c53b53", OnRedFill: "#ffffff",
	}
	day = Palette{
		Bg: "#e1e2e7", BgDark: "#d0d5e3", BgFooter: "#c8cddd",
		BgCursor: "#b6bfe2", BgCursorIdle: "#ccd2e8",
		BgPicked: "#c9e3bb", BgPickedCursor: "#aed596",
		Fg: "#3760bf", FgDark: "#6172b0", Comment: "#848cb5", Gutter: "#a8aecb",
		Blue: "#2e7de9", Cyan: "#007197",
		Orange: "#b15c00", Yellow: "#8c6c3e", Green: "#587539",
		// Deeper than Tokyo Night day's own #f52a65, which is barely legible
		// as text on the bars this one has to be read on.
		Red: "#a4243b", RedFill: "#c64343", OnRedFill: "#ffffff",
	}
)

// Theme is a resolved style sheet.
type Theme struct {
	Dark bool
	// Colors exposes the source palette to non-Lip Gloss renderers.
	Colors Palette

	// Screen is the background everything that is not a bar sits on. Anything
	// drawn outside a [text.Line] has to carry it explicitly.
	Screen lipgloss.Style

	// Everyday text, as foregrounds only: these go on top of whatever bar or
	// row fill they land in, so setting a background here would fight it.
	Text  lipgloss.Style
	Muted lipgloss.Style
	Faint lipgloss.Style
	// Faded is the one colour the screen behind a dialog is flattened to.
	Faded color.Color

	// The banner: a bar carrying an agent pill, some counts, and the mode that
	// changes what the keys do. The bar itself never changes colour — a whole
	// line of saturated red fights everything around it. The mode shows in the
	// pill, which is the smallest thing on the line that cannot be missed.
	Banner     lipgloss.Style
	Pill       lipgloss.Style
	PillSelect lipgloss.Style
	PillDanger lipgloss.Style
	Mode       lipgloss.Style
	ModeDanger lipgloss.Style

	// The session list. The Row styles are backgrounds for the whole line; the
	// rest are foregrounds drawn on top of one of them.
	RowCursor       lipgloss.Style
	RowCursorIdle   lipgloss.Style
	RowPicked       lipgloss.Style
	RowPickedCursor lipgloss.Style
	Cursor          lipgloss.Style
	Picked          lipgloss.Style
	Day             lipgloss.Style
	Clock           lipgloss.Style
	Project         lipgloss.Style
	Guide           lipgloss.Style
	Client          lipgloss.Style
	Title           lipgloss.Style
	Archived        lipgloss.Style
	Orphan          lipgloss.Style
	Highlight       lipgloss.Style

	// The conversation pane.
	DetailTitle lipgloss.Style
	DetailMeta  lipgloss.Style
	ArchivedTag lipgloss.Style
	OrphanTag   lipgloss.Style
	UserTag     lipgloss.Style
	AgentTag    lipgloss.Style
	UserBar     lipgloss.Style
	AgentBar    lipgloss.Style
	Body        lipgloss.Style
	Truncated   lipgloss.Style
	Placeholder lipgloss.Style

	// The status line, and the divider between the panes. The divider is how
	// the interface says which pane the keys are talking to.
	Status       lipgloss.Style
	StatusOK     lipgloss.Style
	StatusError  lipgloss.Style
	Divider      lipgloss.Style
	DividerFocus lipgloss.Style

	// The search bar.
	SearchBar lipgloss.Style
	Sigil     lipgloss.Style

	// The footer, on a bar a shade below the others so the two do not read as
	// one three-line block.
	Footer  lipgloss.Style
	Key     lipgloss.Style
	KeyDesc lipgloss.Style

	// Dialogs, and the opening screen. Float is the inside of a box, which
	// every line of one has to carry: a Lip Gloss background set on the box
	// itself stops at the first reset sequence within a line.
	Float        lipgloss.Style
	Modal        lipgloss.Style
	ModalDanger  lipgloss.Style
	ModalTitle   lipgloss.Style
	ModalWarning lipgloss.Style
	Button       lipgloss.Style
	ButtonFocus  lipgloss.Style
	ButtonDanger lipgloss.Style
	Logo         lipgloss.Style
	LogoShadow   lipgloss.Style
	Shortcut     lipgloss.Style
	Spark        lipgloss.Style
	Count        lipgloss.Style
}

// New resolves the style sheet for a dark or light terminal.
func New(dark bool) Theme {
	p := Colors(dark)
	hue := func(hex string) color.Color { return lipgloss.Color(hex) }

	plain := lipgloss.NewStyle()
	// Two bases. Anything inside a pane is drawn on the screen; anything on a
	// bar is drawn on that bar. Foreground-only styles belong to neither and
	// take whatever they are rendered into.
	on := plain.Background(hue(p.Bg))
	bar := plain.Background(hue(p.BgDark))
	foot := plain.Background(hue(p.BgFooter))
	float := plain.Background(hue(p.BgDark))
	pill := func(background string) lipgloss.Style {
		return plain.Background(hue(background)).Foreground(hue(p.BgDark)).Bold(true)
	}

	return Theme{
		Dark:   dark,
		Colors: p,
		Screen: on,

		Text:  plain.Foreground(hue(p.Fg)),
		Muted: plain.Foreground(hue(p.FgDark)),
		Faint: plain.Foreground(hue(p.Comment)),
		Faded: hue(p.Comment),

		Banner: bar.Foreground(hue(p.Fg)),
		// One pill, three accents. Each is the light member of its pair, so
		// they all take the same dark text and read as the same object in a
		// different state rather than as three different things.
		Pill:       pill(p.Blue),
		PillSelect: pill(p.Green),
		PillDanger: pill(p.Red),
		Mode:       plain.Foreground(hue(p.Green)).Bold(true),
		ModeDanger: plain.Foreground(hue(p.Red)).Bold(true),

		// Where the cursor stands and what has been picked out are the two
		// things read off this list continuously, so both are a fill across
		// the whole row. Blue is the cursor everywhere in the interface and
		// green is the selection everywhere in the interface, which is why the
		// two coincide on one row as the brighter green rather than as some
		// third colour that would have to be learned. The hues are far enough
		// apart that neither depends on being the lighter of the two.
		RowCursor:       plain.Background(hue(p.BgCursor)),
		RowCursorIdle:   plain.Background(hue(p.BgCursorIdle)),
		RowPicked:       plain.Background(hue(p.BgPicked)),
		RowPickedCursor: plain.Background(hue(p.BgPickedCursor)),

		// The gutter marks repeat what the fills already say, for terminals
		// that render background colour poorly and for eyes that do not
		// separate these hues.
		Cursor:  plain.Foreground(hue(p.Blue)).Bold(true),
		Picked:  plain.Foreground(hue(p.Green)).Bold(true),
		Day:     plain.Foreground(hue(p.Fg)),
		Clock:   plain.Foreground(hue(p.Comment)),
		Project: plain.Foreground(hue(p.Cyan)),
		Guide:   plain.Foreground(hue(p.Gutter)),
		Client:  plain.Foreground(hue(p.FgDark)),
		Title:   plain.Foreground(hue(p.Fg)),
		// Archived sessions were set aside deliberately; orphaned sub-agents
		// were left behind by accident. Fade the one, flag the other.
		Archived: plain.Foreground(hue(p.Comment)),
		Orphan:   plain.Foreground(hue(p.Yellow)).Italic(true),
		// A background of its own, so it survives whatever row it lands on.
		// Orange on dark is what Tokyo Night uses for the match under the
		// cursor, and nothing else here is a filled block of it.
		Highlight: plain.Background(hue(p.Orange)).Foreground(hue(p.BgDark)),

		DetailTitle: on.Foreground(hue(p.Fg)).Bold(true),
		DetailMeta:  on.Foreground(hue(p.FgDark)),
		ArchivedTag: on.Foreground(hue(p.Yellow)).Bold(true),
		OrphanTag:   on.Foreground(hue(p.Yellow)),
		UserTag:     on.Foreground(hue(p.Green)).Bold(true),
		AgentTag:    on.Foreground(hue(p.Blue)).Bold(true),
		UserBar:     on.Foreground(hue(p.Green)),
		AgentBar:    on.Foreground(hue(p.Blue)),
		Body:        on.Foreground(hue(p.Fg)),
		Truncated:   on.Foreground(hue(p.Comment)).Italic(true),
		Placeholder: on.Foreground(hue(p.Comment)),

		// The bar stays the colour every other bar is; only the words on it
		// change. A status line that repaints itself green and red is louder
		// than the message it carries, and it is the line that changes most.
		Status:       bar.Foreground(hue(p.FgDark)),
		StatusOK:     bar.Foreground(hue(p.Green)),
		StatusError:  bar.Foreground(hue(p.Red)).Bold(true),
		Divider:      on.Foreground(hue(p.Gutter)),
		DividerFocus: on.Foreground(hue(p.Blue)),

		SearchBar: bar.Foreground(hue(p.Fg)),
		Sigil:     bar.Foreground(hue(p.Blue)).Bold(true),

		// Orange keys against muted descriptions, the way LazyVim's own
		// dashboard puts its shortcuts.
		Footer:  foot.Foreground(hue(p.FgDark)),
		Key:     foot.Foreground(hue(p.Orange)).Bold(true),
		KeyDesc: foot.Foreground(hue(p.FgDark)),

		Float:        float,
		Modal:        float.Border(lipgloss.RoundedBorder()).BorderForeground(hue(p.Blue)).BorderBackground(hue(p.BgDark)).Padding(1, 2),
		ModalDanger:  float.Border(lipgloss.RoundedBorder()).BorderForeground(hue(p.Red)).BorderBackground(hue(p.BgDark)).Padding(1, 2),
		ModalTitle:   plain.Foreground(hue(p.Red)).Bold(true),
		ModalWarning: plain.Foreground(hue(p.FgDark)),
		Button:       float.Foreground(hue(p.FgDark)).Padding(0, 2),
		ButtonFocus:  plain.Background(hue(p.Blue)).Foreground(hue(p.BgDark)).Bold(true).Padding(0, 2),
		// The deeper red, not the bright one the text uses: a whole filled
		// button in signal red shouts over the question it is answering.
		ButtonDanger: plain.Background(hue(p.RedFill)).Foreground(hue(p.OnRedFill)).Bold(true).Padding(0, 2),

		Logo:       plain.Foreground(hue(p.Blue)),
		LogoShadow: plain.Foreground(hue(p.Gutter)),
		Shortcut:   plain.Foreground(hue(p.Orange)),
		Spark:      plain.Foreground(hue(p.Yellow)),
		Count:      plain.Foreground(hue(p.Cyan)),
	}
}

// Gutter markers, in the two cells every row reserves on its left.
const (
	// CursorMark shows which row the keys act on.
	CursorMark = "▌"
	// PickedMark shows a row picked out for a bulk action.
	PickedMark = "◆"
	// Blank fills a gutter cell that has nothing to say.
	Blank = " "
)

// Guide is the glyph pair for one cell of the tree drawing.
func Guide(g session.Guide) string {
	switch g {
	case session.GuideTrunk:
		return "│ "
	case session.GuideBranch:
		return "├─"
	case session.GuideLast:
		return "╰─"
	case session.GuideSevered:
		// The link to the conversation that spawned this one is broken.
		return "╌╌"
	case session.GuideUnknown:
		// A sub-agent that never recorded where it came from: neither rooted
		// nor provably stranded.
		return "··"
	default:
		return "  "
	}
}
