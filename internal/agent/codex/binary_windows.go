package codex

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// platformNativeBinary bypasses the npm cmd -> Node launcher when the native
// executable bundled with @openai/codex is available. Any unfamiliar package
// manager layout falls back to the ordinary command on PATH.
func platformNativeBinary() string {
	command, err := exec.LookPath(Binary)
	if err != nil {
		return ""
	}
	if strings.EqualFold(filepath.Ext(command), ".exe") {
		return command
	}
	return npmNativeBinary(filepath.Dir(command), runtime.GOARCH)
}

func npmNativeBinary(prefix, arch string) string {
	var packageName, target string
	switch arch {
	case "amd64":
		packageName = "codex-win32-x64"
		target = "x86_64-pc-windows-msvc"
	case "arm64":
		packageName = "codex-win32-arm64"
		target = "aarch64-pc-windows-msvc"
	default:
		return ""
	}

	openai := filepath.Join(prefix, "node_modules", "@openai")
	candidates := []string{
		filepath.Join(openai, "codex", "node_modules", "@openai", packageName,
			"vendor", target, "bin", "codex.exe"),
		filepath.Join(openai, packageName, "vendor", target, "bin", "codex.exe"),
		filepath.Join(openai, "codex", "vendor", target, "bin", "codex.exe"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}
