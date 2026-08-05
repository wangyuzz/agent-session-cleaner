# Changelog / 更新记录

Each version section is used verbatim as its GitHub Release notes.

每个版本的小节会原样用作 GitHub Release 说明。

## 0.4.1

### English

- Added complete English and Simplified Chinese interfaces for the manager, agent chooser,
  command-line help, confirmations, status messages, and backend errors.
- The display language now follows the system locale and can be overridden with
  `AGENT_SESSION_CLEANER_LANG=en` or `AGENT_SESSION_CLEANER_LANG=zh-CN`.
- English is now the default README language, with a complete Chinese edition linked from it.
  Release notes include English first and Chinese second.
- Build checks and release automation now run against the English interface and use English logs.

### 中文

- 主界面、来源选择、命令行帮助、确认框、状态提示和后端错误现已完整支持英文与
  简体中文。
- 显示语言会跟随系统 locale，也可以通过 `AGENT_SESSION_CLEANER_LANG=en` 或
  `AGENT_SESSION_CLEANER_LANG=zh-CN` 强制指定。
- README 默认显示英文，并链接到完整中文版；发布日志先显示英文，再显示中文。
- 构建检查和发布自动化现改为验证英文界面，并统一使用英文日志。

## 0.4.0

### English

- Codex now shows every session and arranges sub-agent sessions into trees rooted at their source
  session. The former `v` and `s` visibility toggles have been removed.
- Added `O` to delete orphaned sub-agent sessions whose source no longer exists. Orphaned,
  archived, and source-unknown sessions have distinct markers to reduce accidental deletion.
- Archive and delete operations include descendant sub-agent sessions and process the deepest
  descendants first. If one fails, the source session is kept to avoid creating new orphans.
- Added compatibility for older Codex relationship fields and fixed sub-agent session ID parsing,
  preventing resume and delete operations from targeting the source session.
- Added the `h` shortcut reference and a two-row footer; reorganized session details, refreshed the
  screenshot, and tightened interface copy throughout.

### 中文

- Codex 现在会完整显示所有会话，并将子代理会话按来源关系组织成树；原有的 `v`、
  `s` 显示开关已移除。
- 新增 `O`，可一次删除来源会话已不存在的孤立子代理会话。孤立、已归档和来源不明的
  会话使用不同标记，避免误删。
- 归档或删除会自动包含其下的子代理会话，并从最深层开始处理；中途失败时保留来源
  会话，避免产生新的孤立记录。
- 兼容早期 Codex 的来源关系字段，并修正子代理会话 ID 识别错误，恢复和删除不再误
  指向来源会话。
- 新增 `h` 按键说明和双行底栏；重排右侧详情信息，更新界面截图，并统一精简所有提示
  文案。

## 0.3.0

### English

The first stable release, with standalone binaries for two architectures on both macOS and Linux.

- Browse Codex and Claude Code session history in the terminal, with a session list on the left and
  the complete conversation on the right.
- Search with `/`, move between matches with `n` / `N`, and press `c` to copy a ready-to-run command
  for resuming the selected session in its recorded project context.
- Codex archive, unarchive, delete, and archived-session cleanup operations are delegated to the
  Codex CLI.
- Claude Code can delete a transcript and its same-name sidecar directory; `E` deletes all empty
  sessions.
- In danger mode (`!`), deletion skips confirmation.
- Sessions are ordered by their start time rather than file modification time.

### 中文

首个正式版本，提供 macOS 和 Linux 的免安装可执行文件，均支持两种架构。

- 在终端中浏览 Codex 和 Claude Code 的历史会话：左侧显示列表，右侧显示完整对话。
- 使用 `/` 搜索，使用 `n` / `N` 在匹配项之间跳转；按 `c` 可复制用于恢复所选会话的
  命令。命令会在需要时先进入会话记录的工作目录。
- Codex 的归档、取消归档、删除和清空归档操作均由 Codex 命令行工具执行。
- Claude Code 支持删除会话记录及其同名附属目录；按 `E` 可删除所有空会话。
- 危险模式（`!`）下，删除会跳过二次确认。
- 列表按会话开始时间而非文件修改时间排序，更贴近实际使用顺序。
