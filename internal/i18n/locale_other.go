//go:build !windows

package i18n

// Unix-like systems expose their locale through the environment variables
// Detect already reads. Keep the platform fallback empty there.
func nativeLocale() string { return "" }
