"""Shared protocol implemented by every agent backend."""

from __future__ import annotations

from pathlib import Path
from typing import Protocol

from ..model import Message, OpResult, Session


class Backend(Protocol):
    #: Stable key used on the command line and in ``Session.backend``.
    id: str
    #: Human-facing name for the banner and the picker.
    label: str
    #: How this agent signs its replies in the conversation view.
    agent_label: str
    #: Where this agent keeps its state.
    home: Path
    #: Whether archive operations are available; false disables ``a``, ``u``, and ``D``.
    supports_archive: bool
    #: Client name common enough that showing it on every row is noise.
    default_client: str
    #: Single letter that picks this agent in the chooser.
    shortcut: str
    #: Label for sessions removed by ``E``; ``None`` hides the key.
    empty_label: str | None
    #: Label for sessions removed by ``O``. ``None`` means this backend cannot
    #: leave orphans and hides the key; see ``model.orphans``.
    orphan_label: str | None
    #: Command this backend needs in order to change anything, if any.
    requires_cli: str | None

    def missing_cli(self) -> str | None:
        """Return the missing required command, or ``None`` when usable."""

    def resume_command(self, session: Session) -> str:
        """Shell command that reopens this session in the agent itself."""

    def discover(self) -> list[Session]:
        """Return every known session, newest first."""

    def load_messages(self, session: Session) -> list[Message]:
        """The user/assistant exchange, without tool traffic or injected context."""

    async def archive(self, session: Session) -> OpResult: ...

    async def unarchive(self, session: Session) -> OpResult: ...

    async def delete(self, session: Session) -> OpResult: ...
