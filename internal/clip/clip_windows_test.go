package clip

import (
	"context"
	"testing"
)

func TestWindowsClipboardStillDefersToSSH(t *testing.T) {
	getenv := func(name string) string {
		if name == "SSH_CONNECTION" {
			return "set"
		}
		return ""
	}
	if copyFor(context.Background(), helpers, "text", getenv) {
		t.Error("a platform clipboard integration ran over SSH")
	}
}
