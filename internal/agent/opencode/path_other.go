//go:build !windows

package opencode

func dataDirName(name string) bool {
	return name == DataDirName
}
