"""Putting text on the system clipboard.

Textual can do this over OSC 52, but that escape sequence is ignored by a fair
number of terminals (macOS Terminal among them), so a native helper is tried
first and OSC 52 is left as the fallback.
"""

from __future__ import annotations

import shutil
import subprocess

#: First one that exists wins.
_TOOLS = (
    ("pbcopy", ()),  # macOS
    ("wl-copy", ()),  # Wayland
    ("xclip", ("-selection", "clipboard")),
    ("xsel", ("--clipboard", "--input")),
)


def copy(text: str) -> bool:
    """Put `text` on the clipboard. False if no native tool could do it."""
    for name, args in _TOOLS:
        executable = shutil.which(name)
        if executable is None:
            continue
        try:
            subprocess.run(
                [executable, *args],
                input=text.encode("utf-8"),
                check=True,
                timeout=5,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
        except (OSError, subprocess.SubprocessError):
            continue
        return True
    return False
