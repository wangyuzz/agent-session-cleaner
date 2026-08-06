package session_test

import (
	"strings"
	"testing"
	"time"

	"github.com/haowang02/agent-session-cleaner/internal/session"
)

func TestRecencyAtPrefersRecordedStart(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 3, 1, 9, 0, 0, 0, time.Local)
	written := time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local)

	// A migration can rewrite a whole tree and collapse every mtime onto one
	// instant; the recorded start time is what keeps the order honest.
	withStart := session.Session{CreatedAt: created, UpdatedAt: written}
	if got := withStart.RecencyAt(); !got.Equal(created) {
		t.Errorf("RecencyAt() = %v, want the recorded start %v", got, created)
	}

	withoutStart := session.Session{UpdatedAt: written}
	if got := withoutStart.RecencyAt(); !got.Equal(written) {
		t.Errorf("RecencyAt() = %v, want the mtime %v", got, written)
	}
}

func TestCondense(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		text  string
		limit int
		want  string
	}{
		{"collapses whitespace", "fix  the\n\tbuild", 40, "fix the build"},
		{"leaves short text alone", "hello", 40, "hello"},
		{"clips with an ellipsis", "abcdefghij", 5, "abcd…"},
		{"counts characters, not bytes", "中文标题很长很长", 4, "中文标…"},
		{"keeps text at exactly the limit", "abcde", 5, "abcde"},
		{"handles emptiness", "   \n ", 40, ""},
		{"a non-positive limit keeps nothing", "abc", 0, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := session.Condense(test.text, test.limit); got != test.want {
				t.Errorf("Condense(%q, %d) = %q, want %q", test.text, test.limit, got, test.want)
			}
		})
	}
}

func TestNewMessage(t *testing.T) {
	t.Parallel()

	t.Run("drops a turn with nothing in it", func(t *testing.T) {
		t.Parallel()
		if _, ok := session.NewMessage(session.User, "  \n ", 0); ok {
			t.Error("NewMessage kept an empty turn")
		}
	})

	t.Run("keeps a turn that is only an image", func(t *testing.T) {
		t.Parallel()
		message, ok := session.NewMessage(session.User, "", 2)
		if !ok {
			t.Fatal("NewMessage dropped an image-only turn")
		}
		if message.Images != 2 || message.Text != "" {
			t.Errorf("NewMessage() = %+v, want 2 images and no text", message)
		}
	})

	t.Run("truncates by character, not byte", func(t *testing.T) {
		t.Parallel()
		text := strings.Repeat("中", session.MaxMessageChars+5)
		message, ok := session.NewMessage(session.Assistant, text, 0)
		if !ok {
			t.Fatal("NewMessage dropped a long turn")
		}
		if message.Truncated != 5 {
			t.Errorf("Truncated = %d, want 5", message.Truncated)
		}
		if got := len([]rune(message.Text)); got != session.MaxMessageChars {
			t.Errorf("kept %d characters, want %d", got, session.MaxMessageChars)
		}
	})
}

func TestSortByRecency(t *testing.T) {
	t.Parallel()

	day := func(n int) time.Time { return time.Date(2026, 8, n, 0, 0, 0, 0, time.Local) }
	sessions := []session.Session{
		{ID: "old", UpdatedAt: day(1)},
		{ID: "newest", UpdatedAt: day(1), CreatedAt: day(9)},
		{ID: "middle", UpdatedAt: day(5)},
	}
	session.SortByRecency(sessions)

	if got := ids(sessions); got != "newest,middle,old" {
		t.Errorf("SortByRecency ordered %s, want newest,middle,old", got)
	}
}

func ids(sessions []session.Session) string {
	names := make([]string, len(sessions))
	for i, s := range sessions {
		names[i] = s.ID
	}
	return strings.Join(names, ",")
}

// Sessions started within the same second are common — a conversation and the
// sub-agent it spawns — so the order must not depend on which one the
// directory happened to hand over first.
func TestSortByRecencyBreaksTiesDeterministically(t *testing.T) {
	t.Parallel()

	same := time.Date(2026, 8, 5, 12, 0, 0, 0, time.Local)
	forward := []session.Session{
		{ID: "b", CreatedAt: same}, {ID: "a", CreatedAt: same}, {ID: "c", CreatedAt: same},
	}
	backward := []session.Session{
		{ID: "c", CreatedAt: same}, {ID: "b", CreatedAt: same}, {ID: "a", CreatedAt: same},
	}
	session.SortByRecency(forward)
	session.SortByRecency(backward)

	if ids(forward) != ids(backward) {
		t.Errorf("%s and %s, want the same order from either input", ids(forward), ids(backward))
	}
	if got := ids(forward); got != "a,b,c" {
		t.Errorf("SortByRecency = %s, want ties broken by id", got)
	}
}
