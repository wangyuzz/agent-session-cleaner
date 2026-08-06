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

// palette is one Tokyo Night variant, named the way the upstream theme names
// its colours so the two can be compared.
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
type palette struct {
	bg, bgDark, bgFooter        string
	bgCursor, bgCursorIdle      string
	bgPicked, bgPickedCursor    string
	fg, fgDark, comment, gutter string
	blue, cyan                  string
	orange, yellow, green       string
	red, redFill, onRedFill     string
}

// moon and day are the two variants, kept side by side so a colour can never
// be changed in one and forgotten in the other.
var (
	moon = palette{
		bg: "#222436", bgDark: "#1e2030", bgFooter: "#1b1d2b",
		bgCursor: "#2d3f76", bgCursorIdle: "#26314f",
		bgPicked: "#244a2d", bgPickedCursor: "#2e6039",
		fg: "#c8d3f5", fgDark: "#828bb8", comment: "#636da6", gutter: "#545c7e",
		blue: "#82aaff", cyan: "#86e1fc",
		orange: "#ff966c", yellow: "#ffc777", green: "#c3e88d",
		red: "#ff757f", redFill: "#c53b53", onRedFill: "#ffffff",
	}
	day = palette{
		bg: "#e1e2e7", bgDark: "#d0d5e3", bgFooter: "#c8cddd",
		bgCursor: "#b6bfe2", bgCursorIdle: "#ccd2e8",
		bgPicked: "#c9e3bb", bgPickedCursor: "#aed596",
		fg: "#3760bf", fgDark: "#6172b0", comment: "#848cb5", gutter: "#a8aecb",
		blue: "#2e7de9", cyan: "#007197",
		orange: "#b15c00", yellow: "#8c6c3e", green: "#587539",
		// Deeper than Tokyo Night day's own #f52a65, which is barely legible
		// as text on the bars this one has to be read on.
		red: "#a4243b", redFill: "#c64343", onRedFill: "#ffffff",
	}
)

// Theme is a resolved style sheet.
type Theme struct {
	Dark bool

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
	p := day
	if dark {
		p = moon
	}
	hue := func(hex string) color.Color { return lipgloss.Color(hex) }

	plain := lipgloss.NewStyle()
	// Two bases. Anything inside a pane is drawn on the screen; anything on a
	// bar is drawn on that bar. Foreground-only styles belong to neither and
	// take whatever they are rendered into.
	on := plain.Background(hue(p.bg))
	bar := plain.Background(hue(p.bgDark))
	foot := plain.Background(hue(p.bgFooter))
	float := plain.Background(hue(p.bgDark))
	pill := func(background string) lipgloss.Style {
		return plain.Background(hue(background)).Foreground(hue(p.bgDark)).Bold(true)
	}

	return Theme{
		Dark:   dark,
		Screen: on,

		Text:  plain.Foreground(hue(p.fg)),
		Muted: plain.Foreground(hue(p.fgDark)),
		Faint: plain.Foreground(hue(p.comment)),
		Faded: hue(p.comment),

		Banner: bar.Foreground(hue(p.fg)),
		// One pill, three accents. Each is the light member of its pair, so
		// they all take the same dark text and read as the same object in a
		// different state rather than as three different things.
		Pill:       pill(p.blue),
		PillSelect: pill(p.green),
		PillDanger: pill(p.red),
		Mode:       plain.Foreground(hue(p.green)).Bold(true),
		ModeDanger: plain.Foreground(hue(p.red)).Bold(true),

		// Where the cursor stands and what has been picked out are the two
		// things read off this list continuously, so both are a fill across
		// the whole row. Blue is the cursor everywhere in the interface and
		// green is the selection everywhere in the interface, which is why the
		// two coincide on one row as the brighter green rather than as some
		// third colour that would have to be learned. The hues are far enough
		// apart that neither depends on being the lighter of the two.
		RowCursor:       plain.Background(hue(p.bgCursor)),
		RowCursorIdle:   plain.Background(hue(p.bgCursorIdle)),
		RowPicked:       plain.Background(hue(p.bgPicked)),
		RowPickedCursor: plain.Background(hue(p.bgPickedCursor)),

		// The gutter marks repeat what the fills already say, for terminals
		// that render background colour poorly and for eyes that do not
		// separate these hues.
		Cursor:  plain.Foreground(hue(p.blue)).Bold(true),
		Picked:  plain.Foreground(hue(p.green)).Bold(true),
		Day:     plain.Foreground(hue(p.fg)),
		Clock:   plain.Foreground(hue(p.comment)),
		Project: plain.Foreground(hue(p.cyan)),
		Guide:   plain.Foreground(hue(p.gutter)),
		Client:  plain.Foreground(hue(p.fgDark)),
		Title:   plain.Foreground(hue(p.fg)),
		// Archived sessions were set aside deliberately; orphaned sub-agents
		// were left behind by accident. Fade the one, flag the other.
		Archived: plain.Foreground(hue(p.comment)),
		Orphan:   plain.Foreground(hue(p.yellow)).Italic(true),
		// A background of its own, so it survives whatever row it lands on.
		// Orange on dark is what Tokyo Night uses for the match under the
		// cursor, and nothing else here is a filled block of it.
		Highlight: plain.Background(hue(p.orange)).Foreground(hue(p.bgDark)),

		DetailTitle: on.Foreground(hue(p.fg)).Bold(true),
		DetailMeta:  on.Foreground(hue(p.fgDark)),
		ArchivedTag: on.Foreground(hue(p.yellow)).Bold(true),
		OrphanTag:   on.Foreground(hue(p.yellow)),
		UserTag:     on.Foreground(hue(p.green)).Bold(true),
		AgentTag:    on.Foreground(hue(p.blue)).Bold(true),
		UserBar:     on.Foreground(hue(p.green)),
		AgentBar:    on.Foreground(hue(p.blue)),
		Body:        on.Foreground(hue(p.fg)),
		Truncated:   on.Foreground(hue(p.comment)).Italic(true),
		Placeholder: on.Foreground(hue(p.comment)),

		// The bar stays the colour every other bar is; only the words on it
		// change. A status line that repaints itself green and red is louder
		// than the message it carries, and it is the line that changes most.
		Status:       bar.Foreground(hue(p.fgDark)),
		StatusOK:     bar.Foreground(hue(p.green)),
		StatusError:  bar.Foreground(hue(p.red)).Bold(true),
		Divider:      on.Foreground(hue(p.gutter)),
		DividerFocus: on.Foreground(hue(p.blue)),

		SearchBar: bar.Foreground(hue(p.fg)),
		Sigil:     bar.Foreground(hue(p.blue)).Bold(true),

		// Orange keys against muted descriptions, the way LazyVim's own
		// dashboard puts its shortcuts.
		Footer:  foot.Foreground(hue(p.fgDark)),
		Key:     foot.Foreground(hue(p.orange)).Bold(true),
		KeyDesc: foot.Foreground(hue(p.fgDark)),

		Float:        float,
		Modal:        float.Border(lipgloss.RoundedBorder()).BorderForeground(hue(p.blue)).BorderBackground(hue(p.bgDark)).Padding(1, 2),
		ModalDanger:  float.Border(lipgloss.RoundedBorder()).BorderForeground(hue(p.red)).BorderBackground(hue(p.bgDark)).Padding(1, 2),
		ModalTitle:   plain.Foreground(hue(p.red)).Bold(true),
		ModalWarning: plain.Foreground(hue(p.fgDark)),
		Button:       float.Foreground(hue(p.fgDark)).Padding(0, 2),
		ButtonFocus:  plain.Background(hue(p.blue)).Foreground(hue(p.bgDark)).Bold(true).Padding(0, 2),
		// The deeper red, not the bright one the text uses: a whole filled
		// button in signal red shouts over the question it is answering.
		ButtonDanger: plain.Background(hue(p.redFill)).Foreground(hue(p.onRedFill)).Bold(true).Padding(0, 2),

		Logo:       plain.Foreground(hue(p.blue)),
		LogoShadow: plain.Foreground(hue(p.gutter)),
		Shortcut:   plain.Foreground(hue(p.orange)),
		Spark:      plain.Foreground(hue(p.yellow)),
		Count:      plain.Foreground(hue(p.cyan)),
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
