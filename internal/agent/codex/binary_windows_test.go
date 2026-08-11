package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNPMNativeBinary(t *testing.T) {
	prefix := t.TempDir()
	want := filepath.Join(prefix, "node_modules", "@openai", "codex", "node_modules",
		"@openai", "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "bin", "codex.exe")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, nil, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := npmNativeBinary(prefix, "amd64"); got != want {
		t.Errorf("npmNativeBinary() = %q, want %q", got, want)
	}
	if got := npmNativeBinary(prefix, "386"); got != "" {
		t.Errorf("unsupported architecture resolved %q", got)
	}
}
