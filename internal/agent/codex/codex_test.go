package codex_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/agent/codex"
	"github.com/haowang02/agent-session-cleaner/internal/execx"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// rollout assembles a transcript from its lines.
func rollout(lines ...string) *fstest.MapFile {
	return &fstest.MapFile{
		Data:    []byte(strings.Join(lines, "\n") + "\n"),
		ModTime: time.Date(2026, 8, 1, 12, 0, 0, 0, time.Local),
	}
}

func meta(fields string) string {
	return `{"type":"session_meta","payload":{` + fields + `}}`
}

func said(kind, text string) string {
	return `{"type":"event_msg","payload":{"type":"` + kind + `","message":"` + text + `"}}`
}

func completed(item string) string {
	return `{"type":"event_msg","payload":{"type":"item_completed","turn_id":"t1",` +
		`"item":{` + item + `}}}`
}

func wrote(id, text string) string {
	return `"type":"UserMessage","id":"` + id + `","content":[{"type":"text","text":"` + text + `"}]`
}

func replied(id, text string) string {
	return `"type":"AgentMessage","id":"` + id + `","phase":"final_answer",` +
		`"content":[{"type":"Text","text":"` + text + `"}]`
}

func discover(t *testing.T, tree fstest.MapFS) []session.Session {
	t.Helper()
	found, err := codex.New("/codex", codex.WithFS(tree)).Discover(t.Context())
	if err != nil {
		t.Fatalf("Discover() = %v", err)
	}
	return found
}

func read(t *testing.T, tree fstest.MapFS) []session.Message {
	t.Helper()
	a := codex.New("/codex", codex.WithFS(tree))
	messages, err := a.Messages(t.Context(), only(t, discover(t, tree)))
	if err != nil {
		t.Fatalf("Messages() = %v", err)
	}
	return messages
}

func only(t *testing.T, found []session.Session) session.Session {
	t.Helper()
	if len(found) != 1 {
		t.Fatalf("found %d sessions, want 1", len(found))
	}
	return found[0]
}

const activePath = "sessions/2026/08/01/rollout-2026-08-01T09-30-00-" +
	"11111111-1111-1111-1111-111111111111.jsonl"

func TestDiscoverReadsBothTrees(t *testing.T) {
	t.Parallel()

	found := discover(t, fstest.MapFS{
		activePath: rollout(meta(`"id":"active","source":"cli"`), said("user_message", "live one")),
		"archived_sessions/rollout-2026-07-01T08-00-00-" +
			"22222222-2222-2222-2222-222222222222.jsonl": rollout(
			meta(`"id":"put-aside","source":"cli"`), said("user_message", "old one"),
		),
	})

	if len(found) != 2 {
		t.Fatalf("found %d sessions, want 2", len(found))
	}
	// Newest first, and archived state comes from which tree it was in.
	if found[0].ID != "active" || found[0].Archived {
		t.Errorf("first session = %+v, want the active one", found[0])
	}
	if found[1].ID != "put-aside" || !found[1].Archived {
		t.Errorf("second session = %+v, want the archived one", found[1])
	}
}

// In a sub-agent rollout, session_id holds the *parent's* thread id while id
// matches the uuid in the filename. Reading them the other way round would
// point resume and delete at the conversation that spawned this one.
func TestDiscoverPrefersIDOverSessionID(t *testing.T) {
	t.Parallel()

	got := only(t, discover(t, fstest.MapFS{
		activePath: rollout(meta(`"id":"mine","session_id":"my-parents","source":"cli"`)),
	}))
	if got.ID != "mine" {
		t.Errorf("ID = %q, want the rollout's own id", got.ID)
	}
}

func TestDiscoverFallsBackToTheFilename(t *testing.T) {
	t.Parallel()

	got := only(t, discover(t, fstest.MapFS{
		activePath: rollout(`{"type":"turn_context","payload":{}}`),
	}))
	if got.ID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("ID = %q, want the uuid from the filename", got.ID)
	}
}

// The stamp in the filename is local time, unlike the UTC one inside the file.
func TestDiscoverReadsTheStartTimeFromTheFilename(t *testing.T) {
	t.Parallel()

	got := only(t, discover(t, fstest.MapFS{
		activePath: rollout(meta(`"id":"a","source":"cli"`)),
	}))
	want := time.Date(2026, 8, 1, 9, 30, 0, 0, time.Local)
	if !got.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want)
	}
	if !got.RecencyAt().Equal(want) {
		t.Errorf("RecencyAt() = %v, want the recorded start", got.RecencyAt())
	}
}

func TestTitles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tree fstest.MapFS
		want string
	}{
		{
			name: "the opening message",
			tree: fstest.MapFS{activePath: rollout(
				meta(`"id":"a","source":"cli"`),
				said("agent_message", "thinking"),
				said("user_message", "fix   the\\n build"),
			)},
			want: "fix the build",
		},
		{
			name: "the opening message of a paginated rollout",
			tree: fstest.MapFS{activePath: rollout(
				meta(`"id":"a","source":"cli","history_mode":"paginated"`),
				completed(replied("m1", "thinking")),
				completed(wrote("u1", "fix   the\\n build")),
			)},
			want: "fix the build",
		},
		{
			name: "a recorded thread name wins",
			tree: fstest.MapFS{
				activePath:            rollout(meta(`"id":"a","source":"cli"`), said("user_message", "opening")),
				"session_index.jsonl": rollout(`{"id":"a","thread_name":"first try"}`, `{"id":"a","thread_name":"final name"}`),
			},
			want: "final name",
		},
		{
			// What to call a session with nothing in it is the interface's
			// decision, not this package's.
			name: "an empty session keeps an empty title",
			tree: fstest.MapFS{activePath: rollout(meta(`"id":"a","source":"cli"`))},
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := only(t, discover(t, test.tree))
			if got.Title != test.want {
				t.Errorf("Title = %q, want %q", got.Title, test.want)
			}
		})
	}
}

func TestClientAndNoise(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fields     string
		client     string
		noise      bool
		sideThread bool
	}{
		{"the terminal", `"source":"cli","originator":"codex-tui"`, "cli", false, false},
		{"the default originator", `"source":"cli","originator":"codex_cli_rs"`, "cli", false, false},
		// source is unreliable: VSCode is the default variant of Codex's enum,
		// so anything that omits a source is recorded as vscode.
		{"the editor extension", `"source":"vscode","originator":"codex_vscode"`, "vscode", false, false},
		{"the desktop app", `"source":"vscode","originator":"Codex Desktop"`, "app", false, false},
		{"an unknown first-party build", `"source":"vscode","originator":"Codex Nightly"`, "app", false, false},
		{"a non-interactive run", `"source":"exec","originator":"codex_exec"`, "exec", true, false},
		{"a plain sub-agent", `"source":"subagent"`, "subagent", true, true},
		{"a named sub-agent", `"source":{"subagent":"guardian"}`, "subagent:guardian", true, true},
		{"a tagged sub-agent", `"source":{"subagent":{"other":"guardian"}}`, "subagent:guardian", true, true},
		{"an unrecognised shape", `"source":42`, "unknown", true, false},
		{"nothing recorded", `"cwd":"/w"`, "unknown", true, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := only(t, discover(t, fstest.MapFS{
				activePath: rollout(meta(`"id":"a",` + test.fields)),
			}))
			if got.Client != test.client {
				t.Errorf("Client = %q, want %q", got.Client, test.client)
			}
			if got.Noise != test.noise {
				t.Errorf("Noise = %v, want %v", got.Noise, test.noise)
			}
			if got.SideThread != test.sideThread {
				t.Errorf("SideThread = %v, want %v", got.SideThread, test.sideThread)
			}
		})
	}
}

func TestParentRelationship(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		fields string
		want   string
	}{
		{"current releases", `"source":"subagent","parent_thread_id":"p"`, "p"},
		{"0.135 and 0.136", `"source":"subagent","forked_from_id":"p"`, "p"},
		{
			// On an ordinary session a fork means the user forked a resumable
			// conversation, which does not make the result a side thread.
			name:   "a forked conversation is not a sub-agent",
			fields: `"source":"cli","forked_from_id":"p"`,
			want:   "",
		},
		{"0.133, which recorded nothing", `"source":"subagent"`, ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := only(t, discover(t, fstest.MapFS{
				activePath: rollout(meta(`"id":"a",` + test.fields)),
			}))
			if got.Parent != test.want {
				t.Errorf("Parent = %q, want %q", got.Parent, test.want)
			}
		})
	}
}

func TestMessages(t *testing.T) {
	t.Parallel()

	tree := fstest.MapFS{activePath: rollout(
		meta(`"id":"a","source":"cli"`),
		said("user_message", "hello"),
		// The parallel response_item stream carries the same conversation with
		// injected environment context, and is deliberately ignored.
		`{"type":"response_item","payload":{"type":"message","content":"<environment_context>"}}`,
		said("agent_message", "hi there"),
		`{"type":"event_msg","payload":{"type":"user_message","message":"look","images":["a","b"]}}`,
	)}

	messages := read(t, tree)
	if len(messages) != 3 {
		t.Fatalf("read %d messages, want 3", len(messages))
	}
	if messages[0].Role != session.User || messages[0].Text != "hello" {
		t.Errorf("first message = %+v", messages[0])
	}
	if messages[1].Role != session.Assistant || messages[1].Text != "hi there" {
		t.Errorf("second message = %+v", messages[1])
	}
	if messages[2].Images != 2 {
		t.Errorf("third message carried %d images, want 2", messages[2].Images)
	}
}

func TestMessagesFromAPaginatedRollout(t *testing.T) {
	t.Parallel()

	messages := read(t, fstest.MapFS{activePath: rollout(
		meta(`"id":"a","source":"cli","history_mode":"paginated"`),
		completed(wrote("u1", "hello")),
		completed(`"type":"Reasoning","id":"r1","summary_text":["pondering"]`),
		completed(`"type":"CommandExecution","id":"c1","command":["grep","x"],`+
			`"stdout":"item.type == \"UserMessage\""`),
		completed(`"type":"UserMessage","id":"u2","content":[`+
			`{"type":"text","text":"one "},`+
			`{"type":"image","image_url":"data:image/png;base64,AAAA"},`+
			`{"type":"text","text":"two"},`+
			`{"type":"local_image","path":"/tmp/shot.png"}]`),
		completed(`"type":"AgentMessage","id":"m1","phase":"commentary",`+
			`"content":[{"type":"Text","text":"working on it"}]`),
		completed(replied("m2", "done")),
	)})

	want := []session.Message{
		{Role: session.User, Text: "hello"},
		{Role: session.User, Text: "one two", Images: 2},
		{Role: session.Assistant, Text: "working on it"},
		{Role: session.Assistant, Text: "done"},
	}
	if len(messages) != len(want) {
		t.Fatalf("read %d messages, want %d: %+v", len(messages), len(want), messages)
	}
	for i, message := range messages {
		if message != want[i] {
			t.Errorf("message %d = %+v, want %+v", i, message, want[i])
		}
	}
}

// A single transcript line is routinely megabytes of base64 image data. Losing
// the sessions recorded after one would be a silent, total failure.
func TestDiscoverSurvivesAHugeLine(t *testing.T) {
	t.Parallel()

	got := only(t, discover(t, fstest.MapFS{
		activePath: rollout(
			meta(`"id":"a","source":"cli"`),
			`{"type":"event_msg","payload":{"type":"screenshot","data":"`+
				strings.Repeat("A", 1<<20)+`"}}`,
			said("user_message", "after the blob"),
		),
	}))
	if got.Title != "after the blob" {
		t.Errorf("Title = %q, want the message after the blob", got.Title)
	}
}

func TestDiscoverIgnoresRubbish(t *testing.T) {
	t.Parallel()

	found := discover(t, fstest.MapFS{
		activePath:                         rollout(meta(`"id":"good","source":"cli"`)),
		"sessions/2026/08/01/notes.txt":    rollout("not a rollout"),
		"sessions/2026/08/01/broken.jsonl": rollout("{not json", `{"type":"session_meta"`),
		"archived_sessions/README":         rollout("ignore me"),
	})

	// The malformed rollout has no id anywhere and no usable filename, so it
	// is dropped; nothing else is.
	if len(found) != 1 || found[0].ID != "good" {
		t.Errorf("found %d sessions %v, want only the readable one", len(found), found)
	}
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	a := codex.New("/tmp/codex")
	got := a.Meta()
	// Codex sub-agent rollouts record their parent, so a deleted conversation
	// can leave one stranded; there is no empty-session sweep here.
	if got.OrphanLabel == 0 {
		t.Error("Codex should offer an orphan sweep")
	}
	if got.EmptyLabel != 0 {
		t.Error("Codex should not offer an empty-session sweep")
	}
	if _, ok := any(a).(agent.Archiver); !ok {
		t.Error("Codex should be able to archive")
	}
}

func TestPreflightAcceptsAnyDirectory(t *testing.T) {
	t.Parallel()

	// CODEX_HOME names the directory itself, so any of them will do.
	if err := codex.New("/tmp/anywhere").Preflight(); err != nil {
		t.Errorf("Preflight() = %v", err)
	}
}

// recorder stands in for the Codex command line.
type recorder struct {
	calls []execx.Command
	err   error
}

func (r *recorder) run(_ context.Context, cmd execx.Command) error {
	r.calls = append(r.calls, cmd)
	return r.err
}

func TestOperationsDelegateToTheCodexCommand(t *testing.T) {
	t.Parallel()

	var seen recorder
	a := codex.New("/tmp/codex", codex.WithRunner(seen.run))
	s := session.Session{ID: "abc"}

	if err := a.Archive(t.Context(), s); err != nil {
		t.Fatalf("Archive() = %v", err)
	}
	if err := a.Unarchive(t.Context(), s); err != nil {
		t.Fatalf("Unarchive() = %v", err)
	}
	if err := a.Delete(t.Context(), s); err != nil {
		t.Fatalf("Delete() = %v", err)
	}

	want := [][]string{
		{"archive", "abc"},
		{"unarchive", "abc"},
		// `delete --force` refuses anything that is not a uuid, which is why
		// ids are passed rather than names.
		{"delete", "abc", "--force"},
	}
	for i, call := range seen.calls {
		if strings.Join(call.Args, " ") != strings.Join(want[i], " ") {
			t.Errorf("call %d = %v, want %v", i, call.Args, want[i])
		}
		// The command must act on the same tree the listing was read from.
		if call.Env["CODEX_HOME"] != "/tmp/codex" {
			t.Errorf("call %d ran against %q", i, call.Env["CODEX_HOME"])
		}
	}
}
