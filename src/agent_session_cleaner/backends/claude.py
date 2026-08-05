"""Claude Code backend.

Layout under ``$CLAUDE_CONFIG_DIR`` (default ``~/.claude``)::

    projects/<sanitized-cwd>/<session-uuid>.jsonl          transcript
    projects/<sanitized-cwd>/<session-uuid>/               co-located sidecar
        subagents/agent-*.jsonl                            sub-agent transcripts
        tool-results/                                      offloaded tool output

Two details differ substantially from Codex:

* Claude Code has no local archive operation to delegate to: ``claude project
  purge`` removes an entire project. Session deletion is therefore a direct
  filesystem operation, and ``supports_archive`` is ``False``.
* The filename is only a UUID, so the start time has to come from the first
  record's timestamp rather than the name.

Deletion follows cc-switch's ``session_manager/providers/claude.rs``:
verify the session id recorded inside the file, remove the same-stem sidecar,
then remove the transcript. Like cc-switch, it deliberately leaves the
top-level ``file-history/<id>``, ``session-env/<id>``, ``image-cache/<id>``,
``tasks/<id>`` directories and ``history.jsonl`` untouched.
"""

from __future__ import annotations

import asyncio
import json
import os
import shlex
import shutil
from datetime import datetime
from pathlib import Path

from ..i18n import t
from ..model import Message, OpResult, Session, condense, make_message

CLAUDE_BIN = "claude"
PROJECTS_SUBDIR = "projects"
# Sub-agent transcripts live inside the sidecar and are named `agent-<hash>`.
AGENT_PREFIX = "agent-"
# cwd/version only appear on user/assistant records, which follow a short
# preamble of mode/permission-mode/file-history-snapshot lines.
HEAD_SCAN_LINES = 200


def default_home() -> Path:
    override = os.environ.get("CLAUDE_CONFIG_DIR")
    return Path(override).expanduser() if override else Path.home() / ".claude"


def _parse_timestamp(value: object) -> datetime | None:
    if not isinstance(value, str) or not value:
        return None
    try:
        # Recorded as UTC with a trailing Z, which fromisoformat accepts from
        # Python 3.11 on. Everything on screen is local time, so convert.
        parsed = datetime.fromisoformat(value)
    except ValueError:
        return None
    return parsed.astimezone().replace(tzinfo=None)


def _block_text(content: object) -> tuple[str, int]:
    """Flatten a message body into (text, image_count).

    Content is either a bare string or a list of blocks; only `text` blocks are
    human-facing. `thinking`, `tool_use` and `tool_result` are dropped.
    """
    if isinstance(content, str):
        return content, 0
    if not isinstance(content, list):
        return "", 0
    parts: list[str] = []
    images = 0
    for block in content:
        if not isinstance(block, dict):
            continue
        kind = block.get("type")
        if kind == "text" and isinstance(block.get("text"), str):
            parts.append(block["text"])
        elif kind == "image":
            images += 1
    return "\n".join(parts), images


def _is_real_exchange(record: dict) -> bool:
    """Return whether a record belongs to the visible conversation.

    Most ``user`` records are tool traffic rather than user messages. In the
    largest transcript inspected, 213 of 224 were tool results and another 3
    were injected metadata.
    """
    if record.get("type") not in ("user", "assistant"):
        return False
    if record.get("toolUseResult") is not None:
        return False
    return not record.get("isMeta") and not record.get("isSidechain")


def _parse(path: Path) -> dict:
    """Single pass over a transcript, collecting what the list needs."""
    info: dict = {
        "session_id": None,
        "cwd": None,
        "version": None,
        "client": None,
        "created_at": None,
        "ai_title": None,
        "first_user": None,
    }
    try:
        with path.open(encoding="utf-8", errors="replace") as fh:
            for lineno, line in enumerate(fh):
                # Inspect a line when it may contain a late ``ai-title``, belongs
                # to the metadata preamble, or could be the first user message.
                # That last condition may require scanning the whole file: the
                # bulk-delete key must not mistake a long preamble for an empty
                # session.
                wants_title = '"ai-title"' in line
                wants_head = lineno < HEAD_SCAN_LINES
                wants_user = info["first_user"] is None and '"user"' in line
                if not wants_title and not wants_head and not wants_user:
                    continue
                try:
                    record = json.loads(line)
                except ValueError:
                    continue
                if not isinstance(record, dict):
                    continue

                if record.get("type") == "ai-title":
                    title = record.get("aiTitle")
                    if isinstance(title, str) and title.strip():
                        info["ai_title"] = title.strip()  # last one wins
                    continue

                if info["session_id"] is None and isinstance(record.get("sessionId"), str):
                    info["session_id"] = record["sessionId"]
                if info["created_at"] is None:
                    info["created_at"] = _parse_timestamp(record.get("timestamp"))
                for key, field in (
                    ("cwd", "cwd"),
                    ("version", "version"),
                    ("client", "entrypoint"),
                ):
                    if info[key] is None and isinstance(record.get(field), str):
                        info[key] = record[field]

                if (
                    info["first_user"] is None
                    and record.get("type") == "user"
                    and _is_real_exchange(record)
                ):
                    message = record.get("message") or {}
                    text, _ = _block_text(message.get("content"))
                    if text.strip():
                        info["first_user"] = text.strip()
    except OSError:
        return info
    return info


def load_session(path: Path) -> Session | None:
    try:
        stat = path.stat()
    except OSError:
        return None

    info = _parse(path)
    session_id = info["session_id"] or path.stem
    title = info["ai_title"] or info["first_user"] or ""

    return Session(
        backend="claude",
        path=path,
        session_id=session_id,
        title=condense(title) if title else t("empty_session"),
        client=info["client"] or "cli",
        updated_at=datetime.fromtimestamp(stat.st_mtime),
        size=stat.st_size,
        archived=False,  # Claude Code has no archive concept
        named=False,  # ai-title is generated, not user-assigned
        noise=info["first_user"] is None,
        cwd=info["cwd"],
        version=info["version"],
        created_at=info["created_at"],
    )


def sidecar_of(path: Path) -> Path:
    """`.../<uuid>.jsonl` -> `.../<uuid>`, the co-located sidecar directory."""
    return path.with_suffix("")


def _recorded_session_id(path: Path) -> str | None:
    try:
        with path.open(encoding="utf-8", errors="replace") as fh:
            for lineno, line in enumerate(fh):
                if lineno >= HEAD_SCAN_LINES:
                    break
                if '"sessionId"' not in line:
                    continue
                try:
                    record = json.loads(line)
                except ValueError:
                    continue
                if isinstance(record, dict) and isinstance(record.get("sessionId"), str):
                    return record["sessionId"]
    except OSError:
        return None
    return None


def _delete_on_disk(session: Session) -> OpResult:
    path = session.path
    if not path.exists():
        return OpResult(False, t("session_file_missing"))

    # Guard against deleting the wrong transcript, as cc-switch does: the id we
    # are about to act on must match the one recorded inside the file.
    recorded = _recorded_session_id(path)
    if recorded is not None and recorded != session.session_id:
        return OpResult(False, t("session_id_mismatch"))

    sidecar = sidecar_of(path)
    try:
        if sidecar.is_dir():
            shutil.rmtree(sidecar)
        elif sidecar.exists():
            sidecar.unlink()
    except OSError as error:
        return OpResult(False, t("sidecar_delete_failed", error=error))

    try:
        path.unlink()
    except OSError as error:
        return OpResult(False, t("session_delete_failed", error=error))
    return OpResult(True, "")


class ClaudeBackend:
    id = "claude"
    label = "Claude Code"
    agent_label = "Claude"
    shortcut = "c"
    supports_archive = False
    default_client = "cli"
    empty_label = t("empty_sessions")
    #: Sub-agent transcripts live inside the parent's sidecar directory, which
    #: is removed along with the parent, so a stranded one cannot arise.
    orphan_label = None
    #: Each deletion is a few filesystem calls on paths of its own.
    bulk_concurrency = 4

    def __init__(self, home: Path | None = None) -> None:
        self.home = home or default_home()

    def missing_cli(self) -> str | None:
        return None

    def home_problem(self) -> str | None:
        return None

    def resume_command(self, session: Session) -> str:
        parts = []
        if self.home != default_home():
            parts.append(f"CLAUDE_CONFIG_DIR={shlex.quote(str(self.home))}")
        parts += [CLAUDE_BIN, "--resume", session.session_id]
        return " ".join(parts)

    def discover(self) -> list[Session]:
        root = self.home / PROJECTS_SUBDIR
        found: list[Session] = []
        if not root.is_dir():
            return found
        for project in sorted(root.iterdir()):
            if not project.is_dir():
                continue
            for path in project.glob("*.jsonl"):
                # Sub-agent transcripts live one level down, inside the sidecar,
                # and are excluded by name the way cc-switch does it.
                if path.name.startswith(AGENT_PREFIX) or not path.is_file():
                    continue
                session = load_session(path)
                if session is not None:
                    found.append(session)
        found.sort(key=lambda session: session.recency_at, reverse=True)
        return found

    def load_messages(self, session: Session) -> list[Message]:
        messages: list[Message] = []
        try:
            with session.path.open(encoding="utf-8", errors="replace") as fh:
                for line in fh:
                    if '"user"' not in line and '"assistant"' not in line:
                        continue
                    try:
                        record = json.loads(line)
                    except ValueError:
                        continue
                    if not isinstance(record, dict) or not _is_real_exchange(record):
                        continue
                    body = record.get("message") or {}
                    text, images = _block_text(body.get("content"))
                    role = "user" if record["type"] == "user" else "agent"
                    message = make_message(role, text, images)
                    if message is not None:
                        messages.append(message)
        except OSError:
            return messages
        return messages

    async def archive(self, session: Session) -> OpResult:
        return OpResult(False, t("agent_no_archive", agent=self.label))

    async def unarchive(self, session: Session) -> OpResult:
        return OpResult(False, t("agent_no_archive", agent=self.label))

    async def delete(self, session: Session) -> OpResult:
        return await asyncio.to_thread(_delete_on_disk, session)
