"""Backend-agnostic data types shared by every agent.

The two agents store sessions very differently, so anything backend-specific
(how a title is found, what counts as noise, whether archiving exists at all)
is resolved inside the backend and flattened into these types.
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
    #: A side-thread or an empty session: hidden until the user asks for it.
    noise: bool = False
    cwd: str | None = None
    version: str | None = None
    created_at: datetime | None = None

    @property
    def recency_at(self) -> datetime:
        """When this session started, for ordering and display.

        Prefers a recorded start time over mtime: bulk rewrites of a session
        tree (an agent upgrade/migration will do this) reset every mtime to the
        same instant, while the recorded time is written once.
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
