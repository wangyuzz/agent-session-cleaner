package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandHome resolves a leading ~ against the current user's home directory,
// which is what a value typed on a command line or left in an environment
// variable is expected to mean.
func ExpandHome(path string) string {
	if path != "~" && (len(path) < 2 || path[0] != '~' || path[1] != '/' && path[1] != '\\') {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// UnderHome shortens a path for display, which keeps a home directory out of
// screenshots and off the status line.
func UnderHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return filepath.Join("~", rel)
}

// Resolve picks the directory an agent should use: an override when one was
// given, the agent's own default otherwise.
func Resolve(override string, fallback func() string) string {
	if override == "" {
		return fallback()
	}
	return ExpandHome(override)
}
