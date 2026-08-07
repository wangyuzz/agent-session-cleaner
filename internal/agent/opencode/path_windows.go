package opencode

import "strings"

func dataDirName(name string) bool {
	return strings.EqualFold(name, DataDirName)
}
