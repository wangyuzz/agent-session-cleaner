package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// talker is one session whose conversation is whatever a test needs to see
// rendered.
func talker(t *testing.T, messages ...session.Message) *Model {
	t.Helper()
	f := newFake(session.Session{ID: "abc", Title: "talk", Client: "cli"})
	f.talk["abc"] = messages
	return start(t, f)
}

func TestTheConversationIsMarkedUp(t *testing.T) {
	t.Parallel()

	m := talker(t, session.Message{
		Role: session.Assistant,
		Text: "Try this **first**, then run `make test`:\n\n- one\n- two\n",
	})

	// The source is gone and what it stood for is on screen in its place.
	contains(t, m, "Try this first, then run make test")
	contains(t, m, "• one")
	omits(t, m, "**first**")
	omits(t, m, "`make test`")
	omits(t, m, "- one")
}

// A message that is not Markdown is most of what a transcript holds, and it
// has to come through as it was written.
func TestPlainMessagesAreLeftAsTheyAre(t *testing.T) {
	t.Parallel()

	m := talker(t, session.Message{Role: session.User, Text: "line one\nline two\nline three"})
	for _, want := range []string{"line one", "line two", "line three"} {
		contains(t, m, want)
	}
}

// An ordinary conversation is drawn once, in its finished form. Putting the
// plain text up and replacing it costs a second repaint of the pane, which
// over a slow connection is the visible part.
func TestASmallConversationIsNeverDrawnTwice(t *testing.T) {
	t.Parallel()

	f := newFake(session.Session{ID: "abc", Title: "talk"})
	f.talk["abc"] = []session.Message{{Role: session.Assistant, Text: "a **strong** word"}}

	m := New(t.Context(), f, i18n.New(i18n.English))
	drive(t, m, m.Init())

	// The very first frame the pane can produce, before anything it hands back
	// has been run.
	send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	contains(t, m, "a strong word")
	omits(t, m, "**strong**")
}

// The largest conversations take the better part of a second, and a resize
// being dragged would freeze on every step. Those put the plain text up first.
func TestALargeConversationShowsItsPlainTextFirst(t *testing.T) {
	t.Parallel()

	f := newFake(session.Session{ID: "abc", Title: "talk"})
	f.talk["abc"] = []session.Message{{
		Role: session.Assistant,
		Text: strings.Repeat("a **strong** word, said again and again.\n\n", 600),
	}}
	if conversationBytes(f.talk["abc"]) <= syncRenderBytes {
		t.Fatal("the fixture is not large enough to be deferred")
	}

	m := New(t.Context(), f, i18n.New(i18n.English))
	drive(t, m, m.Init())

	cmd := send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	contains(t, m, "**strong**")

	drive(t, m, cmd)
	contains(t, m, "a strong word")
	omits(t, m, "**strong**")
}

// A conversation is marked up for one width. The pane growing or shrinking
// makes the lines it holds the wrong shape, and nothing downstream wraps them.
func TestResizingLaysTheConversationOutAgain(t *testing.T) {
	t.Parallel()

	m := talker(t, session.Message{
		Role: session.Assistant,
		Text: "A reply long enough that it has to be broken somewhere, " +
			"whatever width the pane happens to be, plus 一段中文用来占位。",
	})

	for _, width := range []int{60, 120, 200, 46} {
		drive(t, m, send(t, m, tea.WindowSizeMsg{Width: width, Height: 30}))
		for i, line := range strings.Split(m.detail.View(), "\n") {
			if got := ansi.StringWidth(strings.TrimRight(line, " ")); got > m.detailWidth() {
				t.Errorf("at width %d, conversation line %d is %d cells wide, pane is %d:\n%s",
					width, i, got, m.detailWidth(), ansi.Strip(line))
			}
		}
		if _, held := m.renders.get(m.want()); !held {
			t.Errorf("at width %d, the conversation was not laid out again", m.detailWidth())
		}
	}
}

func TestAConversationIsMarkedUpOnce(t *testing.T) {
	t.Parallel()

	f := newFake(
		session.Session{ID: "abc", Title: "first"},
		session.Session{ID: "xyz", Title: "second"},
	)
	f.talk["abc"] = []session.Message{{Role: session.Assistant, Text: "the **first** reply"}}
	f.talk["xyz"] = []session.Message{{Role: session.Assistant, Text: "the **second** reply"}}

	m := start(t, f)
	first := m.want()
	if _, held := m.renders.get(first); !held {
		t.Fatal("the conversation was never marked up")
	}

	press(t, m, "j")
	contains(t, m, "the second reply")
	press(t, m, "k")
	contains(t, m, "the first reply")

	if _, held := m.renders.get(first); !held {
		t.Error("coming back to a conversation threw its rendering away")
	}
	if len(m.renders.order) != 2 {
		t.Errorf("holding %d renderings for two sessions", len(m.renders.order))
	}
	if cmd := m.renderDetail(); cmd != nil {
		t.Error("a conversation already on screen was marked up again")
	}
}

func TestTheRenderCacheIsBoundedByWhatItHolds(t *testing.T) {
	t.Parallel()

	var cache renderCache
	big := []string{strings.Repeat("x", renderCacheBytes+1)}
	cache.put(bodyKey{width: 1}, big)
	if _, held := cache.get(bodyKey{width: 1}); !held {
		t.Error("the newest rendering was dropped, and it is the one on screen")
	}

	cache.put(bodyKey{width: 2}, []string{"small"})
	if _, held := cache.get(bodyKey{width: 1}); held {
		t.Error("an oversized rendering was kept once something newer arrived")
	}
	if _, held := cache.get(bodyKey{width: 2}); !held {
		t.Error("the newest rendering was dropped")
	}
	if cache.bytes != len("small") {
		t.Errorf("cache weighs %d, want %d", cache.bytes, len("small"))
	}

	cache.clear()
	if _, held := cache.get(bodyKey{width: 2}); held || cache.bytes != 0 {
		t.Error("clear() left something behind")
	}
}

func TestDeferredRendersAreCoalesced(t *testing.T) {
	t.Parallel()

	f := newFake(session.Session{ID: "abc", Title: "talk"})
	f.talk["abc"] = []session.Message{{
		Role: session.Assistant,
		Text: strings.Repeat("a **strong** word\n\n", 1000),
	}}
	m := New(t.Context(), f, i18n.New(i18n.English))
	drive(t, m, m.Init())

	first := send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if first == nil || m.rendering == nil {
		t.Fatal("the first large layout did not start")
	}
	old := *m.rendering
	if next := send(t, m, tea.WindowSizeMsg{Width: 160, Height: 30}); next != nil {
		t.Fatal("a resize started a second render while one was running")
	}
	latest := m.want()

	drive(t, m, first)
	if m.rendering != nil {
		t.Error("rendering did not settle")
	}
	if _, held := m.renders.get(old); held {
		t.Error("the obsolete width was cached")
	}
	if _, held := m.renders.get(latest); !held {
		t.Error("the latest width was not rendered")
	}
}

func TestEvictingMessagesAlsoEvictsTheirRendering(t *testing.T) {
	t.Parallel()

	m := &Model{messages: map[cacheKey][]session.Message{}}
	var first cacheKey
	for i := range detailCacheLimit + 1 {
		key := cacheKey{id: fmt.Sprintf("session-%d", i)}
		if i == 0 {
			first = key
		}
		m.renders.put(bodyKey{session: key, width: 40}, []string{"rendered"})
		m.remember(key, []session.Message{{Text: "message"}})
	}
	if _, held := m.messages[first]; held {
		t.Fatal("the oldest messages were not evicted")
	}
	if _, held := m.renders.get(bodyKey{session: first, width: 40}); held {
		t.Error("the rendering outlived its messages")
	}
}

func TestNotesAboutAMessageAreNotMarkedUp(t *testing.T) {
	t.Parallel()

	m := talker(t,
		session.Message{Role: session.User, Text: "look", Images: 2},
		session.Message{Role: session.Assistant, Text: "long", Truncated: 1200},
	)
	contains(t, m, "[2 images]")
	contains(t, m, "Message truncated: 1200 trailing characters omitted")
}

func TestAnsweringLightRedrawsTheConversation(t *testing.T) {
	t.Parallel()

	m := talker(t, session.Message{Role: session.Assistant, Text: "a **strong** word"})
	dark := m.detail.View()
	inTheDark := m.want()
	if !m.theme.Dark {
		t.Fatal("the pane did not start out dark")
	}

	drive(t, m, send(t, m, tea.BackgroundColorMsg{Color: lipgloss.White}))
	if m.theme.Dark {
		t.Fatal("the pane stayed dark")
	}
	light := m.detail.View()

	if _, held := m.renders.get(inTheDark); held {
		t.Error("the rendering for the other colour scheme was kept")
	}

	held := len(m.renders.order)
	drive(t, m, send(t, m, tea.BackgroundColorMsg{Color: lipgloss.White}))
	if len(m.renders.order) != held {
		t.Errorf("being told light again left %d renderings, had %d",
			len(m.renders.order), held)
	}

	if light == dark {
		t.Error("the conversation looks the same on a light terminal as on a dark one")
	}
	if ansi.Strip(light) != ansi.Strip(dark) {
		t.Errorf("the words changed with the colour scheme:\n%s\n---\n%s",
			ansi.Strip(dark), ansi.Strip(light))
	}
	contains(t, m, "a strong word")
}
