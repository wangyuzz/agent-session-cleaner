// Package opencode manages OpenCode session history.
//
// Layout under $XDG_DATA_HOME/opencode (default ~/.local/share/opencode):
//
//	opencode.db                     sessions, messages and message parts
//	storage/session_diff/<id>.json  per-session diffs left by an old migration
//
// Three things differ from the other agents. Sessions are rows in SQLite rather
// than files, and the whole listing comes from one read-only connection.
// `opencode session delete` already removes a session's children, so deletion
// is delegated to it — but archiving has no such command, so this agent does
// not implement [agent.Archiver] and archived sessions are shown as archived
// and left alone. And a session row only appears once something has been said,
// so there is no such thing as an empty OpenCode session to sweep.
package opencode

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/execx"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// Identity and layout.
const (
	ID     = "opencode"
	Label  = "OpenCode"
	Binary = "opencode"

	// DataDirName is the directory OpenCode appends to XDG_DATA_HOME.
	DataDirName = "opencode"

	databaseFile = "opencode.db"
	// sessionDiffDir was written once by the migration out of the JSON storage
	// era and never cleaned up again: one file per session, named after it.
	sessionDiffDir = "storage/session_diff"

	// defaultAgent runs every session unless it was told otherwise. Sub-agents
	// spawned by the task tool run under their own.
	defaultAgent = "build"
)

// sessionID matches the ids OpenCode mints. Anything else is not something to
// hand to a command line or to build a path from.
var sessionID = regexp.MustCompile(`^ses_[0-9A-Za-z]+$`)

// retryable matches a refusal that is really a queue.
//
// SQLite admits one writer at a time, and OpenCode already waits five seconds
// for the lock. Some releases name the lock; 1.18 reports only "Error:
// Unexpected error" for the same failed query. Both are safe to retry, because
// deleting a session is idempotent.
var retryable = regexp.MustCompile(
	`(?i)database (?:is|table is) locked|SQLITE_BUSY|^Error:\s*Unexpected error$`,
)

// retryDelays spread the retries of a batch out rather than have them collide
// again. Deletion is one transaction, so a refusal leaves nothing half-written.
var retryDelays = []time.Duration{300 * time.Millisecond, time.Second, 2500 * time.Millisecond}

const retryJitter = 250 * time.Millisecond

var _ agent.Agent = (*Agent)(nil)

// Agent is the OpenCode session store.
type Agent struct {
	home      string
	run       execx.Runner
	installed func() bool
}

// Option adjusts an agent, for tests that stand in for the OpenCode command.
type Option func(*Agent)

// WithRunner sends changes to run rather than the real OpenCode command, and
// treats the command as installed.
func WithRunner(run execx.Runner) Option {
	return func(a *Agent) {
		a.run = run
		a.installed = func() bool { return true }
	}
}

// New returns the OpenCode agent for home, or for the default location when
// home is empty.
func New(home string, opts ...Option) *Agent {
	a := &Agent{home: agent.Resolve(home, DefaultHome), run: execx.Run}
	for _, opt := range opts {
		opt(a)
	}
	if a.installed == nil {
		a.installed = func() bool { return execx.Available(Binary) }
	}
	return a
}

// DefaultHome is the directory OpenCode resolves at startup.
func DefaultHome() string {
	if data := os.Getenv("XDG_DATA_HOME"); data != "" {
		return filepath.Join(agent.ExpandHome(data), DataDirName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "share", DataDirName)
	}
	return filepath.Join(home, ".local", "share", DataDirName)
}

// Meta describes OpenCode to the interface.
func (a *Agent) Meta() agent.Meta {
	return agent.Meta{
		ID:            ID,
		Label:         Label,
		Reply:         Label,
		Shortcut:      'o',
		Home:          a.home,
		DefaultClient: defaultAgent,
		// A session row is written when the first message is sent, so an
		// opened and abandoned session leaves nothing behind to clean up.
		EmptyLabel: 0,
		// Deleting a conversation takes its sub-agents with it, so orphans
		// should not arise — but data migrated from the JSON era predates that
		// guarantee.
		OrphanLabel: i18n.OrphanSessions,
		// Every deletion is a write and SQLite serialises those, but most of
		// what a run costs is starting OpenCode, which does overlap: four at a
		// time measured 130ms per session against 470ms one at a time, and no
		// slower than eight. Sixteen starts hitting lock timeouts.
		BulkConcurrency: 4,
	}
}

// Preflight refuses a directory the command line could never be pointed at.
//
// XDG_DATA_HOME names the *parent*: OpenCode appends "opencode" to it. A
// directory by any other name can be read, but a delete command built from it
// would name a sibling directory instead, which is a different session tree.
func (a *Agent) Preflight() error {
	if !dataDirName(filepath.Base(a.home)) {
		return i18n.Wrap(agent.ErrUnusableHome, i18n.OpenCodeHomeNotDataDir, i18n.Args{
			"agent": Label, "path": a.home,
		})
	}
	return nil
}

// Writable reports whether the OpenCode command line is installed. Listing
// reads the database directly; deleting does not.
func (a *Agent) Writable() bool { return a.installed() }

func (a *Agent) database() string { return filepath.Join(a.home, databaseFile) }

// Discover reads every session row, newest first.
func (a *Agent) Discover(ctx context.Context) ([]session.Session, error) {
	if _, err := os.Stat(a.database()); err != nil {
		// OpenCode is installed but has never been used. That is an answer,
		// not a failure.
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, i18n.Wrap(err, i18n.UnexpectedError, i18n.Args{"error": err})
	}
	db, err := open(a.database())
	if err != nil {
		return nil, i18n.Wrap(err, i18n.UnexpectedError, i18n.Args{"error": err})
	}
	defer db.Close()

	result, err := db.QueryContext(ctx, "SELECT * FROM session")
	if err != nil {
		return nil, i18n.Wrap(err, i18n.UnexpectedError, i18n.Args{"error": err})
	}
	records, err := rows(result)
	result.Close()
	if err != nil {
		return nil, i18n.Wrap(err, i18n.UnexpectedError, i18n.Args{"error": err})
	}

	sizes := partSizes(ctx, db)
	found := make([]session.Session, 0, len(records))
	for _, record := range records {
		if s, ok := a.load(ctx, db, record, sizes); ok {
			found = append(found, s)
		}
	}
	session.SortByRecency(found)
	return found, nil
}

// partSizes is only used to key the detail cache. A database that cannot
// answer — an older schema, a table OpenCode has since renamed — is no reason
// to list nothing at all.
func partSizes(ctx context.Context, db *sql.DB) map[string]int64 {
	result, err := db.QueryContext(ctx,
		"SELECT session_id, sum(length(data)) FROM part GROUP BY session_id")
	if err != nil {
		return nil
	}
	defer result.Close()

	sizes := make(map[string]int64)
	for result.Next() {
		var id string
		var size sql.NullInt64
		if result.Scan(&id, &size) == nil {
			sizes[id] = size.Int64
		}
	}
	return sizes
}

func (a *Agent) load(
	ctx context.Context, db *sql.DB, record row, sizes map[string]int64,
) (session.Session, bool) {
	id := record.text("id")
	if id == "" {
		return session.Session{}, false
	}

	title := record.text("title")
	if isPlaceholder(title) {
		// Both spellings mean "not titled yet", so the opening message is a
		// better answer than either.
		title = ""
	}
	if title == "" {
		title = firstUserText(ctx, db, id)
	}

	client := record.text("agent")
	if client == "" {
		client = defaultAgent
	}
	created := record.moment("time_created")
	updated := record.moment("time_updated")
	if updated.IsZero() {
		updated = created
	}
	parent := record.text("parent_id")

	return session.Session{
		Agent: ID,
		// Every session lives in the same file. It is still the honest answer
		// to "where is this?", and the detail cache keys on the id as well.
		Path:      a.database(),
		ID:        id,
		Title:     session.Condense(title, session.MaxTitleChars),
		Client:    client,
		UpdatedAt: updated,
		CreatedAt: created,
		Size:      sizes[id],
		Archived:  record.present("time_archived"),
		// A title is either the model's summary or a rename, and the row does
		// not say which, so neither is treated as one the user assigned.
		Noise:      title == "",
		SideThread: parent != "",
		Parent:     parent,
		Cwd:        record.text("directory"),
		Version:    record.text("version"),
	}, true
}

// firstUserText is the opening message, for sessions the model never got round
// to naming.
func firstUserText(ctx context.Context, db *sql.DB, id string) string {
	turns, err := conversation(ctx, db, id)
	if err != nil {
		return ""
	}
	for _, t := range turns {
		if t.role != "user" {
			continue
		}
		for _, part := range t.parts {
			if text := strings.TrimSpace(spokenText(part)); text != "" {
				return text
			}
		}
	}
	return ""
}

// Messages rebuilds the exchange from its parts, without the tool traffic.
//
// Parts carry the whole run: tool calls and their output, reasoning, step
// boundaries, patches. Only what a person wrote or read is kept.
func (a *Agent) Messages(ctx context.Context, s session.Session) ([]session.Message, error) {
	db, err := open(a.database())
	if err != nil {
		return nil, i18n.Wrap(err, i18n.UnexpectedError, i18n.Args{"error": err})
	}
	defer db.Close()

	turns, err := conversation(ctx, db, s.ID)
	if err != nil {
		return nil, i18n.Wrap(err, i18n.UnexpectedError, i18n.Args{"error": err})
	}

	var found []session.Message
	for _, t := range turns {
		role := session.Assistant
		switch t.role {
		case "user":
			role = session.User
		case "assistant":
		default:
			continue
		}
		var spoken []string
		images := 0
		for _, part := range t.parts {
			if text := spokenText(part); text != "" {
				spoken = append(spoken, text)
			}
			if isImage(part) {
				images++
			}
		}
		if message, ok := session.NewMessage(role, strings.Join(spoken, "\n"), images); ok {
			found = append(found, message)
		}
	}
	return found, nil
}

// Delete hands the session to OpenCode, which also removes its sub-agents.
func (a *Agent) Delete(ctx context.Context, s session.Session) error {
	if err := a.Preflight(); err != nil {
		// Startup rejects such a home outright; this is the backstop for an
		// agent built directly rather than through the command line.
		return err
	}
	if !sessionID.MatchString(s.ID) {
		return i18n.Wrap(agent.ErrBadSessionID, i18n.OpenCodeBadSessionID, i18n.Args{
			"session_id": s.ID,
		})
	}
	if err := a.deleteWithRetries(ctx, s); err != nil {
		return err
	}
	a.dropLeftovers(s.ID)
	return nil
}

// deleteWithRetries asks OpenCode to remove the session, waiting out a busy
// database.
//
// A batch of deletions, or an OpenCode window open elsewhere, can hold the
// write lock for longer than OpenCode's own five-second wait. That is a queue,
// not a refusal, so wait a moment and ask again.
func (a *Agent) deleteWithRetries(ctx context.Context, s session.Session) error {
	var err error
	for attempt := 0; attempt <= len(retryDelays); attempt++ {
		if attempt > 0 {
			delay := retryDelays[attempt-1] + rand.N(retryJitter)
			if err := sleep(ctx, delay); err != nil {
				return err
			}
		}
		err = a.run(ctx, execx.Command{
			Name:  Binary,
			Args:  []string{"session", "delete", s.ID},
			Env:   map[string]string{"XDG_DATA_HOME": filepath.Dir(a.home)},
			Label: Label,
		})
		switch {
		case err == nil:
			return nil
		case ctx.Err() != nil:
			return err
		case !a.present(ctx, s.ID):
			// What the caller asked for is that the session stop existing, so
			// settle it against the database rather than the exit status: the
			// command can fail after the row is already gone, and deleting one
			// somebody else removed first is not worth complaining about.
			return nil
		case !retryable.MatchString(err.Error()):
			return err
		}
	}
	return err
}

// present reports whether the session is still in the database. Anything
// unreadable answers "still there": success is not something to assume on the
// strength of a question that could not be asked.
func (a *Agent) present(ctx context.Context, id string) bool {
	db, err := open(a.database())
	if err != nil {
		return true
	}
	defer db.Close()

	var found int
	err = db.QueryRowContext(ctx, "SELECT 1 FROM session WHERE id = ?", id).Scan(&found)
	return !errors.Is(err, sql.ErrNoRows)
}

// dropLeftovers removes the diff file the JSON-era migration left behind.
// `opencode session delete` clears the database and stops there; the file is
// named after a session that no longer exists, so nothing will read it again.
func (a *Agent) dropLeftovers(id string) {
	// The session itself is already gone; a stale diff is not worth an error.
	_ = os.Remove(filepath.Join(a.home, filepath.FromSlash(sessionDiffDir), id+".json"))
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
