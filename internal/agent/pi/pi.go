// Package pi manages Pi session history.
//
// Layout under $PI_CODING_AGENT_DIR (default ~/.pi/agent):
//
//	sessions/--<cwd>--/<started>_<session-id>.jsonl
//
// The directory name is the working directory with its separators turned into
// hyphens, which is lossy — a project whose path already contains a hyphen
// encodes to the same name as one with a slash there — so the working
// directory shown on a row is read from the file rather than from its
// directory.
//
// Three things differ from the other agents. Pi has no archive, so this agent
// does not implement [agent.Archiver]. It also has no non-interactive command
// to delete a session with — its own picker removes the file — so deletion is
// a filesystem operation, guarded the way Claude Code's is: the file has to
// still be a regular file inside the configured tree and belong to the
// session being deleted.
//
// And nothing here is ever a side thread. Sub-agents run with --no-session and
// leave no file behind, so no orphan can arise. A header does record a
// parentSession for a session made by /fork or /clone, but that is a complete,
// independently resumable copy rather than a disposable side thread: nesting
// one would make deleting the original take every fork of it along, and would
// report a fork whose source is gone as unrecoverable when it is nothing of
// the kind. Forks stay at the top level.
package pi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// Identity and layout.
const (
	ID     = "pi"
	Label  = "Pi"
	Binary = "pi"

	sessionsDir = "sessions"
)

// sessionID is the shape pi accepts for a session id: the uuid it mints for
// itself, or whatever --session-id was handed.
var sessionID = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)

var _ agent.Agent = (*Agent)(nil)

// Agent is the Pi session store.
type Agent struct {
	home string
	fsys fs.FS
}

// Option adjusts an agent, for tests that supply their own session tree.
type Option func(*Agent)

// WithFS reads sessions from fsys instead of the home directory itself.
func WithFS(fsys fs.FS) Option {
	return func(a *Agent) { a.fsys = fsys }
}

// New returns the Pi agent for home, or for the default location when home is
// empty.
func New(home string, opts ...Option) *Agent {
	a := &Agent{home: agent.Resolve(home, DefaultHome)}
	for _, opt := range opts {
		opt(a)
	}
	if a.fsys == nil {
		a.fsys = os.DirFS(a.home)
	}
	return a
}

// DefaultHome is where Pi keeps its state unless told otherwise. It is the
// agent directory inside the configuration one, which is what pi's own
// environment variable names.
func DefaultHome() string {
	if override := os.Getenv("PI_CODING_AGENT_DIR"); override != "" {
		return agent.ExpandHome(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".pi", "agent")
	}
	return filepath.Join(home, ".pi", "agent")
}

// Meta describes Pi to the interface.
func (a *Agent) Meta() agent.Meta {
	return agent.Meta{
		ID:       ID,
		Label:    Label,
		Reply:    Label,
		Shortcut: 'p',
		Home:     a.home,
		// Pi is one program with one way in, so a badge naming it on every row
		// would say nothing.
		DefaultClient: "cli",
		// Pi defers persistence until the session has content, so it leaves no
		// empty session files for the E sweep to remove.
		EmptyLabel: 0,
		// Sub-agents run with --no-session, so there is nothing to strand.
		OrphanLabel: 0,
		// Each deletion is one unlink on a path of its own.
		BulkConcurrency: 4,
	}
}

// Preflight always passes: PI_CODING_AGENT_DIR names the directory itself, so
// any directory can be pointed at.
func (a *Agent) Preflight() error { return nil }

// Writable is always true. Deleting a session here is a filesystem operation,
// so nothing has to be installed for it.
func (a *Agent) Writable() bool { return true }

// Discover reads every session file, one directory per project.
func (a *Agent) Discover(ctx context.Context) ([]session.Session, error) {
	projects, err := fs.ReadDir(a.fsys, sessionsDir)
	if err != nil {
		// No sessions directory means no sessions, which is an answer rather
		// than a failure.
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, i18n.Wrap(err, i18n.UnexpectedError, i18n.Args{"error": err})
	}

	var (
		found    []session.Session
		failures []error
	)
	for _, project := range projects {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !project.IsDir() {
			continue
		}
		dir := path.Join(sessionsDir, project.Name())
		entries, err := fs.ReadDir(a.fsys, dir)
		if err != nil {
			// Preserve everything readable, but report that the listing is
			// incomplete so the interface can keep it browse-only.
			failures = append(failures, fmt.Errorf("read project %q: %w", project.Name(), err))
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			if s, ok := a.load(path.Join(dir, name)); ok {
				found = append(found, s)
			}
		}
	}

	session.SortByRecency(found)
	return found, errors.Join(failures...)
}

// load builds a list entry from one session file, reporting false for a file
// that is not one.
func (a *Agent) load(name string) (session.Session, bool) {
	stat, err := fs.Stat(a.fsys, name)
	if err != nil || !stat.Mode().IsRegular() {
		return session.Session{}, false
	}
	file, err := a.fsys.Open(name)
	if err != nil {
		return session.Session{}, false
	}
	defer file.Close()

	// A file whose first line is not a session header is not a pi session,
	// which is how pi's own listing decides it too.
	found, ok := scan(file)
	if !ok {
		return session.Session{}, false
	}
	id := found.id
	if id == "" {
		id = nameID(path.Base(name))
	}
	if !sessionID.MatchString(id) {
		return session.Session{}, false
	}

	// A name the user assigned beats the opening line, being a summary of the
	// whole conversation rather than of how it started.
	title := found.name
	if title == "" {
		title = found.firstUser
	}

	return session.Session{
		Agent: ID,
		Path:  filepath.Join(a.home, filepath.FromSlash(name)),
		ID:    id,
		// An empty title stays empty. What to call a session with nothing in
		// it is the interface's decision, not this one's.
		Title:     session.Condense(title, session.MaxTitleChars),
		Client:    "cli",
		UpdatedAt: stat.ModTime(),
		CreatedAt: found.created,
		Size:      stat.Size(),
		Cwd:       found.cwd,
		// The header records the session format's version, not the release
		// that wrote it, so there is nothing here to show.
		Version: "",
	}, true
}

// nameID recovers a session id from a filename, which pi builds from the
// moment the session started and its id, joined by the one underscore in it.
func nameID(base string) string {
	_, id, ok := strings.Cut(strings.TrimSuffix(base, ".jsonl"), "_")
	if !ok || !sessionID.MatchString(id) {
		return ""
	}
	return id
}

// Messages reads the human side of one session.
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
func (a *Agent) relative(name string) (string, bool) {
	rel, err := filepath.Rel(a.home, name)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// Delete removes a session file. Pi keeps nothing else about a session, so
// there is nothing beside it to take along.
func (a *Agent) Delete(ctx context.Context, s session.Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := a.safeSession(s)
	if err != nil {
		return err
	}

	file, err := os.Open(s.Path)
	if err != nil {
		return i18n.Wrap(err, i18n.SessionDeleteFailed, i18n.Args{"error": err})
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return i18n.Wrap(err, i18n.SessionDeleteFailed, i18n.Args{"error": err})
	}
	if !os.SameFile(info, opened) {
		_ = file.Close()
		return i18n.Wrap(agent.ErrUnsafeSessionPath, i18n.SessionPathUnsafe)
	}
	recorded, validHeader := recordedID(file)
	if closeErr := file.Close(); closeErr != nil {
		return i18n.Wrap(closeErr, i18n.SessionDeleteFailed, i18n.Args{"error": closeErr})
	}
	// Older sessions can omit the id in an otherwise valid header. Their
	// filename was already checked by safeSession; malformed replacements do
	// not get the same fallback.
	if !validHeader || recorded != "" && recorded != s.ID {
		return i18n.Wrap(agent.ErrIDMismatch, i18n.SessionIDMismatch)
	}

	// Recheck after reading the identity. A replacement between discovery and
	// deletion must not inherit the earlier file's approval.
	current, err := os.Lstat(s.Path)
	if err != nil {
		return i18n.Wrap(errors.Join(agent.ErrSessionGone, err), i18n.SessionFileMissing)
	}
	if !os.SameFile(info, current) || current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() {
		return i18n.Wrap(agent.ErrUnsafeSessionPath, i18n.SessionPathUnsafe)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.Remove(s.Path); err != nil {
		return i18n.Wrap(err, i18n.SessionDeleteFailed, i18n.Args{"error": err})
	}
	return nil
}

// safeSession confines deletion to a session file in
// <home>/sessions/<project>/<name>.jsonl. Lstat deliberately rejects a symlink
// even when its target looks like a valid session.
func (a *Agent) safeSession(s session.Session) (os.FileInfo, error) {
	unsafe := func() error {
		return i18n.Wrap(agent.ErrUnsafeSessionPath, i18n.SessionPathUnsafe)
	}
	rel, err := filepath.Rel(a.home, s.Path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, unsafe()
	}
	rel = filepath.Clean(rel)
	if filepath.Dir(filepath.Dir(rel)) != sessionsDir ||
		filepath.Ext(rel) != ".jsonl" || !sessionID.MatchString(s.ID) ||
		nameID(filepath.Base(rel)) != s.ID {
		return nil, unsafe()
	}
	// Lexical containment is not enough when an ancestor such as sessions is a
	// symlink. Resolve both ends and require the file to remain inside the
	// configured home after following those links.
	realHome, homeErr := filepath.EvalSymlinks(a.home)
	realPath, pathErr := filepath.EvalSymlinks(s.Path)
	if homeErr != nil || pathErr != nil {
		joined := errors.Join(homeErr, pathErr)
		if errors.Is(joined, os.ErrNotExist) {
			return nil, i18n.Wrap(errors.Join(agent.ErrSessionGone, joined), i18n.SessionFileMissing)
		}
		return nil, unsafe()
	}
	realRel, err := filepath.Rel(realHome, realPath)
	if err != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
		return nil, unsafe()
	}
	info, err := os.Lstat(s.Path)
	if err != nil {
		return nil, i18n.Wrap(errors.Join(agent.ErrSessionGone, err), i18n.SessionFileMissing)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, unsafe()
	}
	return info, nil
}
