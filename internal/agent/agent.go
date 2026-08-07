// Package agent is the contract between the interface and the coding agents
// whose history it manages.
//
// Each agent stores sessions its own way and offers its own operations. What
// varies is expressed as capability — an agent that cannot archive simply does
// not implement [Archiver], and the keys for it are never shown — rather than
// as flags the interface has to remember to consult.
package agent

import (
	"context"
	"errors"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// Meta is everything the interface needs to know about an agent that does not
// require reading its session tree.
type Meta struct {
	// ID is the name on the command line, and in [session.Session.Agent].
	ID string
	// Label names the agent to the user.
	Label string
	// Reply is how the agent signs its turns in the conversation view.
	Reply string
	// Shortcut picks this agent in the chooser.
	Shortcut rune
	// Home is where this agent keeps its state.
	Home string
	// DefaultClient is the client so common for this agent that naming it on
	// every row would be noise.
	DefaultClient string
	// EmptyLabel names what the "delete empty sessions" key sweeps. Zero means
	// this agent cannot have empty sessions, and the key is hidden.
	EmptyLabel i18n.Key
	// OrphanLabel names what the "delete orphans" key sweeps. Zero means this
	// agent cannot leave orphans behind, and the key is hidden.
	OrphanLabel i18n.Key
	// BulkConcurrency is how many sessions of a batch this agent's own tooling
	// will tolerate being worked on at once. Everything an agent stores is
	// shared state, and each agent knows what its own tooling stands up to.
	BulkConcurrency int
}

// Agent is one coding agent's session store.
type Agent interface {
	Meta() Meta

	// Preflight refuses a session tree that could never be worked with,
	// before anything is listed. An agent whose command line locates its own
	// data has a say in what it will accept being pointed at, and the honest
	// moment to say so is not once a deletion is already under way.
	Preflight() error

	// Writable reports whether sessions can be changed here. Listing works
	// from the stored data alone; changing anything may need a command that
	// is not installed.
	Writable() bool

	// Discover returns every known session. Order does not matter; the
	// interface arranges them.
	Discover(ctx context.Context) ([]session.Session, error)

	// Messages returns the human side of a conversation, without tool traffic
	// or injected context.
	Messages(ctx context.Context, s session.Session) ([]session.Message, error)

	// Delete removes a session for good. No backup is taken.
	Delete(ctx context.Context, s session.Session) error
}

// Archiver is an agent that can set a session aside without destroying it.
// Only some agents have somewhere to put one.
type Archiver interface {
	Archive(ctx context.Context, s session.Session) error
	Unarchive(ctx context.Context, s session.Session) error
}

// Failures an agent reports that the interface, or a test, may want to
// recognise rather than merely display.
var (
	// ErrSessionGone means the session was already removed by something else.
	ErrSessionGone = errors.New("session no longer exists")
	// ErrIDMismatch means the file on disk belongs to a different session
	// than the listing claims, so nothing was touched.
	ErrIDMismatch = errors.New("session id does not match the file")
	// ErrUnusableHome means this directory can never be worked with.
	ErrUnusableHome = errors.New("unusable session directory")
	// ErrBadSessionID means the id is not one the agent could have minted.
	ErrBadSessionID = errors.New("malformed session id")
	// ErrUnsafeSessionPath means a destructive operation was pointed outside
	// the agent's own session layout or at a symbolic link.
	ErrUnsafeSessionPath = errors.New("unsafe session path")
)
