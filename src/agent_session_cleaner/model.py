"""Backend-neutral data types shared by all agents.

The two agents store sessions very differently, so anything backend-specific
(how a title is found, what counts as noise, whether archiving exists at all)
is resolved by each backend and normalized into these types.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from pathlib import Path

# A single transcript line can be megabytes of base64 image or tool output.
MAX_MESSAGE_CHARS = 8000
MAX_TITLE_CHARS = 160


@dataclass(frozen=True)
class Session:
    backend: str
    path: Path
    session_id: str
    title: str
    client: str
    updated_at: datetime
    size: int
    archived: bool = False
    #: Title came from a name the user assigned, so it deserves emphasis.
    named: bool = False
    #: A side thread or empty session: nothing a person intentionally started.
    noise: bool = False
    #: A side thread spawned by the agent rather than opened by a person. If
    #: ``parent_id`` is unset, the relationship was not recorded; that does not
    #: prove the session had no parent. See ``orphans``.
    side_thread: bool = False
    #: The session that started this one, when the agent records the link.
    parent_id: str | None = None
    cwd: str | None = None
    version: str | None = None
    created_at: datetime | None = None

    @property
    def recency_at(self) -> datetime:
        """When this session started, for ordering and display.

        Prefer the recorded start time over mtime. An upgrade or migration may
        rewrite the entire session tree and collapse mtimes to one instant,
        while the original start time remains stable.
        """
        return self.created_at or self.updated_at


@dataclass(frozen=True)
class Message:
    role: str  # "user" | "agent"
    text: str
    truncated_chars: int


@dataclass(frozen=True)
class OpResult:
    ok: bool
    message: str


def orphans(sessions: list[Session]) -> list[Session]:
    """Return sub-agent sessions whose parent is no longer on disk.

    A sub-agent session depends on the conversation that spawned it and cannot
    be resumed once that parent is gone. The function must receive the complete
    listing: an archived parent still exists, so its children are not orphans.
    """
    known = {session.session_id for session in sessions}
    return [s for s in sessions if s.parent_id and s.parent_id not in known]


def condense(text: str, limit: int = MAX_TITLE_CHARS) -> str:
    condensed = " ".join(text.split())
    return condensed[: limit - 1] + "…" if len(condensed) > limit else condensed


def make_message(role: str, text: str, images: int = 0) -> Message | None:
    text = text.strip()
    if images:
        text = f"{text}\n[{images} 张图片]".strip()
    if not text:
        return None
    truncated = max(0, len(text) - MAX_MESSAGE_CHARS)
    return Message(role, text[:MAX_MESSAGE_CHARS], truncated)
