//go:build !windows

package clip

func copyNative(string) bool {
	return false
}
