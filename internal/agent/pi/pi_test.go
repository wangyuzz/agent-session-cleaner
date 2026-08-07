package pi_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/agent/pi"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// The directory name encodes the working directory by turning its separators
// into hyphens, and the filename is the moment the session started followed by
// its id.
const (
	project = "sessions/--work-app--/"
	file    = "2026-08-01T09-30-00-000Z_abc.jsonl"
)

func stored(lines ...string) *fstest.MapFile {
	return &fstest.MapFile{
		Data:    []byte(strings.Join(lines, "\n") + "\n"),
		ModTime: time.Date(2026, 8, 1, 12, 0, 0, 0, time.Local),
	}
}

// head is the session header, the one line that is not part of the tree.
func head(id, cwd string) string {
	return `{"type":"session","version":3,"id":"` + id +
		`","timestamp":"2026-08-01T09:30:00.000Z","cwd":"` + cwd + `"}`
}

// said builds a message entry with a plain string body.
func said(id, parent, role, text string) string {
	return `{"type":"message","id":"` + id + `","parentId":` + parentOf(parent) +
		`,"timestamp":"2026-08-01T09:30:01.000Z","message":{"role":"` + role +
		`","content":"` + text + `"}}`
}

func parentOf(id string) string {
	if id == "" {
		return "null"
	}
	return `"` + id + `"`
}

func discover(t *testing.T, tree fstest.MapFS) []session.Session {
	t.Helper()
	found, err := pi.New("/pi", pi.WithFS(tree)).Discover(t.Context())
	if err != nil {
		t.Fatalf("Discover() = %v", err)
	}
	return found
}

func only(t *testing.T, found []session.Session) session.Session {
	t.Helper()
	if len(found) != 1 {
		t.Fatalf("found %d sessions, want 1", len(found))
	}
	return found[0]
}

func TestDiscover(t *testing.T) {
	t.Parallel()

	got := only(t, discover(t, fstest.MapFS{
		project + file: stored(
			head("abc", "/work/app"),
			`{"type":"model_change","id":"m1","parentId":null,"provider":"anthropic","modelId":"claude-sonnet-4-5"}`,
			said("u1", "m1", "user", "add a health check"),
		),
	}))

	if got.ID != "abc" || got.Title != "add a health check" {
		t.Errorf("session = %+v", got)
	}
	// The directory name is lossy — a project path that already contains a
	// hyphen encodes to the same name as one with a slash there — so the
	// working directory is read from the file.
	if got.Cwd != "/work/app" || got.Client != "cli" {
		t.Errorf("metadata = %+v", got)
	}
	// Recorded as UTC; everything on screen is local time.
	if want := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC); !got.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want)
	}
	// The header records the session format's version, not the release that
	// wrote it, so there is nothing to show.
	if got.Version != "" {
		t.Errorf("Version = %q, want nothing", got.Version)
	}
	// There is no archive concept here.
	if got.Archived {
		t.Error("session claimed to be archived")
	}
	if got.Noise {
		t.Error("Pi session claimed to be empty")
	}
}

// Pi decides a file is one of its sessions by its first line, and so does this.
// An HTML export or a stray note dropped in the directory is not a session.
func TestDiscoverSkipsFilesThatAreNotSessions(t *testing.T) {
	t.Parallel()

	found := discover(t, fstest.MapFS{
		project + file: stored(head("abc", "/work/app")),
		project + "2026-08-01T09-31-00-000Z_valid.jsonl": stored(
			head("bad;id", "/work/app"),
		),
		project + "notes.jsonl":           stored(`{"type":"message","id":"x","parentId":null}`),
		project + "empty.jsonl":           stored(``),
		project + "pipe.jsonl":            {Data: []byte(head("pipe", "/work/app")), Mode: fs.ModeNamedPipe},
		project + "export.html":           stored(`<html></html>`),
		"sessions/loose_at-the-top.jsonl": stored(head("nope", "/work/app")),
	})

	if len(found) != 1 || found[0].ID != "abc" {
		t.Errorf("found %v, want only the session", found)
	}
}

// A session made by /fork or /clone records the file it came from, but it is a
// complete, independently resumable copy rather than a side thread. Nesting it
// would make deleting the original take every fork of it along, and would call
// a fork whose source is gone unrecoverable when it is nothing of the kind.
func TestForksStayAtTheTopLevel(t *testing.T) {
	t.Parallel()

	found := discover(t, fstest.MapFS{
		project + file: stored(head("abc", "/work/app"), said("u1", "", "user", "original")),
		project + "2026-08-01T10-00-00-000Z_def.jsonl": stored(
			`{"type":"session","version":3,"id":"def","timestamp":"2026-08-01T10:00:00.000Z",`+
				`"cwd":"/work/app","parentSession":"/pi/`+project+file+`"}`,
			said("u1", "", "user", "the fork"),
		),
	})

	if len(found) != 2 {
		t.Fatalf("found %d sessions, want 2", len(found))
	}
	for _, s := range found {
		if s.Parent != "" || s.SideThread {
			t.Errorf("session %q was nested under another: %+v", s.ID, s)
		}
	}
}

func TestTitles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name:  "the opening message",
			lines: []string{said("u1", "", "user", "fix   the\\n build")},
			want:  "fix the build",
		},
		{
			name: "a name the user assigned wins, and the last one at that",
			lines: []string{
				said("u1", "", "user", "opening"),
				`{"type":"session_info","id":"n1","parentId":"u1","name":"first guess"}`,
				`{"type":"session_info","id":"n2","parentId":"n1","name":"Refactor the parser"}`,
			},
			want: "Refactor the parser",
		},
		{
			// /name with nothing after it takes the name away again, and the
			// opening line has to come back with it.
			name: "a cleared name falls back to the opening message",
			lines: []string{
				said("u1", "", "user", "opening"),
				`{"type":"session_info","id":"n1","parentId":"u1","name":"a name"}`,
				`{"type":"session_info","id":"n2","parentId":"n1","name":"  "}`,
			},
			want: "opening",
		},
		{
			name: "tool traffic does not count as a conversation",
			lines: []string{
				`{"type":"message","id":"r1","parentId":null,"message":{"role":"toolResult",` +
					`"toolName":"bash","content":[{"type":"text","text":"a user of the system"}]}}`,
				`{"type":"message","id":"b1","parentId":"r1","message":{"role":"bashExecution",` +
					`"command":"id -un","output":"user"}}`,
			},
			want: "",
		},
		{
			name: "block content is flattened",
			lines: []string{
				`{"type":"message","id":"u1","parentId":null,"message":{"role":"user","content":[` +
					`{"type":"text","text":"look at this"},` +
					`{"type":"image","mimeType":"image/png","data":"AAAA"}]}}`,
			},
			want: "look at this",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := only(t, discover(t, fstest.MapFS{
				project + file: stored(append([]string{head("abc", "/work/app")}, test.lines...)...),
			}))
			if got.Title != test.want {
				t.Errorf("Title = %q, want %q", got.Title, test.want)
			}
		})
	}
}

// A session file is a tree, not a transcript: /tree and /fork leave the paths
// they moved off still written down. Pi takes the last line as the leaf and
// walks parents from there, and so must the preview — reading the file in order
// would show the same question answered twice.
func TestMessagesFollowTheBranchPiWouldResume(t *testing.T) {
	t.Parallel()

	tree := fstest.MapFS{project + file: stored(
		head("abc", "/work/app"),
		said("u1", "", "user", "how do I start"),
		said("a1", "u1", "assistant", "like this"),
		// One path, tried and left behind.
		said("u2", "a1", "user", "abandoned question"),
		said("a2", "u2", "assistant", "abandoned answer"),
		// The parent chain runs through whatever else sits on the tree.
		`{"type":"model_change","id":"m1","parentId":"a1","provider":"openai","modelId":"gpt-5"}`,
		said("u3", "m1", "user", "asked another way"),
		said("a3", "u3", "assistant", "answered again"),
	)}

	if got := read(t, tree); got != "how do I start|like this|asked another way|answered again" {
		t.Errorf("read %q, want the branch pi would resume", got)
	}
}

func TestMessagesLeaveOutEverythingThatIsNotConversation(t *testing.T) {
	t.Parallel()

	tree := fstest.MapFS{project + file: stored(
		head("abc", "/work/app"),
		said("u1", "", "user", "hello"),
		`{"type":"message","id":"r1","parentId":"u1","message":{"role":"toolResult",`+
			`"toolName":"read","content":[{"type":"text","text":"file contents"}]}}`,
		`{"type":"message","id":"b1","parentId":"r1","message":{"role":"bashExecution",`+
			`"command":"ls","output":"a b c"}}`,
		`{"type":"custom","id":"c1","parentId":"b1","customType":"plan-mode","data":{"on":true}}`,
		// Thinking and tool calls are the agent working, not talking.
		`{"type":"message","id":"a1","parentId":"c1","message":{"role":"assistant","content":[`+
			`{"type":"thinking","thinking":"hmm"},`+
			`{"type":"toolCall","id":"t","name":"bash","arguments":{}},`+
			`{"type":"text","text":"hi there"}]}}`,
	)}

	if got := read(t, tree); got != "hello|hi there" {
		t.Errorf("read %q, want the conversation without the traffic around it", got)
	}
}

func TestMessagesReadLegacyLinearSessions(t *testing.T) {
	t.Parallel()

	tree := fstest.MapFS{project + file: stored(
		`{"type":"session","version":1,"id":"abc","timestamp":"2026-08-01T09:30:00.000Z","cwd":"/work/app"}`,
		`{"type":"message","timestamp":"2026-08-01T09:30:01.000Z","message":{"role":"user","content":"old question"}}`,
		`{"type":"message","timestamp":"2026-08-01T09:30:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"old answer"}]}}`,
	)}

	if got := read(t, tree); got != "old question|old answer" {
		t.Errorf("read %q, want the version 1 transcript in storage order", got)
	}
}

// read is the conversation of the one session in tree, joined for comparison.
func read(t *testing.T, tree fstest.MapFS) string {
	t.Helper()
	a := pi.New("/pi", pi.WithFS(tree))
	found := only(t, discover(t, tree))
	messages, err := a.Messages(t.Context(), found)
	if err != nil {
		t.Fatalf("Messages() = %v", err)
	}
	spoken := make([]string, len(messages))
	for i, message := range messages {
		spoken[i] = message.Text
	}
	return strings.Join(spoken, "|")
}

func TestSessionIDFallsBackToTheFilename(t *testing.T) {
	t.Parallel()

	got := only(t, discover(t, fstest.MapFS{
		project + file: stored(
			`{"type":"session","version":3,"timestamp":"2026-08-01T09:30:00.000Z","cwd":"/work/app"}`,
			said("u1", "", "user", "unnamed"),
		),
	}))
	if got.ID != "abc" {
		t.Errorf("ID = %q, want the one in the filename", got.ID)
	}
}

func TestDeleteRemovesTheSessionFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "--work-app--")
	mkdirAll(t, dir)
	write(t, filepath.Join(dir, file),
		`{"type":"session","version":1,"timestamp":"2026-08-01T09:30:00.000Z","cwd":"/work/app"}`,
		said("u1", "", "user", "go"))
	keep := filepath.Join(dir, "2026-08-01T11-00-00-000Z_def.jsonl")
	write(t, keep, head("def", "/work/app"), said("u1", "", "user", "stay"))

	a := pi.New(home)
	found, err := a.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Delete(t.Context(), pick(t, found, "abc")); err != nil {
		t.Fatalf("Delete() = %v", err)
	}

	assertGone(t, filepath.Join(dir, file))
	assertPresent(t, keep)
	// Pi keeps nothing else about a session, so the project directory outlives
	// the last session in it.
	assertPresent(t, dir)
}

// The id about to be acted on has to match both the header and the filename.
func TestDeleteRefusesAFileThatIsNotThisSession(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		file  string
		lines []string
		want  error
	}{
		{"a different id in the header", file, []string{head("somebody-else", "/work/app")}, agent.ErrIDMismatch},
		{"no valid header", file, []string{`{"type":"message","id":"u1","parentId":null}`}, agent.ErrIDMismatch},
		{"a different id in the filename", "2026-08-01T09-30-00-000Z_other.jsonl", []string{head("abc", "/work/app")}, agent.ErrUnsafeSessionPath},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			dir := filepath.Join(home, "sessions", "--work-app--")
			mkdirAll(t, dir)
			path := filepath.Join(dir, test.file)
			write(t, path, test.lines...)

			err := pi.New(home).Delete(t.Context(), session.Session{ID: "abc", Path: path})
			if !errors.Is(err, test.want) {
				t.Fatalf("Delete() = %v, want %v", err, test.want)
			}
			assertPresent(t, path)
		})
	}
}

func TestDeleteReportsAFileThatIsAlreadyGone(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	err := pi.New(home).Delete(t.Context(), session.Session{
		ID: "abc", Path: filepath.Join(home, "sessions", "--work-app--", file),
	})
	if !errors.Is(err, agent.ErrSessionGone) {
		t.Errorf("Delete() = %v, want ErrSessionGone", err)
	}
}

func TestDeleteRefusesAPathOutsideTheSessionTree(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	for _, test := range []struct {
		name string
		path func(t *testing.T) string
	}{
		{
			name: "another directory entirely",
			path: func(t *testing.T) string { return filepath.Join(t.TempDir(), file) },
		},
		{
			// sessions/<project>/<file>.jsonl, and nothing shallower or deeper.
			name: "loose in the sessions directory",
			path: func(*testing.T) string { return filepath.Join(home, "sessions", file) },
		},
		{
			name: "buried below a project",
			path: func(*testing.T) string {
				return filepath.Join(home, "sessions", "--work-app--", "deeper", file)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := test.path(t)
			mkdirAll(t, filepath.Dir(path))
			write(t, path, head("abc", "/work/app"))

			err := pi.New(home).Delete(t.Context(), session.Session{ID: "abc", Path: path})
			if !errors.Is(err, agent.ErrUnsafeSessionPath) {
				t.Fatalf("Delete() = %v, want ErrUnsafeSessionPath", err)
			}
			assertPresent(t, path)
		})
	}
}

func TestDeleteRefusesASymlinkedSessionFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "--work-app--")
	mkdirAll(t, dir)
	target := filepath.Join(t.TempDir(), "target.jsonl")
	write(t, target, head("abc", "/work/app"))
	link := filepath.Join(dir, file)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	err := pi.New(home).Delete(t.Context(), session.Session{ID: "abc", Path: link})
	if !errors.Is(err, agent.ErrUnsafeSessionPath) {
		t.Fatalf("Delete() = %v, want ErrUnsafeSessionPath", err)
	}
	assertPresent(t, target)
	assertPresent(t, link)
}

// Lexical containment is not enough when an ancestor is a symlink.
func TestDeleteRefusesASymlinkedSessionAncestor(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	outside := t.TempDir()
	dir := filepath.Join(outside, "--work-app--")
	mkdirAll(t, dir)
	target := filepath.Join(dir, file)
	write(t, target, head("abc", "/work/app"))
	if err := os.Symlink(outside, filepath.Join(home, "sessions")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	err := pi.New(home).Delete(t.Context(), session.Session{
		ID: "abc", Path: filepath.Join(home, "sessions", "--work-app--", file),
	})
	if !errors.Is(err, agent.ErrUnsafeSessionPath) {
		t.Fatalf("Delete() = %v, want ErrUnsafeSessionPath", err)
	}
	assertPresent(t, target)
}

func TestDiscoverSkipsSymlinkedSessionFiles(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "--work-app--")
	mkdirAll(t, dir)
	target := filepath.Join(t.TempDir(), "outside.jsonl")
	write(t, target, head("abc", "/work/app"))
	if err := os.Symlink(target, filepath.Join(dir, file)); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	found, err := pi.New(home).Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("Discover() followed a symlink: %+v", found)
	}
}

// Pi has nowhere to archive to. Its sub-agents run with --no-session, and it
// does not persist empty sessions, so neither bulk sweep applies.
func TestCapabilities(t *testing.T) {
	t.Parallel()

	a := pi.New("/tmp/pi/agent")
	if _, ok := any(a).(agent.Archiver); ok {
		t.Error("Pi cannot archive, but claims to")
	}
	meta := a.Meta()
	if meta.OrphanLabel != 0 {
		t.Error("Pi should not offer an orphan sweep")
	}
	if meta.EmptyLabel != 0 {
		t.Error("Pi should not offer an empty-session sweep")
	}
	// Deleting is a filesystem operation, so nothing has to be installed.
	if !a.Writable() {
		t.Error("Writable() = false")
	}
	if err := a.Preflight(); err != nil {
		t.Errorf("Preflight() = %v", err)
	}
}

func TestDefaultHome(t *testing.T) {
	// Not parallel: the home directory is read from the environment.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PI_CODING_AGENT_DIR", "")

	if want := filepath.Join(home, ".pi", "agent"); pi.DefaultHome() != want {
		t.Errorf("DefaultHome() = %q, want %q", pi.DefaultHome(), want)
	}
	// Pi's own variable names the directory itself, so ours means the same.
	t.Setenv("PI_CODING_AGENT_DIR", "~/somewhere")
	if want := filepath.Join(home, "somewhere"); pi.DefaultHome() != want {
		t.Errorf("DefaultHome() = %q, want %q", pi.DefaultHome(), want)
	}
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path string, lines ...string) {
	t.Helper()
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pick(t *testing.T, found []session.Session, id string) session.Session {
	t.Helper()
	for _, s := range found {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no session %q among %v", id, found)
	return session.Session{}
}

func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s is still there", path)
	}
}

func assertPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("%s should have been left alone: %v", path, err)
	}
}
