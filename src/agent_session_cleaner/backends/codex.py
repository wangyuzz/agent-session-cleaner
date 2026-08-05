"""Codex CLI backend.

Layout under ``$CODEX_HOME`` (default ``~/.codex``)::

    sessions/YYYY/MM/DD/rollout-<local-ts>-<uuid>.jsonl   active
    archived_sessions/rollout-<local-ts>-<uuid>.jsonl     archived (flat)
    session_index.jsonl                                   append-only rename log

Archiving moves a file between those two trees, so archived state is determined
entirely by location. Every mutation is delegated to the non-interactive Codex
CLI, which reports success or failure through its exit status.
"""

from __future__ import annotations

import json
import os
import re
import shlex
import shutil
from datetime import datetime
from pathlib import Path

from ..i18n import t
from ..model import Message, OpResult, Session, condense, make_message
from . import cli

SESSIONS_SUBDIR = "sessions"
ARCHIVED_SESSIONS_SUBDIR = "archived_sessions"
SESSION_INDEX_FILE = "session_index.jsonl"

CODEX_BIN = "codex"

# Mirrors ``INTERACTIVE_SESSION_SOURCES`` in codex-rs/rollout/src/lib.rs: these
# are the sources offered by ``codex resume``. Everything else is a sub-agent
# or ``codex exec`` side thread. ``vscode`` also covers Codex Desktop here (see
# ``ORIGINATOR_CLIENTS``); both are user-initiated sessions.
INTERACTIVE_SOURCES = frozenset({"cli", "vscode", "atlas", "chatgpt"})

# ``session_meta.source`` is not a reliable client identifier because
# ``SessionSource::VSCode`` is the enum's ``#[default]`` variant. Clients that
# omit a source, including Codex Desktop, are therefore recorded as ``vscode``.
# ``originator`` is client-specific and provides the reliable distinction.
ORIGINATOR_CLIENTS = {
    "codex-tui": "cli",
    "codex_cli_rs": "cli",  # DEFAULT_ORIGINATOR
    "codex_vscode": "vscode",  # the actual VS Code extension
    "codex_exec": "exec",
    "codex desktop": "app",
}

_ROLLOUT_RE = re.compile(
    r"^rollout-(?P<ts>\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2})-(?P<uuid>[0-9a-fA-F-]{36})\.jsonl$"
)
_FILENAME_TS_FMT = "%Y-%m-%dT%H-%M-%S"
TITLE_SCAN_LINES = 400


def default_home() -> Path:
    override = os.environ.get("CODEX_HOME")
    return Path(override).expanduser() if override else Path.home() / ".codex"


def _event_payload(line: str, *wanted: str) -> dict | None:
    try:
        record = json.loads(line)
    except ValueError:
        return None
    if not isinstance(record, dict) or record.get("type") != "event_msg":
        return None
    payload = record.get("payload")
    if not isinstance(payload, dict) or payload.get("type") not in wanted:
        return None
    return payload


def _normalize_source(value: object) -> tuple[str, str | None]:
    """Flatten `source` into (kind, detail).

    Seen in the wild: ``"cli"``, ``"vscode"``, ``"exec"`` and the tagged-enum
    form ``{"subagent": {"other": "guardian"}}``.
    """
    if isinstance(value, str):
        return value, None
    if isinstance(value, dict):
        if "subagent" in value:
            kind = value["subagent"]
            if isinstance(kind, str):
                return "subagent", kind
            if isinstance(kind, dict):
                key, detail = next(iter(kind.items()), (None, None))
                return "subagent", detail if isinstance(detail, str) else key
            return "subagent", None
        key = next(iter(value), None)
        return (key if isinstance(key, str) else "unknown"), None
    return "unknown", None


def _resolve_client(source: str, subagent_kind: str | None, originator: str | None) -> str:
    if source == "subagent":
        return f"subagent:{subagent_kind}" if subagent_kind else "subagent"
    if originator:
        known = ORIGINATOR_CLIENTS.get(originator.strip().lower())
        if known:
            return known
        # Every first-party desktop build reports "Codex <something>".
        if originator.startswith("Codex "):
            return "app"
    return source


def _read_thread_names(home: Path) -> dict[str, str]:
    """User-assigned thread names; the last entry for an id wins."""
    names: dict[str, str] = {}
    try:
        with (home / SESSION_INDEX_FILE).open(encoding="utf-8", errors="replace") as fh:
            for line in fh:
                try:
                    entry = json.loads(line)
                except ValueError:
                    continue
                if not isinstance(entry, dict):
                    continue
                session_id, name = entry.get("id"), entry.get("thread_name")
                if isinstance(session_id, str) and isinstance(name, str) and name:
                    names[session_id] = name
    except OSError:
        return names
    return names


def _session_meta(line: str) -> dict:
    """The `session_meta` payload, which is always the first record."""
    try:
        record = json.loads(line)
    except ValueError:
        return {}
    if not isinstance(record, dict) or record.get("type") != "session_meta":
        return {}
    payload = record.get("payload")
    return payload if isinstance(payload, dict) else {}


def _read_head(path: Path) -> tuple[dict, str | None]:
    """Read a rollout's metadata and opening message in one pass.

    Both live near the start of the file. Reading them together halves the file
    opens during discovery and avoids visible stalls on large session trees.
    """
    meta: dict = {}
    try:
        with path.open(encoding="utf-8", errors="replace") as fh:
            for lineno, line in enumerate(fh):
                if lineno == 0:
                    meta = _session_meta(line)
                    continue
                if lineno >= TITLE_SCAN_LINES:
                    break
                if '"user_message"' not in line:
                    continue
                payload = _event_payload(line, "user_message")
                if payload is None:
                    continue
                text = (payload.get("message") or "").strip()
                if text:
                    return meta, text
    except OSError:
        return meta, None
    return meta, None


def load_session(path: Path, *, archived: bool, names: dict[str, str]) -> Session | None:
    """Build a list entry for one rollout file, or None if it is unreadable."""
    try:
        stat = path.stat()
    except OSError:
        return None

    match = _ROLLOUT_RE.match(path.name)
    meta, opening = _read_head(path)

    # Prefer ``id`` deliberately. In sub-agent rollouts, ``session_id`` contains
    # the parent's thread ID (matching ``parent_thread_id`` in all 58 samples
    # inspected), while ``id`` matches the UUID in the filename. Reversing the
    # order would make copy-resume and delete target the parent session.
    session_id = meta.get("id") or meta.get("session_id")
    if not isinstance(session_id, str) or not session_id:
        if match is None:
            return None
        session_id = match["uuid"]

    created_at = None
    if match is not None:
        try:
            # The filename timestamp is local time, unlike the UTC one inside.
            created_at = datetime.strptime(match["ts"], _FILENAME_TS_FMT)
        except ValueError:
            created_at = None

    source, subagent_kind = _normalize_source(meta.get("source"))
    originator = meta.get("originator") if isinstance(meta.get("originator"), str) else None
    name = names.get(session_id)
    title = name or opening or ""
    # Codex has represented this relationship three ways. Across 81 sub-agent
    # rollouts inspected locally:
    #
    #   0.133.0        no link recorded at all (5 files); the transcript does
    #                  not mention any other session, so it is unrecoverable
    #   0.135–0.136    `forked_from_id` (4 files)
    #   later          `parent_thread_id`, alongside `multi_agent_version` (72)
    #
    # Limit the fallback to sub-agents. On ordinary sessions,
    # ``forked_from_id`` means the user forked a resumable conversation; it does
    # not identify a disposable side thread.
    parent = meta.get("parent_thread_id")
    if not parent and source == "subagent":
        parent = meta.get("forked_from_id")

    return Session(
        backend="codex",
        path=path,
        session_id=session_id,
        title=condense(title) if title else t("empty_session"),
        client=_resolve_client(source, subagent_kind, originator),
        updated_at=datetime.fromtimestamp(stat.st_mtime),
        size=stat.st_size,
        archived=archived,
        named=bool(name),
        noise=source not in INTERACTIVE_SOURCES,
        side_thread=source == "subagent",
        parent_id=parent if isinstance(parent, str) and parent else None,
        cwd=meta.get("cwd") if isinstance(meta.get("cwd"), str) else None,
        version=meta.get("cli_version") if isinstance(meta.get("cli_version"), str) else None,
        created_at=created_at,
    )


class CodexBackend:
    id = "codex"
    label = "Codex"
    agent_label = "Codex"
    shortcut = "x"
    supports_archive = True
    default_client = "cli"
    empty_label = None
    #: A sub-agent rollout records the conversation that spawned it, so once
    #: that conversation is deleted the rollout is provably unreachable —
    #: `codex resume` will never offer it and nothing else refers to it.
    orphan_label = t("orphan_sessions")
    #: Archiving moves one file and deleting removes one file, so batches only
    #: contend for the directory itself. Five at once measured clean, at about
    #: 40ms per session either way.
    bulk_concurrency = 4

    def __init__(self, home: Path | None = None) -> None:
        self.home = home or default_home()

    def missing_cli(self) -> str | None:
        """Listing works from the files alone; changing anything needs the CLI."""
        return None if shutil.which(CODEX_BIN) else CODEX_BIN

    def home_problem(self) -> str | None:
        """CODEX_HOME names the directory itself, so any of them will do."""
        return None

    def resume_command(self, session: Session) -> str:
        parts = []
        if self.home != default_home():
            parts.append(f"CODEX_HOME={shlex.quote(str(self.home))}")
        parts += [CODEX_BIN, "resume", session.session_id]
        return " ".join(parts)

    # ------------------------------------------------------------- discovery

    def discover(self) -> list[Session]:
        names = _read_thread_names(self.home)
        found: list[Session] = []
        for base, archived in (
            (self.home / SESSIONS_SUBDIR, False),
            (self.home / ARCHIVED_SESSIONS_SUBDIR, True),
        ):
            if not base.is_dir():
                continue
            for path in base.rglob("*.jsonl"):
                if not path.is_file():
                    continue
                session = load_session(path, archived=archived, names=names)
                if session is not None:
                    found.append(session)
        found.sort(key=lambda session: session.recency_at, reverse=True)
        return found

    def load_messages(self, session: Session) -> list[Message]:
        """Load human-facing text from ``event_msg`` records.

        The parallel ``response_item`` stream also contains injected
        ``<environment_context>`` blocks, so it is intentionally ignored.
        """
        messages: list[Message] = []
        try:
            with session.path.open(encoding="utf-8", errors="replace") as fh:
                for line in fh:
                    if '"user_message"' not in line and '"agent_message"' not in line:
                        continue
                    payload = _event_payload(line, "user_message", "agent_message")
                    if payload is None:
                        continue
                    images = len(payload.get("images") or []) + len(
                        payload.get("local_images") or []
                    )
                    role = "user" if payload["type"] == "user_message" else "agent"
                    message = make_message(role, payload.get("message") or "", images)
                    if message is not None:
                        messages.append(message)
        except OSError:
            return messages
        return messages

    # ------------------------------------------------------------ operations

    async def _run(self, *args: str) -> OpResult:
        """`delete --force` refuses anything that isn't a UUID, which is why we
        pass session ids rather than names. CODEX_HOME is set explicitly so the
        CLI always acts on the same tree we listed."""
        return await cli.run(CODEX_BIN, args, env={"CODEX_HOME": str(self.home)}, label=self.label)

    async def archive(self, session: Session) -> OpResult:
        return await self._run("archive", session.session_id)

    async def unarchive(self, session: Session) -> OpResult:
        return await self._run("unarchive", session.session_id)

    async def delete(self, session: Session) -> OpResult:
        return await self._run("delete", session.session_id, "--force")
