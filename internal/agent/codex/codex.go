// Package codex manages Codex CLI session history.
//
// Layout under $CODEX_HOME (default ~/.codex):
//
//	sessions/YYYY/MM/DD/rollout-<local-ts>-<uuid>.jsonl   active
//	archived_sessions/rollout-<local-ts>-<uuid>.jsonl     archived, flat
//	session_index.jsonl                                   append-only rename log
//
// Archiving moves a file between those two trees, so archived state is decided
// entirely by location. Every change is delegated to the non-interactive Codex
// command line, which knows what else has to move and reports failure through
// its exit status.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/execx"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/jsonl"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// Identity and layout.
const (
	ID     = "codex"
	Label  = "Codex"
	Binary = "codex"

	sessionsDir = "sessions"
	archivedDir = "archived_sessions"
	indexFile   = "session_index.jsonl"
)

var rolloutName = regexp.MustCompile(
	`^rollout-(\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2})-([0-9a-fA-F-]{36})\.jsonl$`,
)

// filenameTime is the local-time stamp in a rollout filename. The timestamp
// recorded inside the file is UTC; this one is not.
const filenameTime = "2006-01-02T15-04-05"

// Codex is the only agent with somewhere to put a session it is not deleting.
var (
	_ agent.Agent    = (*Agent)(nil)
	_ agent.Archiver = (*Agent)(nil)
)

// Agent is the Codex session store.
type Agent struct {
	home      string
	fsys      fs.FS
	run       execx.Runner
	installed func() bool
}

// Option adjusts an agent, for tests that supply their own session tree or
// stand in for the Codex command line.
type Option func(*Agent)

// WithFS reads sessions from fsys instead of the home directory itself.
func WithFS(fsys fs.FS) Option {
	return func(a *Agent) { a.fsys = fsys }
}

// WithRunner sends changes to run rather than the real Codex command, and
// treats the command as installed.
func WithRunner(run execx.Runner) Option {
	return func(a *Agent) {
		a.run = run
		a.installed = func() bool { return true }
	}
}

// New returns the Codex agent for home, or for the default location when home
// is empty.
func New(home string, opts ...Option) *Agent {
	a := &Agent{home: agent.Resolve(home, DefaultHome), run: execx.Run}
	for _, opt := range opts {
		opt(a)
	}
	if a.fsys == nil {
		a.fsys = os.DirFS(a.home)
	}
	if a.installed == nil {
		a.installed = func() bool { return execx.Available(Binary) }
	}
	return a
}

// DefaultHome is where Codex keeps its state unless told otherwise.
func DefaultHome() string {
	if override := os.Getenv("CODEX_HOME"); override != "" {
		return agent.ExpandHome(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".codex"
	}
	return filepath.Join(home, ".codex")
}

// Meta describes Codex to the interface.
func (a *Agent) Meta() agent.Meta {
	return agent.Meta{
		ID:            ID,
		Label:         Label,
		Reply:         Label,
		Shortcut:      'x',
		Home:          a.home,
		DefaultClient: "cli",
		// A session row exists from the moment a rollout is written, but an
		// abandoned one is indistinguishable from a short conversation here.
		EmptyLabel: 0,
		// A sub-agent rollout records the conversation that spawned it, so
		// once that conversation is deleted the rollout is provably
		// unreachable: `codex resume` will never offer it, and nothing else
		// refers to it.
		OrphanLabel: i18n.OrphanSessions,
		// Archiving moves one file and deleting removes one file, so a batch
		// only contends for the directory itself.
		BulkConcurrency: 4,
	}
}

// Preflight always passes: CODEX_HOME names the directory itself, so any
// directory can be pointed at.
func (a *Agent) Preflight() error { return nil }

// Writable reports whether the Codex command line is installed. Listing works
// from the rollout files alone; changing anything does not.
func (a *Agent) Writable() bool { return a.installed() }

// Discover reads every rollout under both session trees.
func (a *Agent) Discover(ctx context.Context) ([]session.Session, error) {
	names := a.threadNames()
	var (
		found    []session.Session
		failures []error
	)

	for _, tree := range []struct {
		dir      string
		archived bool
	}{
		{sessionsDir, false},
		{archivedDir, true},
	} {
		err := fs.WalkDir(a.fsys, tree.dir, func(path string, entry fs.DirEntry, err error) error {
			switch {
			case ctx.Err() != nil:
				return ctx.Err()
			case err != nil:
				if path == tree.dir && errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				// Preserve everything readable, but report that the listing is
				// incomplete so the interface can keep it browse-only.
				failures = append(failures, fmt.Errorf("read %q: %w", path, err))
				return nil
			case entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 || !strings.HasSuffix(path, ".jsonl"):
				return nil
			}
			if s, ok := a.load(path, tree.archived, names); ok {
				found = append(found, s)
			}
			return nil
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			failures = append(failures, err)
		}
	}

	session.SortByRecency(found)
	return found, errors.Join(failures...)
}

// load builds a list entry from one rollout, reporting false when the file
// cannot be made sense of.
func (a *Agent) load(path string, archived bool, names map[string]string) (session.Session, bool) {
	stat, err := fs.Stat(a.fsys, path)
	if err != nil {
		return session.Session{}, false
	}
	file, err := a.fsys.Open(path)
	if err != nil {
		return session.Session{}, false
	}
	defer file.Close()

	info, opening := head(file)
	name := filepath.Base(path)
	match := rolloutName.FindStringSubmatch(name)

	id := info.ID
	if id == "" {
		id = info.SessionID
	}
	if id == "" {
		if match == nil {
			return session.Session{}, false
		}
		id = match[2]
	}

	var created time.Time
	if match != nil {
		if when, err := time.ParseInLocation(filenameTime, match[1], time.Local); err == nil {
			created = when
		}
	}

	kind, subagent := source(info.Source)
	// A recorded thread name beats the opening line, being a summary of the
	// whole conversation rather than of how it started. It says nothing about
	// whether anyone chose it: Codex Desktop titles every thread it creates.
	title := names[id]
	if title == "" {
		title = opening
	}

	// Fall back to ForkedFrom only for sub-agents. On an ordinary session it
	// means the user forked a resumable conversation, which does not make the
	// result a disposable side thread.
	parent := info.ParentThread
	if parent == "" && kind == "subagent" {
		parent = info.ForkedFrom
	}

	return session.Session{
		Agent: ID,
		Path:  filepath.Join(a.home, filepath.FromSlash(path)),
		ID:    id,
		// An empty title stays empty. What to call a session with nothing in
		// it is the interface's decision, not this one's.
		Title:      session.Condense(title, session.MaxTitleChars),
		Client:     client(kind, subagent, info.Originator),
		UpdatedAt:  stat.ModTime(),
		CreatedAt:  created,
		Size:       stat.Size(),
		Archived:   archived,
		Noise:      !interactiveSources[kind],
		SideThread: kind == "subagent",
		Parent:     parent,
		Cwd:        info.Cwd,
		Version:    info.CLIVersion,
	}, true
}

// threadNames reads the names the user has assigned. The log is append-only,
// so the last entry for an id wins.
func (a *Agent) threadNames() map[string]string {
	file, err := a.fsys.Open(indexFile)
	if err != nil {
		return nil
	}
	defer file.Close()

	names := make(map[string]string)
	scanner := jsonl.NewScanner(file)
	for scanner.Scan() {
		var entry struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry.ID != "" && entry.Name != "" {
			names[entry.ID] = entry.Name
		}
	}
	return names
}

// Messages reads the human-facing turns of one rollout.
func (a *Agent) Messages(ctx context.Context, s session.Session) ([]session.Message, error) {
	rel, ok := a.relative(s.Path)
	if !ok {
		return nil, i18n.Errorf(i18n.SessionFileMissing)
	}
	file, err := a.fsys.Open(rel)
	if err != nil {
		return nil, i18n.Wrap(err, i18n.SessionFileMissing)
	}
	defer file.Close()
	found, err := messages(ctx, file)
	if err != nil {
		return nil, i18n.Wrap(err, i18n.UnexpectedError, i18n.Args{"error": err})
	}
	return found, nil
}

// relative converts an absolute session path back to a path inside the
// configured filesystem, which is how a test tree is read.
func (a *Agent) relative(path string) (string, bool) {
	rel, err := filepath.Rel(a.home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// Archive moves a rollout into the archived tree.
func (a *Agent) Archive(ctx context.Context, s session.Session) error {
	return a.command(ctx, "archive", s.ID)
}

// Unarchive moves a rollout back out of the archived tree.
func (a *Agent) Unarchive(ctx context.Context, s session.Session) error {
	return a.command(ctx, "unarchive", s.ID)
}

// Delete removes a rollout for good.
func (a *Agent) Delete(ctx context.Context, s session.Session) error {
	// `delete --force` refuses anything that is not a uuid, which is why
	// session ids are passed rather than names.
	return a.command(ctx, "delete", s.ID, "--force")
}

// command runs the Codex CLI against the same tree the listing was read from.
func (a *Agent) command(ctx context.Context, args ...string) error {
	return a.run(ctx, execx.Command{
		Name:  Binary,
		Args:  args,
		Env:   map[string]string{"CODEX_HOME": a.home},
		Label: Label,
	})
}
