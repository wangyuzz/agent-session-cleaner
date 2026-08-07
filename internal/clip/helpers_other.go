//go:build !windows

package clip

// The first helper that exists wins.
var helpers = []helper{
	{name: "pbcopy"},  // macOS
	{name: "wl-copy"}, // Wayland
	{name: "xclip", args: []string{"-selection", "clipboard"}},
	{name: "xsel", args: []string{"--clipboard", "--input"}},
}
