# agent-session-cleaner

[简体中文](./README.zh-CN.md)

Browse and clean up Codex and Claude Code session history from your terminal.

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
> If macOS blocks a binary downloaded through your browser, run
> `xattr -d com.apple.quarantine agent-session-cleaner`, then try again.

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

Choose Codex or Claude Code at startup, or name the agent directly:

```bash
agent-session-cleaner codex
agent-session-cleaner claude
```

The default data directories are `~/.codex` and `~/.claude`. To use another location:

```bash
agent-session-cleaner --codex-home /path/to/codex
agent-session-cleaner --claude-home /path/to/claude
```

### Language

The interface follows your locale (`LC_ALL`, `LC_MESSAGES`, `LANGUAGE`, or `LANG`). Chinese
locales use Simplified Chinese; all other locales use English. To override detection:

```bash
AGENT_SESSION_CLEANER_LANG=en agent-session-cleaner
AGENT_SESSION_CLEANER_LANG=zh-CN agent-session-cleaner
```

## Keyboard shortcuts

The footer shows the shortcuts currently available. Press `h` for the complete reference.

| Key | Action |
|---|---|
| `↑` `↓` / `j` `k` | Select the previous / next session |
| `g` / `G` | Jump to the top / bottom of the list |
| `Tab` | Switch between the session list and conversation |
| `/` | Search titles, directories, and session IDs |
| `?` | Search backward |
| `n` / `N` | Jump to the next / previous match |
| `c` | Copy the resume command for the selected session |
| `d` | Delete the selected session |
| `r` | Refresh the session list |
| `h` | Show keyboard shortcuts |
| `!` | Toggle danger mode |
| `Esc` | Turn off danger mode or clear the search |
| `q` | Quit |

Codex also supports:

| Key | Action |
|---|---|
| `a` | Archive the selected session |
| `u` | Unarchive the selected session |
| `D` | Delete all archived sessions |
| `O` | Delete all orphaned sub-agent sessions |

Claude Code also supports:

| Key | Action |
|---|---|
| `E` | Delete all empty sessions |

When a Codex session is archived or deleted, its descendant sub-agent sessions are included.

## Resume a session

Press `c` to copy a ready-to-run command for resuming the selected session. If the session
records a working directory, the command changes to it first so the agent resumes in the correct
project context:

```bash
cd /path/to/project && codex resume <session-id>
cd /path/to/project && claude --resume <session-id>
```

## How deletion and archiving work

> [!WARNING]
> This tool does not create backups. Deleted sessions cannot be recovered.

- Codex archive and delete operations are delegated to the Codex CLI. Without it, Codex
  sessions can be browsed but not changed.
- Claude Code deletion removes the transcript and its same-name sidecar directory, but leaves
  unrelated caches untouched.
- In danger mode (`!`), pressing `d` deletes immediately without confirmation.

## Acknowledgements

- [LINUX DO](https://linux.do/) — a community for builders and curious minds
