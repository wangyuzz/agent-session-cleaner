"""Small, dependency-free localization layer for user-facing text."""

from __future__ import annotations

import locale
import os
from collections.abc import Mapping

LANGUAGE_ENV = "AGENT_SESSION_CLEANER_LANG"

_EN = {
    "today": "Today",
    "yesterday": "Yesterday",
    "empty_session": "(Empty session)",
    "images": "[{count} images]",
    "images_one": "[1 image]",
    "operation_archive": "archive",
    "operation_unarchive": "unarchive",
    "operation_delete": "delete",
    "operation_archive_progress": "Archiving",
    "operation_unarchive_progress": "Unarchiving",
    "operation_delete_progress": "Deleting",
    "operation_archive_done": "Archived",
    "operation_unarchive_done": "Unarchived",
    "operation_delete_done": "Deleted",
    "help_browse": "Browse sessions",
    "help_select": "Select the previous / next session",
    "help_top_bottom": "Jump to the top / bottom of the list",
    "help_focus": "Switch between the session list and conversation",
    "help_search": "Search sessions",
    "help_search_fields": "Search titles, directories, and session IDs",
    "help_search_reverse": "Search backward",
    "help_search_match": "Jump to the next / previous match",
    "help_manage": "Manage sessions",
    "help_copy": "Copy a command to resume the selected session",
    "help_delete": "Delete the selected session and all descendant sub-agent sessions",
    "help_archive": "Archive the selected session and all descendant sub-agent sessions",
    "help_unarchive": "Unarchive the selected session and all descendant sub-agent sessions",
    "help_delete_archived": "Delete all archived sessions",
    "help_delete_orphans": "Delete all orphaned sub-agent sessions",
    "help_delete_empty": "Delete all empty sessions (no messages)",
    "help_danger": "Toggle danger mode (delete without confirmation)",
    "help_other": "Other",
    "help_escape": "Turn off danger mode or clear the search",
    "help_reload": "Refresh the session list",
    "help_open": "Show keyboard shortcuts",
    "help_quit": "Quit",
    "help_title": "Keyboard shortcuts",
    "help_close": "Press Esc, q, h, or Enter to close",
    "cancel": "Cancel",
    "delete_irreversible": "This cannot be undone.",
    "delete_descendant_one": "This also deletes 1 descendant sub-agent session.\n{warning}",
    "delete_descendant_many": (
        "This also deletes {count} descendant sub-agent sessions.\n{warning}"
    ),
    "confirm_delete_title": "Delete this session?",
    "delete": "Delete",
    "bulk_count": "{count} {what} selected\n",
    "bulk_remaining": "  · {count} more…",
    "confirm_bulk_title": "Delete all {what}?",
    "confirm_bulk_button": "Delete {count}",
    "danger_line_one": "While danger mode is on, d deletes sessions without confirmation.\n",
    "danger_line_two": "This cannot be undone. Press ! or Esc to turn danger mode off.",
    "confirm_danger_title": "Enable danger mode?",
    "enable": "Enable",
    "binding_archive": "Archive",
    "binding_unarchive": "Unarchive",
    "binding_delete": "Delete",
    "binding_copy": "Copy resume command",
    "binding_delete_archived": "Delete archived",
    "binding_delete_empty": "Delete empty",
    "binding_delete_orphans": "Delete orphans",
    "binding_danger": "Danger mode",
    "binding_search": "Search",
    "binding_reload": "Refresh",
    "binding_help": "Shortcuts",
    "binding_quit": "Quit",
    "binding_focus": "Switch focus",
    "app_title": "{agent} Session Cleaner",
    "missing_cli_browse": (
        "{agent} CLI not found. You can still browse sessions, but archive and delete "
        "actions are unavailable."
    ),
    "no_sessions_here": "No sessions found.",
    "banner_sessions_one": "   1 session",
    "banner_sessions_many": "   {count} sessions",
    "banner_archived_one": "   1 archived",
    "banner_archived_many": "   {count} archived",
    "banner_orphans_one": "   1 {what}",
    "banner_orphans_many": "   {count} {what}",
    "banner_danger": "      ⚠ Danger mode: d deletes without confirmation",
    "no_conversation": "No messages in this session.",
    "archived_marker": "◆ Archived  ",
    "messages_one": "1 message",
    "messages_many": "{count} messages",
    "cwd_unknown": "Working directory not recorded",
    "parent_deleted": "Source session deleted; this sub-agent session cannot be resumed",
    "spawned_by": "Source session: {session_id}",
    "source_unrecorded": (
        "Source not recorded by an earlier Codex version; excluded from the session tree "
        "and orphan cleanup"
    ),
    "you": "You",
    "message_truncated_one": "\nMessage truncated: 1 trailing character omitted",
    "message_truncated_many": "\nMessage truncated: {count} trailing characters omitted",
    "search_empty_list": "No sessions to search.",
    "search_not_started": "No active search. Press / to search.",
    "search_not_found": "No matches for “{query}”",
    "search_wrapped_forward": "  wrapped to top",
    "search_wrapped_backward": "  wrapped to bottom",
    "search_status": "{sigil}{query}   match {current}/{total}{note}",
    "busy_cannot_quit": "Wait for the current operation to finish before quitting.",
    "reloaded": "Session list refreshed.",
    "already_archived": "This session is already archived. Press u to unarchive it.",
    "not_archived": "This session is not archived.",
    "no_selection": "Select a session first.",
    "copy_missing_cli_note": " ({agent} is not installed here; run this on a machine with {agent})",
    "copy_archived_note": " (unarchive this session with u before resuming it)",
    "copy_success": "Resume command copied to clipboard{note}: {command}",
    "danger_off": "Danger mode off. Deletions now require confirmation.",
    "danger_on": "Danger mode on. Pressing d now deletes without confirmation.",
    "no_archived": "No archived sessions found.",
    "cascade_orphans_one": "Also deletes 1 sub-agent session to avoid leaving an orphan.",
    "cascade_orphans_many": "Also deletes {count} sub-agent sessions to avoid leaving orphans.",
    "archived_sessions": "archived sessions",
    "empty_sessions": "empty sessions",
    "orphan_sessions": "orphaned sub-agent sessions",
    "none_of": "No {what} found.",
    "no_orphans": "No {what}: every recorded source session is still available.",
    "orphan_note": "Their source sessions were deleted, so these sessions cannot be resumed.",
    "deleting_progress": "Deleting {done}/{total}: {title}",
    "bulk_failed": "Deleted {done}/{total} sessions; {failed} failed: {message}",
    "bulk_deleted": "Deleted {count} {what}",
    "missing_cli_modify": "{agent} CLI not found. Session changes are unavailable.",
    "previous_busy": "Another operation is still in progress. Please wait.",
    "operation_progress": "{operation} {done}/{total}: {title}",
    "operation_progress_one": "{operation}: {title}",
    "operation_tail_one": " (including 1 sub-agent session)",
    "operation_tail_many": " (including {count} sub-agent sessions)",
    "operation_done": "{operation}: {title}{tail}",
    "subagent_operation_failed": (
        "Could not {operation} a sub-agent session. The source session was left unchanged "
        "to avoid creating an orphan: {message}"
    ),
    "picker_unavailable": "No sessions found",
    "picker_loading": "Loading sessions…",
    "picker_count_one": "1 session",
    "picker_count_many": "{count} sessions",
    "picker_browse_only": "   CLI unavailable; browse only",
    "picker_title": "Session Cleaner",
    "picker_open": "Open",
    "picker_choose": "Choose an agent",
    "picker_hint": "↑↓ Select · Enter Open · or press a shortcut key",
    "picker_none": "No sessions found. Press q to quit.",
    "picker_agent_none": "{agent}: no sessions found",
    "codex_missing": "Codex CLI not found. Sessions cannot be changed.",
    "codex_start_failed": "Could not start Codex CLI: {error}",
    "codex_timeout": "Codex CLI did not respond within {seconds} seconds.",
    "codex_unknown_failure": "Codex CLI did not report why the operation failed.",
    "session_file_missing": "The session file no longer exists. Press r to refresh the list.",
    "session_id_mismatch": (
        "The session ID in the file does not match the list. Deletion was cancelled; "
        "press r to refresh the list."
    ),
    "sidecar_delete_failed": "Could not delete related session data: {error}",
    "session_delete_failed": "Could not delete the session: {error}",
    "claude_no_archive": "Claude Code does not support session archiving.",
    "cli_description": "Browse and clean up Codex and Claude Code session history.",
    "cli_help": "show this help message and exit",
    "cli_agent": "agent to browse or clean; omit to open the chooser",
    "cli_directory": "DIR",
    "cli_codex_home": "Codex session data directory (default: ~/.codex)",
    "cli_claude_home": "Claude Code session data directory (default: ~/.claude)",
    "cli_version": "show the version and exit",
    "cli_missing_home": "{agent} session directory not found: {path}",
}

_ZH = {
    "today": "今天",
    "yesterday": "昨天",
    "empty_session": "(空会话)",
    "images": "[{count} 张图片]",
    "images_one": "[1 张图片]",
    "operation_archive": "归档",
    "operation_unarchive": "取消归档",
    "operation_delete": "删除",
    "operation_archive_progress": "正在归档",
    "operation_unarchive_progress": "正在取消归档",
    "operation_delete_progress": "正在删除",
    "operation_archive_done": "已归档",
    "operation_unarchive_done": "已取消归档",
    "operation_delete_done": "已删除",
    "help_browse": "浏览会话",
    "help_select": "上下选择会话",
    "help_top_bottom": "跳到列表顶部 / 底部",
    "help_focus": "在会话列表和对话详情之间切换",
    "help_search": "搜索会话",
    "help_search_fields": "搜索标题、目录和会话 ID",
    "help_search_reverse": "反向搜索",
    "help_search_match": "跳到下一个 / 上一个匹配项",
    "help_manage": "管理会话",
    "help_copy": "复制用于恢复所选会话的命令",
    "help_delete": "删除所选会话及其全部子代理会话",
    "help_archive": "归档所选会话及其全部子代理会话",
    "help_unarchive": "取消归档所选会话及其全部子代理会话",
    "help_delete_archived": "删除所有已归档会话",
    "help_delete_orphans": "删除所有孤立的子代理会话",
    "help_delete_empty": "删除所有空会话（没有消息）",
    "help_danger": "切换危险模式（删除时不再确认）",
    "help_other": "其他",
    "help_escape": "关闭危险模式，或清除搜索",
    "help_reload": "刷新会话列表",
    "help_open": "显示按键说明",
    "help_quit": "退出",
    "help_title": "按键说明",
    "help_close": "按 Esc、q、h 或 Enter 关闭",
    "cancel": "取消",
    "delete_irreversible": "此操作无法撤销。",
    "delete_descendant_one": "还将一并删除 1 个子代理会话。\n{warning}",
    "delete_descendant_many": "还将一并删除 {count} 个子代理会话。\n{warning}",
    "confirm_delete_title": "删除此会话？",
    "delete": "删除",
    "bulk_count": "已选择 {count} 个{what}\n",
    "bulk_remaining": "  · 还有 {count} 个…",
    "confirm_bulk_title": "删除所有{what}？",
    "confirm_bulk_button": "删除 {count} 个会话",
    "danger_line_one": "危险模式开启期间，按 d 会直接删除会话，不再确认。\n",
    "danger_line_two": "此操作无法撤销。按 ! 或 Esc 可关闭危险模式。",
    "confirm_danger_title": "开启危险模式？",
    "enable": "开启",
    "binding_archive": "归档",
    "binding_unarchive": "取消归档",
    "binding_delete": "删除",
    "binding_copy": "复制恢复命令",
    "binding_delete_archived": "删除已归档",
    "binding_delete_empty": "删除空会话",
    "binding_delete_orphans": "删除孤立会话",
    "binding_danger": "危险模式",
    "binding_search": "搜索",
    "binding_reload": "刷新列表",
    "binding_help": "按键说明",
    "binding_quit": "退出",
    "binding_focus": "切换焦点",
    "app_title": "{agent} 会话清理",
    "missing_cli_browse": "未找到 {agent} CLI。仍可浏览会话，但归档和删除功能不可用。",
    "no_sessions_here": "暂无会话。",
    "banner_sessions_one": "   1 个会话",
    "banner_sessions_many": "   {count} 个会话",
    "banner_archived_one": "   1 个已归档",
    "banner_archived_many": "   {count} 个已归档",
    "banner_orphans_one": "   1 个{what}",
    "banner_orphans_many": "   {count} 个{what}",
    "banner_danger": "      ⚠ 危险模式：按 d 直接删除，不再确认",
    "no_conversation": "此会话暂无消息。",
    "archived_marker": "◆ 已归档  ",
    "messages_one": "1 条消息",
    "messages_many": "{count} 条消息",
    "cwd_unknown": "未记录工作目录",
    "parent_deleted": "来源会话已删除，此子代理会话无法恢复",
    "spawned_by": "来源会话：{session_id}",
    "source_unrecorded": (
        "早期 Codex 版本未记录来源会话；此会话不会加入会话树，也不会被当作孤立会话清理"
    ),
    "you": "你",
    "message_truncated_one": "\n消息过长，末尾 1 个字符未显示",
    "message_truncated_many": "\n消息过长，末尾 {count} 个字符未显示",
    "search_empty_list": "没有可搜索的会话。",
    "search_not_started": "尚未搜索。按 / 开始搜索。",
    "search_not_found": "未找到“{query}”",
    "search_wrapped_forward": "  已从顶部继续",
    "search_wrapped_backward": "  已从底部继续",
    "search_status": "{sigil}{query}   匹配 {current}/{total}{note}",
    "busy_cannot_quit": "操作尚未完成，请稍后退出。",
    "reloaded": "会话列表已刷新。",
    "already_archived": "此会话已归档。按 u 可取消归档。",
    "not_archived": "此会话尚未归档。",
    "no_selection": "请先选择一个会话。",
    "copy_missing_cli_note": "（本机未安装 {agent}，请在可用的设备上运行此命令）",
    "copy_archived_note": "（此会话已归档，请先按 u 取消归档）",
    "copy_success": "恢复命令已复制到剪贴板{note}：{command}",
    "danger_off": "危险模式已关闭，删除操作将再次确认。",
    "danger_on": "危险模式已开启，按 d 将直接删除，不再确认。",
    "no_archived": "没有已归档会话。",
    "cascade_orphans_one": "还会删除 1 个子代理会话，以免留下孤立记录。",
    "cascade_orphans_many": "还会删除 {count} 个子代理会话，以免留下孤立记录。",
    "archived_sessions": "已归档会话",
    "empty_sessions": "空会话",
    "orphan_sessions": "孤立子代理会话",
    "none_of": "没有{what}。",
    "no_orphans": "没有{what}：所有已记录的来源会话都存在。",
    "orphan_note": "这些会话的来源会话已删除，因此无法恢复。",
    "deleting_progress": "正在删除 {done}/{total}：{title}",
    "bulk_failed": "已删除 {done}/{total} 个会话，{failed} 个失败：{message}",
    "bulk_deleted": "已删除 {count} 个{what}",
    "missing_cli_modify": "未找到 {agent} CLI，无法修改会话。",
    "previous_busy": "其他操作尚未完成，请稍候。",
    "operation_progress": "{operation} {done}/{total}：{title}",
    "operation_progress_one": "{operation}：{title}",
    "operation_tail_one": "（含 1 个子代理会话）",
    "operation_tail_many": "（含 {count} 个子代理会话）",
    "operation_done": "{operation}：{title}{tail}",
    "subagent_operation_failed": (
        "无法{operation}子代理会话。来源会话未作处理，以免产生孤立记录：{message}"
    ),
    "picker_unavailable": "未找到会话",
    "picker_loading": "正在加载会话…",
    "picker_count_one": "1 个会话",
    "picker_count_many": "{count} 个会话",
    "picker_browse_only": "   CLI 不可用，仅可浏览",
    "picker_title": "会话清理",
    "picker_open": "打开",
    "picker_choose": "选择 Agent",
    "picker_hint": "↑↓ 选择 · Enter 打开 · 也可按左侧快捷键",
    "picker_none": "未找到会话。按 q 退出。",
    "picker_agent_none": "{agent}：未找到会话",
    "codex_missing": "未找到 Codex CLI，无法修改会话。",
    "codex_start_failed": "无法启动 Codex CLI：{error}",
    "codex_timeout": "Codex CLI 在 {seconds} 秒内没有响应。",
    "codex_unknown_failure": "Codex CLI 未说明操作失败的原因。",
    "session_file_missing": "会话文件已不存在。按 r 刷新列表。",
    "session_id_mismatch": "文件中的会话 ID 与列表不一致，已取消删除。按 r 刷新列表。",
    "sidecar_delete_failed": "无法删除会话附属数据：{error}",
    "session_delete_failed": "无法删除会话：{error}",
    "claude_no_archive": "Claude Code 不支持会话归档。",
    "cli_description": "浏览和清理 Codex、Claude Code 的历史会话。",
    "cli_help": "显示帮助并退出",
    "cli_agent": "要浏览或清理的 Agent；省略时打开选择界面",
    "cli_directory": "目录",
    "cli_codex_home": "Codex 会话数据目录（默认：~/.codex）",
    "cli_claude_home": "Claude Code 会话数据目录（默认：~/.claude）",
    "cli_version": "显示版本号并退出",
    "cli_missing_home": "找不到 {agent} 会话目录：{path}",
}

_CATALOGS = {"en": _EN, "zh": _ZH}

_EN_SINGULAR_LABELS = {
    _EN["archived_sessions"]: "archived session",
    _EN["empty_sessions"]: "empty session",
    _EN["orphan_sessions"]: "orphaned sub-agent session",
}

_ARGPARSE_ZH = {
    "usage: ": "用法：",
    "%(heading)s:": "%(heading)s：",
    "positional arguments": "位置参数",
    "options": "选项",
    "argument %(argument_name)s: %(message)s": "参数 %(argument_name)s：%(message)s",
    "unrecognized arguments: %s": "无法识别的参数：%s",
    "expected one argument": "需要一个值",
    "invalid choice: %(value)r (choose from %(choices)s)": (
        "无效选择：%(value)r（可选值：%(choices)s）"
    ),
    "%(prog)s: error: %(message)s\n": "%(prog)s：错误：%(message)s\n",
}


def _normalize(value: str | None) -> str | None:
    if not value:
        return None
    first = value.split(":", 1)[0].strip().replace("-", "_").lower()
    if first.startswith("zh"):
        return "zh"
    if first.startswith("en") or first in {"c", "posix"}:
        return "en"
    return None


def detect_language(
    environ: Mapping[str, str] | None = None,
    *,
    system_locale: str | None = None,
) -> str:
    """Return ``zh`` for Chinese locales and ``en`` for everything else."""
    env = os.environ if environ is None else environ
    explicit = _normalize(env.get(LANGUAGE_ENV))
    if explicit:
        return explicit
    for name in ("LC_ALL", "LC_MESSAGES", "LANGUAGE", "LANG"):
        value = env.get(name)
        if value:
            return _normalize(value) or "en"
    if system_locale is None:
        system_locale = locale.getlocale()[0]
    return _normalize(system_locale) or "en"


_language = detect_language()


def language() -> str:
    return _language


def t(key: str, /, **values: object) -> str:
    template = _CATALOGS[_language].get(key, _EN[key])
    return template.format(**values)


def n(one: str, many: str, count: int, /, **values: object) -> str:
    return t(one if count == 1 else many, count=count, **values)


def count_label(label: str, count: int) -> str:
    """Return the singular form of a translated category when needed."""
    if _language == "en" and count == 1:
        return _EN_SINGULAR_LABELS.get(label, label)
    return label


def catalogs_match() -> bool:
    """Expose a cheap completeness check without leaking the catalogs."""
    return _EN.keys() == _ZH.keys()


def argparse_text(message: str) -> str:
    """Translate argparse's built-in labels and errors with the app locale."""
    return _ARGPARSE_ZH.get(message, message) if _language == "zh" else message
