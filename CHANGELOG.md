# Changelog / 更新记录

Each version section is used verbatim as its GitHub Release notes.

每个版本的小节会原样用作 GitHub Release 说明。

## 0.6.0

### English

- Rebuilt the application in Go with Bubble Tea, Bubbles, and Lip Gloss while preserving the
  existing Codex, Claude Code, and OpenCode workflows. Releases are now self-contained binaries,
  and the former Python, uv, Docker, and platform-specific build paths have been removed.
- Moved session discovery, conversation loading, clipboard work, and session changes into
  message-driven commands so filesystem and CLI operations never block the terminal event loop.
- Strengthened destructive-operation safety with stale-list protection, strict session ID and
  path validation, symlink refusal, ordered sub-agent cascades, bounded concurrency, retryable
  OpenCode deletions, and consistent recovery after partial failures.
- Improved terminal behavior across narrow windows, resizing, long and Unicode text, scrolling,
  focus changes, search, multi-select, confirmation dialogs, and cached conversation previews.
- Refined the English and Simplified Chinese interface, command-line help, errors, and README
  documentation. Data-directory defaults and CLI requirements are now described consistently.
- Added broad unit and interaction coverage, `go vet`, Staticcheck, and CI checks, with a unified
  four-platform release workflow and checksummed archives.

### 中文

- 使用 Go、Bubble Tea、Bubbles 和 Lip Gloss 重写应用，同时保留 Codex、Claude Code 与
  OpenCode 的现有工作流。发布产物改为独立可执行文件，并移除原有的 Python、uv、Docker
  和按平台拆分的构建流程。
- 将会话发现、对话加载、剪贴板处理和会话修改改为消息驱动命令，文件系统和 CLI 操作不再
  阻塞终端事件循环。
- 加强高风险操作的安全性：避免基于过期列表修改数据，严格校验会话 ID 与路径，拒绝符号
  链接，按安全顺序处理子代理会话，限制并发，重试 OpenCode 临时冲突，并在部分失败后保持
  状态一致且可继续操作。
- 改进窄窗口、终端缩放、长文本与 Unicode、滚动、焦点切换、搜索、多选、确认框和对话缓存
  等交互表现。
- 统一打磨英文和简体中文界面、命令行帮助、错误提示与 README，准确说明数据目录默认值和
  CLI 依赖。
- 补充完整的单元测试与交互测试，引入 `go vet`、Staticcheck 和 CI 检查，并统一四个平台的
  发布流程与校验和归档。

## 0.5.0

### English

- Added OpenCode support. You can browse, search, and preview OpenCode sessions, copy their
  resume commands, and delete them from the same interface used for Codex and Claude Code.
- OpenCode sub-agent sessions are shown beneath their source sessions. Placeholder titles are
  replaced with the opening message, and tool activity is excluded from the conversation preview.
- Added orphan cleanup for OpenCode. Archived sessions remain visible, while archive and
  unarchive actions stay in OpenCode itself.
- Added multi-select: press Space or double-click to select sessions, then archive, unarchive, or
  delete the entire selection. Descendant sub-agent sessions are included automatically.
- Bulk changes are faster and safer: independent session trees run concurrently, related sessions
  keep their required order, temporary OpenCode database conflicts are retried, and unfinished
  selections remain available to retry.
- Added `--opencode-home` for custom OpenCode data locations. The selected data directory must be
  named `opencode`.
- Refined the English and Simplified Chinese interface text and documentation, updated both
  screenshots, consolidated the README shortcut reference with clear agent availability, and
  added the MIT license file.

### 中文

- 新增 OpenCode 支持，可在同一界面中浏览、搜索、预览、复制恢复命令和删除 OpenCode 会话。
- OpenCode 子代理会话会显示在来源会话下方；尚未生成标题的会话改用开场消息，并从对话预览中
  排除工具调用等非对话内容。
- OpenCode 现已支持清理孤立子代理会话。已归档会话仍会正常显示，归档和取消归档则继续在
  OpenCode 中操作。
- 新增多选模式：按空格键或双击选择会话后，可批量归档、取消归档或删除；相关子代理会话会自动
  纳入操作范围。
- 批量操作现在更快、更稳妥：不同会话树会并发处理，同一会话树仍按安全顺序执行；遇到临时的
  OpenCode 数据库冲突时会自动重试，未完成的会话也会保留在多选中，方便再次操作。
- 新增 `--opencode-home`，用于指定自定义 OpenCode 数据目录；所选目录必须命名为 `opencode`。
- 继续打磨英文和简体中文界面及文档，更新双语界面截图，将 README 快捷键说明合并为一张表并
  明确标注各快捷键的适用范围，同时补充 MIT 许可证文件。

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
