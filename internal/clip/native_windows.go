package clip

import "github.com/atotto/clipboard"

func copyNative(text string) bool {
	return clipboard.WriteAll(text) == nil
}
