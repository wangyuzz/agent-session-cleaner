"""Backend registry."""

from __future__ import annotations

from pathlib import Path

from .base import Backend
from .claude import ClaudeBackend
from .codex import CodexBackend

BACKEND_CLASSES = {
    CodexBackend.id: CodexBackend,
    ClaudeBackend.id: ClaudeBackend,
}
BACKEND_IDS = tuple(BACKEND_CLASSES)


def build(backend_id: str, home: Path | None = None) -> Backend:
    return BACKEND_CLASSES[backend_id](home)


__all__ = ["BACKEND_CLASSES", "BACKEND_IDS", "Backend", "ClaudeBackend", "CodexBackend", "build"]
