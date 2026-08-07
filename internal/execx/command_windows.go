package execx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const shimPathEnv = "ASC_INTERNAL_EXECX_SHIM_PATH"

// newProcess routes Node-style command shims through cmd.exe. Go's
// LookPath deliberately finds .cmd and .bat files on Windows, but CreateProcess
// cannot start those files directly. Codex and OpenCode are commonly installed
// through npm, so their PATH entries are exactly this kind of shim.
func newProcess(ctx context.Context, name string, args []string) (*exec.Cmd, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".cmd" && ext != ".bat" {
		return exec.CommandContext(ctx, path, args...), nil
	}

	commandInterpreter := os.Getenv("ComSpec")
	if commandInterpreter == "" {
		commandInterpreter, err = exec.LookPath("cmd.exe")
		if err != nil {
			return nil, err
		}
	}

	// Agent arguments are fixed subcommands, flags, and validated session IDs.
	// The path travels through one environment expansion so characters such as
	// percent signs cannot be interpreted as part of the command itself.
	fields := make([]string, 0, len(args)+1)
	fields = append(fields, quoteCommandField("%"+shimPathEnv+"%"))
	for _, arg := range args {
		fields = append(fields, quoteCommandField(arg))
	}
	line := `/d /s /v:off /c "` + strings.Join(fields, " ") + `"`
	cmd := exec.CommandContext(ctx, commandInterpreter)
	cmd.Args = nil
	cmd.Env = append(os.Environ(), shimPathEnv+"="+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line}
	return cmd, nil
}

func quoteCommandField(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
