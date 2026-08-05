"""Agent chooser shown when no backend is named on the command line."""

from __future__ import annotations

import asyncio
from pathlib import Path
from typing import ClassVar

from rich.cells import set_cell_size
from rich.text import Text
from textual import events, work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Vertical
from textual.widgets import Footer, ListItem, ListView, Static

from .backends import BACKEND_CLASSES, Backend

LABEL_WIDTH = 13


class AgentRow(ListItem):
    """One agent per line: shortcut, name, and how many sessions it has."""

    def __init__(self, backend: Backend) -> None:
        self.backend = backend
        self.shortcut = backend.shortcut
        self.usable = backend.home.is_dir()
        self._label = Static(self._summary(None))
        super().__init__(self._label, disabled=not self.usable)

    def set_count(self, count: int) -> None:
        self._label.update(self._summary(count))

    def _summary(self, count: int | None) -> Text:
        text = Text(no_wrap=True, overflow="ellipsis")
        text.append(f" {self.shortcut} ", style="bold reverse" if self.usable else "dim reverse")
        text.append("  ")
        text.append(set_cell_size(self.backend.label, LABEL_WIDTH), style="bold")

        if not self.usable:
            text.append("暂无会话记录", style="dim")
            return text
        if count is None:
            text.append("正在读取…", style="dim")
            return text
        text.append(f"{count} 个会话", style="")
        if self.backend.missing_cli():
            text.append("   未安装命令行工具，只能浏览", style="dim")
        return text


class AgentPicker(App[str | None]):
    """Returns the chosen backend id, or None if the user quit."""

    CSS_PATH = "picker.tcss"
    TITLE = "会话清理"

    BINDINGS: ClassVar[list[Binding]] = [
        Binding("enter", "choose", "打开"),
        Binding("j", "cursor_down", show=False),
        Binding("k", "cursor_up", show=False),
        Binding("q", "quit", "退出"),
    ]

    def __init__(self, homes: dict[str, Path | None] | None = None) -> None:
        super().__init__()
        self._homes: dict[str, Path | None] = homes or {}

    def compose(self) -> ComposeResult:
        with Vertical(id="picker-box"):
            yield Static("选择会话来源", id="picker-title")
            yield ListView(
                *(AgentRow(cls(self._homes.get(key))) for key, cls in BACKEND_CLASSES.items()),
                id="picker-list",
            )
            yield Static("", id="picker-hint")
        yield Footer()

    def on_mount(self) -> None:
        rows = list(self.query(AgentRow))
        if any(row.usable for row in rows):
            self._hint("↑↓ 选择 · Enter 打开 · 或按左侧字母")
        else:
            self._hint("没有找到会话记录，按 q 退出")
        self.query_one("#picker-list", ListView).focus()
        self._count_sessions()

    @work(group="counts")
    async def _count_sessions(self) -> None:
        for row in self.query(AgentRow):
            if not row.usable:
                continue
            try:
                sessions = await asyncio.to_thread(row.backend.discover)
            except OSError:
                row.set_count(0)
                continue
            row.set_count(len(sessions))

    def action_cursor_down(self) -> None:
        self.query_one("#picker-list", ListView).action_cursor_down()

    def action_cursor_up(self) -> None:
        self.query_one("#picker-list", ListView).action_cursor_up()

    def _hint(self, message: str) -> None:
        self.query_one("#picker-hint", Static).update(Text(message, style="dim"))

    def _open(self, row: AgentRow) -> None:
        if row.usable:
            self.exit(row.backend.id)
            return
        # Explain why the shortcut did nothing instead of leaving it ambiguous.
        self._hint(f"{row.backend.label} 暂无会话记录")

    def action_choose(self) -> None:
        item = self.query_one("#picker-list", ListView).highlighted_child
        if isinstance(item, AgentRow):
            self._open(item)

    def on_list_view_selected(self, event: ListView.Selected) -> None:
        if isinstance(event.item, AgentRow):
            self._open(event.item)

    def on_key(self, event: events.Key) -> None:
        for row in self.query(AgentRow):
            if event.key == row.shortcut:
                event.stop()
                self._open(row)
                return
