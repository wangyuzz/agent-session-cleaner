"""Interactive session browser: session list on the left, conversation on the right."""

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

# ``FooterKey`` is not re-exported by ``textual.widgets``. Yielding the same
# widget as ``Footer.compose`` keeps the custom two-row footer visually native.
from textual.widgets._footer import FooterKey

from . import clipboard
from .backends import Backend
from .i18n import count_label, n, t
from .model import Message, Session, orphans

DETAIL_CACHE_LIMIT = 48
# htop-style tree connectors. Sub-agent sessions sit beneath the conversation
# that spawned them; a broken connector marks one whose parent is gone.
TREE_BRANCH = "├─ "
TREE_LAST = "└─ "
TREE_TRUNK = "│  "
TREE_GAP = "   "
TREE_SEVERED = "╌╌ "
# Older Codex versions did not record a sub-agent's parent. Such a session is
# neither a root nor a proven orphan, so it gets a distinct marker.
TREE_UNKNOWN = "·· "
# Column widths, in terminal cells. The date and project columns size themselves
# to the data; the date only grows when sessions from another year are in view.
DAY_WIDTH_MIN = 5
PROJECT_WIDTH_MIN = 8
PROJECT_WIDTH_MAX = 18
# How many titles the bulk-delete dialog lists before collapsing the rest.
BULK_PREVIEW_LIMIT = 6
# Chinese help fits comfortably at this width; longer translations may expand
# the dialog to match their longest line.
HELP_WIDTH_MIN = 72
# Rendering a conversation mounts one widget per message. Debounce so that
# holding `j` or typing a search doesn't render every session passed over; the
# worker is exclusive, so a newer selection cancels this wait.
DETAIL_DEBOUNCE_SECONDS = 0.06

_OPERATION_INFINITIVES = {
    "archive": t("operation_archive"),
    "unarchive": t("operation_unarchive"),
    "delete": t("operation_delete"),
}
_OPERATION_PROGRESS_LABELS = {
    "archive": t("operation_archive_progress"),
    "unarchive": t("operation_unarchive_progress"),
    "delete": t("operation_delete_progress"),
}
_OPERATION_DONE_LABELS = {
    "archive": t("operation_archive_done"),
    "unarchive": t("operation_unarchive_done"),
    "delete": t("operation_delete_done"),
}
#: Only meaningful for backends that can archive; hidden entirely otherwise.
_ARCHIVE_ACTIONS = frozenset({"archive", "unarchive", "delete_archived"})
#: Everything that writes; hidden when the agent's command line is missing.
_CHANGING_ACTIONS = frozenset(
    {
        "archive",
        "unarchive",
        "delete",
        "delete_archived",
        "delete_empty",
        "delete_orphans",
        "toggle_danger",
    }
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
        return t("today")
    if day == today - timedelta(days=1):
        return t("yesterday")
    # Include the year when needed so an old session cannot look recent.
    if day.year != today.year:
        return when.strftime("%y-%m-%d")
    return when.strftime("%m-%d")


def _project(session: Session) -> str:
    """The last path segment of cwd — enough to tell projects apart."""
    return Path(session.cwd).name if session.cwd else ""


def _arrange(sessions: list[Session], stranded: set[str]) -> list[tuple[Session, str]]:
    """Arrange sessions as a forest, with each parent followed by its children.

    Returns (session, prefix) in display order. The input order is preserved
    among siblings, so whatever the backend sorted by still holds within each
    branch — a parent's children stay newest-first under it.

    Orphans remain at the top level because they have no parent to nest under;
    their broken connector still makes that missing relationship visible.
    """
    by_id = {session.session_id: session for session in sessions}
    children: dict[str, list[Session]] = {}
    roots: list[Session] = []
    for session in sessions:
        parent = by_id.get(session.parent_id) if session.parent_id else None
        if parent is None or parent is session:
            roots.append(session)
        else:
            children.setdefault(session.parent_id, []).append(session)

    laid_out: list[tuple[Session, str]] = []
    seen: set[int] = set()

    def walk(session: Session, base: str, connector: str) -> None:
        if id(session) in seen:  # a parent cycle would otherwise never return
            return
        seen.add(id(session))
        laid_out.append((session, base + connector))
        kin = children.get(session.session_id, [])
        if not kin:
            return
        # A root's children start at column 0. At deeper levels, the parent's
        # connector determines whether the vertical trunk continues.
        if not connector:
            below = base
        else:
            below = base + (TREE_TRUNK if connector == TREE_BRANCH else TREE_GAP)
        for index, child in enumerate(kin):
            walk(child, below, TREE_LAST if index == len(kin) - 1 else TREE_BRANCH)

    for root in roots:
        if root.session_id in stranded:
            mark = TREE_SEVERED
        elif root.side_thread:
            mark = TREE_UNKNOWN  # a sub-agent that never recorded its parent
        else:
            mark = ""
        walk(root, "", mark)

    # Parent cycles have no root and therefore remain unvisited. Append them so
    # every session stays visible and manageable.
    laid_out += [(s, "") for s in sessions if id(s) not in seen]
    return laid_out


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
        prefix: str,
        stranded: bool,
        odd: bool,
        default_client: str,
    ) -> None:
        self._day = day
        self._day_width = day_width
        self._project_width = project_width
        self._prefix = prefix
        self._stranded = stranded
        self._default_client = default_client
        self._label = Static(self._summary(session))
        classes = " ".join(
            filter(
                None,
                [
                    "-odd" if odd else "",
                    "-archived" if session.archived else "",
                    "-orphan" if stranded else "",
                ],
            )
        )
        super().__init__(self._label, classes=classes)
        self.session = session

    def highlight(self, query: str) -> None:
        self._label.update(self._summary(self.session, query))

    def _summary(self, session: Session, query: str = "") -> Text:
        # Archived and orphaned rows carry no per-span colour so that a single
        # CSS rule can recolour the whole line; the rest shade their columns
        # individually.
        muted = session.archived or self._stranded
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
        if self._prefix:
            text.append(self._prefix, style="" if muted else "dim")
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


class FooterRow(Footer):
    """One footer row containing only the supplied actions.

    Textual's single-row footer clips trailing bindings in narrow terminals.
    Two rows keep the full set visible: session actions above, navigation and
    global actions below.
    """

    def __init__(self, *actions: str, **kwargs) -> None:
        super().__init__(compact=True, show_command_palette=False, **kwargs)
        self._actions = actions

    def compose(self) -> ComposeResult:
        if not self._bindings_ready:
            return
        # Listed in the order this row names them, not the order the app
        # declares its bindings in.
        rank = {action: index for index, action in enumerate(self._actions)}
        shown = [
            (rank[binding.action], binding, enabled, tooltip)
            for (_, binding, enabled, tooltip) in self.screen.active_bindings.values()
            if binding.show and binding.action in rank
        ]
        for _, binding, enabled, tooltip in sorted(shown, key=lambda item: item[0]):
            yield FooterKey(
                binding.key,
                self.app.get_key_display(binding),
                binding.description,
                binding.action,
                disabled=not enabled,
                tooltip=tooltip,
            ).data_bind(compact=Footer.compact)


#: Help entries in display order: (key, description, gated action). An action
#: of ``None`` is always available. Reusing ``check_action`` keeps help and the
#: footer in sync for each backend and installation.
_HELP: tuple[tuple[str, tuple[tuple[str, str, str | None], ...]], ...] = (
    (
        t("help_browse"),
        (
            ("↑ ↓ / j k", t("help_select"), None),
            ("g / G", t("help_top_bottom"), None),
            ("Tab", t("help_focus"), None),
        ),
    ),
    (
        t("help_search"),
        (
            ("/", t("help_search_fields"), None),
            ("?", t("help_search_reverse"), None),
            ("n / N", t("help_search_match"), None),
        ),
    ),
    (
        t("help_manage"),
        (
            ("c", t("help_copy"), "copy_resume"),
            ("d", t("help_delete"), "delete"),
            ("a", t("help_archive"), "archive"),
            ("u", t("help_unarchive"), "unarchive"),
            ("D", t("help_delete_archived"), "delete_archived"),
            ("O", t("help_delete_orphans"), "delete_orphans"),
            ("E", t("help_delete_empty"), "delete_empty"),
            ("!", t("help_danger"), "toggle_danger"),
        ),
    ),
    (
        t("help_other"),
        (
            ("Esc", t("help_escape"), None),
            ("r", t("help_reload"), None),
            ("h", t("help_open"), None),
            ("q", t("help_quit"), None),
        ),
    ),
)


class HelpScreen(ModalScreen[None]):
    """What every key does, minus the ones this agent doesn't have."""

    BINDINGS: ClassVar[list[Binding]] = [
        Binding("escape", "close", show=False),
        Binding("q", "close", show=False),
        Binding("h", "close", show=False),
        Binding("enter", "close", show=False),
    ]

    def __init__(self, sections: list[tuple[str, list[tuple[str, str]]]], title: str) -> None:
        super().__init__()
        self._sections = sections
        self._heading = title
        self._key_width = max(
            (cell_len(key) for _, entries in sections for key, _ in entries), default=0
        )
        line_widths = [cell_len(title), cell_len(t("help_close"))]
        line_widths.extend(cell_len(name) for name, _ in sections)
        line_widths.extend(
            2 + self._key_width + 3 + cell_len(what)
            for _, entries in sections
            for _, what in entries
        )
        # Four cells of horizontal padding and two border cells surround the
        # content. CSS max-width still keeps this inside a narrow terminal.
        self._box_width = max(HELP_WIDTH_MIN, max(line_widths, default=0) + 6)

    def compose(self) -> ComposeResult:
        box = Vertical(id="help-box")
        box.styles.width = self._box_width
        with box:
            yield Static(self._heading, id="help-title")
            with VerticalScroll(id="help-body"):
                for name, entries in self._sections:
                    yield Static(name, classes="help-section")
                    text = Text()
                    for index, (key, what) in enumerate(entries):
                        if index:
                            text.append("\n")
                        text.append("  " + set_cell_size(key, self._key_width), style="bold")
                        text.append("   " + what)
                    yield Static(text, classes="help-keys")
            yield Static(t("help_close"), id="help-footer")

    def action_close(self) -> None:
        self.dismiss(None)


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
                yield Button(f"{t('cancel')} (n)", id="confirm-no")

    def action_confirm(self) -> None:
        self.dismiss(True)

    def action_cancel(self) -> None:
        self.dismiss(False)

    def on_button_pressed(self, event: Button.Pressed) -> None:
        self.dismiss(event.button.id == "confirm-yes")


def confirm_one(session: Session, extra: int = 0) -> ConfirmScreen:
    subject = Text(no_wrap=True, overflow="ellipsis")
    subject.append(session.title + "\n", style="bold")
    subject.append(session.session_id, style="dim")
    # State the cascade explicitly: the action affects more than the row under
    # the cursor, even though removing the children prevents new orphans.
    body = t("delete_irreversible")
    if extra:
        body = n("delete_descendant_one", "delete_descendant_many", extra, warning=body)
    return ConfirmScreen(
        title=t("confirm_delete_title"),
        subject=subject,
        body=body,
        confirm_label=t("delete"),
    )


def confirm_bulk(targets: list[Session], what: str, note: str = "") -> ConfirmScreen:
    subject = Text(no_wrap=True, overflow="ellipsis")
    subject.append(
        t("bulk_count", count=len(targets), what=count_label(what, len(targets))),
        style="bold",
    )
    for session in targets[:BULK_PREVIEW_LIMIT]:
        subject.append(f"  · {session.title}\n", style="dim")
    remaining = len(targets) - BULK_PREVIEW_LIMIT
    if remaining > 0:
        subject.append(t("bulk_remaining", count=remaining), style="dim")

    warning = t("delete_irreversible")
    body = f"{note}\n{warning}" if note else warning
    return ConfirmScreen(
        title=t("confirm_bulk_title", what=what),
        subject=subject,
        body=body,
        confirm_label=t("confirm_bulk_button", count=len(targets)),
    )


def confirm_danger() -> ConfirmScreen:
    subject = Text()
    subject.append(t("danger_line_one"), style="bold")
    subject.append(t("danger_line_two"), style="dim")
    return ConfirmScreen(
        title=t("confirm_danger_title"),
        subject=subject,
        body="",
        confirm_label=t("enable"),
    )


class SessionCleanerApp(App[None]):
    CSS_PATH = "app.tcss"
    #: There is nothing to search for in a palette here, and its footer entry
    #: costs a dozen columns that the actual keys need.
    ENABLE_COMMAND_PALETTE = False

    BINDINGS: ClassVar[list[Binding]] = [
        Binding("a", "archive", t("binding_archive")),
        Binding("u", "unarchive", t("binding_unarchive")),
        Binding("d", "delete", t("binding_delete")),
        Binding("c", "copy_resume", t("binding_copy")),
        Binding("D", "delete_archived", t("binding_delete_archived")),
        Binding("E", "delete_empty", t("binding_delete_empty")),
        Binding("O", "delete_orphans", t("binding_delete_orphans")),
        Binding("exclamation_mark", "toggle_danger", t("binding_danger")),
        Binding("slash", "search_forward", t("binding_search")),
        Binding("r", "reload", t("binding_reload")),
        Binding("h", "help", t("binding_help")),
        Binding("q", "quit", t("binding_quit")),
        Binding("question_mark", "search_backward", show=False),
        Binding("n", "search_next", show=False),
        Binding("N", "search_previous", show=False),
        Binding("escape", "clear_search", show=False),
        Binding("tab", "focus_next", t("binding_focus"), show=False),
    ]

    #: The top footer row: what you do to the session under the cursor.
    _FOOTER_TOP = (
        "archive",
        "unarchive",
        "delete",
        "copy_resume",
        "delete_archived",
        "delete_empty",
        "delete_orphans",
        "toggle_danger",
    )
    #: The bottom row: getting around, and the way out.
    _FOOTER_BOTTOM = ("search_forward", "reload", "help", "quit")

    def __init__(self, backend: Backend) -> None:
        super().__init__()
        self.backend = backend
        self.title = t("app_title", agent=backend.label)
        #: Without the agent's command line we can still browse, just not change.
        self._missing_cli = backend.missing_cli()
        #: Everything the agent has, in listing order. Nothing is ever filtered
        #: out: a session you cannot see is one you cannot decide about, and
        #: archived rows already say what they are by being greyed out.
        self._sessions: list[Session] = []
        #: Ids of the sessions whose parent conversation is gone, recomputed on
        #: every reload so a deletion can strand another row and say so.
        self._orphan_ids: set[str] = set()
        #: Tree connector per session id, in step with `_sessions`.
        self._prefixes: dict[str, str] = {}
        #: Keyed by (path, mtime, size) so an edited transcript re-reads itself.
        self._detail_cache: dict[tuple[str, float, int], list[Message]] = {}
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
        if action == "delete_empty" and self.backend.empty_label is None:
            return False
        if action == "delete_orphans" and self.backend.orphan_label is None:
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
        with Vertical(id="footer-rows"):
            yield FooterRow(*self._FOOTER_TOP)
            yield FooterRow(*self._FOOTER_BOTTOM)

    async def on_mount(self) -> None:
        self.query_one("#search-bar").display = False
        self.query_one("#sessions", SessionList).focus()
        if self._missing_cli:
            self._set_status(
                t("missing_cli_browse", agent=self.backend.label),
                tone="error",
            )
        else:
            self._set_status(_tilde(self.backend.home))
        await self._reload()

    # ------------------------------------------------------------------ state

    def _selected(self) -> Session | None:
        item = self.query_one("#sessions", SessionList).highlighted_child
        return item.session if isinstance(item, SessionRow) else None

    async def _reload(self, prefer_id: str | None = None, prefer_index: int = 0) -> None:
        found = await asyncio.to_thread(self.backend.discover)
        self._orphan_ids = {s.session_id for s in orphans(found)}
        laid_out = _arrange(found, self._orphan_ids)
        # The list order *is* the tree order, so search, `n`, `g`/`G` and the
        # cursor all keep working on plain indices.
        self._sessions = [session for session, _ in laid_out]
        self._prefixes = {session.session_id: prefix for session, prefix in laid_out}
        await self._repopulate(prefer_id, prefer_index)

    async def _repopulate(self, prefer_id: str | None = None, prefer_index: int = 0) -> None:
        listing = self.query_one("#sessions", SessionList)
        await listing.clear()
        if self._sessions:
            await listing.extend(self._build_rows())
        self._update_banner()

        if not self._sessions:
            await self._show_placeholder(t("no_sessions_here"))
            return

        index = prefer_index
        if prefer_id is not None:
            index = next(
                (i for i, s in enumerate(self._sessions) if s.session_id == prefer_id),
                prefer_index,
            )
        index = max(0, min(index, len(self._sessions) - 1))
        listing.index = index
        self._apply_highlight()
        # Setting `index` only emits Highlighted when the value changes, so kick
        # the detail load off explicitly to cover the unchanged-index case.
        self._load_detail(self._sessions[index])

    def _build_rows(self) -> list[SessionRow]:
        today = date.today()
        widest = max((cell_len(_project(s)) for s in self._sessions), default=0)
        project_width = min(max(widest, PROJECT_WIDTH_MIN), PROJECT_WIDTH_MAX)

        # The date is printed once per day rather than once per row, so the
        # labels have to be worked out before the column can be sized.
        labels: list[str] = []
        previous_day: date | None = None
        for session in self._sessions:
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
                prefix=self._prefixes.get(session.session_id, ""),
                stranded=session.session_id in self._orphan_ids,
                odd=bool(index % 2),
                default_client=self.backend.default_client,
            )
            for index, (session, label) in enumerate(zip(self._sessions, labels, strict=True))
        ]

    def _update_banner(self) -> None:
        archived = sum(1 for session in self._sessions if session.archived)
        text = Text(no_wrap=True, overflow="ellipsis")
        text.append(self.backend.label, style="bold")
        text.append(
            n("banner_sessions_one", "banner_sessions_many", len(self._sessions)),
            style="dim",
        )
        # Only show counts that are non-zero. The banner is the one line that
        # remains on screen, so a zero count would only add noise.
        if self.backend.supports_archive and archived:
            text.append(n("banner_archived_one", "banner_archived_many", archived), style="dim")
        if self.backend.orphan_label and self._orphan_ids:
            text.append(
                n(
                    "banner_orphans_one",
                    "banner_orphans_many",
                    len(self._orphan_ids),
                    what=count_label(self.backend.orphan_label, len(self._orphan_ids)),
                ),
                style="dim",
            )
        if self._danger:
            text.append(t("banner_danger"), style="bold")
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
            widgets.append(Static(t("no_conversation"), classes="placeholder"))

        await pane.mount_all(widgets)
        pane.scroll_home(animate=False)

    def _detail_header(self, session: Session, messages: list[Message]) -> Text:
        # Keep every field on its own line. The pane may be narrow, so long
        # titles, ids, and paths are clipped rather than wrapped into the next
        # field's row.
        text = Text(no_wrap=True, overflow="ellipsis")
        if session.archived:
            text.append(t("archived_marker"), style="yellow")
        text.append(session.title + "\n", style="bold")

        text.append(session.session_id, style="dim")

        meta = []
        if session.created_at:
            meta.append(session.created_at.strftime("%Y-%m-%d %H:%M"))
        meta.extend(
            (
                session.client,
                n("messages_one", "messages_many", len(messages)),
            )
        )
        if session.version:
            meta.append(f"v{session.version}")
        text.append("\n" + " · ".join(meta), style="dim")

        cwd = _tilde(session.cwd) if session.cwd else t("cwd_unknown")
        text.append("\n" + cwd, style="dim")
        # Where this side-thread came from, and whether that conversation is
        # still around — the one thing that decides if it is worth keeping.
        if session.parent_id:
            if session.session_id in self._orphan_ids:
                text.append("\n" + t("parent_deleted"), style="yellow")
            else:
                text.append("\n" + t("spawned_by", session_id=session.parent_id), style="dim")
        elif session.side_thread:
            # Otherwise this row just looks like a top-level conversation that
            # forgot to be one; say why it stands alone, and why it is safe.
            text.append(
                "\n" + t("source_unrecorded"),
                style="dim",
            )
        return text

    def _message_text(self, message: Message) -> Text:
        is_user = message.role == "user"
        text = Text()
        text.append(
            f"▶ {t('you')}\n" if is_user else f"◀ {self.backend.agent_label}\n",
            style="bold green" if is_user else "bold blue",
        )
        text.append(message.text)
        if message.truncated_chars:
            text.append(
                n(
                    "message_truncated_one",
                    "message_truncated_many",
                    message.truncated_chars,
                ),
                style="dim italic",
            )
        return text

    # ----------------------------------------------------------------- search

    def action_search_forward(self) -> None:
        self._begin_search(1)

    def action_search_backward(self) -> None:
        self._begin_search(-1)

    def _begin_search(self, direction: int) -> None:
        if not self._sessions:
            self._set_status(t("search_empty_list"), tone="error")
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
        if restore and self._sessions:
            listing.index = min(self._search_origin, len(self._sessions) - 1)
        listing.focus()

    def action_search_next(self) -> None:
        self._repeat_search(self._search_direction)

    def action_search_previous(self) -> None:
        self._repeat_search(-self._search_direction)

    def _repeat_search(self, direction: int) -> None:
        if not self._query:
            self._set_status(t("search_not_started"), tone="error")
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
        return [index for index, session in enumerate(self._sessions) if needle in hay(session)]

    def _jump(self, start: int, direction: int, *, inclusive: bool) -> None:
        matches = self._matches()
        if not matches:
            self._set_status(t("search_not_found", query=self._query), tone="error")
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
        note = (
            t("search_wrapped_forward" if direction > 0 else "search_wrapped_backward")
            if wrapped
            else ""
        )
        self._set_status(
            t(
                "search_status",
                sigil=sigil,
                query=self._query,
                current=matches.index(target) + 1,
                total=len(matches),
                note=note,
            )
        )

    def _apply_highlight(self) -> None:
        for row in self.query(SessionRow):
            row.highlight(self._query)

    async def _show_placeholder(self, message: str) -> None:
        pane = self.query_one("#detail", DetailPane)
        await pane.remove_children()
        await pane.mount(Static(message, classes="placeholder"))

    # ---------------------------------------------------------------- actions

    async def action_quit(self) -> None:
        """Quitting mid-delete would kill the worker between two sessions and
        leave the sweep half done, so it waits."""
        if self._busy:
            self._set_status(t("busy_cannot_quit"), tone="error")
            return
        self.exit()

    def help_sections(self) -> list[tuple[str, list[tuple[str, str]]]]:
        """The help text this agent should show, with the rest left out.

        Gating on `check_action` rather than a second list means the help can
        never drift from the keys that actually work: whatever the footer hides,
        this hides too — Claude's keys under Codex, and everything that writes
        when the agent's command line isn't installed.
        """
        sections = []
        for name, entries in _HELP:
            usable = [
                (key, what)
                for key, what, action in entries
                if action is None or self.check_action(action, ()) is not False
            ]
            if usable:
                sections.append((name, usable))
        return sections

    def action_help(self) -> None:
        self.push_screen(HelpScreen(self.help_sections(), t("help_title")))

    def action_reload(self) -> None:
        if self._reject_while_busy():
            return
        self._reload_worker()

    @work(group="view", exclusive=True)
    async def _reload_worker(self) -> None:
        current = self._selected()
        await self._reload(current.session_id if current else None, self._current_index())
        self._set_status(t("reloaded"))

    def action_archive(self) -> None:
        session = self._require_selection()
        if session is None:
            return
        if session.archived:
            self._set_status(t("already_archived"), tone="error")
            return
        self._run_operation("archive", session)

    def action_unarchive(self) -> None:
        session = self._require_selection()
        if session is None:
            return
        if not session.archived:
            self._set_status(t("not_archived"), tone="error")
            return
        self._run_operation("unarchive", session)

    def action_copy_resume(self) -> None:
        """Put a `cd … && … resume …` line on the clipboard."""
        session = self._selected()
        if session is None:
            self._set_status(t("no_selection"), tone="error")
            return

        command = self.backend.resume_command(session)
        if session.cwd:
            command = f"cd {shlex.quote(session.cwd)} && {command}"

        if not clipboard.copy(command):
            # No native helper; OSC 52 works in terminals that support it.
            self.copy_to_clipboard(command)

        if self._missing_cli:
            note = t("copy_missing_cli_note", agent=self.backend.label)
        elif session.archived:
            note = t("copy_archived_note")
        else:
            note = ""
        self._set_status(t("copy_success", note=note, command=command), tone="ok")

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
        extra = len(self._descendants(session))
        if await self.push_screen_wait(confirm_one(session, extra)):
            self._run_operation("delete", session)

    def action_toggle_danger(self) -> None:
        if self._danger:
            self._danger = False
            self._update_banner()
            self._set_status(t("danger_off"))
            return
        if self._reject_while_busy():
            return
        self._confirm_danger()

    @work(group="confirm", exclusive=True)
    async def _confirm_danger(self) -> None:
        if await self.push_screen_wait(confirm_danger()):
            self._danger = True
            self._update_banner()
            self._set_status(t("danger_on"), tone="error")

    def action_delete_archived(self) -> None:
        if self._reject_while_busy():
            return
        archived = [session for session in self._sessions if session.archived]
        if not archived:
            self._set_status(t("no_archived"), tone="error")
            return
        targets = self._with_sub_agents(archived)
        tagging = len(targets) - len(archived)
        note = (
            n("cascade_orphans_one", "cascade_orphans_many", tagging) if tagging else ""
        )
        self._confirm_bulk(targets, t("archived_sessions"), note)

    def action_delete_empty(self) -> None:
        if self._reject_while_busy():
            return
        what = self.backend.empty_label
        empty = [session for session in self._sessions if session.noise]
        if not empty:
            self._set_status(t("none_of", what=what), tone="error")
            return
        self._confirm_bulk(self._with_sub_agents(empty), what)

    def action_delete_orphans(self) -> None:
        if self._reject_while_busy():
            return
        what = self.backend.orphan_label
        stranded = orphans(self._sessions)
        if not stranded:
            self._set_status(t("no_orphans", what=what), tone="error")
            return
        self._confirm_bulk(
            self._with_sub_agents(stranded), what, note=t("orphan_note")
        )

    @work(group="confirm", exclusive=True)
    async def _confirm_bulk(self, targets: list[Session], what: str, note: str = "") -> None:
        if await self.push_screen_wait(confirm_bulk(targets, what, note)):
            self._busy = True
            self._bulk_delete_worker(targets, what)

    @work(group="op")
    async def _bulk_delete_worker(self, targets: list[Session], what: str) -> None:
        index = self._current_index()
        failures: list[str] = []
        try:
            for done, session in enumerate(targets, start=1):
                self._set_status(
                    t(
                        "deleting_progress",
                        done=done,
                        total=len(targets),
                        title=session.title,
                    )
                )
                result = await self.backend.delete(session)
                if not result.ok:
                    failures.append(result.message)
        finally:
            self._busy = False

        await self._reload(None, index)
        if failures:
            self._set_status(
                t(
                    "bulk_failed",
                    done=len(targets) - len(failures),
                    total=len(targets),
                    failed=len(failures),
                    message=failures[0],
                ),
                tone="error",
            )
        else:
            self._set_status(
                t("bulk_deleted", count=len(targets), what=count_label(what, len(targets))),
                tone="ok",
            )

    # ------------------------------------------------------------- operations

    def _require_selection(self) -> Session | None:
        if self._missing_cli:
            self._set_status(t("missing_cli_modify", agent=self.backend.label), tone="error")
            return None
        if self._reject_while_busy():
            return None
        session = self._selected()
        if session is None:
            self._set_status(t("no_selection"), tone="error")
        return session

    def _reject_while_busy(self) -> bool:
        if self._busy:
            self._set_status(t("previous_busy"), tone="error")
        return self._busy

    def _current_index(self) -> int:
        return self.query_one("#sessions", SessionList).index or 0

    def _children_map(self) -> dict[str, list[Session]]:
        return {
            parent: [s for s in self._sessions if s.parent_id == parent]
            for parent in {
                s.parent_id for s in self._sessions if s.parent_id and s.parent_id != s.session_id
            }
        }

    def _descendants(
        self, session: Session, kin: dict[str, list[Session]] | None = None
    ) -> list[Session]:
        """This session's sub-agents, deepest first.

        Deepest first so that a cascade never has to step over a session whose
        parent it has already removed.
        """
        kin = self._children_map() if kin is None else kin
        found: list[Session] = []
        seen = {session.session_id}

        def walk(node: Session) -> None:
            for child in kin.get(node.session_id, []):
                if child.session_id in seen:
                    continue
                seen.add(child.session_id)
                walk(child)
                found.append(child)

        walk(session)
        return found

    def _cascade(self, kind: str, session: Session) -> list[Session]:
        """What one keypress really acts on, sub-agents first.

        The agent's own command line only ever touches the one session it is
        given, which is how a deleted conversation leaves its sub-agents behind
        as orphans in the first place. A side-thread exists only because of the
        conversation that spawned it, so it goes wherever that conversation
        goes — and it goes *first*, so that a failure part-way through leaves
        the parent standing rather than a fresh orphan.
        """
        if kind == "archive":
            extra = [s for s in self._descendants(session) if not s.archived]
        elif kind == "unarchive":
            extra = [s for s in self._descendants(session) if s.archived]
        else:
            extra = self._descendants(session)
        return [*extra, session]

    def _with_sub_agents(self, targets: list[Session]) -> list[Session]:
        """The same cascade, for the keys that delete a whole set at once.

        Sweeping a set of parents without their sub-agents would manufacture
        exactly the orphans the next key along has to clean up.
        """
        kin = self._children_map()
        ordered: list[Session] = []
        seen: set[str] = set()
        for session in targets:
            for target in [*self._descendants(session, kin), session]:
                if target.session_id not in seen:
                    seen.add(target.session_id)
                    ordered.append(target)
        return ordered

    def _run_operation(self, kind: str, session: Session) -> None:
        # Claim the busy flag synchronously: a worker doesn't start until the
        # next event-loop tick, which would let a double keypress fire twice.
        self._busy = True
        self._operation_worker(kind, self._cascade(kind, session))

    @work(group="op")
    async def _operation_worker(self, kind: str, targets: list[Session]) -> None:
        progress_label = _OPERATION_PROGRESS_LABELS[kind]
        done_label = _OPERATION_DONE_LABELS[kind]
        infinitive = _OPERATION_INFINITIVES[kind]
        session = targets[-1]  # the row under the cursor; the rest ride along
        extra = len(targets) - 1
        index = self._current_index()
        stopped_short = False
        message = ""
        try:
            for done, target in enumerate(targets, start=1):
                if extra:
                    self._set_status(
                        t(
                            "operation_progress",
                            operation=progress_label,
                            done=done,
                            total=len(targets),
                            title=target.title,
                        )
                    )
                else:
                    self._set_status(
                        t("operation_progress_one", operation=progress_label, title=target.title)
                    )
                result = await getattr(self.backend, kind)(target)
                if not result.ok:
                    message = result.message
                    stopped_short = target is not session
                    break
        finally:
            self._busy = False

        # Archiving moves the file, so re-select by id where the row survives
        # and fall back to the same slot where it doesn't.
        prefer_id = None if kind == "delete" else session.session_id
        await self._reload(prefer_id, index)
        # Backends report failures in their own words; success is phrased here so
        # the wording stays the same whichever agent is being managed.
        if not message:
            tail = (
                n("operation_tail_one", "operation_tail_many", extra) if extra else ""
            )
            self._set_status(
                t("operation_done", operation=done_label, title=session.title, tail=tail),
                tone="ok",
            )
        elif stopped_short:
            self._set_status(
                t("subagent_operation_failed", operation=infinitive, message=message),
                tone="error",
            )
        else:
            self._set_status(message, tone="error")
