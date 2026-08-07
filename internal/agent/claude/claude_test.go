package claude_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/agent/claude"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

func transcript(lines ...string) *fstest.MapFile {
	return &fstest.MapFile{
		Data:    []byte(strings.Join(lines, "\n") + "\n"),
		ModTime: time.Date(2026, 8, 1, 12, 0, 0, 0, time.Local),
	}
}

// spoke builds a user or assistant record with a plain string body.
func spoke(kind, id, text string) string {
	return `{"type":"` + kind + `","sessionId":"` + id + `","cwd":"/work/app",` +
		`"version":"2.1.0","entrypoint":"cli","timestamp":"2026-08-01T09:30:00Z",` +
		`"message":{"content":"` + text + `"}}`
}

func discover(t *testing.T, tree fstest.MapFS) []session.Session {
	t.Helper()
	found, err := claude.New("/claude", claude.WithFS(tree)).Discover(t.Context())
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

const project = "projects/-work-app/"

func TestDiscover(t *testing.T) {
	t.Parallel()

	got := only(t, discover(t, fstest.MapFS{
		project + "abc.jsonl": transcript(spoke("user", "abc", "add a health check")),
	}))

	if got.ID != "abc" || got.Title != "add a health check" {
		t.Errorf("session = %+v", got)
	}
	if got.Cwd != "/work/app" || got.Version != "2.1.0" || got.Client != "cli" {
		t.Errorf("metadata = %+v", got)
	}
	// Recorded as UTC; everything on screen is local time.
	if want := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC); !got.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want)
	}
	// There is no archive concept here.
	if got.Archived {
		t.Error("session claimed to be archived")
	}
}

// Sub-agent transcripts live one level down, inside the sidecar, and are
// excluded by name the way cc-switch does it.
func TestDiscoverSkipsSubAgentTranscripts(t *testing.T) {
	t.Parallel()

	found := discover(t, fstest.MapFS{
		project + "abc.jsonl":                      transcript(spoke("user", "abc", "real")),
		project + "agent-deadbeef.jsonl":           transcript(spoke("user", "sub", "side")),
		project + "abc/subagents/agent-cafe.jsonl": transcript(spoke("user", "sub", "deeper")),
	})

	if len(found) != 1 || found[0].ID != "abc" {
		t.Errorf("found %v, want only the top-level transcript", found)
	}
}

func TestTitles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lines []string
		want  string
		noise bool
	}{
		{
			name:  "the opening message",
			lines: []string{spoke("user", "a", "fix   the\\n build")},
			want:  "fix the build",
		},
		{
			name: "a generated title wins, and the last one at that",
			lines: []string{
				spoke("user", "a", "opening"),
				`{"type":"ai-title","aiTitle":"first guess"}`,
				`{"type":"ai-title","aiTitle":"Refactor the parser"}`,
			},
			want: "Refactor the parser",
		},
		{
			// Most user records are tool traffic rather than anything a person
			// said, and a session of nothing but tool traffic is empty.
			name: "tool traffic does not count as a conversation",
			lines: []string{
				`{"type":"user","sessionId":"a","toolUseResult":{"ok":true},` +
					`"message":{"content":"tool output"}}`,
				`{"type":"user","sessionId":"a","isMeta":true,"message":{"content":"injected"}}`,
				`{"type":"user","sessionId":"a","isSidechain":true,"message":{"content":"side"}}`,
			},
			want:  "",
			noise: true,
		},
		{
			name: "block content is flattened",
			lines: []string{
				`{"type":"user","sessionId":"a","message":{"content":[` +
					`{"type":"text","text":"look at this"},` +
					`{"type":"image","source":{}},` +
					`{"type":"tool_use","name":"Read"}]}}`,
			},
			want: "look at this",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := only(t, discover(t, fstest.MapFS{
				project + "a.jsonl": transcript(test.lines...),
			}))
			if got.Title != test.want {
				t.Errorf("Title = %q, want %q", got.Title, test.want)
			}
			if got.Noise != test.noise {
				t.Errorf("Noise = %v, want %v", got.Noise, test.noise)
			}
		})
	}
}

// The metadata preamble can run long, and a session whose first message comes
// after it is not empty. Deciding otherwise would feed it to the bulk sweep.
func TestFirstMessageIsFoundPastThePreamble(t *testing.T) {
	t.Parallel()

	lines := make([]string, 0, 400)
	for range 350 {
		lines = append(lines, `{"type":"file-history-snapshot","messageId":"x"}`)
	}
	lines = append(lines, spoke("user", "a", "buried but real"))

	got := only(t, discover(t, fstest.MapFS{project + "a.jsonl": transcript(lines...)}))
	if got.Noise {
		t.Error("a session with a late first message was called empty")
	}
	if got.Title != "buried but real" {
		t.Errorf("Title = %q", got.Title)
	}
}

func TestMessages(t *testing.T) {
	t.Parallel()

	tree := fstest.MapFS{project + "a.jsonl": transcript(
		spoke("user", "a", "hello"),
		`{"type":"user","sessionId":"a","toolUseResult":{},"message":{"content":"tool"}}`,
		spoke("assistant", "a", "hi there"),
		`{"type":"assistant","sessionId":"a","message":{"content":[`+
			`{"type":"thinking","thinking":"hmm"},{"type":"text","text":"done"}]}}`,
	)}

	found := discover(t, tree)
	messages, err := claude.New("/claude", claude.WithFS(tree)).Messages(t.Context(), found[0])
	if err != nil {
		t.Fatalf("Messages() = %v", err)
	}

	var spokenText []string
	for _, message := range messages {
		spokenText = append(spokenText, message.Text)
	}
	if strings.Join(spokenText, "|") != "hello|hi there|done" {
		t.Errorf("read %q, want the conversation without tool traffic", spokenText)
	}
	if messages[0].Role != session.User || messages[1].Role != session.Assistant {
		t.Error("roles came out wrong")
	}
}

func TestDeleteRemovesTheTranscriptAndItsSidecar(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dir := filepath.Join(home, "projects", "-work-app")
	mkdirAll(t, filepath.Join(dir, "abc", "subagents"))
	mkdirAll(t, filepath.Join(dir, "keep"))
	write(t, filepath.Join(dir, "abc.jsonl"), spoke("user", "abc", "go"))
	write(t, filepath.Join(dir, "abc", "subagents", "agent-1.jsonl"), "{}")
	write(t, filepath.Join(dir, "keep.jsonl"), spoke("user", "keep", "stay"))

	a := claude.New(home)
	found, err := a.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	target := pick(t, found, "abc")

	if err := a.Delete(t.Context(), target); err != nil {
		t.Fatalf("Delete() = %v", err)
	}

	assertGone(t, filepath.Join(dir, "abc.jsonl"))
	assertGone(t, filepath.Join(dir, "abc"))
	assertPresent(t, filepath.Join(dir, "keep.jsonl"))
	assertPresent(t, filepath.Join(dir, "keep"))
}

// The id about to be acted on must match the one recorded inside the file, as
// cc-switch checks. Anything else risks deleting the wrong conversation.
func TestDeleteRefusesAMismatchedFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dir := filepath.Join(home, "projects", "-work-app")
	mkdirAll(t, dir)
	path := filepath.Join(dir, "abc.jsonl")
	write(t, path, spoke("user", "somebody-else", "go"))

	err := claude.New(home).Delete(t.Context(), session.Session{ID: "abc", Path: path})
	if !errors.Is(err, agent.ErrIDMismatch) {
		t.Fatalf("Delete() = %v, want ErrIDMismatch", err)
	}
	assertPresent(t, path)
}

func TestDeleteReportsAFileThatIsAlreadyGone(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	err := claude.New(home).Delete(t.Context(), session.Session{
		ID: "abc", Path: filepath.Join(home, "projects", "p", "abc.jsonl"),
	})
	if !errors.Is(err, agent.ErrSessionGone) {
		t.Errorf("Delete() = %v, want ErrSessionGone", err)
	}
}

func TestDeleteRefusesAPathOutsideTheSessionTree(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	outside := filepath.Join(t.TempDir(), "abc.jsonl")
	write(t, outside, spoke("user", "abc", "keep"))

	err := claude.New(home).Delete(t.Context(), session.Session{ID: "abc", Path: outside})
	if !errors.Is(err, agent.ErrUnsafeSessionPath) {
		t.Fatalf("Delete() = %v, want ErrUnsafeSessionPath", err)
	}
	assertPresent(t, outside)
}

func TestDeleteRefusesASymlinkedTranscript(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dir := filepath.Join(home, "projects", "-work-app")
	mkdirAll(t, dir)
	target := filepath.Join(t.TempDir(), "target.jsonl")
	write(t, target, spoke("user", "abc", "keep"))
	link := filepath.Join(dir, "abc.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	err := claude.New(home).Delete(t.Context(), session.Session{ID: "abc", Path: link})
	if !errors.Is(err, agent.ErrUnsafeSessionPath) {
		t.Fatalf("Delete() = %v, want ErrUnsafeSessionPath", err)
	}
	assertPresent(t, target)
	assertPresent(t, link)
}

func TestDeleteRefusesASymlinkedSessionAncestor(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	outside := t.TempDir()
	dir := filepath.Join(outside, "-work-app")
	mkdirAll(t, dir)
	target := filepath.Join(dir, "abc.jsonl")
	write(t, target, spoke("user", "abc", "keep"))
	if err := os.Symlink(outside, filepath.Join(home, "projects")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	throughLink := filepath.Join(home, "projects", "-work-app", "abc.jsonl")

	err := claude.New(home).Delete(t.Context(), session.Session{ID: "abc", Path: throughLink})
	if !errors.Is(err, agent.ErrUnsafeSessionPath) {
		t.Fatalf("Delete() = %v, want ErrUnsafeSessionPath", err)
	}
	assertPresent(t, target)
}

func TestDiscoverSkipsSymlinkedTranscripts(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dir := filepath.Join(home, "projects", "-work-app")
	mkdirAll(t, dir)
	target := filepath.Join(t.TempDir(), "outside.jsonl")
	write(t, target, spoke("user", "abc", "outside"))
	if err := os.Symlink(target, filepath.Join(dir, "abc.jsonl")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	found, err := claude.New(home).Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("Discover() followed a symlink: %+v", found)
	}
}

// Sub-agent transcripts live inside the parent's sidecar and go with it, so
// this agent can never leave one stranded — and it has nowhere to archive to.
func TestCapabilities(t *testing.T) {
	t.Parallel()

	a := claude.New("/tmp/claude")
	if _, ok := any(a).(agent.Archiver); ok {
		t.Error("Claude Code cannot archive, but claims to")
	}
	meta := a.Meta()
	if meta.OrphanLabel != 0 {
		t.Error("Claude Code should not offer an orphan sweep")
	}
	if meta.EmptyLabel == 0 {
		t.Error("Claude Code should offer an empty-session sweep")
	}
	// Deleting is a filesystem operation, so nothing has to be installed.
	if !a.Writable() {
		t.Error("Writable() = false")
	}
	if err := a.Preflight(); err != nil {
		t.Errorf("Preflight() = %v", err)
	}
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
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
