package grok_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/agent/grok"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

const (
	group = "sessions/work/"
	sid   = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	other = "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
)

func summaryJSON(id, cwd, title, created string) string {
	if created == "" {
		created = "2026-08-12T09:30:00.000Z"
	}
	body := map[string]any{
		"info":            map[string]string{"id": id, "cwd": cwd},
		"generated_title": title,
		"created_at":      created,
		"updated_at":      created,
	}
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func chunk(kind, text string) string {
	return `{"method":"session/update","params":{"sessionId":"` + sid +
		`","update":{"sessionUpdate":"` + kind +
		`","content":{"type":"text","text":"` + text + `"}}}}`
}

func stored(data string) *fstest.MapFile {
	return &fstest.MapFile{
		Data:    []byte(data),
		ModTime: time.Date(2026, 8, 12, 12, 0, 0, 0, time.Local),
	}
}

func discover(t *testing.T, tree fstest.MapFS) []session.Session {
	t.Helper()
	found, err := grok.New("/grok", grok.WithFS(tree)).Discover(t.Context())
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
		group + sid + "/summary.json":  stored(summaryJSON(sid, "/work/app", "Read the project", "")),
		group + sid + "/updates.jsonl": stored(chunk("user_message_chunk", "hello")),
		group + "prompt_history.jsonl": stored("ignored"),
	}))

	if got.ID != sid || got.Title != "Read the project" {
		t.Errorf("session = %+v", got)
	}
	if got.Cwd != "/work/app" || got.Client != "grok-build-plan" {
		t.Errorf("metadata = %+v", got)
	}
	if want := time.Date(2026, 8, 12, 9, 30, 0, 0, time.UTC); !got.CreatedAt.Equal(want.Local()) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.Local())
	}
	if got.Archived {
		t.Error("session claimed to be archived")
	}
	if got.Noise {
		t.Error("titled session claimed to be empty")
	}
	if got.Parent != "" || got.SideThread {
		t.Errorf("session was nested: %+v", got)
	}
}

func TestDiscoverSkipsThingsThatAreNotSessions(t *testing.T) {
	t.Parallel()

	found := discover(t, fstest.MapFS{
		group + sid + "/summary.json":     stored(summaryJSON(sid, "/work/app", "keep", "")),
		group + "prompt_history.jsonl":    stored(`{"prompt":"nope"}`),
		group + "not-a-uuid/summary.json": stored(summaryJSON("not-a-uuid", "/work/app", "bad", "")),
		"sessions/session_search.sqlite":  stored("db"),
		"sessions/loose/summary.json":     stored(summaryJSON(other, "/work", "too shallow", "")),
	})

	if len(found) != 1 || found[0].ID != sid {
		t.Errorf("found %v, want only the session", found)
	}
}

func TestTitles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		summary string
		updates string
		want    string
		noise   bool
	}{
		{
			name:    "the generated title",
			summary: summaryJSON(sid, "/work/app", "Generated name", ""),
			want:    "Generated name",
		},
		{
			name: "a session summary when there is no title",
			summary: `{"info":{"id":"` + sid + `","cwd":"/work/app"},` +
				`"session_summary":"fix the build","created_at":"2026-08-12T09:30:00Z"}`,
			want: "fix the build",
		},
		{
			name:    "the opening message when summary is blank",
			summary: summaryJSON(sid, "/work/app", "", ""),
			updates: chunk("user_message_chunk", "look at this"),
			want:    "look at this",
		},
		{
			name:    "an abandoned launch is empty",
			summary: summaryJSON(sid, "/work/app", "", ""),
			noise:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tree := fstest.MapFS{group + sid + "/summary.json": stored(test.summary)}
			if test.updates != "" {
				tree[group+sid+"/updates.jsonl"] = stored(test.updates)
			}
			got := only(t, discover(t, tree))
			if got.Title != test.want {
				t.Errorf("Title = %q, want %q", got.Title, test.want)
			}
			if got.Noise != test.noise {
				t.Errorf("Noise = %v, want %v", got.Noise, test.noise)
			}
		})
	}
}

func TestCwdFallsBackToTheEncodedDirectory(t *testing.T) {
	t.Parallel()

	got := only(t, discover(t, fstest.MapFS{
		"sessions/%2Fwork%2Fapp/" + sid + "/summary.json": stored(
			`{"info":{"id":"` + sid + `"},"generated_title":"x","created_at":"2026-08-12T09:30:00Z"}`,
		),
	}))
	if got.Cwd != "/work/app" {
		t.Errorf("Cwd = %q, want the decoded directory name", got.Cwd)
	}
}

func TestCwdPrefersTheSidecarFile(t *testing.T) {
	t.Parallel()

	got := only(t, discover(t, fstest.MapFS{
		"sessions/slug-abc/.cwd": stored("/very/long/working/directory"),
		"sessions/slug-abc/" + sid + "/summary.json": stored(
			`{"info":{"id":"` + sid + `"},"generated_title":"x","created_at":"2026-08-12T09:30:00Z"}`,
		),
	}))
	if got.Cwd != "/very/long/working/directory" {
		t.Errorf("Cwd = %q, want the .cwd sidecar", got.Cwd)
	}
}

// A session made by /fork records the file it came from, but it is a complete,
// independently resumable copy rather than a side thread.
func TestForksStayAtTheTopLevel(t *testing.T) {
	t.Parallel()

	found := discover(t, fstest.MapFS{
		group + sid + "/summary.json": stored(summaryJSON(sid, "/work/app", "original", "")),
		group + other + "/summary.json": stored(
			`{"info":{"id":"` + other + `","cwd":"/work/app"},` +
				`"generated_title":"the fork","parent_session_id":"` + sid + `",` +
				`"created_at":"2026-08-12T10:00:00Z"}`,
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

func TestMessagesJoinStreamedChunks(t *testing.T) {
	t.Parallel()

	tree := fstest.MapFS{
		group + sid + "/summary.json": stored(summaryJSON(sid, "/work/app", "chat", "")),
		group + sid + "/updates.jsonl": stored(strings.Join([]string{
			chunk("user_message_chunk", "hello"),
			chunk("user_message_chunk", " there"),
			chunk("agent_thought_chunk", "thinking"),
			chunk("agent_message_chunk", "hi"),
			chunk("tool_call", "list_dir"),
			chunk("agent_message_chunk", " again"),
		}, "\n")),
	}

	if got := read(t, tree); got != "hello there|hi again" {
		t.Errorf("read %q, want streamed turns joined and thoughts dropped", got)
	}
}

func TestMessagesLeaveOutEverythingThatIsNotConversation(t *testing.T) {
	t.Parallel()

	tree := fstest.MapFS{
		group + sid + "/summary.json": stored(summaryJSON(sid, "/work/app", "chat", "")),
		group + sid + "/updates.jsonl": stored(strings.Join([]string{
			chunk("user_message_chunk", "go"),
			`{"method":"session/update","params":{"update":{"sessionUpdate":"tool_call","title":"ls"}}}`,
			chunk("agent_thought_chunk", "hmm"),
			chunk("agent_message_chunk", "done"),
		}, "\n")),
	}

	if got := read(t, tree); got != "go|done" {
		t.Errorf("read %q, want the conversation without the traffic around it", got)
	}
}

func TestMessagesTreatAMissingLogAsEmpty(t *testing.T) {
	t.Parallel()

	tree := fstest.MapFS{
		group + sid + "/summary.json": stored(summaryJSON(sid, "/work/app", "empty", "")),
	}
	if got := read(t, tree); got != "" {
		t.Errorf("read %q, want nothing", got)
	}
}

func read(t *testing.T, tree fstest.MapFS) string {
	t.Helper()
	a := grok.New("/grok", grok.WithFS(tree))
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

func TestDeleteRemovesTheSessionDirectory(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "work", sid)
	mkdirAll(t, dir)
	write(t, filepath.Join(dir, "summary.json"), summaryJSON(sid, "/work/app", "go", ""))
	write(t, filepath.Join(dir, "updates.jsonl"), chunk("user_message_chunk", "go"))
	keep := filepath.Join(home, "sessions", "work", other)
	mkdirAll(t, keep)
	write(t, filepath.Join(keep, "summary.json"), summaryJSON(other, "/work/app", "stay", ""))
	history := filepath.Join(home, "sessions", "work", "prompt_history.jsonl")
	write(t, history, "keep me")

	a := grok.New(home)
	found, err := a.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Delete(t.Context(), pick(t, found, sid)); err != nil {
		t.Fatalf("Delete() = %v", err)
	}

	assertGone(t, dir)
	assertPresent(t, keep)
	assertPresent(t, history)
}

func TestDeleteRefusesADirectoryThatIsNotThisSession(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "work", sid)
	mkdirAll(t, dir)
	write(t, filepath.Join(dir, "summary.json"), summaryJSON(other, "/work/app", "mismatch", ""))

	err := grok.New(home).Delete(t.Context(), session.Session{ID: sid, Path: dir})
	if !errors.Is(err, agent.ErrIDMismatch) {
		t.Fatalf("Delete() = %v, want ErrIDMismatch", err)
	}
	assertPresent(t, dir)
}

func TestDeleteReportsADirectoryThatIsAlreadyGone(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	err := grok.New(home).Delete(t.Context(), session.Session{
		ID: sid, Path: filepath.Join(home, "sessions", "work", sid),
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
			path: func(t *testing.T) string { return filepath.Join(t.TempDir(), sid) },
		},
		{
			name: "loose in the sessions directory",
			path: func(*testing.T) string { return filepath.Join(home, "sessions", sid) },
		},
		{
			name: "buried below a session",
			path: func(*testing.T) string {
				return filepath.Join(home, "sessions", "work", sid, "deeper")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := test.path(t)
			mkdirAll(t, path)
			write(t, filepath.Join(path, "summary.json"), summaryJSON(sid, "/work/app", "x", ""))

			err := grok.New(home).Delete(t.Context(), session.Session{ID: sid, Path: path})
			if !errors.Is(err, agent.ErrUnsafeSessionPath) {
				t.Fatalf("Delete() = %v, want ErrUnsafeSessionPath", err)
			}
			assertPresent(t, path)
		})
	}
}

func TestDeleteRefusesASymlinkedSessionDirectory(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	parent := filepath.Join(home, "sessions", "work")
	mkdirAll(t, parent)
	target := filepath.Join(t.TempDir(), sid)
	mkdirAll(t, target)
	write(t, filepath.Join(target, "summary.json"), summaryJSON(sid, "/work/app", "x", ""))
	link := filepath.Join(parent, sid)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	err := grok.New(home).Delete(t.Context(), session.Session{ID: sid, Path: link})
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
	dir := filepath.Join(outside, "work", sid)
	mkdirAll(t, dir)
	write(t, filepath.Join(dir, "summary.json"), summaryJSON(sid, "/work/app", "x", ""))
	if err := os.Symlink(outside, filepath.Join(home, "sessions")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	err := grok.New(home).Delete(t.Context(), session.Session{
		ID: sid, Path: filepath.Join(home, "sessions", "work", sid),
	})
	if !errors.Is(err, agent.ErrUnsafeSessionPath) {
		t.Fatalf("Delete() = %v, want ErrUnsafeSessionPath", err)
	}
	assertPresent(t, dir)
}

func TestDiscoverSkipsSymlinkedSessionDirectories(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	parent := filepath.Join(home, "sessions", "work")
	mkdirAll(t, parent)
	target := filepath.Join(t.TempDir(), sid)
	mkdirAll(t, target)
	write(t, filepath.Join(target, "summary.json"), summaryJSON(sid, "/work/app", "x", ""))
	if err := os.Symlink(target, filepath.Join(parent, sid)); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	found, err := grok.New(home).Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("Discover() followed a symlink: %+v", found)
	}
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	a := grok.New("/tmp/grok")
	if _, ok := any(a).(agent.Archiver); ok {
		t.Error("Grok cannot archive, but claims to")
	}
	meta := a.Meta()
	if meta.OrphanLabel != 0 {
		t.Error("Grok should not offer an orphan sweep")
	}
	if meta.EmptyLabel == 0 {
		t.Error("Grok should offer an empty-session sweep")
	}
	if !a.Writable() {
		t.Error("Writable() = false")
	}
	if err := a.Preflight(); err != nil {
		t.Errorf("Preflight() = %v", err)
	}
}

func TestDefaultHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GROK_HOME", "")

	if want := filepath.Join(home, ".grok"); grok.DefaultHome() != want {
		t.Errorf("DefaultHome() = %q, want %q", grok.DefaultHome(), want)
	}
	t.Setenv("GROK_HOME", "~/somewhere")
	if want := filepath.Join(home, "somewhere"); grok.DefaultHome() != want {
		t.Errorf("DefaultHome() = %q, want %q", grok.DefaultHome(), want)
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
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
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
