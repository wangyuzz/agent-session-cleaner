package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	// A pure-Go SQLite, so the binary needs no cgo and cross-compiles to every
	// platform this ships to.
	_ "modernc.org/sqlite"
)

// open returns a read-only connection. Nothing here ever writes: OpenCode keeps
// its own event log alongside these tables, and reconstructing that by hand is
// not something a session browser should attempt.
func open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", databaseDSN(path))
	if err != nil {
		return nil, err
	}
	// One connection: every query here is a short read, and a pool of them
	// only adds contention with whatever OpenCode itself is doing.
	db.SetMaxOpenConns(1)
	return db, nil
}

func databaseDSN(path string) string {
	uriPath := filepath.ToSlash(path)
	volume := filepath.VolumeName(path)
	if len(volume) == 2 && volume[1] == ':' && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	dsn := url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro"}
	return dsn.String()
}

// row is one record with whatever columns this database happens to have.
// OpenCode has added columns over time, and older databases simply lack them.
type row map[string]any

func rows(result *sql.Rows) ([]row, error) {
	columns, err := result.Columns()
	if err != nil {
		return nil, err
	}
	var found []row
	for result.Next() {
		cells := make([]any, len(columns))
		for i := range cells {
			cells[i] = new(any)
		}
		if err := result.Scan(cells...); err != nil {
			return nil, err
		}
		record := make(row, len(columns))
		for i, name := range columns {
			record[name] = *(cells[i].(*any))
		}
		found = append(found, record)
	}
	return found, result.Err()
}

func (r row) text(name string) string {
	switch value := r[name].(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	}
	return ""
}

// moment reads an epoch-milliseconds column as local time, which is what the
// whole interface shows.
func (r row) moment(name string) time.Time {
	var millis int64
	switch value := r[name].(type) {
	case int64:
		millis = value
	case float64:
		millis = int64(value)
	default:
		return time.Time{}
	}
	if millis <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(millis).Local()
}

func (r row) present(name string) bool { return r[name] != nil }

// payload decodes the JSON column that messages and parts keep everything but
// their keys in.
func payload(value any) map[string]any {
	var raw []byte
	switch typed := value.(type) {
	case string:
		raw = []byte(typed)
	case []byte:
		raw = typed
	default:
		return nil
	}
	var decoded map[string]any
	if json.Unmarshal(raw, &decoded) != nil {
		return nil
	}
	return decoded
}

func field(decoded map[string]any, name string) string {
	text, _ := decoded[name].(string)
	return text
}

// spokenText is the text of one part, or empty for anything that is not
// conversation.
//
// Synthetic text is tool plumbing that OpenCode stores as text: the call it is
// about to make, the file it just read, the note that the user ran a tool. It
// reads like the assistant talking, and is not.
func spokenText(part map[string]any) string {
	if field(part, "type") != "text" {
		return ""
	}
	if synthetic, _ := part["synthetic"].(bool); synthetic {
		return ""
	}
	return field(part, "text")
}

func isImage(part map[string]any) bool {
	return field(part, "type") == "file" && strings.HasPrefix(field(part, "mime"), "image/")
}

// placeholderTitle matches the name OpenCode gives a session the moment it is
// created, before the model has produced a summary. The dated form matches
// OpenCode's own isDefaultTitle; the bare one is what releases before 1.18 used.
var placeholderTitle = regexp.MustCompile(
	`^(New session - |Child session - )\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`,
)

// legacyPlaceholder is capitalised either way depending on the release that
// wrote it.
const legacyPlaceholder = "new conversation"

func isPlaceholder(title string) bool {
	return strings.EqualFold(title, legacyPlaceholder) || placeholderTitle.MatchString(title)
}

// conversation walks a session's messages and their parts in order.
const conversationQuery = `
	SELECT m.id, m.data, p.data
	FROM message m LEFT JOIN part p ON p.message_id = m.id
	WHERE m.session_id = ?
	ORDER BY m.time_created, m.id, p.time_created, p.id`

// turn is one message with the parts belonging to it.
type turn struct {
	role  string
	parts []map[string]any
}

func conversation(ctx context.Context, db *sql.DB, id string) ([]turn, error) {
	result, err := db.QueryContext(ctx, conversationQuery, id)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	var turns []turn
	var current string
	for result.Next() {
		var messageID string
		var messageData, partData any
		if err := result.Scan(&messageID, &messageData, &partData); err != nil {
			return nil, err
		}
		if messageID != current || len(turns) == 0 {
			current = messageID
			turns = append(turns, turn{role: field(payload(messageData), "role")})
		}
		if part := payload(partData); part != nil {
			turns[len(turns)-1].parts = append(turns[len(turns)-1].parts, part)
		}
	}
	return turns, result.Err()
}
