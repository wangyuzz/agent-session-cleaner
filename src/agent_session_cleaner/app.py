"""The session browser: a list on the left, the conversation on the right."""

from __future__ import annotations

import asyncio
import shlex
from datetime import date, datetime, timedelta
from pathlib import Path
from typing import ClassVar

from rich.cells import cell_len, set_cell_size
from rich.text import Text
from textual import work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical, VerticalScroll
from textual.screen import ModalScreen
from textual.widgets import Button, Footer, Input, Label, ListItem, ListView, Static

from . import clipboard
from .backends import Backend
from .model import Message, Session

DETAIL_CACHE_LIMIT = 48
# Column widths, in terminal cells. The date and project columns size themselves
# to the data; the date only grows when sessions from another year are in view.
DAY_WIDTH_MIN = 5
PROJECT_WIDTH_MIN = 8
PROJECT_WIDTH_MAX = 18
# How many titles the bulk-delete dialog lists before collapsing the rest.
BULK_PREVIEW_LIMIT = 6
# Rendering a conversation mounts one widget per message. Debounce so that
# holding `j` or typing a search doesn't render every session passed over; the
# worker is exclusive, so a newer selection cancels this wait.
DETAIL_DEBOUNCE_SECONDS = 0.06

_OPERATION_LABELS = {"archive": "归档", "unarchive": "取消归档", "delete": "删除"}
#: Only meaningful for backends that can archive; hidden entirely otherwise.
_ARCHIVE_ACTIONS = frozenset({"archive", "unarchive", "toggle_view", "delete_archived"})
#: Everything that writes; hidden when the agent's command line is missing.
_CHANGING_ACTIONS = frozenset(
    {"archive", "unarchive", "delete", "delete_archived", "delete_empty", "toggle_danger"}
)


def _tilde(path: str | Path) -> str:
    text = str(path)
    home = str(Path.home())
    return "~" + text[len(home) :] if text.startswith(home) else text


def _case_sensitive(query: str) -> bool:
    """Smart case, as in vim: an uppercase letter makes the search exact."""
    return query != query.lower()


def _day_label(when: datetime, today: date) -> str:
    day = when.date()
    if day == today:
        return "今天"
    if day == today - timedelta(days=1):
        return "昨天"
    # Old sessions are the ones most worth clearing out, so a year-old session
    # must not be able to pass for a recent one by showing only month and day.
    if day.year != today.year:
        return when.strftime("%y-%m-%d")
    return when.strftime("%m-%d")


def _project(session: Session) -> str:
    """The last path segment of cwd — enough to tell projects apart."""
    return Path(session.cwd).name if session.cwd else ""


class SessionRow(ListItem):
    """One session per line, in columns: date · time · project · title.

    The date only appears on the first row of each day, so a run of sessions
    reads as a group without spending a column on the same string 20 times.
    """

    def __init__(
        self,
        session: Session,
        *,
        day: str,
        day_width: int,
        project_width: int,
        odd: bool,
        default_client: str,
    ) -> None:
        self._day = day
        self._day_width = day_width
        self._project_width = project_width
        self._default_client = default_client
        self._label = Static(self._summary(session))
        classes = " ".join(
            filter(None, ["-odd" if odd else "", "-archived" if session.archived else ""])
        )
        super().__init__(self._label, classes=classes)
        self.session = session

    def highlight(self, query: str) -> None:
        self._label.update(self._summary(self.session, query))

    def _summary(self, session: Session, query: str = "") -> Text:
        # Archived rows carry no per-span colour so that a single CSS rule can
        # grey the whole line out; active rows shade their columns individually.
        muted = session.archived
        text = Text(no_wrap=True, overflow="ellipsis")
        text.append(set_cell_size(self._day, self._day_width))
        text.append(" ")
        text.append(session.recency_at.strftime("%H:%M"), style="" if muted else "dim")
        text.append("  ")
        text.append(
            set_cell_size(_project(session), self._project_width),
            style="" if muted else "dim cyan",
        )
        text.append("  ")
        # One client dominates each agent; naming it on every row is noise, so
        # only the exceptions get a badge.
        if session.client != self._default_client:
            text.append(f"{session.client} · ", style="" if muted else "dim")
        text.append(session.title, style="bold" if session.named and not muted else "")

        if query:
            text.highlight_words(
                [query], "black on yellow", case_sensitive=_case_sensitive(query)
            )
        return text


class SessionList(ListView):
    BINDINGS: ClassVar[list[Binding]] = [
        Binding("j", "cursor_down", show=False),
        Binding("k", "cursor_up", show=False),
        Binding("g", "goto_top", show=False),
        Binding("G", "goto_bottom", show=False),
    ]

    def action_goto_top(self) -> None:
        if len(self.children):
            self.index = 0

    def action_goto_bottom(self) -> None:
        if len(self.children):
            self.index = len(self.children) - 1


class SearchInput(Input):
    BINDINGS: ClassVar[list[Binding]] = [Binding("escape", "abandon_search", show=False)]

    def action_abandon_search(self) -> None:
        self.app.cancel_search()


class DetailPane(VerticalScroll):
    can_focus = True

    BINDINGS: ClassVar[list[Binding]] = [
        Binding("j", "scroll_down", show=False),
        Binding("k", "scroll_up", show=False),
        Binding("g", "scroll_home", show=False),
        Binding("G", "scroll_end", show=False),
    ]


class ConfirmScreen(ModalScreen[bool]):
    """Destructive-action confirmation. Cancel is focused, so Enter is safe."""

    # Focusing in on_mount would let the app's default "focus the first widget"
    # land on the delete button for one frame first, which reads as a flicker on
    # a dialog whose whole point is that you don't hit delete by accident.
    AUTO_FOCUS = "#confirm-no"

    BINDINGS: ClassVar[list[Binding]] = [
        Binding("y", "confirm", show=False),
        Binding("n", "cancel", show=False),
        Binding("escape", "cancel", show=False),
    ]

    def __init__(self, *, title: str, subject: Text, body: str, confirm_label: str) -> None:
        super().__init__()
        self._title = title
        self._subject = subject
        self._body = body
        self._confirm_label = confirm_label

    def compose(self) -> ComposeResult:
        with Vertical(id="confirm-box"):
            yield Static(self._title, id="confirm-title")
            yield Static(self._subject, id="confirm-subject")
            # Skipped when empty, so the dialog doesn't carry a blank gap.
            if self._body:
                yield Static(self._body, id="confirm-body")
            with Horizontal(id="confirm-actions"):
                yield Button(f"{self._confirm_label} (y)", variant="error", id="confirm-yes")
                yield Button("取消 (n)", id="confirm-no")

    def action_confirm(self) -> None:
        self.dismiss(True)

    def action_cancel(self) -> None:
        self.dismiss(False)

    def on_button_pressed(self, event: Button.Pressed) -> None:
        self.dismiss(event.button.id == "confirm-yes")


def confirm_one(session: Session) -> ConfirmScreen:
    subject = Text(no_wrap=True, overflow="ellipsis")
    subject.append(session.title + "\n", style="bold")
    subject.append(session.session_id, style="dim")
    return ConfirmScreen(
        title="删除这个会话？",
        subject=subject,
        body="删除后无法恢复。",
        confirm_label="删除",
    )


def confirm_bulk(targets: list[Session], what: str, hidden: int = 0) -> ConfirmScreen:
    subject = Text(no_wrap=True, overflow="ellipsis")
    subject.append(f"共 {len(targets)} 个{what}\n", style="bold")
    for session in targets[:BULK_PREVIEW_LIMIT]:
        subject.append(f"  · {session.title}\n", style="dim")
    remaining = len(targets) - BULK_PREVIEW_LIMIT
    if remaining > 0:
        subject.append(f"  · 还有 {remaining} 个…", style="dim")

    body = "删除后无法恢复。"
    if hidden:
        # Naming a key here would be guesswork: a row can be out of view because
        # of the archive filter, the noise filter, or both.
        body += f"\n其中 {hidden} 个当前没有显示在列表里，也会一起删掉。"
    return ConfirmScreen(
        title=f"删除全部{what}？",
        subject=subject,
        body=body,
        confirm_label=f"删除 {len(targets)} 个",
    )


def confirm_danger() -> ConfirmScreen:
    subject = Text()
    subject.append("开启后按 d 删除会话不再询问，一按即删。\n", style="bold")
    subject.append("删除无法恢复。再按一次 ! 或 Esc 关闭。", style="dim")
    return ConfirmScreen(
        title="开启危险模式？",
        subject=subject,
        body="",
        confirm_label="开启",
    )


class SessionCleanerApp(App[None]):
    CSS_PATH = "app.tcss"
    #: There is nothing to search for in a palette here, and its footer entry
    #: costs a dozen columns that the actual keys need.
    ENABLE_COMMAND_PALETTE = False

    # Footer order is binding order, and a narrow terminal drops the tail, so
    # the keys someone could get stuck without come first and the ones the
    # README can carry — danger mode, refresh — come last.
    BINDINGS: ClassVar[list[Binding]] = [
        Binding("a", "archive", "归档"),
        Binding("u", "unarchive", "取消归档"),
        Binding("d", "delete", "删除"),
        Binding("c", "copy_resume", "恢复命令"),
        Binding("v", "toggle_view", "已归档"),
        Binding("s", "toggle_sources", "杂项会话"),
        Binding("D", "delete_archived", "清空归档"),
        Binding("E", "delete_empty", "清除空会话"),
        Binding("slash", "search_forward", "搜索"),
        Binding("q", "quit", "退出"),
        Binding("exclamation_mark", "toggle_danger", "危险模式"),
        Binding("r", "reload", "刷新"),
        Binding("question_mark", "search_backward", show=False),
        Binding("n", "search_next", show=False),
        Binding("N", "search_previous", show=False),
        Binding("escape", "clear_search", show=False),
        Binding("tab", "focus_next", "切换焦点", show=False),
    ]

    def __init__(self, backend: Backend) -> None:
        super().__init__()
        self.backend = backend
        self.title = f"{backend.label} 会话清理"
        #: Without the agent's command line we can still browse, just not change.
        self._missing_cli = backend.missing_cli()
        self._sessions: list[Session] = []
        self._visible: list[Session] = []
        #: Keyed by (path, mtime, size) so an edited transcript re-reads itself.
        self._detail_cache: dict[tuple[str, float, int], list[Message]] = {}
        self._show_archived = False
        # Agents without a noise concept simply show everything.
        self._all_sources = backend.noise_label is None
        self._danger = False
        self._busy = False
        self._query = ""
        self._query_before_search = ""
        self._search_direction = 1
        self._search_origin = 0

    def check_action(self, action: str, parameters: tuple) -> bool | None:
        """Hide keys that this agent, or this installation, cannot support."""
        if action in _ARCHIVE_ACTIONS and not self.backend.supports_archive:
            return False
        if action == "toggle_sources" and self.backend.noise_label is None:
            return False
        if action == "delete_empty" and self.backend.empty_label is None:
            return False
        if self._missing_cli and action in _CHANGING_ACTIONS:
            return False
        return True

    def compose(self) -> ComposeResult:
        yield Static(id="banner")
        with Horizontal(id="body"):
            yield SessionList(id="sessions")
            yield DetailPane(id="detail")
        yield Static(id="status")
        with Horizontal(id="search-bar"):
            yield Label("/", id="search-sigil")
            yield SearchInput(id="search-input")
        # Compact: the default padding around every key adds up to more than a
        # column of keys on an 80-wide terminal.
        yield Footer(compact=True)

    async def on_mount(self) -> None:
        self.query_one("#search-bar").display = False
        self.query_one("#sessions", SessionList).focus()
        if self._missing_cli:
            self._set_status(
                f"没有安装 {self.backend.label}，只能查看，不能归档或删除", tone="error"
            )
        else:
            self._set_status(_tilde(self.backend.home))
        await self._reload()

    # ------------------------------------------------------------------ state

    def _selected(self) -> Session | None:
        item = self.query_one("#sessions", SessionList).highlighted_child
        return item.session if isinstance(item, SessionRow) else None

    def _filtered(self) -> list[Session]:
        return [
            session
            for session in self._sessions
            if (self._show_archived or not session.archived)
            and (self._all_sources or not session.noise)
        ]

    async def _reload(self, prefer_id: str | None = None, prefer_index: int = 0) -> None:
        self._sessions = await asyncio.to_thread(self.backend.discover)
        await self._repopulate(prefer_id, prefer_index)

    async def _repopulate(self, prefer_id: str | None = None, prefer_index: int = 0) -> None:
        self._visible = self._filtered()
        listing = self.query_one("#sessions", SessionList)
        await listing.clear()
        if self._visible:
            await listing.extend(self._build_rows())
        self._update_banner()

        if not self._visible:
            await self._show_placeholder(f"这里没有会话。{self._filter_hint()}")
            return

        index = prefer_index
        if prefer_id is not None:
            index = next(
                (i for i, s in enumerate(self._visible) if s.session_id == prefer_id),
                prefer_index,
            )
        index = max(0, min(index, len(self._visible) - 1))
        listing.index = index
        self._apply_highlight()
        # Setting `index` only emits Highlighted when the value changes, so kick
        # the detail load off explicitly to cover the unchanged-index case.
        self._load_detail(self._visible[index])

    def _filter_hint(self) -> str:
        """Which keys would widen an empty list — only the ones actually there.

        Something is being hidden only when a filter is switched on, so an
        agent with nothing to hide, or a view with nothing filtered, says
        nothing rather than pointing at a key that does not exist.
        """
        keys = []
        if self.backend.supports_archive and not self._show_archived:
            keys.append("v")
        if self.backend.noise_label is not None and not self._all_sources:
            keys.append("s")
        return f"  按 {' 或 '.join(keys)} 看看隐藏起来的。" if keys else ""

    def _build_rows(self) -> list[SessionRow]:
        today = date.today()
        widest = max((cell_len(_project(s)) for s in self._visible), default=0)
        project_width = min(max(widest, PROJECT_WIDTH_MIN), PROJECT_WIDTH_MAX)

        # The date is printed once per day rather than once per row, so the
        # labels have to be worked out before the column can be sized.
        labels: list[str] = []
        previous_day: date | None = None
        for session in self._visible:
            day = session.recency_at.date()
            labels.append("" if day == previous_day else _day_label(session.recency_at, today))
            previous_day = day
        day_width = max(DAY_WIDTH_MIN, max((cell_len(label) for label in labels), default=0))

        return [
            SessionRow(
                session,
                day=label,
                day_width=day_width,
                project_width=project_width,
                odd=bool(index % 2),
                default_client=self.backend.default_client,
            )
            for index, (session, label) in enumerate(zip(self._visible, labels, strict=True))
        ]

    def _update_banner(self) -> None:
        archived = sum(1 for session in self._sessions if session.archived)
        hidden_noise = sum(1 for session in self._sessions if session.noise)
        text = Text(no_wrap=True, overflow="ellipsis")
        text.append(self.backend.label, style="bold")
        text.append("   显示 ", style="dim")
        text.append(f"{len(self._visible)}", style="bold")
        text.append(f" / {len(self._sessions)} 个会话", style="dim")
        # Only report a filter that is actually holding something back — "已归档的
        # 0 个未显示" is a sentence about nothing.
        if self.backend.supports_archive and archived:
            if self._show_archived:
                text.append(f"   含 {archived} 个已归档")
            else:
                text.append(f"   已归档的 {archived} 个未显示")
        if not self._all_sources and hidden_noise:
            text.append(f"   已隐藏 {hidden_noise} 个{self.backend.noise_label}", style="dim")
        if self._danger:
            text.append("      ⚠ 危险模式：删除不再询问", style="bold")
        banner = self.query_one("#banner", Static)
        banner.set_class(self._danger, "-danger")
        banner.update(text)

    def _set_status(self, message: str, *, tone: str = "") -> None:
        status = self.query_one("#status", Static)
        status.set_classes(f"-{tone}" if tone else "")
        status.update(message)

    # ----------------------------------------------------------------- detail

    def on_list_view_highlighted(self, event: ListView.Highlighted) -> None:
        if isinstance(event.item, SessionRow):
            self._load_detail(event.item.session)

    @work(exclusive=True, group="detail")
    async def _load_detail(self, session: Session) -> None:
        await asyncio.sleep(DETAIL_DEBOUNCE_SECONDS)
        key = (str(session.path), session.updated_at.timestamp(), session.size)
        messages = self._detail_cache.get(key)
        if messages is None:
            messages = await asyncio.to_thread(self.backend.load_messages, session)
            if len(self._detail_cache) >= DETAIL_CACHE_LIMIT:
                self._detail_cache.pop(next(iter(self._detail_cache)))
            self._detail_cache[key] = messages

        current = self._selected()
        if current is None or current.session_id != session.session_id:
            return
        await self._render_detail(session, messages)

    async def _render_detail(self, session: Session, messages: list[Message]) -> None:
        pane = self.query_one("#detail", DetailPane)
        await pane.remove_children()

        widgets: list[Static] = [Static(self._detail_header(session, messages), id="detail-header")]
        if messages:
            widgets.extend(
                Static(self._message_text(message), classes=f"msg {message.role}")
                for message in messages
            )
        else:
            widgets.append(Static("这个会话没有对话内容。", classes="placeholder"))

        await pane.mount_all(widgets)
        pane.scroll_home(animate=False)

    @staticmethod
    def _detail_header(session: Session, messages: list[Message]) -> Text:
        text = Text()
        if session.archived:
            text.append("◆ 已归档  ", style="yellow")
        text.append(session.title + "\n", style="bold")

        meta = [session.session_id, session.client, f"{len(messages)} 条消息"]
        if session.created_at:
            meta.insert(1, session.created_at.strftime("%Y-%m-%d %H:%M"))
        if session.version:
            meta.append(f"v{session.version}")
        text.append(" · ".join(meta), style="dim")
        if session.cwd:
            text.append("\n" + _tilde(session.cwd), style="dim")
        return text

    def _message_text(self, message: Message) -> Text:
        is_user = message.role == "user"
        text = Text()
        text.append(
            "▶ 你\n" if is_user else f"◀ {self.backend.agent_label}\n",
            style="bold green" if is_user else "bold blue",
        )
        text.append(message.text)
        if message.truncated_chars:
            text.append(f"\n…… 太长了，省略了 {message.truncated_chars} 个字符", style="dim italic")
        return text

    # ----------------------------------------------------------------- search

    def action_search_forward(self) -> None:
        self._begin_search(1)

    def action_search_backward(self) -> None:
        self._begin_search(-1)

    def _begin_search(self, direction: int) -> None:
        if not self._visible:
            self._set_status("没有可以搜索的会话", tone="error")
            return
        self._search_direction = direction
        self._search_origin = self._current_index()
        self._query_before_search = self._query
        self.query_one("#search-sigil", Label).update("/" if direction > 0 else "?")
        self.query_one("#search-bar").display = True
        search = self.query_one("#search-input", SearchInput)
        search.value = ""
        search.focus()

    def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id != "search-input":
            return
        self._query = event.value
        self._apply_highlight()
        if self._query:
            # Incremental search starts at the cursor, so a row already under it
            # counts as a match; `n` afterwards is what moves past it.
            self._jump(self._search_origin, self._search_direction, inclusive=True)

    def on_input_submitted(self, event: Input.Submitted) -> None:
        if event.input.id == "search-input":
            self._end_search(restore=False)

    def cancel_search(self) -> None:
        self._query = self._query_before_search
        self._apply_highlight()
        self._set_status("")
        self._end_search(restore=True)

    def action_clear_search(self) -> None:
        """Esc backs out of whatever state we're in, most alarming first."""
        if self._danger:
            self.action_toggle_danger()
            return
        if not self._query:
            return
        self._query = ""
        self._apply_highlight()
        self._set_status("")

    def _end_search(self, *, restore: bool) -> None:
        self.query_one("#search-bar").display = False
        listing = self.query_one("#sessions", SessionList)
        if restore and self._visible:
            listing.index = min(self._search_origin, len(self._visible) - 1)
        listing.focus()

    def action_search_next(self) -> None:
        self._repeat_search(self._search_direction)

    def action_search_previous(self) -> None:
        self._repeat_search(-self._search_direction)

    def _repeat_search(self, direction: int) -> None:
        if not self._query:
            self._set_status("还没有搜索过，按 / 开始", tone="error")
            return
        self._jump(self._current_index(), direction, inclusive=False)

    def _haystack(self, session: Session) -> str:
        parts = [session.title, session.client, session.session_id, session.cwd or ""]
        return " ".join(parts)

    def _matches(self) -> list[int]:
        if not self._query:
            return []
        sensitive = _case_sensitive(self._query)
        needle = self._query if sensitive else self._query.lower()
        hay = (lambda s: self._haystack(s)) if sensitive else (lambda s: self._haystack(s).lower())
        return [index for index, session in enumerate(self._visible) if needle in hay(session)]

    def _jump(self, start: int, direction: int, *, inclusive: bool) -> None:
        matches = self._matches()
        if not matches:
            self._set_status(f"没有找到「{self._query}」", tone="error")
            return

        # "Wrapped" means the search ran off the end and started over, which is
        # exactly "nothing left in the direction we were going" — inferring it
        # from the target's position instead gets a backwards search that stays
        # put wrong.
        if direction > 0:
            ahead = [i for i in matches if (i >= start if inclusive else i > start)]
            target, wrapped = (ahead[0], False) if ahead else (matches[0], True)
        else:
            behind = [i for i in matches if (i <= start if inclusive else i < start)]
            target, wrapped = (behind[-1], False) if behind else (matches[-1], True)

        self.query_one("#sessions", SessionList).index = target
        sigil = "/" if self._search_direction > 0 else "?"
        note = "  已回绕" if wrapped else ""
        self._set_status(
            f"{sigil}{self._query}   匹配 {matches.index(target) + 1}/{len(matches)}{note}"
        )

    def _apply_highlight(self) -> None:
        for row in self.query(SessionRow):
            row.highlight(self._query)

    async def _show_placeholder(self, message: str) -> None:
        pane = self.query_one("#detail", DetailPane)
        await pane.remove_children()
        await pane.mount(Static(message, classes="placeholder"))

    # ---------------------------------------------------------------- actions

    def action_toggle_view(self) -> None:
        self._show_archived = not self._show_archived
        self._repopulate_preserving_selection()

    def action_toggle_sources(self) -> None:
        self._all_sources = not self._all_sources
        self._repopulate_preserving_selection()

    @work(group="view", exclusive=True)
    async def _repopulate_preserving_selection(self) -> None:
        current = self._selected()
        await self._repopulate(current.session_id if current else None, self._current_index())

    async def action_quit(self) -> None:
        """Quitting mid-delete would kill the worker between two sessions and
        leave the sweep half done, so it waits."""
        if self._busy:
            self._set_status("正在处理，完成后再退出", tone="error")
            return
        self.exit()

    def action_reload(self) -> None:
        if self._reject_while_busy():
            return
        self._reload_worker()

    @work(group="view", exclusive=True)
    async def _reload_worker(self) -> None:
        current = self._selected()
        await self._reload(current.session_id if current else None, self._current_index())
        self._set_status("已刷新")

    def action_archive(self) -> None:
        session = self._require_selection()
        if session is None:
            return
        if session.archived:
            self._set_status("这个会话已经归档了，按 u 可以恢复", tone="error")
            return
        self._run_operation("archive", session)

    def action_unarchive(self) -> None:
        session = self._require_selection()
        if session is None:
            return
        if not session.archived:
            self._set_status("这个会话没有归档", tone="error")
            return
        self._run_operation("unarchive", session)

    def action_copy_resume(self) -> None:
        """Put a `cd … && … resume …` line on the clipboard."""
        session = self._selected()
        if session is None:
            self._set_status("没有选中任何会话", tone="error")
            return

        command = self.backend.resume_command(session)
        if session.cwd:
            command = f"cd {shlex.quote(session.cwd)} && {command}"

        if not clipboard.copy(command):
            # No native helper; OSC 52 works in terminals that support it.
            self.copy_to_clipboard(command)

        if self._missing_cli:
            note = f"（这台机器上没有 {self.backend.label}，命令要拿到别处用）"
        elif session.archived:
            note = "（已归档，先按 u 取消归档才能恢复）"
        else:
            note = ""
        self._set_status(f"已复制{note}：{command}", tone="ok")

    def action_delete(self) -> None:
        session = self._require_selection()
        if session is None:
            return
        if self._danger:
            self._run_operation("delete", session)
            return
        self._confirm_delete(session)

    @work(group="confirm", exclusive=True)
    async def _confirm_delete(self, session: Session) -> None:
        if await self.push_screen_wait(confirm_one(session)):
            self._run_operation("delete", session)

    def action_toggle_danger(self) -> None:
        if self._danger:
            self._danger = False
            self._update_banner()
            self._set_status("已关闭危险模式，删除会重新询问")
            return
        if self._reject_while_busy():
            return
        self._confirm_danger()

    @work(group="confirm", exclusive=True)
    async def _confirm_danger(self) -> None:
        if await self.push_screen_wait(confirm_danger()):
            self._danger = True
            self._update_banner()
            self._set_status("危险模式已开启，按 d 会直接删除", tone="error")

    def action_delete_archived(self) -> None:
        if self._reject_while_busy():
            return
        archived = [session for session in self._sessions if session.archived]
        if not archived:
            self._set_status("没有已归档的会话", tone="error")
            return
        visible = {session.session_id for session in self._visible}
        hidden = sum(1 for session in archived if session.session_id not in visible)
        self._confirm_bulk(archived, "已归档的会话", hidden)

    def action_delete_empty(self) -> None:
        if self._reject_while_busy():
            return
        what = self.backend.empty_label
        empty = [session for session in self._sessions if session.noise]
        if not empty:
            self._set_status(f"没有{what}", tone="error")
            return
        self._confirm_bulk(empty, what)

    @work(group="confirm", exclusive=True)
    async def _confirm_bulk(self, targets: list[Session], what: str, hidden: int = 0) -> None:
        if await self.push_screen_wait(confirm_bulk(targets, what, hidden)):
            self._busy = True
            self._bulk_delete_worker(targets, what)

    @work(group="op")
    async def _bulk_delete_worker(self, targets: list[Session], what: str) -> None:
        index = self._current_index()
        failures: list[str] = []
        try:
            for done, session in enumerate(targets, start=1):
                self._set_status(f"正在删除 {done}/{len(targets)}：{session.title}")
                result = await self.backend.delete(session)
                if not result.ok:
                    failures.append(result.message)
        finally:
            self._busy = False

        await self._reload(None, index)
        if failures:
            self._set_status(
                f"删掉了 {len(targets) - len(failures)}/{len(targets)} 个，"
                f"{len(failures)} 个失败：{failures[0]}",
                tone="error",
            )
        else:
            self._set_status(f"已清除全部 {len(targets)} 个{what}", tone="ok")

    # ------------------------------------------------------------- operations

    def _require_selection(self) -> Session | None:
        if self._missing_cli:
            self._set_status(f"没有安装 {self.backend.label}，无法修改会话", tone="error")
            return None
        if self._reject_while_busy():
            return None
        session = self._selected()
        if session is None:
            self._set_status("没有选中任何会话", tone="error")
        return session

    def _reject_while_busy(self) -> bool:
        if self._busy:
            self._set_status("有操作正在执行，请稍候", tone="error")
        return self._busy

    def _current_index(self) -> int:
        return self.query_one("#sessions", SessionList).index or 0

    def _run_operation(self, kind: str, session: Session) -> None:
        # Claim the busy flag synchronously: a worker doesn't start until the
        # next event-loop tick, which would let a double keypress fire twice.
        self._busy = True
        self._operation_worker(kind, session)

    @work(group="op")
    async def _operation_worker(self, kind: str, session: Session) -> None:
        label = _OPERATION_LABELS[kind]
        index = self._current_index()
        self._set_status(f"正在{label}：{session.title}")
        try:
            result = await getattr(self.backend, kind)(session)
        finally:
            self._busy = False

        # Archiving moves the file, so re-select by id where the row survives
        # (the archived view) and fall back to the same slot where it doesn't.
        prefer_id = None if kind == "delete" else session.session_id
        await self._reload(prefer_id, index)
        # Backends report failures in their own words; success is phrased here so
        # the wording stays the same whichever agent is being managed.
        if result.ok:
            self._set_status(f"已{label}：{session.title}", tone="ok")
        else:
            self._set_status(result.message, tone="error")
