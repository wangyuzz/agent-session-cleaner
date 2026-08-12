package grok

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/jsonl"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// summary is the index Grok writes for a session directory.
type summary struct {
	Info struct {
		ID  string `json:"id"`
		Cwd string `json:"cwd"`
	} `json:"info"`
	SessionSummary string `json:"session_summary"`
	GeneratedTitle string `json:"generated_title"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	LastActiveAt   string `json:"last_active_at"`
	AgentName      string `json:"agent_name"`
}

// update is one line of updates.jsonl, the ACP session stream.
type update struct {
	Params struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string `json:"sessionUpdate"`
			Content       struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"update"`
	} `json:"params"`
}

func (u update) kind() string {
	return u.Params.Update.SessionUpdate
}

func (u update) spoken() (session.Role, bool) {
	switch u.kind() {
	case "user_message_chunk", "user_message":
		return session.User, true
	case "agent_message_chunk", "agent_message":
		return session.Assistant, true
	default:
		return 0, false
	}
}

func readSummary(fsys fs.FS, name string) (summary, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return summary{}, err
	}
	var found summary
	if err := json.Unmarshal(data, &found); err != nil {
		return summary{}, err
	}
	return found, nil
}

// firstUser is the opening thing a person said, used when summary.json has no
// title yet. Thinking and tool traffic are ignored.
func firstUser(fsys fs.FS, name string) string {
	file, err := fsys.Open(name)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := jsonl.NewScanner(file)
	for scanner.Scan() {
		var entry update
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		role, ok := entry.spoken()
		if !ok || role != session.User {
			continue
		}
		if text := strings.TrimSpace(entry.Params.Update.Content.Text); text != "" {
			return text
		}
	}
	return ""
}

// messages reads the human side of an updates.jsonl stream. Consecutive
// chunks of the same role are joined, so a streamed reply is one turn rather
// than a cell per token. Tool calls and thoughts are dropped.
func messages(ctx context.Context, r io.Reader) ([]session.Message, error) {
	var (
		found  []session.Message
		buf    strings.Builder
		role   session.Role
		open   bool
		images int
	)
	flush := func() {
		if !open {
			return
		}
		if message, ok := session.NewMessage(role, buf.String(), images); ok {
			found = append(found, message)
		}
		buf.Reset()
		images = 0
		open = false
	}

	scanner := jsonl.NewContextScanner(ctx, r)
	for scanner.Scan() {
		var entry update
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		next, ok := entry.spoken()
		if !ok {
			continue
		}
		if open && next != role {
			flush()
		}
		role = next
		open = true
		switch entry.Params.Update.Content.Type {
		case "image":
			images++
		default:
			buf.WriteString(entry.Params.Update.Content.Text)
		}
	}
	flush()
	return found, scanner.Err()
}

// recordedID is the session id written in summary.json, checked against the
// listing before the directory is removed.
func recordedID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", i18n.Wrap(errors.Join(agent.ErrSessionGone, err), i18n.SessionFileMissing)
		}
		return "", i18n.Wrap(err, i18n.SessionDeleteFailed, i18n.Args{"error": err})
	}
	var found summary
	if json.Unmarshal(data, &found) != nil {
		return "", i18n.Wrap(agent.ErrIDMismatch, i18n.SessionIDMismatch)
	}
	return found.Info.ID, nil
}

// moment parses a recorded timestamp into local time, which is what the whole
// interface shows.
func moment(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	when, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		when, err = time.Parse(time.RFC3339, value)
	}
	if err != nil {
		return time.Time{}
	}
	return when.Local()
}
