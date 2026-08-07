package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/haowang02/agent-session-cleaner/internal/jsonl"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

// titleScanLines bounds the search for an opening message. A conversation that
// has not said anything in its first few hundred records did not start with a
// person typing.
const titleScanLines = 400

// record is the envelope every line of a rollout shares.
type record struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// meta is the session_meta payload, always the first line of a rollout.
type meta struct {
	// ID is deliberately preferred over SessionID. In a sub-agent rollout
	// SessionID holds the *parent's* thread id, while ID matches the uuid in
	// the filename. Reading them the other way round would point resume and
	// delete at the conversation that spawned this one.
	ID        string `json:"id"`
	SessionID string `json:"session_id"`

	Source     json.RawMessage `json:"source"`
	Originator string          `json:"originator"`
	Cwd        string          `json:"cwd"`
	CLIVersion string          `json:"cli_version"`

	// Codex has recorded the parent relationship three ways. Across the
	// sub-agent rollouts inspected: 0.133.0 recorded no link at all, 0.135 and
	// 0.136 used ForkedFrom, and later releases use ParentThread.
	ParentThread string `json:"parent_thread_id"`
	ForkedFrom   string `json:"forked_from_id"`
}

// Since 0.147, paginated rollouts store messages inside item_completed events;
// legacy rollouts keep the flattened user_message and agent_message events.
type event struct {
	Type        string            `json:"type"`
	Message     string            `json:"message"`
	Images      []json.RawMessage `json:"images"`
	LocalImages []json.RawMessage `json:"local_images"`
	Item        item              `json:"item"`
}

type item struct {
	Type    string `json:"type"`
	Content []part `json:"content"`
}

type part struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Codex flattens both item schemas without separators; the two text variants
// differ only because UserInput uses snake_case and AgentMessageContent does not.
func (i item) message() (session.Message, bool) {
	var role session.Role
	switch i.Type {
	case "UserMessage":
		role = session.User
	case "AgentMessage":
		role = session.Assistant
	default:
		return session.Message{}, false
	}
	var text strings.Builder
	images := 0
	for _, piece := range i.Content {
		switch piece.Type {
		case "text", "Text":
			text.WriteString(piece.Text)
		case "image", "local_image":
			images++
		}
	}
	return session.NewMessage(role, text.String(), images)
}

func parseMessage(line []byte) (session.Message, bool) {
	var envelope record
	if json.Unmarshal(line, &envelope) != nil || envelope.Type != "event_msg" {
		return session.Message{}, false
	}
	var payload event
	if json.Unmarshal(envelope.Payload, &payload) != nil {
		return session.Message{}, false
	}
	switch payload.Type {
	case "user_message":
		return session.NewMessage(
			session.User,
			payload.Message,
			len(payload.Images)+len(payload.LocalImages),
		)
	case "agent_message":
		return session.NewMessage(session.Assistant, payload.Message, 0)
	case "item_completed":
		return payload.Item.message()
	}
	return session.Message{}, false
}

// head reads a rollout's metadata and opening message in one pass. Both live
// near the start of the file, and reading them together halves the file opens
// during discovery.
func head(r io.Reader) (info meta, opening string) {
	scanner := jsonl.NewScanner(r)
	for lineno := 0; lineno < titleScanLines && scanner.Scan(); lineno++ {
		line := scanner.Bytes()
		if lineno == 0 {
			info = parseMeta(line)
			continue
		}
		if !bytes.Contains(line, []byte(`"user_message"`)) &&
			!bytes.Contains(line, []byte(`"UserMessage"`)) {
			continue
		}
		if message, ok := parseMessage(line); ok && message.Role == session.User {
			if text := strings.TrimSpace(message.Text); text != "" {
				return info, text
			}
		}
	}
	return info, ""
}

func parseMeta(line []byte) meta {
	var envelope record
	if json.Unmarshal(line, &envelope) != nil || envelope.Type != "session_meta" {
		return meta{}
	}
	var info meta
	if json.Unmarshal(envelope.Payload, &info) != nil {
		return meta{}
	}
	return info
}

// messages reads the human-facing turns of a rollout.
//
// The parallel response_item stream carries the same conversation with
// injected <environment_context> blocks mixed in, so it is left alone.
func messages(ctx context.Context, r io.Reader) ([]session.Message, error) {
	var found []session.Message
	scanner := jsonl.NewContextScanner(ctx, r)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, []byte(`"user_message"`)) &&
			!bytes.Contains(line, []byte(`"agent_message"`)) &&
			!bytes.Contains(line, []byte(`"UserMessage"`)) &&
			!bytes.Contains(line, []byte(`"AgentMessage"`)) {
			continue
		}
		if message, ok := parseMessage(line); ok {
			found = append(found, message)
		}
	}
	return found, scanner.Err()
}

// interactiveSources mirrors INTERACTIVE_SESSION_SOURCES in Codex's own
// rollout crate: these are the sources `codex resume` offers. Anything else is
// a sub-agent or a `codex exec` side thread. "vscode" also covers Codex
// Desktop; both are sessions a person started.
var interactiveSources = map[string]bool{
	"cli": true, "vscode": true, "atlas": true, "chatgpt": true,
}

// originatorClients maps the client that wrote a rollout to a short name.
//
// session_meta.source is not a reliable client identifier, because VSCode is
// the default variant of Codex's source enum: any client that omits a source,
// Codex Desktop included, is recorded as "vscode". The originator is
// client-specific and tells them apart.
var originatorClients = map[string]string{
	"codex-tui":     "cli",
	"codex_cli_rs":  "cli", // Codex's own default originator
	"codex_vscode":  "vscode",
	"codex_exec":    "exec",
	"codex desktop": "app",
}

// source flattens the source field, which is either a plain string or a tagged
// object such as {"subagent": {"other": "guardian"}}.
func source(raw json.RawMessage) (kind, detail string) {
	if len(raw) == 0 {
		return "unknown", ""
	}
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain, ""
	}

	name, value, ok := firstMember(raw)
	if !ok {
		return "unknown", ""
	}
	if name != "subagent" {
		return name, ""
	}
	var kindName string
	if json.Unmarshal(value, &kindName) == nil {
		return "subagent", kindName
	}
	if inner, innerValue, ok := firstMember(value); ok {
		var text string
		if json.Unmarshal(innerValue, &text) == nil {
			return "subagent", text
		}
		return "subagent", inner
	}
	return "subagent", ""
}

// firstMember returns the first key of a JSON object in the order it was
// written. Decoding into a map would lose that order, and the tagged forms
// above are read by position.
func firstMember(raw json.RawMessage) (name string, value json.RawMessage, ok bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return "", nil, false
	}
	token, err := decoder.Token()
	if err != nil {
		return "", nil, false
	}
	name, ok = token.(string)
	if !ok {
		return "", nil, false
	}
	if err := decoder.Decode(&value); err != nil {
		return "", nil, false
	}
	return name, value, true
}

func client(kind, subagent, originator string) string {
	if kind == "subagent" {
		if subagent != "" {
			return "subagent:" + subagent
		}
		return "subagent"
	}
	if originator != "" {
		if known, ok := originatorClients[strings.ToLower(strings.TrimSpace(originator))]; ok {
			return known
		}
		// Every first-party desktop build reports "Codex <something>".
		if strings.HasPrefix(originator, "Codex ") {
			return "app"
		}
	}
	return kind
}
