"""End-to-end checks.

    .venv/bin/python tests/test_e2e.py

Everything behavioural runs against synthetic homes, so the suite behaves the
same on any machine and on a CI runner. Three sections additionally read the
real ``~/.codex``, ``~/.claude`` and OpenCode trees to prove the parsers cope
with data as it is actually written; those skip themselves when there is
nothing to read, and never write to them. Anything that deletes works on a
throwaway copy, and the sections that drive an agent's own command line skip
themselves when it is not installed.

The application is exercised in English — the catalogue these checks quote.
The Chinese one is covered by its own section, in a subprocess of its own.
"""

from __future__ import annotations

import asyncio
import collections
import contextlib
import json
import os
import re
import shlex
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time
import uuid
from datetime import date, datetime, timedelta
from pathlib import Path

# Set before the package is imported: the language is resolved once, at import.
os.environ["AGENT_SESSION_CLEANER_LANG"] = "en"
# CI runners often start with a POSIX locale, which would turn any non-ASCII
# output into an encoding error rather than the check it belongs to.
with contextlib.suppress(AttributeError, OSError, ValueError):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from rich.cells import cell_len

from agent_session_cleaner import __version__, clipboard, model
from agent_session_cleaner import app as app_module
from agent_session_cleaner.app import ConfirmScreen, SessionCleanerApp, SessionRow
from agent_session_cleaner.backends import (
    BACKEND_CLASSES,
    BACKEND_IDS,
    Backend,
    ClaudeBackend,
    CodexBackend,
    OpenCodeBackend,
)
from agent_session_cleaner.backends import claude as claude_backend
from agent_session_cleaner.backends import codex as codex_backend
from agent_session_cleaner.backends import opencode as opencode_backend
from agent_session_cleaner.i18n import _EN, _ZH, catalogs_match, detect_language
from agent_session_cleaner.model import MAX_MESSAGE_CHARS, Message, Session
from agent_session_cleaner.picker import AgentPicker, AgentRow

ROOT = Path(__file__).resolve().parents[1]
#: Keys that change something and therefore vanish without the agent's command.
_WRITING_ACTIONS = ("archive", "delete", "delete_archived", "toggle_danger", "toggle_select")
#: Widest terminal the two footer rows are expected to be readable in. Only the
#: bottom row — the way out — is guaranteed to fit a narrow one; see
#: ``test_footer``.
FOOTER_FULL_WIDTH = 110

checks: list[tuple[bool, str]] = []
skipped: list[str] = []


def check(ok: object, label: str) -> bool:
    checks.append((bool(ok), label))
    print(f"  {'PASS' if ok else 'FAIL'}  {label}")
    return bool(ok)


def skip(label: str) -> None:
    """Record a section that had no material to run against.

    A runner without Codex installed, or with an empty session tree, should get
    a clean run rather than a wall of failures about missing data.
    """
    skipped.append(label)
    print(f"  SKIP  {label}")


# ------------------------------------------------------------------- harness


def _status(app: SessionCleanerApp) -> str:
    return str(app.query_one("#status").visual).strip()


def _banner(app: SessionCleanerApp) -> str:
    return str(app.query_one("#banner").visual).strip()


def _rows(app: SessionCleanerApp) -> dict[str, SessionRow]:
    return {row.session.title: row for row in app.query(SessionRow)}


def _picked(app: SessionCleanerApp) -> set[str]:
    return {s.title for s in app._sessions if s.session_id in app._selection}


def _footer(app: SessionCleanerApp) -> list[list[str]]:
    return [
        [str(key.render()).strip() for key in row.query("FooterKey")]
        for row in app.query(app_module.FooterRow)
    ]


async def _settle(pilot, app: SessionCleanerApp) -> None:
    """Wait for an in-flight delete/archive to finish."""
    for _ in range(200):
        await pilot.pause(0.1)
        if not app._busy:
            break
    await pilot.pause(0.3)


async def _dialog(pilot, app: SessionCleanerApp) -> bool:
    """Wait for a worker-driven modal to reach the screen stack."""
    for _ in range(20):
        if isinstance(app.screen, ConfirmScreen):
            return True
        await pilot.pause(0.1)
    return False


async def _goto(pilot, app: SessionCleanerApp, title: str) -> Session:
    """Put the cursor on the row with this title."""
    return await _goto_id(pilot, app, next(s.session_id for s in app._sessions if s.title == title))


async def _goto_id(pilot, app: SessionCleanerApp, session_id: str) -> Session:
    app.query_one("#sessions").index = next(
        i for i, s in enumerate(app._sessions) if s.session_id == session_id
    )
    await pilot.pause(0.3)
    return app._cursor()


@contextlib.asynccontextmanager
async def _app(backend: Backend, *, size=(120, 40), force_cli: bool = False):
    """Start the app on a backend and hand back (app, pilot), ready to drive.

    ``force_cli`` is for sections that only exercise the interface: every key
    should be visible even where the agent's own command is not installed.
    """
    app = SessionCleanerApp(backend)
    if force_cli:
        app._missing_cli = None
    async with app.run_test(size=size) as pilot:
        await pilot.pause()
        await pilot.pause(0.5)
        yield app, pilot


# ============================================================ [0] contracts


def test_contracts() -> None:
    """What the pieces promise each other, so a fourth agent cannot half-land."""
    print("\n[0] Contracts: backends and the message catalogue")

    scratch = Path(tempfile.mkdtemp(prefix="asc-contract-"))
    try:
        fields = set(Backend.__annotations__)
        methods = ("missing_cli", "home_problem", "resume_command", "discover", "load_messages")
        operations = ("archive", "unarchive", "delete")
        for backend_id, cls in BACKEND_CLASSES.items():
            backend = cls(scratch)
            missing = [name for name in fields | {"home"} if not hasattr(backend, name)]
            missing += [
                name for name in methods + operations
                if not callable(getattr(backend, name, None))
            ]
            check(not missing, f"{cls.label} implements the whole backend protocol")
            check(backend.id == backend_id, f"{cls.label} is registered under its own id")
        shortcuts = [cls.shortcut for cls in BACKEND_CLASSES.values()]
        check(len(set(shortcuts)) == len(shortcuts), f"one shortcut key each: {shortcuts}")
        check(
            all(cls(scratch).discover() == [] for cls in BACKEND_CLASSES.values()),
            "an empty home lists nothing rather than failing",
        )
    finally:
        shutil.rmtree(scratch, ignore_errors=True)

    check(catalogs_match(), "the English and Chinese catalogues hold the same keys")
    placeholders = re.compile(r"\{(\w+)")
    mismatched = [
        key
        for key in _EN
        if set(placeholders.findall(_EN[key])) != set(placeholders.findall(_ZH[key]))
    ]
    check(not mismatched, f"both catalogues take the same placeholders ({mismatched})")
    # A stray brace only shows up when someone hits that exact line, in the
    # language they happen to be running.
    unformattable = []
    for name, catalog in (("English", _EN), ("Chinese", _ZH)):
        for key, template in catalog.items():
            try:
                template.format(**dict.fromkeys(placeholders.findall(template), "x"))
            except (KeyError, IndexError, ValueError) as error:
                unformattable.append(f"{name}/{key}: {error}")
    check(not unformattable, f"every message formats with its own values ({unformattable})")

    # Every key the source asks for has to exist, or the first person to hit
    # that line gets a KeyError instead of a sentence.
    used: set[str] = set()
    for path in sorted((ROOT / "src").rglob("*.py")):
        if path.name == "i18n.py":
            continue
        for one, many in re.findall(r'\b[tn]\(\s*"(\w+)"(?:\s*,\s*"(\w+)")?', path.read_text()):
            used.update(filter(None, (one, many)))
    check(used, f"found {len(used)} message keys in the source")
    check(not (used - set(_EN)), f"every key the source asks for exists ({used - set(_EN)})")
    # Two families are built from an action or backend name and are invisible to
    # the scan above; they are exactly the ones a new agent or action would add.
    forms = ("", "_progress", "_done")
    derived = {f"operation_{kind}{form}" for kind in operations for form in forms}
    derived |= {f"cli_{backend_id}_home" for backend_id in BACKEND_IDS}
    check(not (derived - set(_EN)), f"keys built from a name exist too ({derived - set(_EN)})")


# ============================================================ [1] real data


def test_codex_real_data() -> None:
    print("\n[1] Real ~/.codex")
    backend = CodexBackend()
    found = backend.discover() if backend.home.is_dir() else []
    if not found:
        skip(f"no sessions under {backend.home}")
        return

    check(
        all(s.session_id and s.backend == "codex" for s in found),
        f"parsed an id for every one of {len(found)} sessions",
    )
    check(
        found == sorted(found, key=lambda s: s.recency_at, reverse=True),
        "listed newest first",
    )
    clobbered = collections.Counter(s.updated_at.strftime("%Y-%m-%d %H:%M") for s in found)
    _, top_count = clobbered.most_common(1)[0]
    check(
        all(s.recency_at == (s.created_at or s.updated_at) for s in found),
        f"recency comes from the recorded start time ({top_count}/{len(found)} share an mtime)",
    )

    # A sub-agent rollout carries its parent's thread id in `session_id`, so
    # taking that field at face value points delete at the wrong session. The
    # filename uuid is the tiebreaker: it always matches the rollout's own `id`.
    named = [(s, codex_backend._ROLLOUT_RE.match(s.path.name)) for s in found]
    borrowed = [s for s, m in named if m is not None and m["uuid"] != s.session_id]
    check(not borrowed, f"nobody answers to another session's id ({len(borrowed)} mismatched)")

    # `SessionSource::VSCode` is the #[default] variant, so anything that does
    # not declare a source is recorded as "vscode". The invariant is therefore
    # "labelled vscode implies it said so", not "never vscode".
    labelled = [s for s in found if s.client == "vscode"]
    mislabelled = [
        s for s in labelled
        if codex_backend._read_head(s.path)[0].get("originator") != "codex_vscode"
    ]
    check(not mislabelled, f"the vscode badge only goes to the extension ({len(labelled)} rows)")
    check(
        all(s.archived == (codex_backend.ARCHIVED_SESSIONS_SUBDIR in s.path.parts) for s in found),
        f"archived state matches the directory ({sum(s.archived for s in found)} archived)",
    )
    biggest = max(found, key=lambda s: s.size)
    longest = max((len(m.text) for m in backend.load_messages(biggest)), default=0)
    check(
        longest <= MAX_MESSAGE_CHARS,
        f"the largest file ({biggest.size / 1048576:.1f}MB) truncates to {longest} chars",
    )


def test_claude_real_data() -> None:
    print("\n[2] Real ~/.claude")
    backend = ClaudeBackend()
    found = backend.discover() if backend.home.is_dir() else []
    if not found:
        skip(f"no sessions under {backend.home}")
        return

    check(
        all(s.backend == "claude" and s.session_id == s.path.stem for s in found),
        f"{len(found)} sessions, each answering to its own filename",
    )
    check(
        not any(s.path.name.startswith(claude_backend.AGENT_PREFIX) for s in found)
        and not [s for s in found if s.path.parent.parent.name != "projects"],
        "sub-agent transcripts and nested files stay out of the list",
    )
    check(not any(s.archived for s in found), "Claude Code produces no archived rows")

    # `noise` decides what `E` deletes, and it is settled from the first 200
    # lines; cross-check it against a full parse of every transcript.
    disagree = [
        s
        for s in found
        if s.noise != (not any(m.role == "user" for m in backend.load_messages(s)))
    ]
    check(
        not disagree,
        f"empty-session judgement survives a full parse ({sum(s.noise for s in found)} empty)",
    )

    # The largest transcript is mostly tool traffic, which must not leak into
    # the conversation. Counted structurally: a transcript that *discusses* tool
    # plumbing would fool a substring check.
    biggest = max(found, key=lambda s: s.size)
    messages = backend.load_messages(biggest)
    raw_user = tool_traffic = 0
    for line in biggest.path.open(encoding="utf-8", errors="replace"):
        if '"user"' not in line:
            continue
        try:
            record = json.loads(line)
        except ValueError:
            continue
        if record.get("type") != "user":
            continue
        raw_user += 1
        tool_traffic += bool(record.get("toolUseResult") is not None or record.get("isMeta"))
    kept = sum(m.role == "user" for m in messages)
    check(
        tool_traffic > 0 and kept == raw_user - tool_traffic,
        f"dropped {tool_traffic} tool/injected records, kept {kept}/{raw_user} real messages",
    )
    check(
        all(m.role in ("user", "agent") for m in messages)
        and max((len(m.text) for m in messages), default=0) <= MAX_MESSAGE_CHARS,
        f"the largest file ({biggest.size / 1048576:.1f}MB) yields {len(messages)} messages",
    )


def test_opencode_real_data() -> None:
    print("\n[3] Real OpenCode database")
    backend = OpenCodeBackend()
    found = backend.discover() if backend.database.is_file() else []
    if not found:
        skip(f"no sessions in {backend.database}")
        return

    check(
        all(
            s.backend == "opencode" and opencode_backend._SESSION_ID_RE.match(s.session_id)
            for s in found
        ),
        f"{len(found)} sessions, each with an id OpenCode would recognise",
    )
    check(
        found == sorted(found, key=lambda s: s.recency_at, reverse=True),
        "listed newest first",
    )

    with sqlite3.connect(f"{backend.database.resolve().as_uri()}?mode=ro", uri=True) as connection:
        parents = dict(connection.execute("SELECT id, parent_id FROM session"))
        archived = {
            row[0]
            for row in connection.execute("SELECT id FROM session WHERE time_archived IS NOT NULL")
        }
        # The session with the most tool-generated text is where letting
        # `synthetic` parts through would be most obvious.
        counts: collections.Counter = collections.Counter()
        for session_id, data in connection.execute("SELECT session_id, data FROM part"):
            part = json.loads(data)
            if part.get("type") == "text" and part.get("synthetic"):
                counts[session_id] += 1
        noisiest = counts.most_common(1)[0][0] if counts else None

    check(len(found) == len(parents), f"every one of the {len(parents)} rows was read")
    check(
        all(s.parent_id == parents[s.session_id] for s in found),
        f"parentage matches the column ({sum(bool(s.parent_id) for s in found)} sub-agents)",
    )
    check(
        {s.session_id for s in found if s.archived} == archived,
        f"archived state matches time_archived ({len(archived)} archived)",
    )
    # Otherwise these read as "New session - 2026-…Z", which says nothing.
    left = [s for s in found if opencode_backend._is_placeholder(s.title)]
    check(not left, f"no session is left showing a placeholder title ({len(left)})")

    session = next((s for s in found if s.session_id == noisiest), None)
    if session is None:
        skip("no tool-generated text on this machine")
        return
    messages = backend.load_messages(session)
    with sqlite3.connect(f"{backend.database.resolve().as_uri()}?mode=ro", uri=True) as connection:
        synthetic = [
            part["text"]
            for (data,) in connection.execute(
                "SELECT data FROM part WHERE session_id = ?", (session.session_id,)
            )
            if (part := json.loads(data)).get("synthetic") and isinstance(part.get("text"), str)
        ]
    joined = "\n".join(m.text for m in messages)
    leaked = [text for text in synthetic if text[:60] and text[:60] in joined]
    check(
        not leaked and all(m.role in ("user", "agent") for m in messages),
        f"dropped {len(synthetic)} synthetic parts, leaked {len(leaked)}, kept {len(messages)}",
    )
    check(
        max((len(m.text) for m in messages), default=0) <= MAX_MESSAGE_CHARS,
        "over-long messages are truncated",
    )


# ============================================================== fixture data


def _write_jsonl(path: Path, records: list[dict]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("".join(json.dumps(r) + "\n" for r in records), encoding="utf-8")


_GUARDIAN = {"subagent": {"other": "guardian"}}

#: One synthetic rollout. `parent` is an index into the table when the parent is
#: also in it, the string "gone" when it points at a session that no longer
#: exists, and None when the rollout records no parent at all. `field` is which
#: of Codex's two spellings carries it.
_Rollout = collections.namedtuple(
    "_Rollout", "title cwd originator source parent field", defaults=(None, "parent_thread_id")
)

#: Three "proxy" titles give the search section something with more than one hit
#: but fewer than all.
_CODEX_FIXTURE = [
    _Rollout("fix proxy timeout", "/Users/me/alpha", "codex_cli_rs", "cli"),
    _Rollout("tidy proxy config", "/Users/me/alpha", "codex-tui", "cli"),
    _Rollout("add proxy retries", "/Users/me/beta", "Codex Desktop", "vscode"),
    _Rollout("weekly report", "/Users/me/beta", "codex_cli_rs", "cli"),
    # Wide characters in both the project column and the title: the columns are
    # sized in terminal cells, not characters.
    _Rollout("宽字符标题", "/Users/me/中文目录", "codex_cli_rs", "cli"),
    _Rollout("batch job", "/Users/me/alpha", "codex_exec", "exec"),
    # 0.133.0 wrote no link at all; nothing can be reconstructed from it.
    _Rollout("side thread with no parent recorded", "/Users/me/alpha", "codex_cli_rs", _GUARDIAN),
    # Two under the same parent, so the tree has both a `├─` and a `└─`.
    _Rollout("first side thread", "/Users/me/alpha", "codex_cli_rs", _GUARDIAN, 0),
    _Rollout("second side thread", "/Users/me/alpha", "codex_cli_rs", _GUARDIAN, 0),
    # 0.135 ~ 0.136 spelled it `forked_from_id`.
    _Rollout("early side thread", "/Users/me/beta", "codex_cli_rs", _GUARDIAN, 2, "forked_from_id"),
    _Rollout("orphaned side thread", "/Users/me/beta", "codex_cli_rs", _GUARDIAN, "gone"),
    _Rollout("another orphaned side thread", "/Users/me/gamma", "codex_cli_rs", _GUARDIAN, "gone"),
    # An ordinary session forked from a conversation that is gone. Same field as
    # the early sub-agents, completely different meaning: this is a real
    # conversation you can resume, and it must never count as stranded.
    _Rollout(
        "forked ordinary session", "/Users/me/gamma", "codex_cli_rs", "cli",
        "gone", "forked_from_id",
    ),
]


def make_codex_fixture() -> Path:
    """A synthetic CODEX_HOME for everything that only reads and renders."""
    home = Path(tempfile.mkdtemp(prefix="asc-codexfix-"))
    start = datetime(2026, 7, 20, 15, 0)
    # Ids first: a sub-agent has to name a parent written in the same pass.
    ids = [str(uuid.uuid4()) for _ in _CODEX_FIXTURE]
    for offset, row in enumerate(_CODEX_FIXTURE):
        when = start - timedelta(hours=offset)
        payload = {
            "id": ids[offset],
            "session_id": ids[offset],
            "cwd": row.cwd,
            "originator": row.originator,
            "source": row.source,
            "cli_version": "0.50.0",
        }
        parent = str(uuid.uuid4()) if row.parent == "gone" else None
        if isinstance(row.parent, int):
            parent = ids[row.parent]
        if parent is not None:
            payload[row.field] = parent
            if row.field == "parent_thread_id":
                # Codex puts the *parent's* thread id in `session_id` on a
                # sub-agent rollout. The fixture copies that quirk on purpose.
                payload["session_id"] = parent
        _write_jsonl(
            home
            / "sessions"
            / f"{when:%Y}/{when:%m}/{when:%d}"
            / f"rollout-{when:%Y-%m-%dT%H-%M-%S}-{ids[offset]}.jsonl",
            [
                {"type": "session_meta", "payload": payload},
                {"type": "event_msg", "payload": {"type": "user_message", "message": row.title}},
                {
                    "type": "event_msg",
                    "payload": {"type": "agent_message", "message": f"on it: {row.title}"},
                },
            ],
        )
    return home


#: Only the columns this tool reads, and deliberately not all of them: OpenCode
#: has added columns over the years, so an older database is missing some of
#: what the current one has. Anything the backend cannot find has to default.
_OPENCODE_SCHEMA = """
CREATE TABLE session (
    id TEXT PRIMARY KEY, project_id TEXT NOT NULL, parent_id TEXT,
    directory TEXT NOT NULL, title TEXT NOT NULL, version TEXT, agent TEXT,
    time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, time_archived INTEGER
);
CREATE TABLE message (
    id TEXT PRIMARY KEY, session_id TEXT NOT NULL,
    time_created INTEGER NOT NULL, data TEXT NOT NULL
);
CREATE TABLE part (
    id TEXT PRIMARY KEY, message_id TEXT NOT NULL, session_id TEXT NOT NULL,
    time_created INTEGER NOT NULL, data TEXT NOT NULL
);
"""

#: One synthetic session. `parent` works as it does for Codex above. An empty
#: `opening` means the session only ever contained tool traffic.
_OpenCodeRow = collections.namedtuple(
    "_OpenCodeRow", "title opening cwd agent parent archived", defaults=("build", None, False)
)

_OPENCODE_FIXTURE = [
    _OpenCodeRow("fix proxy timeout", "the proxy keeps timing out", "/Users/me/alpha"),
    _OpenCodeRow("tidy proxy config", "clean up the proxy config", "/Users/me/alpha"),
    _OpenCodeRow("add proxy retries", "add a retry", "/Users/me/beta"),
    _OpenCodeRow("weekly report", "write this week's report", "/Users/me/beta"),
    # Two sub-agents under the same session, so the tree has a ├─ and a └─.
    _OpenCodeRow("trace the timeout", "where does it time out", "/Users/me/alpha", "explore", 0),
    _OpenCodeRow("read the retry logic", "check the retry logic", "/Users/me/alpha", "explore", 0),
    _OpenCodeRow("sub-agent left behind", "parent is gone", "/Users/me/beta", "explore", "gone"),
    # Named before the model summarised anything: the opening message is the
    # only thing that says what either of these is about.
    _OpenCodeRow("New session - 2026-07-20T03:00:00.000Z", "look at this error", "/Users/me/gamma"),
    _OpenCodeRow("New conversation", "carry on from yesterday", "/Users/me/gamma"),
    _OpenCodeRow(
        "an archived session", "this one is archived", "/Users/me/gamma", "build", None, True
    ),
    # Nothing a person said: every text part in it is tool plumbing.
    _OpenCodeRow("", "", "/Users/me/gamma"),
]


def _opencode_reply(text: str, images: int = 0) -> list[dict]:
    """One assistant turn as OpenCode stores it: the answer, and the machinery.

    ``synthetic`` text is the trap — it is stored as ordinary text and reads
    like the assistant talking, but it is the tool call and its output.
    """
    return [
        {"type": "step-start"},
        {"type": "reasoning", "text": "internal reasoning, never shown"},
        {"type": "text", "text": "Called the read tool", "synthetic": True},
        {"type": "tool", "tool": "read", "state": {"status": "completed"}},
        {"type": "text", "text": text},
        {"type": "step-finish"},
        *(
            {"type": "file", "mime": "image/png", "filename": f"shot-{i}.png"}
            for i in range(images)
        ),
    ]


def make_opencode_fixture() -> Path:
    """A synthetic OpenCode data directory, so reading needs no real database."""
    root = Path(tempfile.mkdtemp(prefix="asc-opencodefix-"))
    home = root / opencode_backend.DATA_DIR_NAME
    home.mkdir()
    ids = [f"ses_fixture{index:02d}" for index in range(len(_OPENCODE_FIXTURE))]
    start = datetime(2026, 7, 20, 15, 0)

    connection = sqlite3.connect(home / opencode_backend.DATABASE_FILE)
    try:
        with connection:
            connection.executescript(_OPENCODE_SCHEMA)
            for index, row in enumerate(_OPENCODE_FIXTURE):
                stamp = int((start - timedelta(hours=index)).timestamp() * 1000)
                parent = "ses_deletedparent" if row.parent == "gone" else None
                if isinstance(row.parent, int):
                    parent = ids[row.parent]
                connection.execute(
                    "INSERT INTO session"
                    " VALUES (?, 'proj_fixture', ?, ?, ?, '1.18.13', ?, ?, ?, ?)",
                    (ids[index], parent, row.cwd, row.title, row.agent, stamp, stamp + 60_000,
                     stamp + 120_000 if row.archived else None),
                )
                # A session with nothing said in it still has the tool traffic
                # around it, which is exactly what must not count as a message.
                asked = (
                    [{"type": "text", "text": row.opening}]
                    if row.opening
                    else [{"type": "text", "text": "<injected context>", "synthetic": True}]
                )
                turns = (
                    ("user", asked),
                    ("assistant", _opencode_reply(f"on it: {row.opening or '...'}", images=1)),
                )
                for turn, (role, parts) in enumerate(turns):
                    message_id = f"msg_{index:02d}_{turn}"
                    connection.execute(
                        "INSERT INTO message VALUES (?, ?, ?, ?)",
                        (message_id, ids[index], stamp + turn, json.dumps({"role": role})),
                    )
                    connection.executemany(
                        "INSERT INTO part VALUES (?, ?, ?, ?, ?)",
                        [
                            (f"prt_{index:02d}_{turn}_{order:02d}", message_id, ids[index],
                             stamp + turn, json.dumps(part))
                            for order, part in enumerate(parts)
                        ],
                    )
    finally:
        connection.close()
    return home


def _claude_transcript(
    session_id: str, *, cwd: str, turns: int = 2, preamble: int = 1, ai_title: str | None = None
) -> list[dict]:
    """A transcript shaped like the real thing: preamble, then the exchange.

    `preamble` controls how many metadata records sit in front of the first
    thing the user says — the knob that used to decide whether a session was
    mistaken for an empty one.
    """
    head = {"sessionId": session_id, "timestamp": datetime(2026, 7, 1, 9, 0).isoformat() + "Z"}
    records: list[dict] = [
        {"type": "mode", "mode": "normal", "sessionId": session_id},
        {"type": "permission-mode", "permissionMode": "default", "sessionId": session_id},
        *(
            {"type": "file-history-snapshot", "sessionId": session_id, "messageId": str(i)}
            for i in range(preamble)
        ),
    ]
    for i in range(turns):
        records += [
            {**head, "type": "user", "cwd": cwd, "version": "2.1.0", "entrypoint": "cli",
             "isSidechain": False,
             "message": {"role": "user", "content": f"question {i + 1}"}},
            {**head, "type": "assistant", "message": {"role": "assistant", "content": [
                {"type": "thinking", "thinking": "internal reasoning, never shown"},
                {"type": "text", "text": f"answer {i + 1}"},
                {"type": "tool_use", "id": "t1", "name": "Read", "input": {}},
            ]}},
            # Tool results come back as `user` records and must not be shown.
            {**head, "type": "user", "toolUseResult": {"stdout": "tool output"},
             "message": {"role": "user", "content": [{"type": "tool_result", "content": "x"}]}},
        ]
    if ai_title:
        records.append({"type": "ai-title", "aiTitle": ai_title, "sessionId": session_id})
    return records


def make_claude_home() -> Path:
    """A complete synthetic ~/.claude, so this section needs no real data."""
    home = Path(tempfile.mkdtemp(prefix="asc-claude-"))

    def add(project: str, records: list[dict], name: str | None = None) -> Path:
        path = home / "projects" / project / f"{name or uuid.uuid4()}.jsonl"
        _write_jsonl(path, records)
        return path

    add("-Users-me-alpha", _claude_transcript(str(uuid.uuid4()), cwd="/Users/me/alpha"))
    add(
        "-Users-me-alpha",
        _claude_transcript(str(uuid.uuid4()), cwd="/Users/me/alpha", ai_title="a title of its own"),
    )
    # One whose opening message sits past the head-scan window.
    add(
        "-Users-me-beta",
        _claude_transcript(
            str(uuid.uuid4()), cwd="/Users/me/beta", preamble=claude_backend.HEAD_SCAN_LINES + 50
        ),
    )
    # One with a sidecar directory holding a sub-agent transcript, which has to
    # be removed along with the session it belongs to.
    with_sidecar = str(uuid.uuid4())
    transcript = add(
        "-Users-me-beta", _claude_transcript(with_sidecar, cwd="/Users/me/beta"), with_sidecar
    )
    _write_jsonl(
        claude_backend.sidecar_of(transcript) / "subagents" / "agent-deadbeef.jsonl",
        _claude_transcript(str(uuid.uuid4()), cwd="/Users/me/beta"),
    )
    # A stray sub-agent transcript at project level: excluded by name.
    stray = _claude_transcript(str(uuid.uuid4()), cwd="/Users/me/beta")
    add("-Users-me-beta", stray, "agent-cafebabe")
    # Spare ordinary sessions. The mutation section works its way through the
    # list deleting things, and the checks after that still need rows to act on.
    for _ in range(2):
        add("-Users-me-gamma", _claude_transcript(str(uuid.uuid4()), cwd="/Users/me/gamma"))
    # Sessions opened and abandoned without saying anything — what `E` sweeps.
    for _ in range(3):
        session_id = str(uuid.uuid4())
        add(
            "-tmp-empty-project",
            [
                {"type": "mode", "mode": "normal", "sessionId": session_id},
                {"type": "permission-mode", "permissionMode": "default", "sessionId": session_id},
            ],
            session_id,
        )
    return home


# ========================================================= [4] localization


def test_localization() -> None:
    print("\n[4] Language detection and the Chinese catalogue")
    check(
        detect_language({"AGENT_SESSION_CLEANER_LANG": "en", "LANG": "zh_CN.UTF-8"}) == "en",
        "the dedicated variable wins over the system locale",
    )
    check(
        detect_language({"LC_ALL": "zh_CN.UTF-8", "LANG": "en_US.UTF-8"}) == "zh",
        "LC_ALL beats LANG",
    )
    check(detect_language({"LANG": "zh-Hans"}) == "zh", "a Chinese locale gets Chinese")
    check(detect_language({"LANG": "fr_FR.UTF-8"}) == "en", "anything else falls back to English")

    # This process is the English case, end to end: the interface, the dialogs
    # and the chooser were all built from the catalogue at import time.
    english = [app_module._HELP[0][0], app_module.confirm_danger()._title, AgentPicker.TITLE]
    check(
        english == ["Browse sessions", "Enable danger mode?", "Session Cleaner"],
        f"the running interface is in English: {english}",
    )
    sections = [
        (name, [(key, what) for key, what, _ in entries]) for name, entries in app_module._HELP
    ]
    check(
        app_module.HelpScreen(sections, "Shortcuts")._box_width > app_module.HELP_WIDTH_MIN,
        "the help box widens to fit longer English lines",
    )

    # The Chinese side runs in a subprocess of its own, since the language is
    # settled once at import. PYTHONIOENCODING keeps a POSIX-locale runner from
    # failing on the output rather than on the check.
    env = {
        **os.environ,
        "AGENT_SESSION_CLEANER_LANG": "zh-CN",
        "PYTHONPATH": str(ROOT / "src"),
        "PYTHONIOENCODING": "utf-8",
    }
    probe = subprocess.run(
        [sys.executable, "-c",
         "from agent_session_cleaner.app import _HELP, confirm_danger; "
         "from agent_session_cleaner.picker import AgentPicker; "
         "print(_HELP[0][0]); print(confirm_danger()._title); print(AgentPicker.TITLE)"],
        cwd=ROOT, env=env, text=True, encoding="utf-8", capture_output=True, check=False,
    )
    check(
        probe.returncode == 0
        and probe.stdout.split() == ["浏览会话", "开启危险模式？", "会话清理"],
        f"a Chinese locale gets Chinese: {probe.stdout.split()}",
    )
    for language, expected in (("en", ("usage:", "positional arguments", "options")),
                               ("zh-CN", ("用法：", "位置参数", "选项"))):
        help_text = subprocess.run(
            [sys.executable, "-m", "agent_session_cleaner", "--help"],
            cwd=ROOT, env={**env, "AGENT_SESSION_CLEANER_LANG": language},
            text=True, encoding="utf-8", capture_output=True, check=False,
        )
        check(
            help_text.returncode == 0 and all(word in help_text.stdout for word in expected),
            f"argparse's own labels follow the language too ({language})",
        )


# ================================================== [5] wording and gating


async def test_wording_and_missing_deps(
    codex_home: Path, claude_home: Path, opencode_home: Path
) -> None:
    print("\n[5] Per-agent wording and a missing command line")

    # The signature on a reply follows the agent; it must never be hardcoded.
    for backend, expected in (
        (ClaudeBackend(claude_home), "Claude"),
        (CodexBackend(codex_home), "Codex"),
        (OpenCodeBackend(opencode_home), "OpenCode"),
    ):
        async with _app(backend) as (app, pilot):
            # An empty session renders no replies at all, which would pass the
            # "no wrong signature" reading of this check for the wrong reason.
            app.query_one("#sessions").index = next(
                i for i, s in enumerate(app._sessions) if not s.noise
            )
            await pilot.pause(0.6)
            replies = [str(w.visual) for w in app.query(".msg.agent")]
            check(
                replies and all(r.lstrip().startswith(f"◀ {expected}") for r in replies),
                f"{backend.label} signs its replies “{expected}” ({len(replies)} shown)",
            )

    dialog = app_module.confirm_one(ClaudeBackend(claude_home).discover()[0])
    check(
        not any(
            "codex" in part.lower()
            for part in (dialog._title, dialog._body, dialog._confirm_label)
        ),
        f"the delete dialog names no particular agent: {dialog._body}",
    )
    # Only the copy is checked above; the subject carries the user's own title.
    leaked = [
        line.strip()
        for line in Path(app_module.__file__).read_text().splitlines()
        if any(name in line.lower() for name in ("codex", "claude", "opencode"))
        and '"' in line
        and not line.strip().startswith("#")
    ]
    check(not leaked, f"the shared interface hardcodes no agent name ({len(leaked)} lines)")

    empty = Path(tempfile.mkdtemp(prefix="asc-empty-"))
    try:
        # Without the command line: browsable, not changeable, and the keys that
        # would change something disappear rather than failing when pressed.
        saved = os.environ["PATH"]
        os.environ["PATH"] = str(empty / "nothing-here")
        try:
            backend = CodexBackend(codex_home)
            check(backend.missing_cli() == "codex", "a missing Codex command line is detected")
            check(
                ClaudeBackend(claude_home).missing_cli() is None,
                "Claude Code needs no command line",
            )
            async with _app(backend) as (app, pilot):
                check("CLI not found" in _status(app), f"said so on startup: {_status(app)[:48]}")
                check(
                    all(
                        app.check_action(action, ()) is False
                        for action in _WRITING_ACTIONS
                    ),
                    "every key that would change something is hidden",
                )
                check(
                    app._sessions and app.query("#detail-header").first() is not None,
                    f"still browsable ({len(app._sessions)} sessions)",
                )
                await pilot.press("d")
                await pilot.pause(0.3)
                check(not isinstance(app.screen, ConfirmScreen), "d opens no delete dialog")
        finally:
            os.environ["PATH"] = saved
    finally:
        shutil.rmtree(empty, ignore_errors=True)


async def test_copy_resume(codex_home: Path, claude_home: Path, opencode_home: Path) -> None:
    print("\n[6] Copying a resume command")
    fake = Session(
        backend="codex", path=Path("/x/y.jsonl"), session_id="abc-123", title="t", client="cli",
        updated_at=datetime(2026, 7, 1, 12, 0), size=0, cwd="/Users/me/My Projects/a b",
    )
    for backend, expected in (
        (CodexBackend(), "codex resume abc-123"),
        (ClaudeBackend(), "claude --resume abc-123"),
        (OpenCodeBackend(), "opencode -s abc-123"),
    ):
        check(backend.resume_command(fake) == expected, f"{backend.label}: {expected}")

    # A non-default home has to travel with the command, or it reopens nothing.
    elsewhere = Path(tempfile.mkdtemp(prefix="asc-home-"))
    try:
        for backend, variable in (
            (CodexBackend(elsewhere), "CODEX_HOME="),
            (ClaudeBackend(elsewhere), "CLAUDE_CONFIG_DIR="),
            (OpenCodeBackend(elsewhere), "XDG_DATA_HOME="),
        ):
            check(
                backend.resume_command(fake).startswith(variable),
                f"{backend.label} carries {variable.rstrip('=')}",
            )
    finally:
        shutil.rmtree(elsewhere, ignore_errors=True)
    quoted = f"cd {shlex.quote(fake.cwd)} && {CodexBackend().resume_command(fake)}"
    check(
        quoted == "cd '/Users/me/My Projects/a b' && codex resume abc-123",
        f"a directory with spaces is quoted: {quoted}",
    )

    # Point PATH away from any real clipboard helper: the OSC 52 fallback keeps
    # the test off the developer's own clipboard.
    saved = os.environ["PATH"]
    os.environ["PATH"] = "/nonexistent-for-tests"
    try:
        for backend, verb in (
            (CodexBackend(codex_home), "resume"),
            (ClaudeBackend(claude_home), "--resume"),
            (OpenCodeBackend(opencode_home), "-s"),
        ):
            async with _app(backend, size=(150, 30)) as (app, pilot):
                session = app._cursor()
                await pilot.press("c")
                await pilot.pause(0.3)
                copied = app._clipboard
                prefix = f"cd {shlex.quote(session.cwd)} && " if session.cwd else ""
                check(
                    copied.startswith(prefix) and verb in copied and session.session_id in copied,
                    f"{backend.label}: {copied}",
                )
                check("copied to clipboard" in _status(app), f"and said so: {_status(app)[:40]}")
    finally:
        os.environ["PATH"] = saved


# ==================================================== [7] browsing (read-only)


async def test_browsing(codex_home: Path) -> None:
    print("\n[7] Browsing and search")
    async with _app(CodexBackend(codex_home)) as (app, pilot):
        listing = app.query_one("#sessions")
        bar = app.query_one("#search-bar")
        total = len(app._sessions)
        check(
            total > 0 and app.query("#detail-header").first() is not None,
            f"listed {total} sessions with a detail pane beside them",
        )
        check(
            [r.has_class("-odd") for r in app.query(SessionRow)][:4] == [False, True, False, True],
            "rows alternate shading",
        )
        first = app._cursor()
        await pilot.press("j")
        await pilot.pause()
        check(app._cursor() is not first, "j moves down")
        await pilot.press("G")
        await pilot.pause()
        check(listing.index == total - 1, "G jumps to the bottom")
        await pilot.press("g")
        await pilot.pause()
        check(listing.index == 0, "g jumps back to the top")
        await pilot.press("tab")
        await pilot.pause()
        check(app.focused is app.query_one("#detail"), "Tab moves to the conversation")
        await pilot.press("tab")
        await pilot.pause()

        needle = "proxy"
        hits = sum(needle in app._haystack(s).lower() for s in app._sessions)
        check(1 < hits < len(app._sessions), f"“{needle}” matches {hits}/{len(app._sessions)}")

        await pilot.press("slash")
        await pilot.pause()
        check(bar.display and app.focused.id == "search-input", "/ opens the bar and takes focus")
        for char in needle:
            await pilot.press(char)
        await pilot.pause(0.3)
        matches = app._matches()
        check(
            len(matches) > 1 and listing.index in matches,
            f"incremental search landed on one of {len(matches)}",
        )

        await pilot.press("enter")
        await pilot.pause()
        check(
            not bar.display and app.focused.id == "sessions",
            "Enter closes the bar and gives focus back",
        )

        landed = listing.index
        await pilot.press("n")
        await pilot.pause()
        check(listing.index == matches[matches.index(landed) + 1], "n moves to the next match")
        await pilot.press("N")
        await pilot.pause()
        check(listing.index == landed, "N moves back")

        listing.index = matches[-1]
        await pilot.pause()
        await pilot.press("n")
        await pilot.pause()
        check(
            listing.index == matches[0] and "wrapped to top" in _status(app),
            f"n past the last match wraps and says so: {_status(app)}",
        )

        origin = next(i for i in range(len(app._sessions)) if i not in matches)
        listing.index = origin
        await pilot.pause()
        await pilot.press("slash")
        for char in needle:
            await pilot.press(char)
        await pilot.pause(0.3)
        check(listing.index != origin, "an incremental search moves the cursor")
        await pilot.press("escape")
        await pilot.pause()
        check(listing.index == origin, "Esc puts it back where it was")

        await pilot.press("slash")
        for char in "zzqzz":
            await pilot.press(char)
        await pilot.pause(0.3)
        check("No matches" in _status(app), f"no match says so: {_status(app)}")
        check(app.is_running, "typing q while searching does not quit")
        await pilot.press("escape")
        await pilot.pause()


async def test_tree_and_orphans(codex_home: Path) -> None:
    print("\n[8] Session tree and orphaned sub-agents")
    backend = CodexBackend(codex_home)
    found = backend.discover()
    by_title = {s.title: s for s in found}

    check(
        by_title["first side thread"].parent_id == by_title["fix proxy timeout"].session_id,
        "parent_thread_id names the source session",
    )
    check(
        by_title["side thread with no parent recorded"].parent_id is None
        and by_title["early side thread"].parent_id == by_title["add proxy retries"].session_id,
        "0.133.0 recorded no parent at all; 0.135 put it in forked_from_id",
    )
    check(
        by_title["forked ordinary session"].parent_id is None,
        "the same field on an ordinary session is a fork, not a parent",
    )
    child = by_title["first side thread"]
    check(
        child.session_id != child.parent_id and child.session_id in child.path.name,
        "a sub-agent keeps its own id rather than the parent's",
    )

    stranded = {s.title for s in model.orphans(found)}
    check(
        stranded == {"orphaned side thread", "another orphaned side thread"},
        f"only a genuinely missing parent counts as orphaned: {sorted(stranded)}",
    )
    check(
        "side thread with no parent recorded" not in stranded, "unrecorded is not the same as gone"
    )
    # Archiving only moves the file; the parent still exists.
    parent = by_title["fix proxy timeout"]
    archived_parent = Session(**{**parent.__dict__, "archived": True})
    others = [s for s in found if s.session_id != parent.session_id]
    check(
        not any(s.title == "first side thread" for s in model.orphans([archived_parent, *others])),
        "an archived parent does not strand its children",
    )

    laid_out = app_module._arrange(found, {s.session_id for s in model.orphans(found)})
    prefixes = {session.title: prefix for session, prefix in laid_out}
    order = [session.title for session, _ in laid_out]
    check(
        order.index("first side thread") == order.index("fix proxy timeout") + 1
        and order.index("second side thread") == order.index("fix proxy timeout") + 2,
        "children follow their parent immediately",
    )
    check(
        prefixes["fix proxy timeout"] == ""
        and prefixes["first side thread"] == app_module.TREE_BRANCH
        and prefixes["second side thread"] == app_module.TREE_LAST
        and prefixes["early side thread"] == app_module.TREE_LAST,
        "the parent sits flush left, the last child gets └─, older sub-agents join too",
    )
    check(
        prefixes["orphaned side thread"] == app_module.TREE_SEVERED
        and prefixes["side thread with no parent recorded"] == app_module.TREE_UNKNOWN,
        "gone and never-recorded are different markers, and neither is ordinary",
    )
    check(
        prefixes["weekly report"] == "" and prefixes["forked ordinary session"] == "",
        "ordinary sessions carry no connector",
    )
    check(
        len(laid_out) == len(found) and set(prefixes) == {s.title for s in found},
        f"nothing is lost in the arrangement ({len(laid_out)}/{len(found)})",
    )

    # Cascades: what one keypress really acts on, deepest first.
    app = SessionCleanerApp(backend)
    app._sessions = [s for s, _ in laid_out]
    chain = app._cascade("delete", parent)
    check(
        [s.title for s in chain]
        == ["first side thread", "second side thread", "fix proxy timeout"],
        f"sub-agents go before the session that spawned them: {[s.title for s in chain]}",
    )
    check(
        app._cascade("delete", by_title["weekly report"]) == [by_title["weekly report"]],
        "a session with no sub-agents cascades to itself",
    )
    check(
        len(app._cascade("unarchive", parent)) == 1,
        "unarchive only picks up children that are archived",
    )
    swept = app._with_sub_agents([parent, by_title["weekly report"]])
    check(
        len(swept) == 4 and len({s.session_id for s in swept}) == 4,
        f"a bulk sweep expands to {len(swept)} rows without duplicates",
    )

    def _fake(session_id: str, parent_id: str) -> Session:
        return Session(
            backend="codex", path=Path(f"/x/{session_id}.jsonl"), session_id=session_id,
            title=session_id, client="cli", updated_at=datetime(2026, 1, 1), size=0,
            parent_id=parent_id,
        )

    check(
        len(app_module._arrange([_fake("a", "b"), _fake("b", "a")], set())) == 2,
        "a parent cycle still lists",
    )
    check(
        not app_module._arrange([_fake("a",
        "a")], set())[0][1], "a self-parented row is a root, not a child",
    )

    async with _app(backend, force_cli=True) as (app, pilot):
        check(
            "2 orphaned sub-agent sessions" in _banner(app),
            f"the banner counts orphans: {_banner(app)}",
        )
        rows = _rows(app)
        check(
            app_module.TREE_BRANCH in str(rows["first side thread"]._label.visual),
            "connectors are drawn",
        )
        marked = {title for title, row in rows.items() if row.has_class("-orphan")}
        check(marked == stranded, f"only orphans get the orphan style: {sorted(marked)}")
        check(not any(r.has_class("-archived") for r in rows.values()), "orphaned is not archived")

        for title, expected in (
            ("orphaned side thread", "Source session deleted"),
            ("first side thread", "Source session:"),
            ("side thread with no parent recorded", "not recorded"),
            ("weekly report", None),
        ):
            await _goto(pilot, app, title)
            await pilot.pause(0.3)
            header = str(app.query_one("#detail-header").visual)
            if expected is None:
                check(
                    "Source session" not in header,
                    f"an ordinary session mentions no source: {title}",
                )
            else:
                check(expected in header, f"the detail pane says where “{title}” came from")

        await pilot.press("O")
        if check(await _dialog(pilot, app), "O opens a confirmation"):
            headline = str(app.screen._subject).splitlines()[0]
            check(
                "2 orphaned sub-agent sessions selected" in headline,
                f"naming only orphans: {headline}",
            )
            check("source sessions were deleted" in app.screen._body, "and saying why they can go")
            await pilot.press("escape")
            await pilot.pause()
        check(all(s.path.exists() for s in found), "cancelling touched nothing")


async def test_multi_select(codex_home: Path) -> None:
    """Space picks sessions out; the keys then act on all of them at once."""
    print("\n[9] Multi-select")
    async with _app(CodexBackend(codex_home), size=(110, 40), force_cli=True) as (app, pilot):
        banner = app.query_one("#banner")
        check(not app._selection, "not in multi-select on startup")
        check(
            any(v.startswith("␣ ") for v in _footer(app)[1]),
            f"the way in is advertised: {_footer(app)[1]}",
        )

        parent, kids = "fix proxy timeout", {"first side thread", "second side thread"}
        plain_style = _rows(app)[parent]._label.styles.text_style
        await _goto(pilot, app, parent)
        await pilot.press("space")
        await pilot.pause(0.3)
        check(
            _picked(app) == {parent, *kids},
            f"picking a parent picks its sub-agents: {sorted(_picked(app))}",
        )
        check(banner.has_class("-select"), "the banner takes on the multi-select colour")
        check(
            "Multi-select" in _banner(app) and "3 sessions" in _banner(app),
            f"and says what is going on: {_banner(app)}",
        )
        marked = {title for title, row in _rows(app).items() if row.has_class("-selected")}
        check(marked == _picked(app), f"picked rows are marked: {sorted(marked)}")
        picked_style = _rows(app)[parent]._label.styles.text_style
        check(
            picked_style.bold and not plain_style.bold,
            f"and rendered in bold ({plain_style!r} → {picked_style!r})",
        )

        top, bottom = _footer(app)
        check(
            all(not v.startswith(("c ", "! ", "D ", "O ", "E ")) for v in top),
            f"copy, danger mode and the whole-list sweeps step aside: {top}",
        )
        check(
            all(
                any(word in v for v in top)
                for word in ("Delete selected", "Archive selected", "Unarchive selected")
            ),
            f"what remains says it acts on the selection: {top}",
        )
        check(
            any(v.startswith("␣ ") and "Select / deselect" in v for v in bottom),
            f"space changes its wording: {bottom}",
        )
        check(
            all(
                app.check_action(action, ()) is False
                for action in ("copy_resume", "toggle_danger", "delete_archived", "delete_orphans")
            ),
            "the hidden keys are genuinely off, not merely unlabelled",
        )
        listed = {key for _, entries in app.help_sections() for key, _ in entries}
        check("c" not in listed and "␣" in listed, f"the shortcut list follows: {sorted(listed)}")

        await pilot.press("space")
        await pilot.pause(0.3)
        check(
            not app._selection and not banner.has_class("-select"),
            "space again releases the whole subtree",
        )

        row = _rows(app)[parent]
        await pilot.click(row, offset=(3, 0), times=2)
        await pilot.pause(0.4)
        check(_picked(app) == {parent, *kids}, "a left double-click does what space does")
        await pilot.click(row, offset=(3, 0), times=2)
        await pilot.pause(0.4)
        check(not app._selection, "double-clicking again releases it")
        await pilot.click(row, offset=(3, 0))
        await pilot.pause(0.4)
        check(
            not app._selection and app._cursor().title == parent,
            "a single click only moves the cursor",
        )

        # A sub-agent cannot be spared while the session it hangs from is going,
        # so releasing one releases that session too — and nothing is ever acted
        # on that the rows did not show as picked.
        await _goto(pilot, app, parent)
        await pilot.press("space")
        await pilot.pause(0.3)
        await _goto(pilot, app, "first side thread")
        await pilot.press("space")
        await pilot.pause(0.3)
        check(
            _picked(app) == {"second side thread"},
            f"releasing a sub-agent releases what would drag it back, and no more: "
            f"{sorted(_picked(app))}",
        )
        picked = [s for s in app._sessions if s.session_id in app._selection]
        acted_on = {s.title for s in app._with_sub_agents(picked)}
        check(
            acted_on == _picked(app),
            f"what the keys act on is exactly what is marked: {sorted(acted_on)}",
        )
        await pilot.press("escape")
        await pilot.pause(0.3)

        for title in (parent, "weekly report", "orphaned side thread"):
            await _goto(pilot, app, title)
            await pilot.press("space")
            await pilot.pause(0.2)
        check(
            len(app._selection) == 5,
            f"orphans and ordinary sessions mix freely ({len(app._selection)})",
        )

        await pilot.press("d")
        if check(await _dialog(pilot, app), "d opens a confirmation for the selection"):
            headline = str(app.screen._subject).splitlines()[0]
            check(
                app.screen._title == "Delete the selected sessions?"
                and "5 sessions selected" in headline,
                f"counting what will actually go: {app.screen._title} / {headline}",
            )
            await pilot.press("escape")
            await pilot.pause(0.3)
        check(len(app._selection) == 5, "cancelling keeps the selection")

        await pilot.press("escape")
        await pilot.pause(0.3)
        check(
            not app._selection and "Selection cleared" in _status(app),
            f"Esc leaves multi-select: {_status(app)}",
        )
        check(
            not any(r.has_class("-selected") for r in app.query(SessionRow)), "and clears the marks"
        )

        # Both modes at once: the more alarming one keeps the banner.
        app._danger = True
        await _goto(pilot, app, parent)
        await pilot.press("space")
        await pilot.pause(0.3)
        check(
            banner.has_class("-danger") and not banner.has_class("-select"),
            "danger mode keeps the banner while a selection stands",
        )
        await pilot.press("escape")
        await pilot.pause(0.2)
        check(not app._danger and app._selection, "Esc backs out of the more alarming one first")
        await pilot.press("escape")
        await pilot.pause(0.2)
        check(not app._selection, "and only then out of multi-select")


async def test_opencode_sessions(home: Path) -> None:
    print("\n[10] OpenCode sessions (synthetic database)")
    backend = OpenCodeBackend(home)
    found = backend.discover()
    by_title = {s.title: s for s in found}
    check(len(found) == len(_OPENCODE_FIXTURE), f"read {len(found)} sessions")

    check(
        "look at this error" in by_title
        and "carry on from yesterday" in by_title
        and not any(opencode_backend._is_placeholder(s.title) for s in found),
        "both spellings of a placeholder title give way to the opening message",
    )
    silent = [s for s in found if s.noise]
    check(
        len(silent) == 1 and silent[0].title == "(Empty session)",
        f"only a session nobody spoke in counts as empty ({len(silent)})",
    )
    check(
        by_title["an archived session"].archived and sum(s.archived for s in found) == 1,
        "archived means time_archived is set",
    )
    check(
        by_title["trace the timeout"].client == "explore"
        and by_title["weekly report"].client == backend.default_client,
        "the client column shows the agent the session ran under",
    )
    check(
        by_title["trace the timeout"].parent_id == by_title["fix proxy timeout"].session_id
        and by_title["weekly report"].parent_id is None
        and not by_title["weekly report"].side_thread,
        "parent_id decides what is a sub-agent",
    )
    check(all(s.cwd and s.version == "1.18.13" for s in found), "directory and version are read")
    check(
        {s.title for s in model.orphans(found)} == {"sub-agent left behind"},
        "only a genuinely missing parent counts as orphaned",
    )

    stranded_ids = {s.session_id for s in model.orphans(found)}
    prefixes = {
        session.title: prefix for session, prefix in app_module._arrange(found, stranded_ids)
    }
    check(
        prefixes["fix proxy timeout"] == ""
        and prefixes["trace the timeout"] == app_module.TREE_BRANCH
        and prefixes["read the retry logic"] == app_module.TREE_LAST
        and prefixes["sub-agent left behind"] == app_module.TREE_SEVERED,
        "sub-agents hang under their own session, and an orphan hangs from a broken one",
    )
    check(
        app_module.TREE_UNKNOWN not in prefixes.values(),
        "OpenCode always records a parent, so there is no unknown case",
    )

    messages = backend.load_messages(by_title["weekly report"])
    joined = "\n".join(m.text for m in messages)
    check(
        [m.role for m in messages] == ["user", "agent"],
        f"one exchange becomes two messages: {[m.role for m in messages]}",
    )
    check(
        "Called the read tool" not in joined and "internal reasoning" not in joined,
        "tool plumbing and reasoning stay out of the conversation",
    )
    check(
        "[1 image]" in messages[1].text
        and not any(m.role == "user" for m in backend.load_messages(silent[0])),
        "an attachment is noted, and a session nobody spoke in has no user message",
    )

    # An older database simply does not have some of today's columns.
    with sqlite3.connect(f"{backend.database.resolve().as_uri()}?mode=ro", uri=True) as connection:
        bare = opencode_backend.load_session(
            connection,
            {"id": "ses_old", "title": "written by an older release",
             "directory": "/Users/me/alpha", "time_created": 1_700_000_000_000,
             "time_updated": 1_700_000_060_000},
            backend.database, 0,
        )
    check(
        bare is not None and bare.client == opencode_backend.DEFAULT_AGENT
        and not bare.archived and bare.parent_id is None,
        "missing columns fall back to defaults",
    )
    # A database missing the table the size query needs must still list.
    crippled = Path(tempfile.mkdtemp(prefix="asc-oc-bare-")) / opencode_backend.DATA_DIR_NAME
    crippled.mkdir(parents=True)
    try:
        with sqlite3.connect(crippled / opencode_backend.DATABASE_FILE) as connection:
            connection.executescript(_OPENCODE_SCHEMA.split("CREATE TABLE message")[0])
            connection.execute(
                "INSERT INTO session"
                " VALUES ('ses_x', 'p', NULL, '/x', 'still listed', NULL, NULL, 1, 2, NULL)"
            )
        listed = OpenCodeBackend(crippled).discover()
        check(
            [s.title for s in listed] == ["still listed"],
            f"a database without a part table still lists its sessions ({len(listed)})",
        )
    finally:
        shutil.rmtree(crippled.parent, ignore_errors=True)
    broken = Path(tempfile.mkdtemp(prefix="asc-oc-none-"))
    try:
        check(
            OpenCodeBackend(broken).discover() == [],
            "a home with no database lists nothing rather than failing",
        )
        (broken / opencode_backend.DATABASE_FILE).write_text("not a database", encoding="utf-8")
        check(OpenCodeBackend(broken).discover() == [], "and neither does a corrupt one")
    finally:
        shutil.rmtree(broken, ignore_errors=True)

    check(
        backend.resume_command(found[0])
        == f"XDG_DATA_HOME={shlex.quote(str(home.parent))} opencode -s {found[0].session_id}",
        "XDG_DATA_HOME points at the parent, which is how the command resolves it",
    )
    # A directory by another name can be read, but XDG_DATA_HOME would resolve
    # to a sibling of it — so the command line is never pointed there at all.
    elsewhere = OpenCodeBackend(home.parent / "somewhere-else")
    check(
        backend.home_problem() is None and elsewhere.home_problem(),
        f"a differently named data directory is refused: {elsewhere.home_problem()}",
    )
    stray = await elsewhere.delete(found[0])
    check(
        not stray.ok and "opencode" in stray.message,
        f"and deletion refuses it too: {stray.message[:40]}",
    )
    refused = subprocess.run(
        [sys.executable, "-m", "agent_session_cleaner", "opencode",
         "--opencode-home", str(home.parent)],
        cwd=ROOT,
        env={**os.environ, "PYTHONPATH": str(ROOT / "src"), "PYTHONIOENCODING": "utf-8"},
        text=True, encoding="utf-8", capture_output=True, check=False,
    )
    check(
        refused.returncode != 0 and "must be named" in refused.stderr,
        f"and startup says so rather than listing sessions first: {refused.stderr.strip()[-60:]}",
    )
    check(
        not (await backend.archive(found[0])).ok and not backend.supports_archive,
        "there is no archive command to delegate to, and it says so",
    )

    async with _app(backend, force_cli=True) as (app, pilot):
        check("11 sessions" in _banner(app), f"the banner counts sessions: {_banner(app)}")
        check(
            "1 archived" in _banner(app) and "1 orphaned sub-agent session" in _banner(app),
            f"and archived and orphaned separately: {_banner(app)}",
        )
        check(
            all(
                app.check_action(action, ()) is False
                for action in ("archive", "unarchive", "delete_archived", "delete_empty")
            ),
            "archiving and empty-session keys are hidden for OpenCode",
        )
        check(
            app.check_action("delete_orphans", ()) is not False
            and app.check_action("delete", ()) is not False,
            "deleting and orphan cleanup remain available",
        )
        rows = _rows(app)
        check(
            {t for t, r in rows.items() if r.has_class("-archived")} == {"an archived session"}
            and {t for t, r in rows.items() if r.has_class("-orphan")} == {"sub-agent left behind"},
            "archived and orphaned are two states with two styles, not one",
        )
        check(
            "explore · " in str(rows["trace the timeout"]._label.visual),
            "a non-default agent is badged on the row",
        )

        # Nothing here has an unarchive key, so nothing should be told to press
        # one. The row still says it is archived; only the advice is dropped.
        saved_path = os.environ["PATH"]
        os.environ["PATH"] = "/nonexistent-for-tests"
        try:
            await _goto(pilot, app, "an archived session")
            await pilot.press("c")
            await pilot.pause(0.3)
        finally:
            os.environ["PATH"] = saved_path
        check(
            "copied to clipboard" in _status(app) and "unarchive" not in _status(app),
            f"an archived session is not told to press a key it does not have: {_status(app)[:60]}",
        )

        await _goto(pilot, app, "sub-agent left behind")
        await pilot.pause(0.3)
        check(
            "Source session deleted" in str(app.query_one("#detail-header").visual),
            "the detail pane says the parent is gone",
        )
        await pilot.press("O")
        if check(await _dialog(pilot, app), "O opens a confirmation"):
            check(
                "1 orphaned sub-agent session selected" in str(app.screen._subject).splitlines()[0],
                "naming only the orphan",
            )
            await pilot.press("escape")
            await pilot.pause()
    check(len(backend.discover()) == len(found), "the whole section only read")


async def test_opencode_locking(home: Path) -> None:
    """A busy database is a queue, not an answer.

    SQLite takes one writer at a time. A batch of our own deletions, or an
    OpenCode window open elsewhere, can hold the lock for longer than
    OpenCode's own five-second wait, and the command comes back saying so.
    """
    print("\n[11] OpenCode: a busy database")
    backend = OpenCodeBackend(home)
    session = backend.discover()[0]
    saved_run, saved_delays = opencode_backend.cli.run, opencode_backend.LOCK_RETRY_DELAYS
    opencode_backend.LOCK_RETRY_DELAYS = (0.01, 0.01, 0.01)
    attempts: list[tuple[str, ...]] = []
    try:
        raw = (
            b"\x1b[91m\x1b[1mError: \x1b[0mUnexpected error\n\n"
            b'Failed query: insert into "project" (...)\nparams: project-id,/worktree\n'
        )
        check(
            opencode_backend.cli._complaint(raw) == "Error: Unexpected error",
            "OpenCode's generic database error is kept instead of its trailing parameters",
        )

        answers = [
            model.OpResult(False, "Error: Unexpected error"),
            model.OpResult(False, "Error: Unexpected error"),
            model.OpResult(True, ""),
        ]

        async def flaky(binary, args, **kwargs):
            attempts.append(tuple(args))
            return answers.pop(0)

        opencode_backend.cli.run = flaky
        result = await backend.delete(session)
        check(
            result.ok and len(attempts) == 3,
            f"OpenCode's generic database error is retried, succeeding on attempt {len(attempts)}",
        )

        attempts.clear()

        async def refuses(binary, args, **kwargs):
            attempts.append(tuple(args))
            return model.OpResult(False, "Error: Session not found: x")

        opencode_backend.cli.run = refuses
        result = await backend.delete(session)
        check(
            not result.ok and len(attempts) == 1,
            f"any other refusal is asked only once ({len(attempts)})",
        )

        # Reported failure, row already gone: the database is what settles it.
        gone = backend.discover()[-1]

        async def lies(binary, args, **kwargs):
            return model.OpResult(False, "Error: database is locked")

        opencode_backend.cli.run = lies
        try:
            backend._present = lambda session_id: session_id != gone.session_id
            result = await backend.delete(gone)
            check(result.ok, f"a failure reported after the row went is still a success: {result}")

            # A database that cannot be asked has not said the session is gone.
            backend._present = lambda session_id: True
            result = await backend.delete(gone)
            check(
                not result.ok and "locked" in result.message,
                f"a database that stays locked eventually reports it: {result.message}",
            )
        finally:
            del backend._present
    finally:
        opencode_backend.cli.run = saved_run
        opencode_backend.LOCK_RETRY_DELAYS = saved_delays

    check(
        backend.bulk_concurrency >= 1,
        f"the backend declares its own batch limit ({backend.bulk_concurrency})",
    )
    check(len(backend.discover()) == len(_OPENCODE_FIXTURE), "nothing was actually deleted")


async def test_picker(codex_home: Path, claude_home: Path, opencode_home: Path) -> None:
    print("\n[12] Agent chooser")
    homes = {"codex": codex_home, "claude": claude_home, "opencode": opencode_home}
    expected = {key: len(BACKEND_CLASSES[key](home).discover()) for key, home in homes.items()}

    app = AgentPicker(homes)
    # The first agent's store is made unreadable: counting the rest must carry
    # on regardless, rather than leaving them on "Loading…" for good.
    broken = BACKEND_CLASSES[BACKEND_IDS[0]]
    original_discover = broken.discover
    broken.discover = lambda self: (_ for _ in ()).throw(RuntimeError("unreadable"))
    try:
        async with app.run_test(size=(90, 24)) as pilot:
            await pilot.pause()
            await pilot.pause(0.6)
            labels = [str(r._label.visual) for r in app.query(AgentRow)]
            check(
                "0 sessions" in labels[0] and all("Loading" not in v for v in labels),
                f"an unreadable store does not strand the other rows: {labels[0].strip()}",
            )
            await pilot.press("q")
    finally:
        broken.discover = original_discover

    app = AgentPicker(homes)
    async with app.run_test(size=(90, 24)) as pilot:
        await pilot.pause()
        await pilot.pause(0.6)
        rows = list(app.query(AgentRow))
        check(
            [r.backend.id for r in rows] == list(BACKEND_IDS),
            f"lists every agent: {[r.backend.id for r in rows]}",
        )
        check(
            all(f"{expected[r.backend.id]} sessions" in str(r._label.visual) for r in rows),
            f"counts them in the background: {list(expected.values())}",
        )
        check(
            [r.shortcut for r in rows] == ["x", "c", "o"],
            f"one shortcut each: {[r.shortcut for r in rows]}",
        )
        check(
            all(r.outer_size.height == 1 and r.styles.margin.bottom == 1 for r in rows),
            "one line per agent, with a blank line between",
        )
        await pilot.press("q")
    check(app.return_value is None, "q leaves without choosing")

    routes = (
        (("x",), "codex"), (("c",), "claude"), (("o",), "opencode"), (("j", "enter"), "claude")
    )
    for keys, chosen in routes:
        picker = AgentPicker(homes)
        async with picker.run_test(size=(90, 24)) as pilot:
            await pilot.pause()
            for key in keys:
                await pilot.press(key)
            await pilot.pause(0.3)
        check(picker.return_value == chosen, f"{' + '.join(keys)} chooses {chosen}")

    # Nothing to browse: say so rather than opening an empty screen.
    empty = Path(tempfile.mkdtemp(prefix="asc-none-"))
    try:
        picker = AgentPicker({key: empty / f"no-{key}" for key in BACKEND_IDS})
        async with picker.run_test(size=(100, 20)) as pilot:
            await pilot.pause()
            await pilot.pause(0.4)
            rows = list(picker.query(AgentRow))
            check(
                not any(r.usable for r in rows)
                and all("Data directory not found" in str(r._label.visual) for r in rows),
                "an agent with no data directory says so",
            )
            await pilot.press("x")
            await pilot.pause(0.2)
            hint = str(picker.query_one("#picker-hint").visual)
            check(
                picker.is_running and "data directory not found" in hint,
                f"and its shortcut explains itself rather than doing nothing: {hint.strip()}",
            )
            await pilot.press("q")
        check(picker.return_value is None, "still no agent chosen")
    finally:
        shutil.rmtree(empty, ignore_errors=True)


# ============================================================ [13] mutations


def make_codex_home(count: int = 6) -> Path | None:
    """Copy real rollouts into a scratch CODEX_HOME.

    These have to be real: the section drives the actual `codex` command, and
    the command is entitled to be picky about what it will archive or delete.
    """
    if shutil.which(codex_backend.CODEX_BIN) is None:
        return None
    real = codex_backend.default_home()
    if not (real / "sessions").is_dir():
        return None
    candidates = sorted(
        (real / "sessions").rglob("*.jsonl"), key=lambda p: p.stat().st_mtime, reverse=True
    )
    picked = [
        p for p in candidates
        if (s := codex_backend.load_session(p, archived=False, names={})) and not s.noise
    ][:count]
    if len(picked) < 4:  # the section archives, unarchives and deletes several
        return None

    kept = {codex_backend.load_session(p, archived=False, names={}).session_id for p in picked}
    orphan, family = None, []
    for path in candidates:
        session = codex_backend.load_session(path, archived=False, names={})
        if session is None or not session.parent_id:
            continue
        if session.parent_id in kept:
            # The sub-agents *of* the picked sessions, so the cascade has
            # something real to cascade to.
            family.append(path)
        elif orphan is None:
            # And one sub-agent whose parent is deliberately left behind, so `O`
            # really does have an orphan to sweep — and the command line has to
            # agree that a sub-agent id is deletable.
            orphan = path

    home = Path(tempfile.mkdtemp(prefix="asc-codex-"))
    for path in [*picked, *family, *filter(None, [orphan])]:
        destination = home / path.relative_to(real)
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, destination)
    return home


async def test_codex_mutations(home: Path) -> None:
    print("\n[14] Codex changes (throwaway CODEX_HOME, real command line)")
    async with _app(CodexBackend(home)) as (app, pilot):
        listing = app.query_one("#sessions")
        total = len(app._sessions)
        check(total > 0, f"listed {total} sessions")

        target = app._cursor()
        await pilot.press("a")
        await _settle(pilot, app)
        archived_dir = home / "archived_sessions"
        check(
            bool(list(archived_dir.glob(f"*{target.session_id}.jsonl"))),
            f"a archives: {_status(app)}",
        )
        check(len(app._sessions) == total, "an archived session stays in the list, greyed out")
        check(
            all(r.has_class("-archived") for r in app.query(SessionRow) if r.session.archived),
            "archived rows carry the archived class",
        )
        await _goto_id(pilot, app, target.session_id)
        await pilot.press("u")
        await _settle(pilot, app)
        check(not list(archived_dir.glob(f"*{target.session_id}.jsonl")), "u unarchives")
        check(app.check_action("delete_empty", ()) is False, "E is hidden for Codex")
        check(app.check_action("delete_orphans", ()) is not False, "O is available for Codex")

        # The cascade: the Codex command only ever touches one session, which is
        # how orphans arise in the first place.
        parents = [s for s in app._sessions if app._descendants(s)]
        if not parents:
            skip("no session with sub-agents in the sample")
        else:
            head = parents[0]
            kids = app._descendants(head)
            listing.index = app._sessions.index(head)
            await pilot.pause()
            await pilot.press("a")
            await _settle(pilot, app)
            ids = {k.session_id for k in kids}
            check(
                sum(s.session_id in ids and s.archived for s in app._sessions) == len(kids),
                f"a archives the sub-agents too: {_status(app)}",
            )
            check(
                f"including {len(kids)} sub-agent session" in _status(app),
                f"and says how many: {_status(app)}",
            )
            await _goto_id(pilot, app, head.session_id)
            await pilot.press("u")
            await _settle(pilot, app)
            check(
                sum(s.session_id in ids and not s.archived for s in app._sessions) == len(kids),
                f"u brings them all back: {_status(app)}",
            )

            await _goto_id(pilot, app, head.session_id)
            await pilot.press("d")
            await pilot.pause()
            check(
                f"also deletes {len(kids)} descendant sub-agent session" in app.screen._body,
                f"the dialog says what rides along: {app.screen._body}",
            )
            await pilot.press("y")
            await _settle(pilot, app)
            check(
                not head.path.exists() and not any(k.path.exists() for k in kids),
                f"d takes the sub-agents with it, leaving no orphan: {_status(app)}",
            )
            check(not model.orphans(app._sessions) or head.session_id not in
                  {s.parent_id for s in model.orphans(app._sessions)}, "and made no new orphan")

        stranded = model.orphans(app._sessions)
        if not stranded:
            skip("no orphaned sub-agent in the sample")
        else:
            await pilot.press("O")
            await pilot.pause()
            check(isinstance(app.screen, ConfirmScreen), "O opens a confirmation")
            await pilot.press("y")
            await _settle(pilot, app)
            check(
                not any(s.path.exists() for s in stranded),
                f"O deleted {len(stranded)} orphans: {_status(app)}",
            )
            await pilot.press("O")
            await pilot.pause()
            check(
                not isinstance(app.screen, ConfirmScreen) and "No orphaned" in _status(app),
                f"and says so once there are none: {_status(app)}",
            )

        # Multi-select against the real command line, one process per session.
        listing.index = 0
        await pilot.pause()
        await pilot.press("space")
        await pilot.pause(0.2)
        await pilot.press("j")
        await pilot.pause(0.2)
        await pilot.press("space")
        await pilot.pause(0.2)
        chosen = set(app._selection)
        check(len(chosen) >= 2, f"picked {len(chosen)} for a batch")
        await pilot.press("a")
        await _settle(pilot, app)
        check(
            chosen <= {s.session_id for s in app._sessions if s.archived} and not app._selection,
            f"a archives the lot and leaves multi-select: {_status(app)}",
        )
        for session_id in chosen:
            await _goto_id(pilot, app, session_id)
            await pilot.press("space")
            await pilot.pause(0.2)
        await pilot.press("a")
        await pilot.pause(0.3)
        check(
            "already archived" in _status(app) and app._selection,
            f"archiving what is already archived says so, without asking: {_status(app)}",
        )
        await pilot.press("u")
        await _settle(pilot, app)
        check(
            not (chosen & {s.session_id for s in app._sessions if s.archived}),
            f"u unarchives the selection: {_status(app)}",
        )

        victim = app._cursor()
        await pilot.press("d")
        # No settle: focus must be on Cancel in the very first frame, or the
        # delete button visibly flashes as focused on a dialog whose whole point
        # is that you do not hit delete by accident.
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "d opens a confirmation")
        check(
            app.focused.id == "confirm-no",
            f"with Cancel focused from the first frame ({app.focused.id})",
        )
        await pilot.press("escape")
        await pilot.pause()
        check(victim.path.exists(), "Esc leaves the file alone")
        await pilot.press("d")
        await pilot.pause()
        await pilot.press("y")
        await _settle(pilot, app)
        check(not victim.path.exists(), f"y deletes: {_status(app)}")
        check(app._cursor() is not None, "the cursor lands on a neighbouring row")

        # `D` is Codex's alone, and a whole-list delete must keep asking even
        # where a single one no longer does. Danger mode itself is covered
        # against Claude Code, which needs no command line to run anywhere.
        await pilot.press("exclamation_mark")
        await pilot.pause()
        await pilot.press("y")
        await pilot.pause(0.3)
        check(app._danger, "danger mode on")
        await pilot.press("a")
        await _settle(pilot, app)
        check(any(s.archived for s in app._sessions), "a still archives in danger mode")
        await pilot.press("D")
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "D still asks, even in danger mode")
        await pilot.press("escape")
        await pilot.pause()


async def test_bulk_concurrency(codex_home: Path) -> None:
    """Batches run several at a time, without ever passing a session's own kin.

    The backend is stubbed: this is about how a batch is scheduled, and a stub
    makes the overlap and the ordering observable without a real command line.
    """
    print("\n[13] Batch scheduling")
    backend = CodexBackend(codex_home)
    running = peak = 0
    each = 0.05
    #: ("start"|"end", session id, when) for every call the worker made.
    timeline: list[tuple[str, str, float]] = []

    async def stub_delete(session: Session):
        nonlocal running, peak
        running += 1
        peak = max(peak, running)
        timeline.append(("start", session.session_id, time.monotonic()))
        await asyncio.sleep(each)
        timeline.append(("end", session.session_id, time.monotonic()))
        running -= 1
        return model.OpResult(True, "")

    backend.delete = stub_delete
    async with _app(backend, force_cli=True) as (app, pilot):
        families = {
            s.session_id: [k.session_id for k in app._descendants(s)]
            for s in app._sessions
            if app._descendants(s)
        }
        check(bool(families), f"the sample has {len(families)} sessions with sub-agents")

        app._selection = {s.session_id for s in app._sessions}
        app._selection_changed()
        total = len(app._selection)
        await pilot.press("d")
        check(await _dialog(pilot, app), "select-all then d opens a confirmation")
        await pilot.press("y")
        await _settle(pilot, app)

        finished = [session_id for kind, session_id, _ in timeline if kind == "end"]
        check(len(finished) == total, f"all {total} were acted on ({len(finished)})")
        check(
            1 < peak <= backend.bulk_concurrency,
            f"concurrent, within the declared limit of {backend.bulk_concurrency} (peak {peak})",
        )
        # First start to last end, so the harness's own polling is not counted.
        span = max(m for kind, _, m in timeline if kind == "end") - min(
            m for kind, _, m in timeline if kind == "start"
        )
        check(
            span < total * each * 0.7,
            f"{total} took {span:.2f}s against {total * each:.2f}s one at a time",
        )

        starts = {sid: i for i, (kind, sid, _) in enumerate(timeline) if kind == "start"}
        ends = {sid: i for i, (kind, sid, _) in enumerate(timeline) if kind == "end"}
        late = [(p, k) for p, kin in families.items() for k in kin if ends[k] > starts[p]]
        check(
            not late, f"no session starts before its own sub-agents are done ({len(late)} overlaps)"
        )

    sessions = CodexBackend(codex_home).discover()
    parents = {s.session_id: s.parent_id for s in sessions}
    parent = next(s for s in sessions if s.title == "fix proxy timeout")
    kin = [s for s in sessions if s.parent_id == parent.session_id]
    stranger = next(s for s in sessions if s.title == "weekly report")
    # Hand the parent in first on purpose; it still has to come out last.
    branches = app_module._branches([parent, *kin, stranger], parents)
    together = next(b for b in branches if parent in b)
    check(
        len(branches) == 2 and [stranger] in branches,
        f"unrelated sessions form their own group ({len(branches)})",
    )
    check(
        together[-1] is parent and set(together[:-1]) == set(kin),
        f"one branch per family, parent last: {[s.title for s in together]}",
    )

    # A generation left out of the batch — an already-archived session that
    # archiving would skip — must not make a grandparent and grandchild look
    # unrelated, which would let them run at the same time.
    def _kin(session_id: str, parent_id: str | None) -> Session:
        return Session(
            backend="codex", path=Path(f"/x/{session_id}"), session_id=session_id,
            title=session_id, client="cli", updated_at=datetime(2026, 1, 1), size=0,
            parent_id=parent_id,
        )

    generations = {"P": None, "C": "P", "G": "C"}
    gapped = app_module._branches([_kin("G", "C"), _kin("P", None)], generations)
    check(
        len(gapped) == 1 and [s.session_id for s in gapped[0]] == ["G", "P"],
        f"a skipped middle generation stays one branch, deepest first: "
        f"{[[s.session_id for s in b] for b in gapped]}",
    )

    # Corrupt cycles have no valid child-first order, but they still belong to
    # one family and must not be changed concurrently.
    cycle_parents = {"A": "B", "B": "A", "C": "A"}
    cycle = app_module._branches(
        [_kin("A", "B"), _kin("B", "A"), _kin("C", "A")], cycle_parents
    )
    check(
        len(cycle) == 1 and {s.session_id for s in cycle[0]} == {"A", "B", "C"},
        f"a corrupt parent cycle stays in one sequential group: "
        f"{[[s.session_id for s in b] for b in cycle]}",
    )


def make_opencode_home() -> Path | None:
    """Copy the real database into a scratch data directory.

    It has to be the real one: this section drives ``opencode session delete``,
    which opens the database with its own migrations and is entitled to refuse
    anything it does not recognise. The copy is taken with SQLite's own backup
    so a running OpenCode instance cannot be disturbed by it.
    """
    if shutil.which(opencode_backend.OPENCODE_BIN) is None:
        return None
    source = OpenCodeBackend().database
    if not source.is_file():
        return None
    # The command resolves its data directory as XDG_DATA_HOME/opencode, so the
    # scratch home has to sit inside a parent we can point that variable at.
    home = Path(tempfile.mkdtemp(prefix="asc-opencode-")) / opencode_backend.DATA_DIR_NAME
    home.mkdir()
    live = sqlite3.connect(f"{source.resolve().as_uri()}?mode=ro", uri=True)
    scratch = sqlite3.connect(home / opencode_backend.DATABASE_FILE)
    try:
        live.backup(scratch)
    except sqlite3.Error:
        shutil.rmtree(home.parent, ignore_errors=True)
        return None
    finally:
        live.close()
        scratch.close()
    return home


async def test_opencode_mutations(home: Path) -> None:
    print("\n[15] OpenCode changes (throwaway XDG_DATA_HOME, real command line)")
    backend = OpenCodeBackend(home)
    if not backend.discover():
        skip("the copied database holds no sessions")
        return

    async with _app(backend) as (app, pilot):
        check(
            app._sessions and app.query("#detail-header").first() is not None,
            f"listed {len(app._sessions)} sessions",
        )
        parents = [s for s in app._sessions if app._descendants(s)]
        target = parents[0] if parents else app._cursor()
        kids = app._descendants(target)
        # OpenCode leaves this file behind only in migrated data; deleting the
        # session it belongs to has to take it too.
        leftover = home / opencode_backend.SESSION_DIFF_SUBDIR / f"{target.session_id}.json"
        leftover.parent.mkdir(parents=True, exist_ok=True)
        leftover.write_text("[]", encoding="utf-8")

        app.query_one("#sessions").index = app._sessions.index(target)
        await pilot.pause()
        await pilot.press("d")
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "d opens a confirmation")
        if kids:
            check(
                f"also deletes {len(kids)} descendant sub-agent session" in app.screen._body,
                f"saying what rides along: {app.screen._body}",
            )
        await pilot.press("y")
        await _settle(pilot, app)

        remaining = {s.session_id for s in backend.discover()}
        check(
            target.session_id not in remaining,
            f"handed to the OpenCode command line: {_status(app)}",
        )
        check(
            not any(k.session_id in remaining for k in kids), f"its {len(kids)} sub-agents went too"
        )
        check(not leftover.exists(), "and the migrated leftover file with them")
        check(
            not any(s.session_id == target.session_id for s in app._sessions), "the list refreshed"
        )
        check(
            app.check_action("delete_empty", ()) is False
            and app.check_action("archive", ()) is False,
            "E and a stay hidden for OpenCode",
        )

    # Deleting what is already gone means "it is not there", which is what was
    # asked for — not a failure worth alarming anyone with.
    check((await backend.delete(target)).ok, "deleting an already-deleted session reports success")

    # Something that genuinely cannot be deleted must say so, in plain text.
    survivor = backend.discover()[0]
    database = home / opencode_backend.DATABASE_FILE
    mode = database.stat().st_mode
    database.chmod(0o444)
    try:
        result = await backend.delete(survivor)
    finally:
        database.chmod(mode)
    check(
        not result.ok
        and result.message
        and "\x1b" not in result.message
        and survivor.session_id in {s.session_id for s in backend.discover()},
        f"a real failure is reported, without terminal escapes: {result.message[:60]}",
    )


async def test_claude_mutations(home: Path) -> None:
    print("\n[16] Claude Code changes (throwaway CLAUDE_CONFIG_DIR)")
    backend = ClaudeBackend(home)
    async with _app(backend) as (app, pilot):
        listing = app.query_one("#sessions")
        total = len(app._sessions)
        check(total > 0, f"listed {total} sessions")
        check(
            all(
                app.check_action(a, ()) is False
                for a in ("archive", "unarchive", "delete_archived")
            ),
            "a / u / D are hidden: Claude Code has no archive",
        )
        check(
            app.check_action("delete_orphans", ()) is False and backend.orphan_label is None,
            "O is hidden: a sub-agent transcript goes with its session",
        )
        check(
            app.check_action("delete_empty", ()) is not False
            and [s for s in app._sessions if s.noise],
            f"E is available, with {sum(s.noise for s in app._sessions)} empty sessions for it",
        )
        await pilot.press("a")
        await _settle(pilot, app)
        check(len(app._sessions) == total, "pressing a does nothing at all")

        # The sidecar directory holds the sub-agent transcripts and goes too.
        sidecar_rows = [s for s in app._sessions if claude_backend.sidecar_of(s.path).is_dir()]
        check(bool(sidecar_rows), f"{len(sidecar_rows)} session(s) have a sidecar")
        target = sidecar_rows[0]
        sidecar = claude_backend.sidecar_of(target.path)
        check(bool(list(sidecar.rglob("*.jsonl"))), "with sub-agent transcripts inside")
        listing.index = app._sessions.index(target)
        await pilot.pause()
        await pilot.press("d")
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "d opens a confirmation")
        await pilot.press("y")
        await _settle(pilot, app)
        check(
            not target.path.exists() and not sidecar.exists(),
            f"transcript and sidecar both gone: {_status(app)}",
        )
        check(len(app._sessions) == total - 1, "the list refreshed")

        # cc-switch's guard: the id inside the file has to match the list.
        survivor = app._cursor()
        wrong_id = "00000000-dead-beef-0000-000000000000"
        tampered = Session(**{**survivor.__dict__, "session_id": wrong_id})
        result = await backend.delete(tampered)
        check(
            not result.ok and result.message and survivor.path.exists(),
            f"a mismatched session id refuses deletion: {result.message}",
        )

        empty_before = [s for s in app._sessions if s.noise]
        await pilot.press("E")
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "E opens a confirmation")
        await pilot.press("escape")
        await pilot.pause()
        check(all(s.path.exists() for s in empty_before), "cancelling keeps every empty session")
        await pilot.press("E")
        await pilot.pause()
        await pilot.press("y")
        await _settle(pilot, app)
        check(
            not any(s.path.exists() for s in empty_before)
            and not any(s.noise for s in app._sessions),
            f"confirming sweeps all {len(empty_before)} of them: {_status(app)}",
        )
        await pilot.press("E")
        await pilot.pause()
        check("No empty sessions" in _status(app), f"and says so next time: {_status(app)}")

        # Multi-select, straight to the filesystem — no command line involved.
        listing.index = 0
        await pilot.pause()
        await pilot.press("space")
        await pilot.pause(0.2)
        first = app._cursor()
        await pilot.press("j")
        await pilot.pause(0.2)
        await pilot.press("space")
        await pilot.pause(0.2)
        second = app._cursor()
        check(len(app._selection) == 2, f"picked two ({len(app._selection)})")
        check(
            app.check_action("archive", ()) is False,
            "no archive key appears just because a selection stands",
        )
        remaining = len(app._sessions)
        await pilot.press("d")
        check(await _dialog(pilot, app), "d opens a confirmation for the selection")
        await pilot.press("y")
        await _settle(pilot, app)
        check(
            not first.path.exists() and not second.path.exists(), f"both are gone: {_status(app)}"
        )
        check(
            len(app._sessions) == remaining - 2 and not app._selection,
            "and multi-select ends with them",
        )

        check(not app._danger, "danger mode is off by default")
        await pilot.press("exclamation_mark")
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "! asks before turning it on")
        await pilot.press("n")
        await pilot.pause()
        check(not app._danger, "declining leaves it off")
        await pilot.press("exclamation_mark")
        await pilot.pause()
        await pilot.press("y")
        await pilot.pause(0.3)
        check(
            app._danger and app.query_one("#banner").has_class("-danger"),
            "confirming turns it on, banner and all",
        )
        doomed = app._cursor()
        await pilot.press("d")
        await pilot.pause(0.2)
        check(not isinstance(app.screen, ConfirmScreen), "no confirmation in danger mode")
        await _settle(pilot, app)
        check(not doomed.path.exists(), "and the session is deleted")
        await pilot.press("escape")
        await pilot.pause()
        check(not app._danger, "Esc leaves danger mode")
        await pilot.press("d")
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "and confirmation comes back")
        await pilot.press("escape")
        await pilot.pause()


async def test_failure_handling(claude_home: Path, codex_home: Path) -> None:
    """A backend that misbehaves must not take the application with it."""
    print("\n[17] When a backend misbehaves")
    backend = ClaudeBackend(claude_home)
    calls = 0

    async def explodes(session: Session):
        nonlocal calls
        calls += 1
        raise RuntimeError("the backend blew up")

    backend.delete = explodes
    async with _app(backend) as (app, pilot):
        total = len(app._sessions)
        app._selection = {s.session_id for s in app._sessions}
        app._selection_changed()
        await pilot.press("d")
        check(await _dialog(pilot, app), "a batch over every session opens a confirmation")
        await pilot.press("y")
        await _settle(pilot, app)
        check(app.is_running, "an exception from the backend does not kill the application")
        check(calls == total, f"every branch was still attempted ({calls}/{total})")
        check(not app._busy, "and the busy flag was released")
        check(
            "Unexpected error" in _status(app) and "RuntimeError" in _status(app),
            f"reported honestly: {_status(app)[:70]}",
        )
        check(len(app._sessions) == total, "nothing was deleted")
        # Nothing succeeded, so nothing was released from the selection either.
        check(len(app._selection) == total, "and the selection is still there to retry with")
        footer_top = _footer(app)[0]
        check(
            any("Delete selected" in v for v in footer_top),
            f"the footer still names the selection: {footer_top}",
        )

        await pilot.press("escape")
        await pilot.pause(0.3)
        check(not app._selection, "Esc clears it")
        await pilot.pause(0.2)
        check(
            all("selected" not in v for v in _footer(app)[0]),
            f"and the footer goes back to acting on one row: {_footer(app)[0]}",
        )

    # A branch that fails leaves its ancestors untried. Those sessions are still
    # standing, and the tally has to account for them rather than naming only
    # the refusal itself. Nothing here reaches the filesystem.
    codex = CodexBackend(codex_home)
    async with _app(codex, force_cli=True) as (app, pilot):
        blocked = next(s for s in app._sessions if app._descendants(s))
        doomed = app._descendants(blocked)[0]

        async def one_bad_apple(session: Session):
            return model.OpResult(session.session_id != doomed.session_id, "refused")

        codex.delete = one_bad_apple
        app._selection = {blocked.session_id, doomed.session_id}
        app._selection_changed()
        await pilot.press("d")
        check(await _dialog(pilot, app), "a batch holding a doomed sub-agent opens a confirmation")
        await pilot.press("y")
        await _settle(pilot, app)
        check(
            "1/3" in _status(app) and "2 not completed" in _status(app),
            f"the untried ancestor counts as still there, not as done: {_status(app)}",
        )
        check(
            app._selection == {blocked.session_id, doomed.session_id},
            f"the refusal and what it blocks stay picked out to retry ({len(app._selection)})",
        )
        sibling = app._descendants(blocked)[1]
        check(
            sibling.session_id not in app._selection,
            "while the sibling it says nothing about went through and was released",
        )


# =========================================================== [18] regressions


def test_parsing_regressions(claude_home: Path) -> None:
    """Things that were once wrong, expressed so they cannot go wrong again."""
    print("\n[18] Parsing regressions")

    # A long preamble used to push the opening message out of the scan window,
    # which marked a real conversation as empty — and `E` deletes those.
    backend = ClaudeBackend(claude_home)
    sessions = backend.discover()
    buried = [s for s in sessions if s.cwd == "/Users/me/beta" and s.title == "question 1"]
    check(
        buried and not any(s.noise for s in buried) and all(s.cwd and s.created_at for s in buried),
        f"a preamble longer than {claude_backend.HEAD_SCAN_LINES} lines is not an empty session",
    )
    check(
        any(s.title == "a title of its own" for s in sessions),
        "ai-title wins over the first user message",
    )
    empty = [s for s in sessions if s.noise]
    check(
        len(empty) == 3 and all(s.title == "(Empty session)" for s in empty),
        f"empty sessions are still recognised ({len(empty)})",
    )
    talkative = next(s for s in sessions if not s.noise)
    check(
        [m.text for m in backend.load_messages(talkative)]
        == ["question 1", "answer 1", "question 2", "answer 2"],
        "thinking, tool calls and tool results stay out of the conversation",
    )

    # Codex metadata and the opening message come from a single pass.
    scratch = Path(tempfile.mkdtemp(prefix="asc-rollout-"))
    session_id = str(uuid.uuid4())
    path = scratch / f"rollout-2026-03-04T05-06-07-{session_id}.jsonl"
    _write_jsonl(path, [
        {"type": "session_meta", "payload": {
            "session_id": session_id, "cwd": "/Users/me/gamma", "originator": "codex_vscode",
            "source": "vscode", "cli_version": "0.9.9"}},
        {"type": "event_msg", "payload": {"type": "user_message", "message": "the opening line"}},
        {"type": "event_msg", "payload": {"type": "agent_message", "message": "the reply"}},
    ])
    try:
        meta, opening = codex_backend._read_head(path)
        check(
            meta.get("cwd") == "/Users/me/gamma" and opening == "the opening line",
            "one read yields both",
        )
        parsed = codex_backend.load_session(path, archived=False, names={})
        check(
            parsed.title == "the opening line"
            and parsed.client == "vscode"
            and parsed.version == "0.9.9",
            f"parsed: {parsed.title} / {parsed.client} / v{parsed.version}",
        )
        check(
            parsed.created_at == datetime(2026, 3, 4, 5, 6, 7),
            "the start time comes from the filename, in local time",
        )
        names = {session_id: "a name I gave it"}
        named = codex_backend.load_session(path, archived=False, names=names)
        check(
            named.title == "a name I gave it" and named.named,
            "a user-assigned name wins over the opening line",
        )
    finally:
        shutil.rmtree(scratch, ignore_errors=True)


def test_day_labels() -> None:
    print("\n[19] The date column")
    today = date(2026, 8, 5)
    for when, expected in {
        datetime(2026, 8, 5, 9, 0): "Today",
        datetime(2026, 8, 4, 9, 0): "Yesterday",
        datetime(2026, 3, 4, 9, 0): "03-04",
        datetime(2025, 12, 31, 9, 0): "25-12-31",
    }.items():
        got = app_module._day_label(when, today)
        check(got == expected, f"{when:%Y-%m-%d} shows as “{got}”")
    check(
        app_module._day_label(datetime(2025, 8, 5), today)
        != app_module._day_label(datetime(2026, 8, 5), today),
        "the same day last year cannot be mistaken for today",
    )


async def test_ui_regressions(claude_home: Path, codex_home: Path) -> None:
    print("\n[20] Interface regressions")
    app = SessionCleanerApp(ClaudeBackend(claude_home))
    preview = Session(
        backend="claude", path=Path("/x/preview.jsonl"),
        session_id="00000000-1111-2222-3333-444444444444", title="a very long session title " * 20,
        client="cli", updated_at=datetime(2026, 8, 5, 16, 36), size=0, cwd="/Users/me/alpha",
        version="0.146.0", created_at=datetime(2026, 8, 5, 16, 36),
    )
    header = app._detail_header(preview, [Message("user", "x", 0)] * 17)
    check(
        header.plain.splitlines() == [
            preview.title,
            preview.session_id,
            "2026-08-05 16:36 · cli · 17 messages · v0.146.0",
            "/Users/me/alpha",
        ],
        "the detail header is four lines: title, id, metadata, directory",
    )
    check(header.no_wrap and header.overflow == "ellipsis", "and clips rather than wrapping")

    async with _app(ClaudeBackend(claude_home)) as (app, pilot):
        listing = app.query_one("#sessions")
        # A backward search that stays put must not claim it wrapped.
        app._query = "question"
        matches = app._matches()
        check(len(matches) > 1, f"“question” matches {len(matches)} rows")
        listing.index = matches[1]
        await pilot.pause()
        app._search_direction = -1
        app._jump(matches[1], -1, inclusive=True)
        await pilot.pause()
        check(
            "wrapped to bottom" not in _status(app) and listing.index == matches[1],
            f"staying put is not wrapping: {_status(app)}",
        )
        app._jump(matches[0], -1, inclusive=False)
        await pilot.pause()
        check("wrapped to bottom" in _status(app), "actually crossing the edge is")
        app._query = ""
        app._apply_highlight()

        app._busy = True
        await pilot.press("q")
        await pilot.pause(0.2)
        check(
            app.is_running and "quitting" in _status(app), f"q mid-operation waits: {_status(app)}"
        )
        app._busy = False

    # Wide characters are measured in cells, so the columns still line up.
    async with _app(CodexBackend(codex_home)) as (app, pilot):
        rows = _rows(app)
        offsets = []
        for title in ("宽字符标题", "weekly report"):
            plain = str(rows[title]._label.visual)
            offsets.append(cell_len(plain[: plain.index(title)]))
        check(
            offsets[0] == offsets[1],
            f"a CJK project name keeps the title column aligned ({offsets})",
        )

    empty = SessionCleanerApp(CodexBackend(claude_home))  # a Claude home has no rollouts
    async with empty.run_test(size=(120, 40)) as pilot:
        await pilot.pause()
        await pilot.pause(0.4)
        check(
            "No sessions found" in str(empty.query_one(".placeholder").visual),
            "an empty tree explains itself",
        )

    fake = [
        Session(
            backend="codex", path=Path(f"/x/{i}.jsonl"), session_id=str(i), title=f"session {i}",
            client="cli", updated_at=datetime(2026, 1, 1), size=0, archived=True,
        )
        for i in range(9)
    ]
    dialog = app_module.confirm_bulk(fake, "archived sessions")
    check(
        dialog._body == "This cannot be undone.",
        f"with no reason to give, state the consequence: {dialog._body}",
    )
    with_note = app_module.confirm_bulk(
        fake, "orphaned sub-agent sessions", "Their source sessions were deleted."
    )
    check(
        "source sessions" in with_note._body,
        f"with one, give it first: {with_note._body.splitlines()[0]}",
    )
    check(
        f"{len(fake) - app_module.BULK_PREVIEW_LIMIT} more" in str(dialog._subject),
        "a long list of titles collapses",
    )


async def test_footer(codex_home: Path, claude_home: Path, opencode_home: Path) -> None:
    """The footer is the only place the keys are advertised.

    Textual lays a row of keys out and simply runs off the edge of a narrow
    terminal, taking whatever sat at the end with it — which is why the keys are
    split over two rows, with the ones you cannot get out without on the second.
    """
    print("\n[21] Footer width")
    for label, backend in (
        ("Codex", CodexBackend(codex_home)),
        ("Claude Code", ClaudeBackend(claude_home)),
        ("OpenCode", OpenCodeBackend(opencode_home)),
    ):
        for width in (80, 100, FOOTER_FULL_WIDTH):
            async with _app(backend, size=(width, 25), force_cli=True) as (app, _):
                rows = list(app.query(app_module.FooterRow))
                check(len(rows) == 2, f"{label} at {width}: two footer rows ({len(rows)})")
                needed = [
                    max((k.region.right for k in row.query("FooterKey")), default=0) for row in rows
                ]
                # The way out has to be visible at any width worth using.
                check(
                    needed[1] <= width,
                    f"{label} at {width}: the navigation row fits (needs {needed[1]})",
                )
                keys = [str(k.render()).strip() for k in rows[1].query("FooterKey")]
                check(
                    any(v.startswith("q ") for v in keys) and any(v.startswith("h ") for v in keys),
                    f"{label} at {width}: quit and help are on the second row",
                )
                if width == FOOTER_FULL_WIDTH:
                    check(
                        needed[0] <= width,
                        f"{label}: every session key fits {width} columns (needs {needed[0]})",
                    )


async def test_help(codex_home: Path, claude_home: Path, opencode_home: Path) -> None:
    """The help must describe this agent and no other.

    It is gated on the same `check_action` the footer uses, so the two cannot
    drift: whatever key is hidden is also unexplained.
    """
    print("\n[22] Keyboard help")

    def keys_of(app: SessionCleanerApp) -> dict[str, str]:
        return {key: what for _, entries in app.help_sections() for key, what in entries}

    codex = SessionCleanerApp(CodexBackend(codex_home))
    codex._missing_cli = None
    opencode = SessionCleanerApp(OpenCodeBackend(opencode_home))
    opencode._missing_cli = None
    codex_keys = keys_of(codex)
    claude_keys = keys_of(SessionCleanerApp(ClaudeBackend(claude_home)))
    opencode_keys = keys_of(opencode)

    check(
        all(k in codex_keys for k in ("a", "u", "D", "O")) and "E" not in codex_keys,
        f"Codex help holds no Claude-only key: {sorted(codex_keys)}",
    )
    check(
        "E" in claude_keys and not any(k in claude_keys for k in ("a", "u", "D", "O")),
        f"Claude Code help holds no Codex-only key: {sorted(claude_keys)}",
    )
    check(
        "O" in opencode_keys and "d" in opencode_keys
        and not any(k in opencode_keys for k in ("a", "u", "D", "E")),
        f"OpenCode help holds only what OpenCode has: {sorted(opencode_keys)}",
    )
    check(
        all(
            k in codex_keys
            for k in ("c", "d", "␣", "/", "?", "n / N", "Esc", "r", "h", "q", "g / G")
        ),
        "every common key is explained, including the ones the footer has no room for",
    )
    sections = {name: {key for key, _ in entries} for name, entries in codex.help_sections()}
    check(
        {"Esc", "r"} <= sections["Other"] and not {"Esc", "r"} & sections["Search sessions"],
        "Esc and r sit under Other",
    )

    saved = os.environ["PATH"]
    os.environ["PATH"] = "/nonexistent"
    try:
        crippled = keys_of(SessionCleanerApp(CodexBackend(codex_home)))
    finally:
        os.environ["PATH"] = saved
    check(
        not any(k in crippled for k in ("a", "u", "d", "D", "O", "!", "␣")) and "c" in crippled,
        f"without the command line, nothing unchangeable is promised: {sorted(crippled)}",
    )

    async with codex.run_test(size=(120, 40)) as pilot:
        await pilot.pause()
        await pilot.pause(0.4)
        await pilot.press("h")
        await pilot.pause(0.3)
        check(isinstance(codex.screen, app_module.HelpScreen), "h opens the help")
        check(
            str(codex.screen.query_one("#help-title").visual).strip() == "Keyboard shortcuts",
            "with no tool name in the title",
        )
        shown = " ".join(str(w.visual) for w in codex.screen.query(".help-keys"))
        check("Copy a command to resume" in shown, "describing what copy actually does")
        check(
            "first one starts" not in shown and "individual deletions" in shown,
            "describing user-facing results rather than how modes are implemented",
        )
        check("empty session" not in shown, "and not mentioning a key this agent does not have")
        await pilot.press("escape")
        await pilot.pause(0.3)
        check(not isinstance(codex.screen, app_module.HelpScreen), "Esc closes it")


def test_clipboard_helper() -> None:
    print("\n[23] Clipboard")
    scratch = Path(tempfile.mkdtemp(prefix="asc-clip-"))
    saved = os.environ["PATH"]
    try:
        sink = scratch / "pasted.txt"
        fake = scratch / "pbcopy"
        # Written in Python rather than shell: PATH is about to point at this
        # directory alone, so the stand-in cannot rely on finding `cat`.
        fake.write_text(
            f"#!{sys.executable}\nimport pathlib, sys\n"
            f"pathlib.Path({str(sink)!r}).write_bytes(sys.stdin.buffer.read())\n",
            encoding="utf-8",
        )
        fake.chmod(0o755)
        os.environ["PATH"] = str(scratch)
        check(clipboard.copy("hello 世界"), "a native helper is found and used")
        check(sink.read_text(encoding="utf-8") == "hello 世界", "the text arrives unchanged")

        os.environ["PATH"] = str(scratch / "nothing")
        check(clipboard.copy("x") is False, "with no helper it says so rather than pretending")

        fake.write_text(f"#!{sys.executable}\nraise SystemExit(3)\n", encoding="utf-8")
        os.environ["PATH"] = str(scratch)
        check(clipboard.copy("x") is False, "a helper that fails is not reported as success")
    finally:
        os.environ["PATH"] = saved
        shutil.rmtree(scratch, ignore_errors=True)


def test_packaging() -> None:
    print("\n[24] Packaging")
    from importlib.metadata import PackageNotFoundError, version

    pyproject = (ROOT / "pyproject.toml").read_text(encoding="utf-8")
    check(
        'dynamic = ["version"]' in pyproject, "the version is read from the package at build time"
    )
    check(f'"{__version__}"' not in pyproject, f"and written down only once ({__version__})")
    try:
        installed = version("agent-session-cleaner")
    except PackageNotFoundError:
        skip("not installed as a package")
    else:
        check(installed == __version__, f"the installed version matches the source: {installed}")
    readme = (ROOT / "README.md").read_text(encoding="utf-8").splitlines()
    keys = [line for line in readme if line.startswith("| `")]
    check(len(keys) > 10, f"the README documents {len(keys)} keys")


def main() -> int:
    # Read-only sections share synthetic homes so the suite behaves the same
    # everywhere; anything that deletes gets a home of its own, because it
    # leaves the tree in pieces.
    temporary: list[Path] = []

    def fixture(build) -> Path:
        home = build()
        temporary.append(home)
        return home

    try:
        test_contracts()
        test_codex_real_data()
        test_claude_real_data()
        test_opencode_real_data()
        test_localization()

        codex_fixture = fixture(make_codex_fixture)
        claude_fixture = fixture(make_claude_home)
        # An OpenCode data directory has to be named "opencode", so the
        # throwaway one lives inside a parent that gets cleaned up instead.
        opencode_fixture = make_opencode_fixture()
        temporary.append(opencode_fixture.parent)

        asyncio.run(test_wording_and_missing_deps(codex_fixture, claude_fixture, opencode_fixture))
        asyncio.run(test_copy_resume(codex_fixture, claude_fixture, opencode_fixture))
        asyncio.run(test_browsing(codex_fixture))
        asyncio.run(test_tree_and_orphans(codex_fixture))
        asyncio.run(test_multi_select(codex_fixture))
        asyncio.run(test_opencode_sessions(opencode_fixture))
        asyncio.run(test_opencode_locking(opencode_fixture))
        asyncio.run(test_picker(codex_fixture, claude_fixture, opencode_fixture))
        asyncio.run(test_bulk_concurrency(fixture(make_codex_fixture)))

        codex_home = make_codex_home()
        if codex_home is None:
            print("\n[14] Codex changes (throwaway CODEX_HOME, real command line)")
            skip("no codex command line or no usable sessions to copy")
        else:
            temporary.append(codex_home)
            asyncio.run(test_codex_mutations(codex_home))

        opencode_home = make_opencode_home()
        if opencode_home is None:
            print("\n[15] OpenCode changes (throwaway XDG_DATA_HOME, real command line)")
            skip("no opencode command line or no usable database to copy")
        else:
            temporary.append(opencode_home.parent)
            asyncio.run(test_opencode_mutations(opencode_home))

        asyncio.run(test_claude_mutations(fixture(make_claude_home)))
        asyncio.run(test_failure_handling(fixture(make_claude_home), codex_fixture))

        regression_home = fixture(make_claude_home)
        test_parsing_regressions(regression_home)
        test_day_labels()
        asyncio.run(test_ui_regressions(regression_home, codex_fixture))
        asyncio.run(test_footer(codex_fixture, regression_home, opencode_fixture))
        asyncio.run(test_help(codex_fixture, regression_home, opencode_fixture))
        test_clipboard_helper()
        test_packaging()
    finally:
        for home in temporary:
            shutil.rmtree(home, ignore_errors=True)

    failed = [label for ok, label in checks if not ok]
    tail = f", {len(skipped)} skipped" if skipped else ""
    print(f"\n{len(checks) - len(failed)}/{len(checks)} passed{tail}")
    for label in skipped:
        print(f"  SKIPPED: {label}")
    for label in failed:
        print(f"  FAILED: {label}")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
