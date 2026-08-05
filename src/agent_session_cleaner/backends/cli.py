"""Running an agent's own command line for the changes it owns.

Where an agent ships a command that archives or deletes sessions, that command
is the only thing allowed to do it here: it knows what else has to move, and it
reports failure through its exit status instead of leaving half a session
behind.
"""

from __future__ import annotations

import asyncio
import os
import re
import shutil
from collections.abc import Mapping, Sequence

from ..i18n import t
from ..model import OpResult

DEFAULT_TIMEOUT_SECONDS = 60.0

#: Colour and style escapes. Some agents style their errors even when stderr is
#: a pipe, and the sequences would otherwise land in the status bar verbatim.
_ANSI = re.compile(r"\x1b\[[0-9;]*[A-Za-z]")


def _complaint(raw: bytes) -> str:
    """Pick one useful, unstyled error line from command output.

    Most commands put the useful explanation last. OpenCode is an exception:
    database failures start with ``Error: ...`` and end with a bare ``params:``
    line, which is neither actionable nor suitable for the status bar.
    """
    text = _ANSI.sub("", raw.decode("utf-8", errors="replace")).strip()
    lines = [line.strip() for line in text.splitlines() if line.strip()]
    if not lines:
        return ""
    return next(
        (line for line in reversed(lines) if line.casefold().startswith("error:")),
        lines[-1],
    )


async def run(
    executable_name: str,
    args: Sequence[str],
    *,
    env: Mapping[str, str],
    label: str,
    timeout: float = DEFAULT_TIMEOUT_SECONDS,
) -> OpResult:
    """Run one agent command to completion and translate the outcome.

    ``env`` is merged over the current environment; backends use it to pin the
    command to the same session tree the list was read from.
    """
    executable = shutil.which(executable_name)
    if executable is None:
        return OpResult(False, t("backend_cli_missing", agent=label))

    try:
        process = await asyncio.create_subprocess_exec(
            executable,
            *args,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            env={**os.environ, **env},
        )
    except OSError as error:
        return OpResult(False, t("backend_cli_start_failed", agent=label, error=error))

    try:
        stdout, stderr = await asyncio.wait_for(process.communicate(), timeout)
    except TimeoutError:
        process.kill()
        await process.wait()
        return OpResult(False, t("backend_cli_timeout", agent=label, seconds=f"{timeout:.0f}"))

    if process.returncode == 0:
        # The command's own wording ("Archived session <uuid>.") is for scripts;
        # the caller phrases the success message for people.
        return OpResult(True, "")
    complaint = _complaint(stderr) or _complaint(stdout)
    return OpResult(False, complaint or t("backend_cli_unknown_failure", agent=label))
