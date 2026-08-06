// Package session holds the agent-neutral view of a session tree.
//
// Every agent stores its history differently. Each one resolves its own
// quirks — where a title hides, what counts as noise, whether archiving
// exists at all — and hands back the types here. Nothing in this package
// touches the filesystem, runs a command, or renders anything, so the rules
// that decide what an action really affects can be tested on their own.
package session

import (
	"strings"
	"time"
	"unicode/utf8"
)

// MaxMessageChars caps one message. A single transcript line can be megabytes
// of base64 image or tool output, and none of it belongs on screen.
const MaxMessageChars = 8000

// MaxTitleChars caps a title, which is drawn from whatever the user typed
// first and is otherwise unbounded.
const MaxTitleChars = 160

// Session is one conversation, as the list shows it.
type Session struct {
	// Agent is the id of the agent this belongs to.
	Agent string
	// Path is where it lives: a transcript file, or the database holding it.
	Path string
	// ID is what the agent's own command line calls this session.
	ID string
	// Title is the first thing said, a generated summary, or a name the user
	// assigned — whichever the agent could offer.
	Title string
	// Client is what started it: a CLI, an editor extension, a sub-agent.
	Client string
	// UpdatedAt is the last write. See [Session.RecencyAt] before ordering by it.
	UpdatedAt time.Time
	// CreatedAt is when the session started; zero when the agent did not record it.
	CreatedAt time.Time
	// Size is roughly how much this session occupies, for cache keying.
	Size int64
	// Archived is set for sessions the user put aside.
	Archived bool
	// Noise marks a side thread or an empty session: nothing a person
	// intentionally started.
	Noise bool
	// SideThread marks a session the agent spawned rather than one a person
	// opened. An empty Parent does not disprove it: the link may simply never
	// have been recorded. See [Forest.Orphans].
	SideThread bool
	// Parent is the session that spawned this one, when the agent records it.
	Parent string
	// Cwd is the directory the session ran in.
	Cwd string
	// Version is the agent release that wrote it.
	Version string
}

// RecencyAt is when this session started, for ordering and display.
//
// The recorded start time is preferred over the last write. An upgrade or a
// migration can rewrite a whole session tree and collapse every mtime onto one
// instant, while the original start times stay put.
func (s Session) RecencyAt() time.Time {
	if !s.CreatedAt.IsZero() {
		return s.CreatedAt
	}
	return s.UpdatedAt
}

// Role says who spoke.
type Role uint8

const (
	// User is the person at the keyboard.
	User Role = iota
	// Assistant is the agent replying.
	Assistant
)

// Message is one turn of a conversation, with the tool traffic already gone.
type Message struct {
	Role Role
	Text string
	// Images counts attachments that cannot be shown in a terminal.
	Images int
	// Truncated is how many characters were dropped off the end.
	Truncated int
}

// NewMessage trims a turn to what a terminal can show. The second result is
// false for a turn with nothing in it, which the caller should drop.
func NewMessage(role Role, text string, images int) (Message, bool) {
	text = strings.TrimSpace(text)
	if text == "" && images == 0 {
		return Message{}, false
	}
	message := Message{Role: role, Text: text, Images: images}
	if n := utf8.RuneCountInString(text); n > MaxMessageChars {
		message.Text = string([]rune(text)[:MaxMessageChars])
		message.Truncated = n - MaxMessageChars
	}
	return message, true
}

// Condense collapses whitespace and clips to limit, which is what a
// single-line title has room for.
func Condense(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	condensed := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(condensed) <= limit {
		return condensed
	}
	return string([]rune(condensed)[:limit-1]) + "…"
}
