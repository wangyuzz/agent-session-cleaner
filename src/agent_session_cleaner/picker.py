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
from .i18n import n, t

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
            text.append(t("picker_unavailable"), style="dim")
            return text
        if count is None:
            text.append(t("picker_loading"), style="dim")
            return text
        text.append(n("picker_count_one", "picker_count_many", count), style="")
        if self.backend.missing_cli():
            text.append(t("picker_browse_only"), style="dim")
        return text


class AgentPicker(App[str | None]):
    """Returns the chosen backend id, or None if the user quit."""

    CSS_PATH = "picker.tcss"
    TITLE = t("picker_title")

    BINDINGS: ClassVar[list[Binding]] = [
        Binding("enter", "choose", t("picker_open")),
        Binding("j", "cursor_down", show=False),
        Binding("k", "cursor_up", show=False),
        Binding("q", "quit", t("binding_quit")),
    ]

    def __init__(self, homes: dict[str, Path | None] | None = None) -> None:
        super().__init__()
        self._homes: dict[str, Path | None] = homes or {}

    def compose(self) -> ComposeResult:
        with Vertical(id="picker-box"):
            yield Static(t("picker_choose"), id="picker-title")
            yield ListView(
                *(AgentRow(cls(self._homes.get(key))) for key, cls in BACKEND_CLASSES.items()),
                id="picker-list",
            )
            yield Static("", id="picker-hint")
        yield Footer()

    def on_mount(self) -> None:
        rows = list(self.query(AgentRow))
        if any(row.usable for row in rows):
            self._hint(t("picker_hint"))
        else:
            self._hint(t("picker_none"))
        self.query_one("#picker-list", ListView).focus()
        self._count_sessions()

    @work(group="counts")
    async def _count_sessions(self) -> None:
        for row in self.query(AgentRow):
            if not row.usable:
                continue
            try:
                sessions = await asyncio.to_thread(row.backend.discover)
            except Exception:
                # One agent's unreadable store must not leave every row after it
                # stuck on "Loading…", which is what killing this worker would do.
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
        self._hint(t("picker_agent_none", agent=row.backend.label))

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
