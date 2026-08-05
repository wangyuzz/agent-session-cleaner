<div align="center">
  <h1>agent-session-cleaner</h1>
  <p><strong>All your Codex, Claude Code and OpenCode sessions in one terminal—easy to find, resume and clean up.</strong></p>
  <p>
    <a href="https://github.com/haowang02/agent-session-cleaner/releases/latest"><img src="https://img.shields.io/github/v/release/haowang02/agent-session-cleaner?label=release" alt="Latest release"></a>
    <a href="https://github.com/haowang02/agent-session-cleaner/actions/workflows/release.yml"><img src="https://github.com/haowang02/agent-session-cleaner/actions/workflows/release.yml/badge.svg" alt="Build status"></a>
    <img src="https://img.shields.io/badge/platforms-macOS%20%7C%20Linux-blue" alt="Platforms: macOS and Linux">
    <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT License"></a>
  </p>
  <p><strong>English</strong> · <a href="./README.zh-CN.md">简体中文</a></p>
</div>

![Interface preview](./example.png)

## Install

macOS · Apple silicon:

```bash
curl -L -o agent-session-cleaner \
  https://github.com/haowang02/agent-session-cleaner/releases/latest/download/agent-session-cleaner-macos-arm64
chmod +x agent-session-cleaner
```

macOS · Intel:

```bash
curl -L -o agent-session-cleaner \
  https://github.com/haowang02/agent-session-cleaner/releases/latest/download/agent-session-cleaner-macos-x86_64
chmod +x agent-session-cleaner
```

Linux · x86_64:

```bash
curl -L -o agent-session-cleaner \
  https://github.com/haowang02/agent-session-cleaner/releases/latest/download/agent-session-cleaner-linux-x86_64
chmod +x agent-session-cleaner
```

Linux · arm64:

```bash
curl -L -o agent-session-cleaner \
  https://github.com/haowang02/agent-session-cleaner/releases/latest/download/agent-session-cleaner-linux-arm64
chmod +x agent-session-cleaner
```

> [!WARNING]
> If macOS blocks a binary downloaded through your browser, run `xattr -d com.apple.quarantine agent-session-cleaner`, then try again.

You can also install from source. Install [uv](https://github.com/astral-sh/uv) first:

```bash
git clone https://github.com/haowang02/agent-session-cleaner
cd agent-session-cleaner
uv tool install .
```

## Use

```bash
agent-session-cleaner
```

Choose an agent at startup, or name it directly:

```bash
agent-session-cleaner codex
agent-session-cleaner claude
agent-session-cleaner opencode
```

The default data directories are `~/.codex`, `~/.claude` and `~/.local/share/opencode` (`$XDG_DATA_HOME/opencode`). To use another location:

```bash
agent-session-cleaner --codex-home /path/to/codex
agent-session-cleaner --claude-home /path/to/claude
agent-session-cleaner --opencode-home /path/to/opencode
```

The OpenCode path must point to the directory containing `opencode.db`, and that directory must be named `opencode`.

### Language

The interface follows your locale (`LC_ALL`, `LC_MESSAGES`, `LANGUAGE`, or `LANG`). Chinese locales use Simplified Chinese; all other locales use English. To override detection:

```bash
AGENT_SESSION_CLEANER_LANG=en agent-session-cleaner
AGENT_SESSION_CLEANER_LANG=zh-CN agent-session-cleaner
```

## Keyboard shortcuts

The footer shows the shortcuts currently available. Press `h` for the complete reference.

| Key | Action | Available for |
|---|---|---|
| `↑` `↓` / `j` `k` | Move to the previous / next session | All agents |
| `g` / `G` | Jump to the top / bottom of the list | All agents |
| `Tab` | Switch between the session list and conversation | All agents |
| `␣` | Select or deselect the current session | All agents |
| `/` | Search titles, directories, session IDs, and sources | All agents |
| `?` | Search backward | All agents |
| `n` / `N` | Jump to the next / previous match | All agents |
| `c` | Copy the resume command for the current session | All agents |
| `d` | Delete the current session or selected sessions | All agents |
| `a` | Archive the current session or selected sessions | Codex only |
| `u` | Unarchive the current session or selected sessions | Codex only |
| `D` | Delete all archived sessions | Codex only |
| `O` | Delete all orphaned sub-agent sessions | Codex and OpenCode |
| `E` | Delete all empty sessions | Claude Code only |
| `r` | Refresh the session list | All agents |
| `h` | Show keyboard shortcuts | All agents |
| `!` | Toggle danger mode for individual deletions | All agents |
| `Esc` | Exit multi-select, turn off danger mode, or clear the search | All agents |
| `q` | Quit | All agents |

Press Space or double-click to select or deselect a session. Selecting a session includes all of its descendant sub-agent sessions. Actions supported by the current agent apply to every selected session; press `Esc` to clear the selection.

## Resume a session

Press `c` to copy a ready-to-run command for resuming the current session. If the session records a working directory, the command changes to it first so the agent resumes in the correct project context:

```bash
cd /path/to/project && codex resume <session-id>
cd /path/to/project && claude --resume <session-id>
cd /path/to/project && opencode -s <session-id>
```

## Deletion and archiving

> [!WARNING]
> This tool does not create backups. Deleted sessions cannot be recovered.

- Deleting or archiving a session also includes its descendant sub-agent sessions.
- Codex changes are performed through the Codex CLI.
- Claude Code deletion removes each selected session and its related session data.
- OpenCode deletion is performed through the OpenCode CLI.
- In danger mode (`!`), individual deletions skip confirmation. Bulk deletion still asks for confirmation.

## Acknowledgements

- [LINUX DO](https://linux.do/) — a community for builders and curious minds
