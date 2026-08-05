"""OpenCode backend.

Layout under ``$XDG_DATA_HOME/opencode`` (default ``~/.local/share/opencode``)::

    opencode.db                     sessions, messages and message parts
    storage/session_diff/<id>.json  per-session diffs left by an old migration

Three things differ from the other two agents:

* Sessions are rows in SQLite rather than files. The whole listing comes from
  one read-only connection; nothing here ever writes to the database, because
  OpenCode keeps its own event log alongside these tables and reconstructing
  that by hand is not something a session browser should attempt.
* ``opencode session delete`` already removes a session's children, so deletion
  is delegated to it. Archiving has no such command — the field exists and the
  desktop app writes it — so archived sessions are shown as archived and left
  alone; see ``supports_archive``.
* A session row only appears once something has been said, so there is no such
  thing as an empty OpenCode session to sweep.
"""

from __future__ import annotations

import asyncio
import contextlib
import json
import os
import random
import re
import shlex
import shutil
import sqlite3
from collections.abc import Iterator
from datetime import datetime
from pathlib import Path

from ..i18n import t
from ..model import Message, OpResult, Session, condense, make_message
from . import cli

OPENCODE_BIN = "opencode"
DATABASE_FILE = "opencode.db"
#: The directory name OpenCode appends to XDG_DATA_HOME.
DATA_DIR_NAME = "opencode"
#: Written once by the migration out of the JSON storage era and never cleaned
#: up again. One file per session, named after it.
SESSION_DIFF_SUBDIR = "storage/session_diff"
#: IDs are minted by OpenCode and only ever look like this; anything else is not
#: something to hand to a command line or to build a path from.
_SESSION_ID_RE = re.compile(r"^ses_[0-9A-Za-z]+$")

#: SQLite admits one writer at a time, and OpenCode already waits five seconds
#: for the lock (``PRAGMA busy_timeout = 5000``). Some releases name the lock;
#: 1.18 reports only ``Error: Unexpected error`` for the same failed query. Both
#: are safe to retry because deleting a session is idempotent.
_RETRYABLE = re.compile(
    r"database (?:is|table is) locked|SQLITE_BUSY|^Error:\s*Unexpected error$",
    re.IGNORECASE,
)
#: How long to wait before asking again. Deletion is one transaction, so a
#: refusal leaves nothing half-written and trying again is safe.
LOCK_RETRY_DELAYS = (0.3, 1.0, 2.5)
#: Spread the retries of a batch out rather than have them collide again.
LOCK_RETRY_JITTER = 0.25

#: OpenCode names a session the moment it is created and only replaces that with
#: a summary once the model has produced one. Both spellings mean "not titled
#: yet", so the opening message is a better answer than either. The dated form
#: matches ``isDefaultTitle`` in OpenCode's own source; the bare one is what
#: releases before 1.18 used.
_PLACEHOLDER_TITLE = re.compile(
    r"^(New session - |Child session - )\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$"
)
#: Capitalised either way depending on the release that wrote it.
LEGACY_PLACEHOLDER_TITLE = "new conversation"

#: The agent every session runs under unless it was told otherwise. Sub-agents
#: spawned by the task tool run under their own (``explore``, ``plan``, …).
DEFAULT_AGENT = "build"


def default_home() -> Path:
    """``$XDG_DATA_HOME/opencode``, the directory OpenCode resolves at startup."""
    data_home = os.environ.get("XDG_DATA_HOME")
    root = Path(data_home).expanduser() if data_home else Path.home() / ".local" / "share"
    return root / "opencode"


def _connect(database: Path) -> sqlite3.Connection:
    """Open the database read-only, so a mistake here cannot cost sessions."""
    connection = sqlite3.connect(f"{database.resolve().as_uri()}?mode=ro", uri=True)
    connection.row_factory = sqlite3.Row
    return connection


@contextlib.contextmanager
def _reader(database: Path) -> Iterator[sqlite3.Connection | None]:
    """A read-only connection, or ``None`` when there is nothing to read.

    Missing, unreadable and not-a-database all mean the same thing to every
    caller here, and each of them has an honest answer for it.
    """
    try:
        connection = _connect(database)
    except (OSError, ValueError, sqlite3.Error):
        yield None
        return
    try:
        yield connection
    finally:
        connection.close()


def _text(value: object) -> str:
    return value.strip() if isinstance(value, str) else ""


def _moment(value: object) -> datetime | None:
    """OpenCode records epoch milliseconds; everything on screen is local time."""
    if not isinstance(value, (int, float)) or isinstance(value, bool) or value <= 0:
        return None
    try:
        return datetime.fromtimestamp(value / 1000)
    except (OSError, OverflowError, ValueError):
        return None


def _payload(raw: object) -> dict:
    """Messages and parts keep everything but their keys in a JSON column."""
    if not isinstance(raw, str):
        return {}
    try:
        value = json.loads(raw)
    except ValueError:
        return {}
    return value if isinstance(value, dict) else {}


def _visible_text(part: dict) -> str:
    """The text of one part, or empty for anything that is not conversation.

    ``synthetic`` text is tool plumbing that OpenCode stores as text: the call
    it is about to make, the file it just read, the note that the user ran a
    tool. It reads like the assistant talking and is not.
    """
    if part.get("type") != "text" or part.get("synthetic"):
        return ""
    return part.get("text") if isinstance(part.get("text"), str) else ""


def _is_image(part: dict) -> bool:
    mime = part.get("mime")
    return part.get("type") == "file" and isinstance(mime, str) and mime.startswith("image/")


def _is_placeholder(title: str) -> bool:
    return title.lower() == LEGACY_PLACEHOLDER_TITLE or bool(_PLACEHOLDER_TITLE.match(title))


def _first_user_text(connection: sqlite3.Connection, session_id: str) -> str:
    """The opening message, for sessions the model never got round to naming."""
    for message in connection.execute(
        "SELECT id, data FROM message WHERE session_id = ? ORDER BY time_created, id",
        (session_id,),
    ):
        if _payload(message["data"]).get("role") != "user":
            continue
        for part in connection.execute(
            "SELECT data FROM part WHERE message_id = ? ORDER BY time_created, id",
            (message["id"],),
        ):
            text = _visible_text(_payload(part["data"])).strip()
            if text:
                return text
    return ""


def load_session(
    connection: sqlite3.Connection, row: dict, database: Path, size: int
) -> Session | None:
    """Build a list entry from one ``session`` row.

    The row is a plain dict rather than a cursor row because OpenCode has added
    columns over time and older databases simply do not have them.
    """
    session_id = row["id"] if isinstance(row.get("id"), str) else ""
    if not session_id:
        return None

    recorded = _text(row.get("title"))
    title = "" if _is_placeholder(recorded) else recorded
    opening = _first_user_text(connection, session_id) if not title else ""
    parent = row.get("parent_id")
    created = _moment(row.get("time_created"))
    updated = _moment(row.get("time_updated"))

    return Session(
        backend="opencode",
        # Every session lives in the same file. It is still the honest answer to
        # "where is this?", and the detail cache keys on the session id as well.
        path=database,
        session_id=session_id,
        title=condense(title or opening) if (title or opening) else t("empty_session"),
        client=_text(row.get("agent")) or DEFAULT_AGENT,
        updated_at=updated or created or datetime.fromtimestamp(0),
        size=size,
        archived=row.get("time_archived") is not None,
        # A title is either the model's summary or a rename; the row does not say
        # which, so neither is treated as one the user assigned.
        named=False,
        noise=not title and not opening,
        side_thread=isinstance(parent, str) and bool(parent),
        parent_id=parent if isinstance(parent, str) and parent else None,
        cwd=_text(row.get("directory")) or None,
        version=_text(row.get("version")) or None,
        created_at=created,
    )


class OpenCodeBackend:
    id = "opencode"
    label = "OpenCode"
    agent_label = "OpenCode"
    shortcut = "o"
    #: OpenCode records an archive timestamp, but only its desktop app writes
    #: one: there is no command to delegate to, and editing the database behind
    #: a running OpenCode instance is not a safe alternative. Archived sessions
    #: are listed as archived and otherwise left alone.
    supports_archive = False
    default_client = DEFAULT_AGENT
    #: A session row is written when the first message is sent, so an opened and
    #: abandoned session leaves nothing behind to clean up.
    empty_label = None
    #: A sub-agent session records the conversation that spawned it. Deleting
    #: that conversation takes its sub-agents with it, so orphans should not
    #: arise — but data migrated from the JSON era predates that guarantee.
    orphan_label = t("orphan_sessions")
    #: Every deletion is a write and SQLite serialises those, but most of what a
    #: run costs is starting OpenCode, which does overlap: four at a time
    #: measured 130ms per session against 470ms one at a time, and no slower
    #: than eight. Sixteen starts hitting lock timeouts. ``_delete_once`` waits
    #: out both explicit lock errors and the generic database error emitted by
    #: current OpenCode releases.
    bulk_concurrency = 4

    def __init__(self, home: Path | None = None) -> None:
        self.home = home or default_home()

    @property
    def database(self) -> Path:
        return self.home / DATABASE_FILE

    def missing_cli(self) -> str | None:
        """Listing reads the database directly; deleting needs the command."""
        return None if shutil.which(OPENCODE_BIN) else OPENCODE_BIN

    def home_problem(self) -> str | None:
        """Refuse a directory the command line could never be pointed at.

        XDG_DATA_HOME names the *parent*: OpenCode appends ``opencode`` to it.
        A directory by any other name can be read, but every command built from
        it — deleting, and the resume line handed to the clipboard — would name
        a sibling directory instead, which is a different session tree.
        """
        if self.home.name != DATA_DIR_NAME:
            return t("opencode_home_not_data_dir", agent=self.label, path=self.home)
        return None

    def resume_command(self, session: Session) -> str:
        parts = []
        if self.home != default_home():
            # OpenCode appends "opencode" to XDG_DATA_HOME, so the variable
            # names the parent of the directory we were pointed at. That only
            # resolves back to this tree because ``home_problem`` refused any
            # directory by another name before anything got this far.
            parts.append(f"XDG_DATA_HOME={shlex.quote(str(self.home.parent))}")
        parts += [OPENCODE_BIN, "-s", session.session_id]
        return " ".join(parts)

    # ------------------------------------------------------------- discovery

    def discover(self) -> list[Session]:
        found: list[Session] = []
        with _reader(self.database) as connection:
            if connection is None:
                return found
            sizes: dict[str, int] = {}
            # Only feeds the detail cache key. A database that cannot answer
            # this — an older schema, a table opencode has since renamed — is no
            # reason to list nothing at all.
            with contextlib.suppress(sqlite3.Error):
                sizes = {
                    row[0]: row[1] or 0
                    for row in connection.execute(
                        "SELECT session_id, sum(length(data)) FROM part GROUP BY session_id"
                    )
                }
            try:
                for row in connection.execute("SELECT * FROM session").fetchall():
                    fields = dict(row)
                    session = load_session(
                        connection, fields, self.database, sizes.get(fields.get("id"), 0)
                    )
                    if session is not None:
                        found.append(session)
            except sqlite3.Error:
                return []
        found.sort(key=lambda session: session.recency_at, reverse=True)
        return found

    def load_messages(self, session: Session) -> list[Message]:
        """Rebuild the exchange from its parts, without the tool traffic.

        Parts carry the whole run: tool calls and their output, reasoning,
        step boundaries, patches. Only what a person wrote or read is kept.
        """
        messages: list[Message] = []
        with _reader(self.database) as connection:
            if connection is None:
                return messages
            try:
                grouped: dict[str, list[dict]] = {}
                for row in connection.execute(
                    "SELECT message_id, data FROM part WHERE session_id = ? "
                    "ORDER BY time_created, id",
                    (session.session_id,),
                ):
                    grouped.setdefault(row["message_id"], []).append(_payload(row["data"]))

                for row in connection.execute(
                    "SELECT id, data FROM message WHERE session_id = ? ORDER BY time_created, id",
                    (session.session_id,),
                ):
                    role = _payload(row["data"]).get("role")
                    if role not in ("user", "assistant"):
                        continue
                    parts = grouped.get(row["id"], [])
                    text = "\n".join(filter(None, (_visible_text(part) for part in parts)))
                    images = sum(1 for part in parts if _is_image(part))
                    message = make_message("user" if role == "user" else "agent", text, images)
                    if message is not None:
                        messages.append(message)
            except sqlite3.Error:
                return messages
        return messages

    # ------------------------------------------------------------ operations

    def _misdirected(self) -> OpResult | None:
        """The same refusal as ``home_problem``, for anything built by hand.

        Startup rejects such a home outright, so this is the backstop for a
        backend constructed directly rather than through the command line.
        """
        problem = self.home_problem()
        return OpResult(False, problem) if problem else None

    def _env(self) -> dict[str, str]:
        return {"XDG_DATA_HOME": str(self.home.parent)}

    def _drop_leftovers(self, session_id: str) -> None:
        """Remove the diff file the JSON-era migration left for this session.

        ``opencode session delete`` clears the database and stops there. The
        file is named after a session that no longer exists, so nothing will
        ever read it again.
        """
        if not _SESSION_ID_RE.match(session_id):
            return
        # The session itself is already gone; a stale diff is not worth an error.
        with contextlib.suppress(OSError):
            (self.home / SESSION_DIFF_SUBDIR / f"{session_id}.json").unlink(missing_ok=True)

    async def archive(self, session: Session) -> OpResult:
        return OpResult(False, t("agent_no_archive", agent=self.label))

    async def unarchive(self, session: Session) -> OpResult:
        return OpResult(False, t("agent_no_archive", agent=self.label))

    def _present(self, session_id: str) -> bool:
        """Whether the session is still in the database.

        Used to settle what actually happened when the command reports failure.
        Anything unreadable answers "still there": success is not something to
        assume on the strength of a question we could not ask.
        """
        with _reader(self.database) as connection:
            if connection is None:
                return True
            try:
                found = connection.execute(
                    "SELECT 1 FROM session WHERE id = ?", (session_id,)
                ).fetchone()
            except sqlite3.Error:
                return True
            return found is not None

    async def _delete_once(self, session: Session) -> OpResult:
        """Ask OpenCode to remove the session, waiting out a busy database.

        A batch of deletions, or an OpenCode instance open in another window, can hold
        the write lock for longer than OpenCode's own five-second wait. That is
        a queue, not a refusal, so wait a moment and ask again.
        """
        result = OpResult(False, "")
        for attempt, delay in enumerate((0.0, *LOCK_RETRY_DELAYS)):
            if attempt:
                await asyncio.sleep(delay + random.uniform(0, LOCK_RETRY_JITTER))
            result = await cli.run(
                OPENCODE_BIN,
                ("session", "delete", session.session_id),
                env=self._env(),
                label=self.label,
            )
            if result.ok:
                return result
            # What the caller asked for is that the session stop existing, so
            # settle it against the database rather than the exit status: the
            # command can fail after the row is already gone, and deleting one
            # that somebody else removed first is not something to complain
            # about either.
            if not await asyncio.to_thread(self._present, session.session_id):
                return OpResult(True, "")
            if not _RETRYABLE.search(result.message):
                return result
        return result

    async def delete(self, session: Session) -> OpResult:
        """Hand the session to OpenCode, which also removes its sub-agents."""
        misdirected = self._misdirected()
        if misdirected is not None:
            return misdirected
        if not _SESSION_ID_RE.match(session.session_id):
            return OpResult(False, t("opencode_bad_session_id", session_id=session.session_id))
        result = await self._delete_once(session)
        if result.ok:
            await asyncio.to_thread(self._drop_leftovers, session.session_id)
        return result
