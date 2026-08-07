package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/haowang02/agent-session-cleaner/internal/jsonl"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// header is the first line of a session file, and the only line that is not
// part of the tree below it.
type header struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
}

// entry is one line past the header.
//
// Every entry names its parent, and the file is one tree rather than one
// conversation: branching with /tree or /fork leaves the abandoned path in
// place. Non-message entries — a model change, a bookmark — sit on the same
// tree, so the parent chain runs through them.
type entry struct {
	Type     string `json:"type"`
	Version  int    `json:"version"`
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	// Name is the display name the user assigned, on a session_info entry.
	Name    string `json:"name"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// details is what one pass over a session file collects for the listing.
type details struct {
	id        string
	cwd       string
	created   time.Time
	name      string
	firstUser string
}

// scan reads a session file once, gathering everything the list needs. The
// second result is false for a file that is not a pi session.
func scan(r io.Reader) (details, bool) {
	var found details
	scanner := jsonl.NewScanner(r)
	if !scanner.Scan() {
		return found, false
	}
	var head header
	if json.Unmarshal(scanner.Bytes(), &head) != nil || head.Type != "session" {
		return found, false
	}
	found.id, found.cwd, found.created = head.ID, head.Cwd, moment(head.Timestamp)

	for scanner.Scan() {
		line := scanner.Bytes()
		// A line is worth decoding when it may rename the session or could be
		// the first thing a person said.
		wantsName := bytes.Contains(line, []byte(`"session_info"`))
		wantsUser := found.firstUser == "" && bytes.Contains(line, []byte(`"user"`))
		if !wantsName && !wantsUser {
			continue
		}

		var e entry
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		switch {
		case e.Type == "session_info":
			// The last one wins, and an empty name is a name being taken away.
			found.name = strings.TrimSpace(e.Name)
		case e.Type == "message" && e.Message.Role == "user" && found.firstUser == "":
			text, _ := blocks(e.Message.Content)
			found.firstUser = strings.TrimSpace(text)
		}
	}
	return found, true
}

// messages reads the conversation pi itself would resume with.
//
// A session file is a tree, not a transcript: /tree and /fork leave the paths
// they moved off still written down. Pi takes the last line as the leaf and
// walks parents from there, so that is what the preview shows. Reading the
// file in order would show the same question answered twice.
func messages(ctx context.Context, r io.Reader) ([]session.Message, error) {
	var (
		parents = map[string]string{}
		spoken  = map[string]session.Message{}
		leaf    string
		version = 1
		linear  []session.Message
	)

	scanner := jsonl.NewContextScanner(ctx, r)
	for scanner.Scan() {
		var e entry
		if json.Unmarshal(scanner.Bytes(), &e) != nil {
			continue
		}
		if e.Type == "session" {
			version = e.Version
			continue
		}
		message, hasMessage := spokenMessage(e)
		// Version 1 predates the entry tree. Pi migrates it on first open; until
		// then the file is simply the conversation in storage order.
		if version < 2 {
			if hasMessage {
				linear = append(linear, message)
			}
			continue
		}
		if e.ID == "" {
			continue
		}
		// A repeated id is not something pi writes — it checks for collisions
		// when it mints one — but the later entry wins here as it does in pi's
		// own index, and the walk below refuses to go round in a circle.
		parents[e.ID] = e.ParentID
		leaf = e.ID
		delete(spoken, e.ID)
		if hasMessage {
			spoken[e.ID] = message
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if version < 2 {
		return linear, nil
	}

	var found []session.Message
	seen := map[string]bool{}
	for id := leaf; id != "" && !seen[id]; id = parents[id] {
		seen[id] = true
		if message, ok := spoken[id]; ok {
			found = append(found, message)
		}
	}
	slices.Reverse(found)
	return found, nil
}

func spokenMessage(e entry) (session.Message, bool) {
	if e.Type != "message" {
		return session.Message{}, false
	}
	var role session.Role
	switch e.Message.Role {
	case "user":
		role = session.User
	case "assistant":
		role = session.Assistant
	default:
		return session.Message{}, false
	}
	text, images := blocks(e.Message.Content)
	return session.NewMessage(role, text, images)
}

// recordedID reads the identity used before deletion. The second result says
// whether the file starts with a valid session header; old headers may be
// valid without carrying an id.
func recordedID(r io.Reader) (string, bool) {
	scanner := jsonl.NewScanner(r)
	if !scanner.Scan() {
		return "", false
	}
	var head header
	if json.Unmarshal(scanner.Bytes(), &head) != nil || head.Type != "session" {
		return "", false
	}
	return head.ID, true
}

// blocks flattens a message body into its human-facing text and a count of the
// images it carried.
//
// Content is either a bare string or a list of blocks, of which only text is
// for people: thinking and toolCall are the agent working, not talking.
func blocks(content json.RawMessage) (text string, images int) {
	if len(content) == 0 {
		return "", 0
	}
	var plain string
	if json.Unmarshal(content, &plain) == nil {
		return plain, 0
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &parts) != nil {
		return "", 0
	}
	var spoken []string
	for _, part := range parts {
		switch part.Type {
		case "text":
			spoken = append(spoken, part.Text)
		case "image":
			images++
		}
	}
	return strings.Join(spoken, "\n"), images
}

// moment parses a recorded timestamp into local time, which is what the whole
// interface shows.
func moment(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	when, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return when.Local()
}
