package opencode_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/agent/opencode"
	"github.com/haowang02/agent-session-cleaner/internal/execx"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// The schema, reduced to the columns this program reads. OpenCode has added
// columns over time, so a fixture leaving some out is a realistic old database
// rather than an invalid one.
const schema = `
CREATE TABLE session (
	id TEXT PRIMARY KEY, title TEXT, time_created INTEGER, time_updated INTEGER,
	time_archived INTEGER, parent_id TEXT, agent TEXT, directory TEXT, version TEXT);
CREATE TABLE message (
	id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, data TEXT);
CREATE TABLE part (
	id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT, time_created INTEGER, data TEXT);`

// home builds an OpenCode data directory, correctly named, holding a database
// built by setup.
func home(t *testing.T, ddl string, setup func(*sql.DB)) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	if setup != nil {
		setup(db)
	}
	return dir
}

// conversation writes one message and its parts.
func conversation(t *testing.T, db *sql.DB, sessionID, messageID, role string, parts ...string) {
	t.Helper()
	exec(t, db, `INSERT INTO message VALUES (?,?,?,?)`,
		messageID, sessionID, 1, `{"role":"`+role+`"}`)
	for i, part := range parts {
		exec(t, db, `INSERT INTO part VALUES (?,?,?,?,?)`,
			messageID+"-p"+string(rune('a'+i)), messageID, sessionID, i, part)
	}
}

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func discover(t *testing.T, dir string) []session.Session {
	t.Helper()
	found, err := opencode.New(dir).Discover(t.Context())
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

	dir := home(t, schema, func(db *sql.DB) {
		exec(t, db, `INSERT INTO session VALUES
			('ses_root','Refactor the parser',1754000000000,1754000100000,NULL,NULL,
			 'build','/work/app','1.20'),
			('ses_child','Child work',1753000000000,1753000000000,NULL,'ses_root',
			 'explore','/work/app','1.20')`)
	})

	found := discover(t, dir)
	if len(found) != 2 {
		t.Fatalf("found %d sessions, want 2", len(found))
	}

	// Newest first.
	root := found[0]
	if root.ID != "ses_root" || root.Title != "Refactor the parser" {
		t.Errorf("first session = %+v", root)
	}
	if root.Client != "build" || root.Cwd != "/work/app" || root.Version != "1.20" {
		t.Errorf("metadata = %+v", root)
	}
	if root.Archived || root.SideThread || root.Parent != "" {
		t.Errorf("root claimed archived=%v sideThread=%v parent=%q",
			root.Archived, root.SideThread, root.Parent)
	}
	if !root.CreatedAt.Equal(root.CreatedAt.Local()) {
		t.Error("timestamps should be local")
	}

	child := found[1]
	if child.Parent != "ses_root" || !child.SideThread || child.Client != "explore" {
		t.Errorf("child session = %+v", child)
	}
}

// OpenCode names a session the moment it is created and only replaces that
// once the model produces a summary. Both spellings mean "not titled yet", so
// the opening message is a better answer than either.
func TestPlaceholderTitlesFallBackToTheOpeningMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		recorded string
		want     string
		noise    bool
	}{
		{"a real title is kept", "Fix the parser", "Fix the parser", false},
		{"the dated placeholder", "New session - 2026-08-01T09:30:00.000Z", "what I asked", false},
		{"a child placeholder", "Child session - 2026-08-01T09:30:00.000Z", "what I asked", false},
		{"the pre-1.18 placeholder", "New Conversation", "what I asked", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := home(t, schema, func(db *sql.DB) {
				exec(t, db, `INSERT INTO session VALUES
					('ses_a',?,1754000000000,1754000000000,NULL,NULL,'build','/w','1.20')`,
					test.recorded)
				conversation(t, db, "ses_a", "msg_1", "user", `{"type":"text","text":"what I asked"}`)
			})
			got := only(t, discover(t, dir))
			if got.Title != test.want {
				t.Errorf("Title = %q, want %q", got.Title, test.want)
			}
			if got.Noise != test.noise {
				t.Errorf("Noise = %v, want %v", got.Noise, test.noise)
			}
		})
	}
}

// Synthetic text is tool plumbing that OpenCode stores as text. It reads like
// the assistant talking, and is not.
func TestSyntheticPartsAreNotConversation(t *testing.T) {
	t.Parallel()

	dir := home(t, schema, func(db *sql.DB) {
		exec(t, db, `INSERT INTO session VALUES
			('ses_a','New Conversation',1754000000000,1754000000000,NULL,NULL,'build','/w','1.20')`)
		conversation(t, db, "ses_a", "msg_1", "user",
			`{"type":"text","text":"Called the Read tool","synthetic":true}`,
			`{"type":"text","text":"actually asked this"}`)
	})

	if got := only(t, discover(t, dir)); got.Title != "actually asked this" {
		t.Errorf("Title = %q, want the real message", got.Title)
	}
}

func TestArchivedSessions(t *testing.T) {
	t.Parallel()

	dir := home(t, schema, func(db *sql.DB) {
		exec(t, db, `INSERT INTO session VALUES
			('ses_a','Put aside',1754000000000,1754000000000,1754000200000,NULL,'build','/w','1.20')`)
	})

	if got := only(t, discover(t, dir)); !got.Archived {
		t.Error("a session with an archive timestamp was not marked archived")
	}
}

// An older database simply does not have every column this program reads.
func TestDiscoverToleratesAnOlderSchema(t *testing.T) {
	t.Parallel()

	dir := home(t, `
		CREATE TABLE session (id TEXT PRIMARY KEY, title TEXT, time_created INTEGER);
		CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, data TEXT);
		CREATE TABLE part (id TEXT, message_id TEXT, session_id TEXT, time_created INTEGER, data TEXT);`,
		func(db *sql.DB) {
			exec(t, db, `INSERT INTO session VALUES ('ses_a','Ancient',1754000000000)`)
		})

	got := only(t, discover(t, dir))
	if got.ID != "ses_a" || got.Title != "Ancient" {
		t.Errorf("session = %+v", got)
	}
	if got.Archived || got.Client != "build" {
		t.Errorf("missing columns should read as defaults, got %+v", got)
	}
}

func TestDiscoverWithNoDatabaseYet(t *testing.T) {
	t.Parallel()

	// OpenCode is installed but has never been used. That is an answer, not a
	// failure.
	dir := filepath.Join(t.TempDir(), "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if found := discover(t, dir); len(found) != 0 {
		t.Errorf("found %v, want nothing", found)
	}
}

func TestMessages(t *testing.T) {
	t.Parallel()

	dir := home(t, schema, func(db *sql.DB) {
		exec(t, db, `INSERT INTO session VALUES
			('ses_a','Talk',1754000000000,1754000000000,NULL,NULL,'build','/w','1.20')`)
		conversation(t, db, "ses_a", "msg_1", "user",
			`{"type":"text","text":"hello"}`,
			`{"type":"file","mime":"image/png","filename":"shot.png"}`)
		conversation(t, db, "ses_a", "msg_2", "assistant",
			`{"type":"tool","tool":"read"}`,
			`{"type":"text","text":"hi there"}`)
		// Step boundaries and reasoning are part of the run, not the talk.
		conversation(t, db, "ses_a", "msg_3", "assistant",
			`{"type":"reasoning","text":"hmm"}`)
	})

	found := discover(t, dir)
	messages, err := opencode.New(dir).Messages(t.Context(), found[0])
	if err != nil {
		t.Fatalf("Messages() = %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("read %d messages, want 2", len(messages))
	}
	if messages[0].Role != session.User || messages[0].Text != "hello" {
		t.Errorf("first message = %+v", messages[0])
	}
	if messages[0].Images != 1 {
		t.Errorf("first message carried %d images, want 1", messages[0].Images)
	}
	if messages[1].Role != session.Assistant || messages[1].Text != "hi there" {
		t.Errorf("second message = %+v", messages[1])
	}
}

// XDG_DATA_HOME names the *parent*: OpenCode appends "opencode" to it. A
// directory by any other name can be read, but a delete command built from it
// would name a sibling directory, which is a different session tree.
func TestPreflightRefusesAMisnamedDirectory(t *testing.T) {
	t.Parallel()

	err := opencode.New("/tmp/my-sessions").Preflight()
	if !errors.Is(err, agent.ErrUnusableHome) {
		t.Fatalf("Preflight() = %v, want ErrUnusableHome", err)
	}
	message := i18n.New(i18n.English).Err(err)
	if !strings.Contains(message, "/tmp/my-sessions") {
		t.Errorf("message = %q, want it to name the directory", message)
	}

	if err := opencode.New("/tmp/whatever/opencode").Preflight(); err != nil {
		t.Errorf("Preflight() on a correctly named directory = %v", err)
	}
}

func TestDeleteRefusesAMisnamedDirectory(t *testing.T) {
	t.Parallel()

	// The backstop for an agent built directly rather than through the
	// command line, which refuses such a home at startup.
	a := opencode.New("/tmp/my-sessions", opencode.WithRunner(func(context.Context, execx.Command) error {
		t.Error("the command should never have run")
		return nil
	}))
	if err := a.Delete(t.Context(), session.Session{ID: "ses_a"}); !errors.Is(err, agent.ErrUnusableHome) {
		t.Errorf("Delete() = %v, want ErrUnusableHome", err)
	}
}

func TestDeleteRefusesAMalformedID(t *testing.T) {
	t.Parallel()

	a := opencode.New("/tmp/x/opencode", opencode.WithRunner(func(context.Context, execx.Command) error {
		t.Error("the command should never have run")
		return nil
	}))
	err := a.Delete(t.Context(), session.Session{ID: "../../etc/passwd"})
	if !errors.Is(err, agent.ErrBadSessionID) {
		t.Errorf("Delete() = %v, want ErrBadSessionID", err)
	}
}

func TestDeleteDelegatesAndClearsLeftovers(t *testing.T) {
	t.Parallel()

	dir := home(t, schema, func(db *sql.DB) {
		exec(t, db, `INSERT INTO session VALUES
			('ses_a','Talk',1754000000000,1754000000000,NULL,NULL,'build','/w','1.20')`)
	})
	// Written once by the migration out of the JSON storage era and never
	// cleaned up again.
	diff := filepath.Join(dir, "storage", "session_diff", "ses_a.json")
	if err := os.MkdirAll(filepath.Dir(diff), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(diff, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	var seen recorder
	a := opencode.New(dir, opencode.WithRunner(seen.run))
	if err := a.Delete(t.Context(), session.Session{ID: "ses_a"}); err != nil {
		t.Fatalf("Delete() = %v", err)
	}

	call := seen.only(t)
	if strings.Join(call.Args, " ") != "session delete ses_a" {
		t.Errorf("ran %v", call.Args)
	}
	// The command must act on the same tree the listing was read from, and
	// XDG_DATA_HOME names the parent.
	if call.Env["XDG_DATA_HOME"] != filepath.Dir(dir) {
		t.Errorf("ran against %q", call.Env["XDG_DATA_HOME"])
	}
	if _, err := os.Stat(diff); !os.IsNotExist(err) {
		t.Error("the stale diff file was left behind")
	}
}

// The command can fail after the row is already gone, and deleting one that
// somebody else removed first is not worth complaining about.
func TestDeleteSettlesAgainstTheDatabase(t *testing.T) {
	t.Parallel()

	var db *sql.DB
	dir := home(t, schema, func(open *sql.DB) {
		exec(t, open, `INSERT INTO session VALUES
			('ses_a','Talk',1754000000000,1754000000000,NULL,NULL,'build','/w','1.20')`)
	})
	db, err := sql.Open("sqlite", filepath.Join(dir, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	a := opencode.New(dir, opencode.WithRunner(func(context.Context, execx.Command) error {
		exec(t, db, `DELETE FROM session WHERE id = 'ses_a'`)
		return i18n.Raw("Error: something went wrong afterwards")
	}))

	if err := a.Delete(t.Context(), session.Session{ID: "ses_a"}); err != nil {
		t.Errorf("Delete() = %v, want success once the row is gone", err)
	}
}

// A batch of deletions, or an OpenCode window open elsewhere, can hold the
// write lock. That is a queue, not a refusal.
func TestDeleteWaitsOutABusyDatabase(t *testing.T) {
	t.Parallel()

	dir := home(t, schema, func(db *sql.DB) {
		exec(t, db, `INSERT INTO session VALUES
			('ses_a','Talk',1754000000000,1754000000000,NULL,NULL,'build','/w','1.20')`)
	})

	var attempts int
	a := opencode.New(dir, opencode.WithRunner(func(context.Context, execx.Command) error {
		attempts++
		if attempts == 1 {
			return i18n.Raw("Error: database is locked")
		}
		return nil
	}))

	if err := a.Delete(t.Context(), session.Session{ID: "ses_a"}); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if attempts != 2 {
		t.Errorf("ran %d times, want a retry after the lock", attempts)
	}
}

func TestDeleteGivesUpOnARealRefusal(t *testing.T) {
	t.Parallel()

	dir := home(t, schema, func(db *sql.DB) {
		exec(t, db, `INSERT INTO session VALUES
			('ses_a','Talk',1754000000000,1754000000000,NULL,NULL,'build','/w','1.20')`)
	})

	var attempts int
	a := opencode.New(dir, opencode.WithRunner(func(context.Context, execx.Command) error {
		attempts++
		return i18n.Raw("Error: no such session")
	}))

	if err := a.Delete(t.Context(), session.Session{ID: "ses_a"}); err == nil {
		t.Fatal("Delete() succeeded on a refusal")
	}
	if attempts != 1 {
		t.Errorf("ran %d times, want no retry", attempts)
	}
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	a := opencode.New("/tmp/x/opencode")
	// OpenCode records an archive timestamp, but only its desktop app writes
	// one: there is no command to delegate to, and editing the database behind
	// a running instance is not a safe alternative.
	if _, ok := any(a).(agent.Archiver); ok {
		t.Error("OpenCode cannot archive, but claims to")
	}
	meta := a.Meta()
	if meta.EmptyLabel != 0 {
		t.Error("a session row only exists once something was said; there are no empty ones")
	}
	if meta.OrphanLabel == 0 {
		t.Error("migrated data predates the cascade guarantee, so orphans can exist")
	}
}

type recorder struct {
	mu    sync.Mutex
	calls []execx.Command
}

func (r *recorder) run(_ context.Context, cmd execx.Command) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, cmd)
	return nil
}

func (r *recorder) only(t *testing.T) execx.Command {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) != 1 {
		t.Fatalf("ran %d commands, want 1", len(r.calls))
	}
	return r.calls[0]
}
