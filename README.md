# agent-session-cleaner

在终端中浏览和清理 Codex、Claude Code 的历史会话。

![界面预览](./example.png)

## 安装

请先安装 [uv](https://github.com/astral-sh/uv)，然后运行：

```bash
git clone https://github.com/haowang02/agent-session-cleaner
cd agent-session-cleaner
uv tool install .
```

## 使用

```bash
agent-session-cleaner
```

启动后选择 Codex 或 Claude Code，也可以直接指定：

```bash
agent-session-cleaner codex
agent-session-cleaner claude
```

## 快捷键

可用快捷键会显示在界面底部。

| 键 | 作用 |
|---|---|
| `↑` `↓` / `j` `k` | 上下移动 |
| `g` / `G` | 跳到开头 / 末尾 |
| `Tab` | 切换列表与会话详情 |
| `/` | 搜索 |
| `n` / `N` | 下一个 / 上一个搜索结果 |
| `c` | 复制恢复会话的命令 |
| `d` | 删除当前会话 |
| `r` | 刷新会话列表 |
| `!` | 开启或关闭危险模式 |
| `Esc` | 关闭危险模式或清除搜索 |
| `q` | 退出 |

Codex 还支持：

| 键 | 作用 |
|---|---|
| `a` | 归档当前会话 |
| `u` | 取消归档 |
| `v` | 显示或隐藏已归档会话 |
| `s` | 显示或隐藏子代理、自动化会话 |
| `D` | 删除全部已归档会话 |

Claude Code 还支持：

| 键 | 作用 |
|---|---|
| `E` | 删除全部空会话 |

`a`、`c`、`d`、`u` 始终作用于左侧选中的会话。

## 恢复会话

按 `c` 会复制一条恢复命令。它会先进入会话原来的工作目录，再恢复对话：

```bash
cd /path/to/project && codex resume <session-id>
cd /path/to/project && claude --resume <session-id>
```

## 删除与归档

删除操作无法恢复，也不会自动备份。Codex 会话如需保留，建议先归档。

- Codex 的归档和删除由 Codex 命令行工具执行。
- Claude Code 会删除会话记录及其同名附属目录，不会清理其他缓存。
- ⚠️ 危险模式下，按 `d` 会直接删除。

Claude Code 不支持归档。Codex 默认隐藏子代理和自动化会话，可按 `s` 显示；Claude Code 会显示全部会话，可按 `E` 清理空会话。

## 数据目录

默认读取 `~/.codex` 和 `~/.claude`。如需指定其他目录：

```bash
agent-session-cleaner --codex-home /path/to/codex
agent-session-cleaner --claude-home /path/to/claude
```

只有一侧存在会话记录也可以正常使用。未安装 Codex 命令行工具时仍可浏览 Codex 会话，但不能归档或删除；浏览和删除 Claude Code 会话不依赖 Claude Code 命令行工具。

## 致谢

- [LINUX DO](https://linux.do/) - 新的理想型社区
