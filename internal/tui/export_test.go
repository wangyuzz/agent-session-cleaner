package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haowang02/agent-session-cleaner/internal/session"
)

func TestExportWritesTheCurrentSession(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	f := newFake(session.Session{
		ID: "abc", Title: "fix the build", Cwd: "/work/app",
		CreatedAt: time.Date(2026, 8, 12, 15, 58, 0, 0, time.Local),
	})
	f.talk["abc"] = []session.Message{
		{Role: session.User, Text: "hello"},
		{Role: session.Assistant, Text: "hi there"},
	}
	m := start(t, f)
	press(t, m, "s")

	contains(t, m, "Exported 1 session(s) to")
	got, err := os.ReadFile(filepath.Join(dir, "fix-the-build-abc.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	for _, want := range []string{
		"# fix the build",
		"- Session: `abc`",
		"- Resume: `fake --resume abc`",
		"- Directory: `/work/app`",
		"## You",
		"hello",
		"## Fake",
		"hi there",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("export missing %q:\n%s", want, body)
		}
	}
}

func TestExportWritesSelectedSessionsTogether(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	f := newFake(
		session.Session{ID: "one", Title: "first"},
		session.Session{ID: "two", Title: "second"},
	)
	f.talk["one"] = []session.Message{{Role: session.User, Text: "a"}}
	f.talk["two"] = []session.Message{{Role: session.User, Text: "b"}}
	m := start(t, f)
	press(t, m, "space", "j", "space", "s")

	matches, err := filepath.Glob(filepath.Join(dir, "asc-export-*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("export files = %v, %v", matches, err)
	}
	got, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	if !strings.Contains(body, "# first") || !strings.Contains(body, "# second") {
		t.Errorf("combined export missing a session:\n%s", body)
	}
	if !strings.Contains(body, "\n---\n") {
		t.Error("combined export has no separator")
	}
}

func TestExportDoesNothingWithAnEmptyList(t *testing.T) {
	t.Parallel()

	m := start(t, newFake())
	press(t, m, "s")
	contains(t, m, "No session is available.")
}

func TestSafeFileName(t *testing.T) {
	t.Parallel()

	got := safeFileName(`fix: the /build?`, "abc")
	if got != "fix-the-build-abc" {
		t.Errorf("safeFileName = %q", got)
	}
	if got := safeFileName("", "abc"); got != "session-abc" {
		t.Errorf("empty title = %q", got)
	}
}
