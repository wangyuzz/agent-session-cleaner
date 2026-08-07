package agent_test

import (
	"os"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/agent/claude"
	"github.com/haowang02/agent-session-cleaner/internal/agent/codex"
	"github.com/haowang02/agent-session-cleaner/internal/agent/opencode"
	"github.com/haowang02/agent-session-cleaner/internal/agent/pi"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// These read the session trees on this machine, to prove the parsers cope with
// data as it is actually written rather than only with fixtures. They never
// write anything, and skip themselves where there is nothing to read — which
// is what a CI runner looks like.
func TestRealSessionTrees(t *testing.T) {
	t.Parallel()

	for _, target := range []agent.Agent{codex.New(""), claude.New(""), opencode.New(""), pi.New("")} {
		meta := target.Meta()
		t.Run(meta.ID, func(t *testing.T) {
			t.Parallel()
			if info, err := os.Stat(meta.Home); err != nil || !info.IsDir() {
				t.Skipf("no %s session tree on this machine", meta.Label)
			}

			found, err := target.Discover(t.Context())
			if err != nil {
				t.Fatalf("Discover() = %v", err)
			}
			if len(found) == 0 {
				t.Skipf("%s has no sessions here", meta.Label)
			}
			t.Logf("%s: %d sessions", meta.Label, len(found))

			checkListing(t, meta, found)
			checkConversation(t, target, found[0])
		})
	}
}

func checkListing(t *testing.T, meta agent.Meta, found []session.Session) {
	t.Helper()

	seen := make(map[string]bool, len(found))
	newest := time.Time{}
	for i, s := range found {
		switch {
		case s.ID == "":
			t.Errorf("session %d has no id", i)
		case seen[s.ID]:
			t.Errorf("two sessions answer to %q", s.ID)
		case s.Agent != meta.ID:
			t.Errorf("session %q claims to belong to %q", s.ID, s.Agent)
		case s.Path == "":
			t.Errorf("session %q says nothing about where it lives", s.ID)
		case !utf8.ValidString(s.Title):
			t.Errorf("session %q has a title that is not valid UTF-8", s.ID)
		case utf8.RuneCountInString(s.Title) > session.MaxTitleChars:
			t.Errorf("session %q has an unclipped title", s.ID)
		case s.RecencyAt().IsZero():
			t.Errorf("session %q has no time at all", s.ID)
		}
		seen[s.ID] = true

		// Newest first.
		if i > 0 && s.RecencyAt().After(newest) {
			t.Errorf("session %q is newer than the one before it", s.ID)
		}
		newest = s.RecencyAt()

		// A session cannot be its own parent.
		if s.Parent == s.ID && s.Parent != "" {
			t.Errorf("session %q claims itself as its parent", s.ID)
		}
	}

	// The tree has to hold together: every row is laid out exactly once.
	forest := session.Build(found)
	if got := len(forest.Rows()); got != len(found) {
		t.Errorf("arranged %d rows out of %d sessions", got, len(found))
	}
	if stranded := forest.Orphans(); len(stranded) > 0 {
		t.Logf("%d orphaned sub-agent sessions", len(stranded))
	}
}

func checkConversation(t *testing.T, target agent.Agent, s session.Session) {
	t.Helper()

	messages, err := target.Messages(t.Context(), s)
	if err != nil {
		t.Fatalf("Messages(%q) = %v", s.ID, err)
	}
	for i, message := range messages {
		switch {
		case message.Role != session.User && message.Role != session.Assistant:
			t.Errorf("message %d has role %d", i, message.Role)
		case message.Text == "" && message.Images == 0:
			t.Errorf("message %d is empty and should have been dropped", i)
		case utf8.RuneCountInString(message.Text) > session.MaxMessageChars:
			t.Errorf("message %d was not clipped", i)
		case !utf8.ValidString(message.Text):
			t.Errorf("message %d is not valid UTF-8", i)
		}
	}
}
