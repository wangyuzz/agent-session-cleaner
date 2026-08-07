package opencode

import (
	"testing"
)

func TestPreflightAcceptsWindowsDataDirectoryCase(t *testing.T) {
	if err := New(`C:\Users\me\.local\share\OpenCode`).Preflight(); err != nil {
		t.Errorf("Preflight() = %v", err)
	}
}

func TestWindowsDatabaseDSN(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{`C:\Users\me\Open Code\opencode.db`, `file:///C:/Users/me/Open%20Code/opencode.db?mode=ro`},
		{`\\server\share\Open Code\opencode.db`, `file:////server/share/Open%20Code/opencode.db?mode=ro`},
	}
	for _, test := range tests {
		if got := databaseDSN(test.path); got != test.want {
			t.Errorf("databaseDSN(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}
