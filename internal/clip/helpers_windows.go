package clip

const powershellCopy = "[Console]::InputEncoding = [Text.UTF8Encoding]::new($false); Set-Clipboard -Value ([Console]::In.ReadToEnd())"

// Windows PowerShell is present on supported Windows releases. PowerShell 7 is
// also accepted for environments that removed or disabled the inbox version.
var helpers = []helper{
	{name: "powershell.exe", args: []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", powershellCopy}},
	{name: "pwsh.exe", args: []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", powershellCopy}},
}
