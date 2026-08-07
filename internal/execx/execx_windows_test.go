package execx_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haowang02/agent-session-cleaner/internal/execx"
	"github.com/haowang02/agent-session-cleaner/internal/i18n"
)

func commandShim(t *testing.T, extension, body string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "command %PATH% & shims")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "test-agent"+extension)
	if err := os.WriteFile(path, []byte("@echo off\r\n"+body+"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunStartsWindowsCommandShims(t *testing.T) {
	for _, extension := range []string{".cmd", ".bat"} {
		t.Run(extension, func(t *testing.T) {
			shim := commandShim(t, extension, "if not \"%~1\"==\"delete\" exit /b 3\r\nif not \"%XDG_DATA_HOME%\"==\"pinned\" exit /b 4")
			if err := execx.Run(t.Context(), execx.Command{
				Name: shim, Args: []string{"delete"},
				Env: map[string]string{"XDG_DATA_HOME": "pinned"}, Label: "OpenCode",
			}); err != nil {
				t.Errorf("Run() = %v, want success", err)
			}
		})
	}
}

func TestRunSurfacesAWindowsCommandShimsComplaint(t *testing.T) {
	shim := commandShim(t, ".cmd", `echo Error: session not found 1>&2 & exit /b 1`)
	err := execx.Run(t.Context(), execx.Command{Name: shim, Label: "Codex"})
	if got := i18n.New(i18n.English).Err(err); got != "Error: session not found" {
		t.Errorf("message = %q", got)
	}
}

func TestRunReportsAMissingWindowsCommand(t *testing.T) {
	err := execx.Run(t.Context(), execx.Command{
		Name: "definitely-not-installed-anywhere", Label: "Codex",
	})
	if !errors.Is(err, execx.ErrNotInstalled) {
		t.Fatalf("Run() = %v, want ErrNotInstalled", err)
	}
	if got := i18n.New(i18n.English).Err(err); !strings.Contains(got, "Codex CLI is unavailable") {
		t.Errorf("message = %q", got)
	}
}

func TestAvailableFindsWindowsCommandShimsOnPath(t *testing.T) {
	for _, extension := range []string{".cmd", ".bat"} {
		t.Run(extension, func(t *testing.T) {
			shim := commandShim(t, extension, "exit /b 0")
			t.Setenv("PATH", filepath.Dir(shim)+string(os.PathListSeparator)+os.Getenv("PATH"))
			if !execx.Available("test-agent") {
				t.Errorf("Available() did not find a %s shim", extension)
			}
		})
	}
}
