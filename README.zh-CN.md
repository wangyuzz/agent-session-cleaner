<div align="center">
  <h1>agent-session-cleaner</h1>
  <p><strong>一个终端，统一管理 Codex、Claude Code 与 OpenCode 的全部会话：查找、恢复、清理，都更轻松。</strong></p>
  <p>
    <a href="https://github.com/haowang02/agent-session-cleaner/releases/latest"><img src="https://img.shields.io/github/v/release/haowang02/agent-session-cleaner?label=release" alt="最新版本"></a>
    <a href="https://github.com/haowang02/agent-session-cleaner/actions/workflows/release.yml"><img src="https://github.com/haowang02/agent-session-cleaner/actions/workflows/release.yml/badge.svg" alt="构建状态"></a>
    <img src="https://img.shields.io/badge/platforms-macOS%20%7C%20Linux-blue" alt="支持 macOS 和 Linux">
    <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT 许可证"></a>
  </p>
  <p><a href="./README.md">English</a> · <strong>简体中文</strong></p>
</div>

![界面预览](./example.zh-CN.png)

## 安装

macOS · Apple 芯片：

```bash
curl -L -o agent-session-cleaner \
  https://github.com/haowang02/agent-session-cleaner/releases/latest/download/agent-session-cleaner-macos-arm64
chmod +x agent-session-cleaner
```

macOS · Intel：

```bash
curl -L -o agent-session-cleaner \
  https://github.com/haowang02/agent-session-cleaner/releases/latest/download/agent-session-cleaner-macos-x86_64
chmod +x agent-session-cleaner
```

Linux · x86_64：

```bash
curl -L -o agent-session-cleaner \
  https://github.com/haowang02/agent-session-cleaner/releases/latest/download/agent-session-cleaner-linux-x86_64
chmod +x agent-session-cleaner
```

Linux · arm64：

```bash
curl -L -o agent-session-cleaner \
  https://github.com/haowang02/agent-session-cleaner/releases/latest/download/agent-session-cleaner-linux-arm64
chmod +x agent-session-cleaner
```

> [!WARNING]
> 如果 macOS 拦截了浏览器下载的文件，先执行 `xattr -d com.apple.quarantine agent-session-cleaner`，再重新运行。

也可以从源码安装。请先安装 [uv](https://github.com/astral-sh/uv)：

```bash
git clone https://github.com/haowang02/agent-session-cleaner
cd agent-session-cleaner
uv tool install .
```

## 使用

```bash
agent-session-cleaner
```

启动后选择要管理的 Agent；也可以在命令中直接指定：

```bash
agent-session-cleaner codex
agent-session-cleaner claude
agent-session-cleaner opencode
```

默认读取 `~/.codex`、`~/.claude` 和 `~/.local/share/opencode`（即 `$XDG_DATA_HOME/opencode`）。如需使用其他目录：

```bash
agent-session-cleaner --codex-home /path/to/codex
agent-session-cleaner --claude-home /path/to/claude
agent-session-cleaner --opencode-home /path/to/opencode
```

OpenCode 路径必须指向包含 `opencode.db` 的目录，且目录名必须为 `opencode`。

### 语言

界面会跟随系统 locale（`LC_ALL`、`LC_MESSAGES`、`LANGUAGE` 或 `LANG`）：中文 locale 显示简体中文，其他语言显示英文。如需覆盖自动检测：

```bash
AGENT_SESSION_CLEANER_LANG=en agent-session-cleaner
AGENT_SESSION_CLEANER_LANG=zh-CN agent-session-cleaner
```

## 快捷键

界面底部会列出当前可用的快捷键。按 `h` 可查看完整说明。

| 键 | 作用 | 适用范围 |
|---|---|---|
| `↑` `↓` / `j` `k` | 移到上一条 / 下一条会话 | 全部 Agent |
| `g` / `G` | 跳到列表顶部 / 底部 | 全部 Agent |
| `Tab` | 在会话列表和对话详情之间切换 | 全部 Agent |
| `␣` | 选择或取消选择当前会话 | 全部 Agent |
| `/` | 搜索标题、目录、会话 ID 和来源 | 全部 Agent |
| `?` | 反向搜索 | 全部 Agent |
| `n` / `N` | 下一个 / 上一个匹配项 | 全部 Agent |
| `c` | 复制当前会话的恢复命令 | 全部 Agent |
| `d` | 删除当前会话或所有已选会话 | 全部 Agent |
| `a` | 归档当前会话或所有已选会话 | 仅 Codex |
| `u` | 取消归档当前会话或所有已选会话 | 仅 Codex |
| `D` | 删除所有已归档会话 | 仅 Codex |
| `O` | 删除所有孤立的子代理会话 | Codex 和 OpenCode |
| `E` | 删除所有空会话 | 仅 Claude Code |
| `r` | 刷新会话列表 | 全部 Agent |
| `h` | 按键说明 | 全部 Agent |
| `!` | 开启或关闭危险模式（仅影响单个会话删除） | 全部 Agent |
| `Esc` | 退出多选模式、关闭危险模式，或清除搜索 | 全部 Agent |
| `q` | 退出 | 全部 Agent |

按空格键或双击即可选择或取消选择会话。选择一条会话时，它派生的所有子代理会话也会一并选中。当前 Agent 支持的操作会应用到全部已选会话；按 `Esc` 可清除选择。

## 恢复会话

按 `c` 可复制一条直接恢复当前会话的命令。如果会话记录了工作目录，命令会先进入该目录，确保 Agent 在正确的项目上下文中恢复：

```bash
cd /path/to/project && codex resume <session-id>
cd /path/to/project && claude --resume <session-id>
cd /path/to/project && opencode -s <session-id>
```

## 删除与归档

> [!WARNING]
> 本工具不会备份数据，删除后无法恢复！

- 删除或归档一条会话时，它派生的子代理会话也会一并处理。
- Codex 会话通过 Codex CLI 修改。
- Claude Code 会删除选中的会话及其相关数据。
- OpenCode 会话通过 OpenCode CLI 删除。
- 危险模式（`!`）下，删除单个会话时会跳过确认；批量删除仍会要求确认。

## 致谢

- [LINUX DO](https://linux.do/)——面向创造者与好奇者的社区
