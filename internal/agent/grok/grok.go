// Package grok manages Grok session history.
//
// Layout under $GROK_HOME (default ~/.grok):
//
//	sessions/<url-encoded-cwd>/<session-uuid>/
//	    summary.json       index: title, timestamps, model, parent
//	    updates.jsonl      ACP conversation log (the preview)
//	    chat_history.jsonl raw model traffic
//	sessions/<url-encoded-cwd>/.cwd   original path when the encoded name is a slug
//	sessions/<url-encoded-cwd>/prompt_history.jsonl
//
// Two things differ from the other agents. A session is a directory, not a
// file, so deletion is RemoveAll of that directory after the id in
// summary.json has been checked. And Grok has no archive and no
// non-interactive delete command — `/delete` in the TUI just unlinks the
// directory — so this agent does not implement [agent.Archiver] and does not
// need the Grok binary installed.
//
// parent_session_id records a /fork or restore. That child is a complete,
// independently resumable copy, the way Pi's parentSession is: nesting one
// would make deleting the original take every fork of it along. Forks stay
// at the top level. Sub-agent sessions live in the same tree under their own
// ids; they are listed, but not nested, because the parent link is not
// recorded in a way that distinguishes a disposable side thread from a fork.
package grok

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
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
	ID     = "grok"
	Label  = "Grok"
	Binary = "grok"

	sessionsDir = "sessions"
	summaryFile = "summary.json"
	updatesFile = "updates.jsonl"
	cwdFile     = ".cwd"

	// defaultAgent is what Grok writes when the session was not handed to a
	// named sub-agent. Naming it on every row would be noise.
	defaultAgent = "grok-build-plan"
)

// sessionID is the shape Grok mints (UUIDv7) and the shape -s accepts.
var sessionID = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

var _ agent.Agent = (*Agent)(nil)

// Agent is the Grok session store.
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

// New returns the Grok agent for home, or for the default location when home
// is empty.
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

// DefaultHome is where Grok keeps its state unless told otherwise.
func DefaultHome() string {
	if override := os.Getenv("GROK_HOME"); override != "" {
		return agent.ExpandHome(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".grok"
	}
	return filepath.Join(home, ".grok")
}

// Meta describes Grok to the interface.
func (a *Agent) Meta() agent.Meta {
	return agent.Meta{
		ID:            ID,
		Label:         Label,
		Reply:         Label,
		Shortcut:      'g',
		Home:          a.home,
		DefaultClient: defaultAgent,
		// A directory is created as soon as the TUI opens, so an abandoned
		// launch leaves an empty session behind.
		EmptyLabel: i18n.EmptySessions,
		// Forks are independently resumable, and sub-agents are not linked
		// in a way that would make an orphan sweep safe.
		OrphanLabel: 0,
		// Each deletion is one RemoveAll on a path of its own.
		BulkConcurrency: 4,
		Resume:          "grok --resume %s",
	}
}

// Preflight always passes: GROK_HOME names the directory itself.
func (a *Agent) Preflight() error { return nil }

// Writable is always true. Deleting a session here is a filesystem operation,
// so nothing has to be installed for it.
func (a *Agent) Writable() bool { return true }

// Discover reads every project's session directories.
func (a *Agent) Discover(ctx context.Context) ([]session.Session, error) {
	projects, err := fs.ReadDir(a.fsys, sessionsDir)
	if err != nil {
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
		if !project.IsDir() || project.Type()&fs.ModeSymlink != 0 {
			continue
		}
		dir := path.Join(sessionsDir, project.Name())
		entries, err := fs.ReadDir(a.fsys, dir)
		if err != nil {
			failures = append(failures, fmt.Errorf("read project %q: %w", project.Name(), err))
			continue
		}
		groupCwd := projectCwd(a.fsys, dir, project.Name())
		for _, entry := range entries {
			name := entry.Name()
			if entry.Type()&fs.ModeSymlink != 0 || !entry.IsDir() || !sessionID.MatchString(name) {
				continue
			}
			if s, ok := a.load(path.Join(dir, name), groupCwd); ok {
				found = append(found, s)
			}
		}
	}

	session.SortByRecency(found)
	return found, errors.Join(failures...)
}

// load builds a list entry from one session directory, reporting false for a
// directory that is not one.
func (a *Agent) load(dir, groupCwd string) (session.Session, bool) {
	stat, err := fs.Stat(a.fsys, dir)
	if err != nil || !stat.IsDir() {
		return session.Session{}, false
	}

	sum, err := readSummary(a.fsys, path.Join(dir, summaryFile))
	if err != nil {
		return session.Session{}, false
	}

	id := sum.Info.ID
	if id == "" {
		id = path.Base(dir)
	}
	if !sessionID.MatchString(id) {
		return session.Session{}, false
	}

	title := strings.TrimSpace(sum.GeneratedTitle)
	if title == "" {
		title = strings.TrimSpace(sum.SessionSummary)
	}
	if title == "" {
		title = firstUser(a.fsys, path.Join(dir, updatesFile))
	}

	cwd := strings.TrimSpace(sum.Info.Cwd)
	if cwd == "" {
		cwd = groupCwd
	}

	client := strings.TrimSpace(sum.AgentName)
	if client == "" {
		client = defaultAgent
	}

	created := moment(sum.CreatedAt)
	updated := moment(sum.UpdatedAt)
	if updated.IsZero() {
		updated = moment(sum.LastActiveAt)
	}
	if updated.IsZero() {
		updated = stat.ModTime()
	}

	return session.Session{
		Agent:     ID,
		Path:      filepath.Join(a.home, filepath.FromSlash(dir)),
		ID:        id,
		Title:     session.Condense(title, session.MaxTitleChars),
		Client:    client,
		UpdatedAt: updated,
		CreatedAt: created,
		Size:      dirSize(a.fsys, dir),
		Noise:     title == "",
		Cwd:       cwd,
	}, true
}

// Messages reads the human side of one session.
func (a *Agent) Messages(ctx context.Context, s session.Session) ([]session.Message, error) {
	rel, ok := a.relative(s.Path)
	if !ok {
		return nil, i18n.Errorf(i18n.SessionFileMissing)
	}
	file, err := a.fsys.Open(path.Join(rel, updatesFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, i18n.Wrap(err, i18n.SessionFileMissing)
	}
	defer file.Close()
	found, err := messages(ctx, file)
	if err != nil {
		return nil, i18n.Wrap(err, i18n.UnexpectedError, i18n.Args{"error": err})
	}
	return found, nil
}

func (a *Agent) relative(name string) (string, bool) {
	rel, err := filepath.Rel(a.home, name)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// Delete removes a session directory. Grok keeps the conversation, rewind
// points and search crumbs together under that one path.
func (a *Agent) Delete(ctx context.Context, s session.Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := a.safeSession(s)
	if err != nil {
		return err
	}

	recorded, err := recordedID(filepath.Join(s.Path, summaryFile))
	if err != nil {
		return err
	}
	if recorded != "" && recorded != s.ID {
		return i18n.Wrap(agent.ErrIDMismatch, i18n.SessionIDMismatch)
	}

	// Recheck after reading the identity. A replacement between discovery and
	// deletion must not inherit the earlier directory's approval.
	current, err := os.Lstat(s.Path)
	if err != nil {
		return i18n.Wrap(errors.Join(agent.ErrSessionGone, err), i18n.SessionFileMissing)
	}
	if !os.SameFile(info, current) || current.Mode()&os.ModeSymlink != 0 || !current.IsDir() {
		return i18n.Wrap(agent.ErrUnsafeSessionPath, i18n.SessionPathUnsafe)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.RemoveAll(s.Path); err != nil {
		return i18n.Wrap(err, i18n.SessionDeleteFailed, i18n.Args{"error": err})
	}
	return nil
}

// safeSession confines deletion to a session directory in
// <home>/sessions/<project>/<uuid>. Lstat deliberately rejects a symlink
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
		!sessionID.MatchString(s.ID) || filepath.Base(rel) != s.ID {
		return nil, unsafe()
	}
	// Lexical containment is not enough when an ancestor such as sessions is
	// a symlink. Resolve both ends and require the directory to remain inside
	// the configured home after following those links.
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
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, unsafe()
	}
	return info, nil
}

// projectCwd recovers the working directory for a group: the .cwd sidecar
// Grok writes when the encoded name is a slug, otherwise the URL-decoded
// directory name.
func projectCwd(fsys fs.FS, dir, name string) string {
	if data, err := fs.ReadFile(fsys, path.Join(dir, cwdFile)); err == nil {
		if cwd := strings.TrimSpace(string(data)); cwd != "" {
			return cwd
		}
	}
	decoded, err := url.PathUnescape(name)
	if err != nil || decoded == name && strings.Contains(name, "%") {
		return ""
	}
	return decoded
}

// dirSize is the sum of the regular files sitting in a session directory,
// which is enough to key the conversation cache. Nested tool logs are left
// out so a listing does not walk them.
func dirSize(fsys fs.FS, dir string) int64 {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}
