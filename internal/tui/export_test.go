package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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

func TestExportUsesConfiguredDirectoryAndPreservesExistingFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	existing := filepath.Join(dir, "fix-the-build-abc.md")
	if err := os.WriteFile(existing, []byte("previous export"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newFake(session.Session{ID: "abc", Title: "fix the build"})
	f.talk["abc"] = []session.Message{{Role: session.User, Text: "new conversation"}}
	m := start(t, f)
	WithExportDirectory(dir)(m)
	press(t, m, "s")
	contains(t, m, "Exported 1 session(s) to")
	got, err := os.ReadFile(existing)
	if err != nil || string(got) != "previous export" {
		t.Fatalf("previous export was changed: %q, %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dir, "fix-the-build-abc-2.md"))
	if err != nil || !strings.Contains(string(got), "new conversation") {
		t.Fatalf("new export = %q, %v", got, err)
	}
}

func TestSafeFileNameIsPortable(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ title, id, want string }{
		{"CON", "", "session-CON"},
		{"nul.txt", "abc", "session-nul.txt-abc"},
		{"LPT1", "", "session-LPT1"},
		{"COM¹", "", "session-COM¹"},
		{"COM10", "", "COM10"},
		{"题目", "../bad/id\\name:*?", "题目-bad-id-name"},
		{"..", "../..", "session"},
		{"hello\x00world", "abc\x00def", "helloworld-abcdef"},
	} {
		t.Run(test.want, func(t *testing.T) {
			if got := safeFileName(test.title, test.id); got != test.want {
				t.Errorf("safeFileName(%q, %q) = %q, want %q", test.title, test.id, got, test.want)
			}
		})
	}
	name := safeFileName(strings.Repeat("题", 100), strings.Repeat("界", 1000)) + ".md"
	if !utf8.ValidString(name) || len(name) > 210 || !filepath.IsLocal(name) || filepath.Base(name) != name {
		t.Fatalf("non-portable filename: %q (%d bytes)", name, len(name))
	}
	path, err := writeExport(t.Context(), t.TempDir(), name, func(out io.Writer) error {
		_, err := io.WriteString(out, "Unicode filename")
		return err
	})
	if err != nil {
		t.Fatalf("could not export to Unicode filename: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentExportsDoNotOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const count = 16
	type result struct {
		path, body string
		err        error
	}
	results := make(chan result, count)
	begin := make(chan struct{})
	var workers sync.WaitGroup
	for i := range count {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-begin
			body := fmt.Sprintf("conversation %d", i)
			path, err := writeExport(t.Context(), dir, "same.md", func(out io.Writer) error {
				_, err := io.WriteString(out, body)
				return err
			})
			results <- result{path, body, err}
		}()
	}
	close(begin)
	workers.Wait()
	close(results)
	seen := map[string]bool{}
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if seen[result.path] {
			t.Fatalf("multiple exports chose %q", result.path)
		}
		seen[result.path] = true
		got, err := os.ReadFile(result.path)
		if err != nil || string(got) != result.body {
			t.Errorf("export was overwritten: %q, want %q (%v)", got, result.body, err)
		}
	}
}

func TestFailedExportsRemovePartialFiles(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"read error", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := errors.New("transcript read failed")
			if failure == "cancelled" {
				want = context.Canceled
			}
			path, err := writeExport(ctx, dir, "partial.md", func(out io.Writer) error {
				// Force a flush, so cleanup must remove bytes already on disk.
				if _, err := io.WriteString(out, strings.Repeat("x", 128<<10)); err != nil {
					return err
				}
				if failure == "cancelled" {
					cancel()
					return nil
				}
				return want
			})
			if !errors.Is(err, want) || path != "" {
				t.Fatalf("failed export = %q, %v, want %v", path, err, want)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("partial files remain: %v, %v", entries, err)
			}
		})
	}
}

func TestCancelledExportDoesNotWriteCachedMessages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := newFake(session.Session{ID: "abc", Title: "cached"})
	f.talk["abc"] = []session.Message{{Role: session.User, Text: "cached content"}}
	m := start(t, f)
	WithExportDirectory(dir)(m)
	ctx, cancel := context.WithCancel(t.Context())
	m.ctx = ctx
	cancel()
	msg := m.exportMarkdown()().(exportedMsg)
	if !errors.Is(msg.err, context.Canceled) {
		t.Fatalf("cancelled export = %v", msg.err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancelled export created files: %v, %v", entries, err)
	}
}

func TestExportReportsMissingDirectory(t *testing.T) {
	t.Parallel()
	m := start(t, newFake(session.Session{ID: "abc"}))
	WithExportDirectory(filepath.Join(t.TempDir(), "missing"))(m)
	press(t, m, "s")
	contains(t, m, "Could not export")
}

func TestExportFilesHavePrivatePermissions(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses ACLs rather than Unix permission bits")
	}
	path, err := writeExport(t.Context(), t.TempDir(), "private.md", func(out io.Writer) error {
		_, err := io.WriteString(out, "conversation")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("export permissions = %o, want owner only", info.Mode().Perm())
	}
}

func TestExportMarksTruncatedMessages(t *testing.T) {
	t.Parallel()
	message, _ := session.NewMessage(session.User, strings.Repeat("x", session.MaxMessageChars+123), 0)
	body := sessionMarkdown(session.Session{ID: "abc", Title: "first\nsecond"}, []session.Message{message}, "", "You", "Agent")
	if !strings.HasPrefix(body, "# first second\n") || !strings.Contains(body, "Preview truncated: 123 character(s) omitted.") {
		t.Errorf("export does not identify truncated content or normalize the title:\n%s", body)
	}
}
