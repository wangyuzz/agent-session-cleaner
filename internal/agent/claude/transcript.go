package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/haowang02/agent-session-cleaner/internal/jsonl"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// headScanLines bounds the search for the metadata preamble. cwd, version and
// entrypoint only appear on user and assistant records, which follow a short
// run of mode, permission-mode and file-history-snapshot lines.
const headScanLines = 200

// record is one line of a Claude Code transcript.
type record struct {
	Type       string `json:"type"`
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	Version    string `json:"version"`
	Entrypoint string `json:"entrypoint"`
	Timestamp  string `json:"timestamp"`
	AITitle    string `json:"aiTitle"`

	// Most user records are tool traffic rather than anything a person said.
	// In the largest transcript inspected, 213 of 224 were tool results and
	// another 3 were injected metadata.
	IsMeta        bool            `json:"isMeta"`
	IsSidechain   bool            `json:"isSidechain"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`

	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// spoken reports whether this record belongs to the visible conversation.
func (r *record) spoken() bool {
	if r.Type != "user" && r.Type != "assistant" {
		return false
	}
	return r.ToolUseResult == nil && !r.IsMeta && !r.IsSidechain
}

// details is what one pass over a transcript collects for the listing.
type details struct {
	id        string
	cwd       string
	version   string
	client    string
	created   time.Time
	aiTitle   string
	firstUser string
}

// scan reads a transcript once, gathering everything the list needs.
func scan(r io.Reader) details {
	var found details
	scanner := jsonl.NewScanner(r)
	for lineno := 0; scanner.Scan(); lineno++ {
		line := scanner.Bytes()
		// A line is worth decoding when it may hold a late ai-title, belongs
		// to the metadata preamble, or could be the first thing a person said.
		// That last condition can mean reading the whole file: the sweep for
		// empty sessions must not mistake a long preamble for one.
		wantsTitle := bytes.Contains(line, []byte(`"ai-title"`))
		wantsHead := lineno < headScanLines
		wantsUser := found.firstUser == "" && bytes.Contains(line, []byte(`"user"`))
		if !wantsTitle && !wantsHead && !wantsUser {
			continue
		}

		var entry record
		if json.Unmarshal(line, &entry) != nil {
			continue
		}

		if entry.Type == "ai-title" {
			if title := strings.TrimSpace(entry.AITitle); title != "" {
				found.aiTitle = title // the last one wins
			}
			continue
		}

		if found.id == "" {
			found.id = entry.SessionID
		}
		if found.created.IsZero() {
			found.created = moment(entry.Timestamp)
		}
		if found.cwd == "" {
			found.cwd = entry.Cwd
		}
		if found.version == "" {
			found.version = entry.Version
		}
		if found.client == "" {
			found.client = entry.Entrypoint
		}

		if found.firstUser == "" && entry.Type == "user" && entry.spoken() {
			text, _ := blocks(entry.Message.Content)
			found.firstUser = strings.TrimSpace(text)
		}
	}
	return found
}

// messages reads the human side of a transcript.
func messages(ctx context.Context, r io.Reader) ([]session.Message, error) {
	var found []session.Message
	scanner := jsonl.NewContextScanner(ctx, r)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, []byte(`"user"`)) && !bytes.Contains(line, []byte(`"assistant"`)) {
			continue
		}
		var entry record
		if json.Unmarshal(line, &entry) != nil || !entry.spoken() {
			continue
		}
		text, images := blocks(entry.Message.Content)
		role := session.Assistant
		if entry.Type == "user" {
			role = session.User
		}
		if message, ok := session.NewMessage(role, text, images); ok {
			found = append(found, message)
		}
	}
	return found, scanner.Err()
}

// recordedID is the session id written inside a transcript, which is checked
// against the listing before anything is deleted.
func recordedID(r io.Reader) string {
	scanner := jsonl.NewScanner(r)
	for lineno := 0; lineno < headScanLines && scanner.Scan(); lineno++ {
		line := scanner.Bytes()
		if !bytes.Contains(line, []byte(`"sessionId"`)) {
			continue
		}
		var entry record
		if json.Unmarshal(line, &entry) == nil && entry.SessionID != "" {
			return entry.SessionID
		}
	}
	return ""
}

// blocks flattens a message body into its human-facing text and a count of
// the images it carried.
//
// Content is either a bare string or a list of blocks, of which only text is
// for people: thinking, tool_use and tool_result are dropped.
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
