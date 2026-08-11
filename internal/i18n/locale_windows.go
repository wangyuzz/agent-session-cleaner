//go:build windows

package i18n

import "golang.org/x/sys/windows"

// nativeLocale follows the user's Windows UI language preference. This is
// deliberately an API call rather than a PowerShell subprocess: language
// detection stays instant, silent and available offline.
func nativeLocale() string {
	languages, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(languages) == 0 {
		return ""
	}
	return languages[0]
}
