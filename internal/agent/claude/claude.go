// Package claude manages Claude Code session history.
//
// Layout under $CLAUDE_CONFIG_DIR (default ~/.claude):
//
//	projects/<sanitized-cwd>/<session-uuid>.jsonl   transcript
//	projects/<sanitized-cwd>/<session-uuid>/        co-located sidecar
//	    subagents/agent-*.jsonl                     sub-agent transcripts
//	    tool-results/                               offloaded tool output
//
// Two things differ from the other agents. There is no archive operation to
// delegate to — `claude project purge` removes a whole project — so this agent
// does not implement [agent.Archiver]. And deletion is a filesystem operation
// rather than a command: it follows cc-switch, verifying the session id
// recorded inside the file, removing the same-stem sidecar, then the
// transcript, and deliberately leaving the top-level file-history, session-env,
// image-cache and tasks directories and history.jsonl alone.
package claude

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// Identity and layout.
const (
	ID     = "claude"
	Label  = "Claude Code"
	Binary = "claude"

	projectsDir = "projects"
	// Sub-agent transcripts live inside a sidecar and are named agent-<hash>.
	subAgentPrefix = "agent-"
)

var _ agent.Agent = (*Agent)(nil)

// Agent is the Claude Code session store.
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

// New returns the Claude Code agent for home, or for the default location when
// home is empty.
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

// DefaultHome is where Claude Code keeps its state unless told otherwise.
func DefaultHome() string {
	if override := os.Getenv("CLAUDE_CONFIG_DIR"); override != "" {
		return agent.ExpandHome(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// Meta describes Claude Code to the interface.
func (a *Agent) Meta() agent.Meta {
	return agent.Meta{
		ID:            ID,
		Label:         Label,
		Reply:         "Claude",
		Shortcut:      'c',
		Home:          a.home,
		DefaultClient: "cli",
		EmptyLabel:    i18n.EmptySessions,
		// Sub-agent transcripts live inside the parent's sidecar directory and
		// are removed along with it, so a stranded one cannot arise.
		OrphanLabel: 0,
		// Each deletion is a few filesystem calls on paths of its own.
		BulkConcurrency: 4,
	}
}

// Preflight always passes: CLAUDE_CONFIG_DIR names the directory itself.
func (a *Agent) Preflight() error { return nil }

// Writable is always true. Deleting a session here is a filesystem operation,
// so nothing has to be installed for it.
func (a *Agent) Writable() bool { return true }

// Discover reads every project's transcripts.
func (a *Agent) Discover(ctx context.Context) ([]session.Session, error) {
	projects, err := fs.ReadDir(a.fsys, projectsDir)
	if err != nil {
		// No projects directory means no sessions, which is an answer rather
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
		entries, err := fs.ReadDir(a.fsys, path.Join(projectsDir, project.Name()))
		if err != nil {
			failures = append(failures, fmt.Errorf("read project %q: %w", project.Name(), err))
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			// Sub-agent transcripts live one level down, inside the sidecar,
			// and are excluded by name the way cc-switch does it.
			if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 ||
				!strings.HasSuffix(name, ".jsonl") ||
				strings.HasPrefix(name, subAgentPrefix) {
				continue
			}
			if s, ok := a.load(path.Join(projectsDir, project.Name(), name)); ok {
				found = append(found, s)
			}
		}
	}

	session.SortByRecency(found)
	return found, errors.Join(failures...)
}

func (a *Agent) load(name string) (session.Session, bool) {
	stat, err := fs.Stat(a.fsys, name)
	if err != nil {
		return session.Session{}, false
	}
	file, err := a.fsys.Open(name)
	if err != nil {
		return session.Session{}, false
	}
	defer file.Close()

	found := scan(file)
	id := found.id
	if id == "" {
		id = strings.TrimSuffix(path.Base(name), ".jsonl")
	}
	title := found.aiTitle
	if title == "" {
		title = found.firstUser
	}
	client := found.client
	if client == "" {
		client = "cli"
	}

	return session.Session{
		Agent:     ID,
		Path:      filepath.Join(a.home, filepath.FromSlash(name)),
		ID:        id,
		Title:     session.Condense(title, session.MaxTitleChars),
		Client:    client,
		UpdatedAt: stat.ModTime(),
		CreatedAt: found.created,
		Size:      stat.Size(),
		// Claude Code has no archive concept, and an ai-title is generated
		// rather than assigned by the user.
		Archived: false,
		Noise:    found.firstUser == "",
		Cwd:      found.cwd,
		Version:  found.version,
	}, true
}

// Messages reads the human side of one transcript.
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

func (a *Agent) relative(name string) (string, bool) {
	rel, err := filepath.Rel(a.home, name)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// Delete removes a transcript and its sidecar, in that order of checking.
func (a *Agent) Delete(ctx context.Context, s session.Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := a.safeTranscript(s)
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
	// Guard against deleting the wrong transcript, as cc-switch does: the id
	// about to be acted on must match the one recorded inside the file.
	recorded := recordedID(file)
	if closeErr := file.Close(); closeErr != nil {
		return i18n.Wrap(closeErr, i18n.SessionDeleteFailed, i18n.Args{"error": closeErr})
	}
	if recorded != "" && recorded != s.ID {
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

	if err := os.RemoveAll(sidecar(s.Path)); err != nil {
		return i18n.Wrap(err, i18n.SidecarDeleteFailed, i18n.Args{"error": err})
	}
	if err := os.Remove(s.Path); err != nil {
		return i18n.Wrap(err, i18n.SessionDeleteFailed, i18n.Args{"error": err})
	}
	return nil
}

// safeTranscript confines deletion to a top-level transcript in
// <home>/projects/<project>/<session-id>.jsonl. Lstat deliberately rejects a
// symlink even when its target looks like a valid transcript.
func (a *Agent) safeTranscript(s session.Session) (os.FileInfo, error) {
	rel, err := filepath.Rel(a.home, s.Path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, i18n.Wrap(agent.ErrUnsafeSessionPath, i18n.SessionPathUnsafe)
	}
	rel = filepath.Clean(rel)
	parent := filepath.Dir(rel)
	if filepath.Dir(parent) != projectsDir || s.ID == "" ||
		filepath.Base(rel) != s.ID+".jsonl" {
		return nil, i18n.Wrap(agent.ErrUnsafeSessionPath, i18n.SessionPathUnsafe)
	}
	// Lexical containment is not enough when an ancestor such as projects is
	// a symlink. Resolve both ends and require the transcript to remain inside
	// the configured home after following those links.
	realHome, homeErr := filepath.EvalSymlinks(a.home)
	realPath, pathErr := filepath.EvalSymlinks(s.Path)
	if homeErr != nil || pathErr != nil {
		joined := errors.Join(homeErr, pathErr)
		if errors.Is(joined, os.ErrNotExist) {
			return nil, i18n.Wrap(errors.Join(agent.ErrSessionGone, joined), i18n.SessionFileMissing)
		}
		return nil, i18n.Wrap(agent.ErrUnsafeSessionPath, i18n.SessionPathUnsafe)
	}
	realRel, err := filepath.Rel(realHome, realPath)
	if err != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
		return nil, i18n.Wrap(agent.ErrUnsafeSessionPath, i18n.SessionPathUnsafe)
	}
	info, err := os.Lstat(s.Path)
	if err != nil {
		return nil, i18n.Wrap(errors.Join(agent.ErrSessionGone, err), i18n.SessionFileMissing)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, i18n.Wrap(agent.ErrUnsafeSessionPath, i18n.SessionPathUnsafe)
	}
	return info, nil
}

// sidecar turns .../<uuid>.jsonl into .../<uuid>, the co-located directory
// holding sub-agent transcripts and offloaded tool output.
func sidecar(transcript string) string {
	return strings.TrimSuffix(transcript, filepath.Ext(transcript))
}
