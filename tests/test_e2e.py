"""End-to-end checks.

The Claude fixtures and every behavioural check are synthetic, so the suite
runs the same on any machine. A few sections additionally read the real
``~/.codex`` and ``~/.claude`` trees to prove the parsers cope with data as it
is actually written; those skip themselves when there is nothing to read, and
never write to them. Anything that deletes runs against a throwaway home.

    .venv/bin/python tests/test_e2e.py
"""

from __future__ import annotations

import asyncio
import collections
import json
import os
import shlex
import shutil
import sys
import tempfile
import uuid
from datetime import date, datetime, timedelta
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from agent_session_cleaner import __version__, clipboard
from agent_session_cleaner import app as app_module
from agent_session_cleaner.app import ConfirmScreen, SessionCleanerApp, SessionRow
from agent_session_cleaner.backends import ClaudeBackend, CodexBackend
from agent_session_cleaner.backends import claude as claude_backend
from agent_session_cleaner.backends import codex as codex_backend
from agent_session_cleaner.model import MAX_MESSAGE_CHARS, Session
from agent_session_cleaner.picker import AgentPicker, AgentRow

checks: list[tuple[bool, str]] = []
skipped: list[str] = []


def check(ok: bool, label: str) -> bool:
    checks.append((bool(ok), label))
    print(f"  {'PASS' if ok else 'FAIL'}  {label}")
    return bool(ok)


def skip(label: str) -> None:
    """Record a section that had no material to run against.

    A machine without Codex installed, or with an empty session tree, should
    get a clean run rather than a wall of failures about missing data.
    """
    skipped.append(label)
    print(f"  SKIP  {label}")


def _status(app: SessionCleanerApp) -> str:
    return str(app.query_one("#status").visual).strip()


async def _settle(pilot, app: SessionCleanerApp) -> None:
    """Wait for an in-flight delete/archive to finish."""
    for _ in range(200):
        await pilot.pause(0.1)
        if not app._busy:
            break
    await pilot.pause(0.3)


# ============================================================ [1] 数据层 codex


def test_codex_data() -> None:
    print("\n[1] 数据层 / 真实 ~/.codex")
    backend = CodexBackend()
    if not backend.home.is_dir():
        skip(f"本机没有 {backend.home}，跳过真实数据检查")
        return
    found = backend.discover()
    if not found:
        skip("本机 ~/.codex 里没有会话，跳过真实数据检查")
        return

    check(len(found) > 0, f"发现 {len(found)} 个 session")
    check(all(s.session_id and s.backend == "codex" for s in found), "每个 session 都解析出 id")
    check(
        found == sorted(found, key=lambda s: s.recency_at, reverse=True),
        "按 recency_at 倒序排列",
    )
    clobbered = collections.Counter(s.updated_at.strftime("%Y-%m-%d %H:%M") for s in found)
    top_mtime, top_count = clobbered.most_common(1)[0]
    check(
        all(s.recency_at == (s.created_at or s.updated_at) for s in found),
        f"recency 用文件名时间戳（本机 {top_count}/{len(found)} 个 mtime 同为 {top_mtime}）",
    )

    # `SessionSource::VSCode` is the #[default] enum variant, so anything that
    # doesn't declare a source lands in session_meta as "vscode". Someone who
    # really does use the VS Code extension should still see it named, so the
    # invariant is "labelled vscode implies it said so", not "never vscode".
    def _originator(session: Session) -> str | None:
        meta, _ = codex_backend._read_head(session.path)
        value = meta.get("originator")
        return value if isinstance(value, str) else None

    labelled = [s for s in found if s.client == "vscode"]
    mislabelled = [s for s in labelled if _originator(s) != "codex_vscode"]
    check(
        not mislabelled,
        f"vscode 标记只给真正的 VS Code 扩展（{len(labelled)} 个，误标 {len(mislabelled)} 个）",
    )
    check(
        bool({s.client for s in found}),
        f"识别出客户端: {sorted({s.client for s in found})}",
    )

    subdir = codex_backend.ARCHIVED_SESSIONS_SUBDIR
    check(
        all(s.archived == (subdir in s.path.parts) for s in found),
        f"归档标记与所在目录一致（当前 {sum(s.archived for s in found)} 个归档）",
    )
    check(
        all(s.noise == (s.client in ("exec",) or s.client.startswith("subagent")) for s in found),
        f"杂项判定与来源一致（{sum(s.noise for s in found)} 个默认隐藏）",
    )

    biggest = max(found, key=lambda s: s.size)
    longest = max((len(m.text) for m in backend.load_messages(biggest)), default=0)
    check(
        longest <= MAX_MESSAGE_CHARS,
        f"最大文件({biggest.size / 1048576:.1f}MB)截断到 {longest} 字符",
    )


# =========================================================== [2] 数据层 claude


def test_claude_data() -> None:
    print("\n[2] 数据层 / 真实 ~/.claude")
    backend = ClaudeBackend()
    if not backend.home.is_dir():
        skip(f"本机没有 {backend.home}，跳过真实数据检查")
        return
    found = backend.discover()
    if not found:
        skip("本机 ~/.claude 里没有会话，跳过真实数据检查")
        return
    check(len(found) > 0, f"发现 {len(found)} 个 session")
    check(all(s.backend == "claude" for s in found), "backend 标记正确")
    check(not backend.supports_archive, "Claude 后端声明不支持归档")
    check(not any(s.archived for s in found), "Claude 不产生归档条目")

    # cc-switch excludes sub-agent transcripts by the `agent-` filename prefix.
    check(
        not any(s.path.name.startswith("agent-") for s in found),
        "排除了 agent-* 子代理 transcript",
    )
    nested = [s for s in found if s.path.parent.parent.name != "projects"]
    check(not nested, f"只收顶层 transcript（{len(nested)} 个来自子目录）")

    check(
        all(s.session_id == s.path.stem for s in found),
        "session id 与文件名一致",
    )
    # The only records in an empty session are `mode`/`permission-mode`, which
    # carry no timestamp at all; those legitimately fall back to mtime.
    undated = [s for s in found if s.created_at is None]
    check(
        all(s.noise for s in undated),
        f"从记录里取到创建时间，只有 {len(undated)} 个空会话回退到 mtime",
    )
    check(
        sum(s.title != "(无消息)" for s in found) > len(found) * 0.8,
        f"{sum(s.title != '(无消息)' for s in found)}/{len(found)} 个解析出标题",
    )
    # How many empty sessions exist is user state (and `E` can take them to
    # zero), so check the invariant: `noise` must mean "no real user message".
    # It is decided from the first 200 lines, so cross-check a full parse.
    disagree = [
        s
        for s in found
        if s.noise != (not any(m.role == "user" for m in backend.load_messages(s)))
    ]
    check(
        not disagree,
        f"空会话判定与全量解析一致（当前 {sum(s.noise for s in found)} 个空会话）",
    )

    # The largest transcript is dominated by tool traffic, which must not leak
    # into the conversation view.
    biggest = max(found, key=lambda s: s.size)
    messages = backend.load_messages(biggest)
    check(
        all(m.role in ("user", "agent") for m in messages),
        f"最大文件({biggest.size / 1048576:.1f}MB)只产出 {len(messages)} 条用户/助手消息",
    )
    # Assert structurally, not by scanning text: a transcript that *discusses*
    # tool plumbing would trip a substring check.
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
        if record.get("toolUseResult") is not None or record.get("isMeta"):
            tool_traffic += 1
    kept_user = sum(m.role == "user" for m in messages)
    check(
        tool_traffic > 0 and kept_user == raw_user - tool_traffic,
        f"剔除了 {tool_traffic} 条工具输出/注入记录，保留 {kept_user}/{raw_user} 条真实用户消息",
    )
    check(
        max((len(m.text) for m in messages), default=0) <= MAX_MESSAGE_CHARS,
        "超长消息被截断",
    )


# ============================================================== 合成的 codex 树


def _codex_rollout(
    session_id: str, *, opening: str, cwd: str, originator: str, source: object
) -> list[dict]:
    return [
        {
            "type": "session_meta",
            "payload": {
                "session_id": session_id,
                "cwd": cwd,
                "originator": originator,
                "source": source,
                "cli_version": "0.50.0",
            },
        },
        {"type": "event_msg", "payload": {"type": "user_message", "message": opening}},
        {"type": "event_msg", "payload": {"type": "agent_message", "message": f"好的，{opening}"}},
    ]


#: (opening, cwd, originator, source). The three "proxy" titles give the search
#: section something with more than one hit but fewer than all.
_CODEX_FIXTURE = [
    ("修复 proxy 超时问题", "/Users/me/alpha", "codex_cli_rs", "cli"),
    ("整理 proxy 配置文件", "/Users/me/alpha", "codex-tui", "cli"),
    ("给 proxy 加上重试", "/Users/me/beta", "Codex Desktop", "vscode"),
    ("写一份周报", "/Users/me/beta", "codex_cli_rs", "cli"),
    ("调研数据库迁移", "/Users/me/gamma", "codex_cli_rs", "cli"),
    ("重构登录流程", "/Users/me/gamma", "Codex Desktop", "vscode"),
    ("自动化跑的批处理", "/Users/me/alpha", "codex_exec", "exec"),
    ("子代理的侧线程", "/Users/me/alpha", "codex_cli_rs", {"subagent": {"other": "guardian"}}),
]


def make_codex_fixture() -> Path:
    """A synthetic CODEX_HOME for everything that only reads and renders."""
    home = Path(tempfile.mkdtemp(prefix="asc-codexfix-"))
    start = datetime(2026, 7, 20, 15, 0)
    for offset, (opening, cwd, originator, source) in enumerate(_CODEX_FIXTURE):
        when = start - timedelta(hours=offset)
        session_id = str(uuid.uuid4())
        path = (
            home
            / "sessions"
            / f"{when:%Y}"
            / f"{when:%m}"
            / f"{when:%d}"
            / f"rollout-{when:%Y-%m-%dT%H-%M-%S}-{session_id}.jsonl"
        )
        _write_jsonl(
            path,
            _codex_rollout(
                session_id, opening=opening, cwd=cwd, originator=originator, source=source
            ),
        )
    return home


# ====================================================== [2.5] 文案与缺失依赖


async def test_wording_and_missing_deps(codex_home: Path, claude_home: Path) -> None:
    print("\n[2.5] 文案 / 缺少依赖时的表现")

    # 会话预览里的落款必须跟着 agent 走，不能写死成 Codex。
    for backend, expected in (
        (ClaudeBackend(claude_home), "Claude"),
        (CodexBackend(codex_home), "Codex"),
    ):
        app = SessionCleanerApp(backend)
        async with app.run_test(size=(120, 30)) as pilot:
            await pilot.pause()
            await pilot.pause(0.6)
            # An empty session would render no replies at all, which would pass
            # the "no wrong signature" reading of this check for the wrong reason.
            app.query_one("#sessions").index = next(
                i for i, s in enumerate(app._visible) if not s.noise
            )
            await pilot.pause(0.6)
            replies = [str(w.visual) for w in app.query(".msg.agent")]
            ok = bool(replies) and all(r.lstrip().startswith(f"◀ {expected}") for r in replies)
            check(ok, f"{backend.label} 的回复落款是「{expected}」（{len(replies)} 条）")

    # 共享界面里不允许出现某一家专有的说法。
    dialog = app_module.confirm_one(ClaudeBackend(claude_home).discover()[0])
    # Only the copy is checked; the subject carries the session title, which is
    # the user's own text and may legitimately mention any agent.
    copy = [dialog._title, dialog._body, dialog._confirm_label]
    check(
        not any("codex" in t.lower() for t in copy),
        f"删除确认框不提某一家的命令：{dialog._body}",
    )
    shared = Path(app_module.__file__).read_text()
    leaked = [
        line.strip()
        for line in shared.splitlines()
        if ("codex" in line.lower() or "claude" in line.lower())
        and '"' in line
        and not line.strip().startswith("#")
    ]
    check(not leaked, f"共享界面代码里没有写死 agent 名（{len(leaked)} 处）")

    # 数据目录都不存在时，选择界面要说人话并且点不进去。
    empty = Path(tempfile.mkdtemp(prefix="asc-empty-"))
    try:
        picker = AgentPicker({"codex": empty / "no", "claude": empty / "no2"})
        async with picker.run_test(size=(100, 20)) as pilot:
            await pilot.pause()
            await pilot.pause(0.4)
            rows = list(picker.query(AgentRow))
            check(
                not any(r.usable for r in rows)
                and all("还没有使用记录" in str(r._label.visual) for r in rows),
                "没有会话记录时如实说明",
            )
            await pilot.press("c")
            await pilot.pause(0.2)
            check(picker.is_running, "按快捷键也进不去没有记录的 agent")
            await pilot.press("q")
        check(picker.return_value is None, "退出时不返回 agent")

        # 一行一个 agent，行与行之间空一行。
        picker2 = AgentPicker({"codex": codex_home, "claude": claude_home})
        async with picker2.run_test(size=(100, 20)) as pilot:
            await pilot.pause()
            await pilot.pause(0.4)
            rows = list(picker2.query(AgentRow))
            check(
                all(r.outer_size.height == 1 for r in rows),
                "选择界面一个 agent 占一行",
            )
            check(
                all(r.styles.margin.bottom == 1 for r in rows),
                "agent 之间空一行",
            )
            await pilot.press("q")
    finally:
        shutil.rmtree(empty, ignore_errors=True)

    # 没装 Codex 命令行时：能看不能改，且相关按键消失。
    saved = os.environ["PATH"]
    os.environ["PATH"] = str(empty / "nothing-here")
    try:
        backend = CodexBackend(codex_home)
        check(backend.missing_cli() == "codex", "检测到 Codex 命令行缺失")
        check(ClaudeBackend(claude_home).missing_cli() is None, "Claude 不需要命令行工具")
        app = SessionCleanerApp(backend)
        async with app.run_test(size=(120, 30)) as pilot:
            await pilot.pause()
            await pilot.pause(0.6)
            check("没有安装" in _status(app), f"启动即提示：{_status(app)}")
            check(
                all(
                    app.check_action(action, ()) is False
                    for action in ("archive", "delete", "delete_archived", "toggle_danger")
                ),
                "会改动数据的按键全部隐藏",
            )
            check(
                len(app._sessions) > 0 and app.query("#detail-header").first() is not None,
                f"仍然可以浏览（{len(app._sessions)} 个会话）",
            )
            await pilot.press("d")
            await pilot.pause(0.3)
            check(not isinstance(app.screen, ConfirmScreen), "按 d 不会弹删除确认框")
    finally:
        os.environ["PATH"] = saved


# ========================================================== [2.6] 复制恢复命令


async def test_copy_resume(codex_home: Path, claude_home: Path) -> None:
    print("\n[2.6] 复制恢复命令")
    fake = Session(
        backend="codex",
        path=Path("/x/y.jsonl"),
        session_id="abc-123",
        title="t",
        client="cli",
        updated_at=datetime(2026, 7, 1, 12, 0),
        size=0,
        cwd="/Users/me/My Projects/a b",
    )
    check(
        CodexBackend().resume_command(fake) == "codex resume abc-123",
        f"Codex 恢复命令：{CodexBackend().resume_command(fake)}",
    )
    check(
        ClaudeBackend().resume_command(fake) == "claude --resume abc-123",
        f"Claude 恢复命令：{ClaudeBackend().resume_command(fake)}",
    )

    # 非默认目录要带上环境变量，否则复制出去的命令找不到这个会话。
    elsewhere = Path(tempfile.mkdtemp(prefix="asc-home-"))
    try:
        check(
            CodexBackend(elsewhere).resume_command(fake).startswith("CODEX_HOME="),
            "自定义目录时带上 CODEX_HOME",
        )
        check(
            ClaudeBackend(elsewhere).resume_command(fake).startswith("CLAUDE_CONFIG_DIR="),
            "自定义目录时带上 CLAUDE_CONFIG_DIR",
        )
    finally:
        shutil.rmtree(elsewhere, ignore_errors=True)

    # 不让测试动到真实剪贴板：把 PATH 指开，走 OSC 52 回退分支。
    saved = os.environ["PATH"]
    os.environ["PATH"] = "/nonexistent-for-tests"
    try:
        for backend, verb in (
            (CodexBackend(codex_home), "resume"),
            (ClaudeBackend(claude_home), "--resume"),
        ):
            app = SessionCleanerApp(backend)
            async with app.run_test(size=(150, 30)) as pilot:
                await pilot.pause()
                await pilot.pause(0.5)
                session = app._selected()
                await pilot.press("c")
                await pilot.pause(0.3)
                copied = app._clipboard
                expected_cd = f"cd {shlex.quote(session.cwd)} && " if session.cwd else ""
                check(
                    copied.startswith(expected_cd)
                    and verb in copied
                    and session.session_id in copied,
                    f"{backend.label}：{copied}",
                )
                check("已复制" in _status(app), "状态栏确认已复制")
    finally:
        os.environ["PATH"] = saved

    # 带空格的目录必须加引号，否则粘出去就断了。
    quoted = f"cd {shlex.quote(fake.cwd)} && {CodexBackend().resume_command(fake)}"
    check(
        quoted == "cd '/Users/me/My Projects/a b' && codex resume abc-123",
        f"路径带空格时正确加引号：{quoted}",
    )


# ============================================================== [3] 搜索（只读）


async def test_search(codex_home: Path) -> None:
    print("\n[3] 搜索")
    app = SessionCleanerApp(CodexBackend(codex_home))
    async with app.run_test(size=(120, 40)) as pilot:
        await pilot.pause()
        await pilot.pause(0.4)
        listing = app.query_one("#sessions")
        bar = app.query_one("#search-bar")

        needle = "proxy"
        hits = sum(needle in app._haystack(s).lower() for s in app._visible)
        check(1 < hits < len(app._visible), f"素材里「{needle}」命中 {hits}/{len(app._visible)} 条")

        await pilot.press("slash")
        await pilot.pause()
        check(bar.display and app.focused.id == "search-input", "/ 打开搜索栏并获得焦点")

        for char in needle:
            await pilot.press(char)
        await pilot.pause(0.3)
        matches = app._matches()
        check(len(matches) > 1, f"增量搜索 {needle!r} 命中 {len(matches)} 条")
        check(listing.index in matches, "光标停在匹配行上")

        await pilot.press("enter")
        await pilot.pause()
        check(not bar.display and app.focused.id == "sessions", "回车关闭搜索栏并归还焦点")

        landed = listing.index
        await pilot.press("n")
        await pilot.pause()
        check(listing.index == matches[matches.index(landed) + 1], "n 跳到下一个匹配")
        await pilot.press("N")
        await pilot.pause()
        check(listing.index == landed, "N 跳回上一个匹配")

        listing.index = matches[-1]
        await pilot.pause()
        await pilot.press("n")
        await pilot.pause()
        check(listing.index == matches[0] and "已回绕" in _status(app), "末尾按 n 回绕")

        origin = next(i for i in range(len(app._visible)) if i not in matches)
        listing.index = origin
        await pilot.pause()
        await pilot.press("slash")
        for char in needle:
            await pilot.press(char)
        await pilot.pause(0.3)
        check(listing.index != origin, "增量搜索移动了光标")
        await pilot.press("escape")
        await pilot.pause()
        check(listing.index == origin, "Esc 恢复搜索前的光标位置")

        await pilot.press("slash")
        for char in "zzqzz":
            await pilot.press(char)
        await pilot.pause(0.3)
        check("没有找到" in _status(app), f"无匹配时提示：{_status(app)}")
        check(app.is_running, "搜索时输入 q 不会退出程序")
        await pilot.press("escape")
        await pilot.pause()


# ================================================================ [4] 选择界面


async def test_picker(codex_home: Path, claude_home: Path) -> None:
    print("\n[4] 选择界面")
    homes = {"codex": codex_home, "claude": claude_home}
    expected = {
        "codex": len(CodexBackend(codex_home).discover()),
        "claude": len(ClaudeBackend(claude_home).discover()),
    }

    app = AgentPicker(homes)
    async with app.run_test(size=(90, 24)) as pilot:
        await pilot.pause()
        await pilot.pause(0.6)
        rows = list(app.query(AgentRow))
        check([r.backend.id for r in rows] == ["codex", "claude"], "列出两个 agent")
        labels = [str(r._label.visual) for r in rows]
        check(
            all(
                f"{expected[r.backend.id]} 个会话" in label
                for r, label in zip(rows, labels, strict=True)
            ),
            f"异步统计出会话数：{list(expected.values())}",
        )
        check(
            [r.shortcut for r in rows] == ["x", "c"],
            f"快捷键：Codex=x、Claude=c（实际 {[r.shortcut for r in rows]}）",
        )
        await pilot.press("c")
        await pilot.pause(0.3)
    check(app.return_value == "claude", "按 c 直接选中 Claude Code")

    app2 = AgentPicker(homes)
    async with app2.run_test(size=(90, 24)) as pilot:
        await pilot.pause()
        await pilot.press("j")
        await pilot.press("enter")
        await pilot.pause(0.3)
    check(app2.return_value == "claude", "j + Enter 选中第二项")

    app3 = AgentPicker(homes)
    async with app3.run_test(size=(90, 24)) as pilot:
        await pilot.pause()
        await pilot.press("q")
        await pilot.pause(0.3)
    check(app3.return_value is None, "q 退出时不返回 agent")

    app4 = AgentPicker(homes)
    async with app4.run_test(size=(90, 24)) as pilot:
        await pilot.pause()
        await pilot.press("x")
        await pilot.pause(0.3)
    check(app4.return_value == "codex", "按 x 直接选中 Codex")


# ============================================== [5] codex 变更（临时 CODEX_HOME）


def make_codex_home(count: int = 6) -> Path | None:
    """Copy real rollouts into a scratch CODEX_HOME.

    These have to be real: the section drives the actual `codex` CLI, and the
    CLI is entitled to be picky about what it will archive or delete.
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
        p
        for p in candidates
        if (s := codex_backend.load_session(p, archived=False, names={})) and not s.noise
    ][:count]
    if len(picked) < 4:  # the section archives, unarchives and deletes several
        return None

    home = Path(tempfile.mkdtemp(prefix="asc-codex-"))
    for path in picked:
        destination = home / path.relative_to(real)
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, destination)
    return home


async def test_codex_mutations(home: Path) -> None:
    print("\n[5] Codex 变更 / 临时 CODEX_HOME")
    app = SessionCleanerApp(CodexBackend(home))
    async with app.run_test(size=(120, 40)) as pilot:
        await pilot.pause()
        await pilot.pause(0.4)
        listing = app.query_one("#sessions")
        total = len(app._visible)
        check(total > 0, f"启动后列出 {total} 个 session")
        check(app.query("#detail-header").first() is not None, "右侧渲染出详情表头")

        first = app._selected()
        await pilot.press("j")
        await pilot.pause()
        check(app._selected() is not first, "j 键在列表中下移")
        await pilot.press("G")
        await pilot.pause()
        check(listing.index == total - 1, "G 跳到底部")
        await pilot.press("g")
        await pilot.pause()
        check(listing.index == 0, "g 回到顶部")

        await pilot.press("tab")
        await pilot.pause()
        check(app.focused is app.query_one("#detail"), "Tab 切到右窗格")
        await pilot.press("tab")
        await pilot.pause()

        target = app._selected()
        await pilot.press("a")
        await _settle(pilot, app)
        archived_dir = home / "archived_sessions"
        moved = list(archived_dir.glob(f"*{target.session_id}.jsonl"))
        check(bool(moved), f"a 归档：{_status(app)}")
        check(len(app._visible) == total - 1, "归档后从活跃视图消失")

        await pilot.press("v")
        await pilot.pause()
        rows = [r.session for r in app.query(SessionRow)]
        check(any(s.session_id == target.session_id and s.archived for s in rows), "归档视图可见")
        stripes = [r.has_class("-odd") for r in app.query(SessionRow)]
        check(stripes[:4] == [False, True, False, True], "斑马纹逐行交替")
        check(
            all(r.has_class("-archived") for r in app.query(SessionRow) if r.session.archived),
            "归档行带 -archived 类",
        )

        listing.index = next(
            i for i, s in enumerate(app._visible) if s.session_id == target.session_id
        )
        await pilot.pause()
        await pilot.press("u")
        await _settle(pilot, app)
        check(not list(archived_dir.glob(f"*{target.session_id}.jsonl")), "u 取消归档")
        await pilot.press("v")
        await pilot.pause()

        check(app.check_action("delete_empty", ()) is False, "E 键在 Codex 模式下被隐藏")
        check(app.check_action("toggle_sources", ()) is not False, "s 键在 Codex 模式下可用")

        victim = app._selected()
        await pilot.press("d")
        # No settle: focus must already be on 取消 in the very first frame,
        # otherwise the delete button flashes as focused before jumping away.
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "d 弹出二次确认框")
        check(
            ConfirmScreen.AUTO_FOCUS == "#confirm-no" and app.focused.id == "confirm-no",
            f"确认框一出现焦点就在取消上，不会闪一下（当前 {app.focused.id}）",
        )
        await pilot.press("escape")
        await pilot.pause()
        check(victim.path.exists(), "Esc 取消后文件仍在")

        await pilot.press("d")
        await pilot.pause()
        await pilot.press("y")
        await _settle(pilot, app)
        check(not victim.path.exists(), f"y 确认后删除：{_status(app)}")
        check(app._selected() is not None, "删除后选中项落到相邻行")

        # 危险模式
        check(not app._danger, "默认不在危险模式")
        await pilot.press("exclamation_mark")
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "! 进入危险模式需要二次确认")
        await pilot.press("n")
        await pilot.pause()
        check(not app._danger, "确认框里选取消则不进入危险模式")

        await pilot.press("exclamation_mark")
        await pilot.pause()
        await pilot.press("y")
        await pilot.pause(0.3)
        check(app._danger, "确认后进入危险模式")
        check(app.query_one("#banner").has_class("-danger"), "顶栏变红提示危险模式")

        doomed = app._selected()
        await pilot.press("d")
        await pilot.pause(0.2)
        check(not isinstance(app.screen, ConfirmScreen), "危险模式下 d 不再弹确认框")
        await _settle(pilot, app)
        check(not doomed.path.exists(), f"危险模式下 d 直接删除：{_status(app)}")

        # D only opens a dialog when something is archived, so make one first.
        await pilot.press("a")
        await _settle(pilot, app)
        check(any(s.archived for s in app._sessions), "危险模式下 a 仍可归档")
        await pilot.press("D")
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "危险模式下 D 批量删除仍然确认")
        await pilot.press("escape")
        await pilot.pause()

        await pilot.press("exclamation_mark")
        await pilot.pause()
        check(
            not app._danger and not app.query_one("#banner").has_class("-danger"),
            "再按 ! 退出危险模式",
        )

        await pilot.press("d")
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "退出危险模式后 d 恢复确认")
        await pilot.press("escape")
        await pilot.pause()


# ============================================= [6] claude 变更（临时 CLAUDE_HOME）


def _write_jsonl(path: Path, records: list[dict]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("".join(json.dumps(r) + "\n" for r in records), encoding="utf-8")


def _claude_transcript(
    session_id: str,
    *,
    cwd: str,
    turns: int = 2,
    preamble: int = 1,
    ai_title: str | None = None,
    when: datetime | None = None,
) -> list[dict]:
    """A transcript shaped like the real thing: preamble, then the exchange.

    `preamble` controls how many metadata records sit in front of the first
    thing the user says — the knob that used to decide whether a session was
    mistaken for an empty one.
    """
    stamp = (when or datetime(2026, 7, 1, 9, 0)).isoformat() + "Z"
    records: list[dict] = [
        {"type": "mode", "mode": "normal", "sessionId": session_id},
        {"type": "permission-mode", "permissionMode": "default", "sessionId": session_id},
    ]
    records += [
        {"type": "file-history-snapshot", "sessionId": session_id, "messageId": str(i)}
        for i in range(preamble)
    ]
    for i in range(turns):
        records.append(
            {
                "type": "user",
                "sessionId": session_id,
                "timestamp": stamp,
                "cwd": cwd,
                "version": "2.1.0",
                "entrypoint": "cli",
                "isSidechain": False,
                "message": {"role": "user", "content": f"第 {i + 1} 个问题"},
            }
        )
        records.append(
            {
                "type": "assistant",
                "sessionId": session_id,
                "timestamp": stamp,
                "message": {
                    "role": "assistant",
                    "content": [
                        {"type": "thinking", "thinking": "内部推理，不应显示"},
                        {"type": "text", "text": f"第 {i + 1} 个回答"},
                        {"type": "tool_use", "id": "t1", "name": "Read", "input": {}},
                    ],
                },
            }
        )
        # Tool results come back as `user` records and must not be shown.
        records.append(
            {
                "type": "user",
                "sessionId": session_id,
                "timestamp": stamp,
                "toolUseResult": {"stdout": "工具输出"},
                "message": {"role": "user", "content": [{"type": "tool_result", "content": "x"}]},
            }
        )
    if ai_title:
        records.append({"type": "ai-title", "aiTitle": ai_title, "sessionId": session_id})
    return records


def make_claude_home() -> Path:
    """A complete synthetic ~/.claude, so this section needs no real data."""
    home = Path(tempfile.mkdtemp(prefix="asc-claude-"))
    projects = home / "projects"

    # An ordinary session, plus one whose title was rewritten mid-session.
    _write_jsonl(
        projects / "-Users-me-alpha" / f"{uuid.uuid4()}.jsonl",
        _claude_transcript(str(uuid.uuid4()), cwd="/Users/me/alpha"),
    )
    _write_jsonl(
        projects / "-Users-me-alpha" / f"{uuid.uuid4()}.jsonl",
        _claude_transcript(str(uuid.uuid4()), cwd="/Users/me/alpha", ai_title="起好的标题"),
    )
    # One whose opening message sits past the head-scan window.
    _write_jsonl(
        projects / "-Users-me-beta" / f"{uuid.uuid4()}.jsonl",
        _claude_transcript(
            str(uuid.uuid4()),
            cwd="/Users/me/beta",
            preamble=claude_backend.HEAD_SCAN_LINES + 50,
        ),
    )

    # One with a sidecar directory holding a sub-agent transcript, which has to
    # be removed along with the session it belongs to.
    with_sidecar = str(uuid.uuid4())
    transcript = projects / "-Users-me-beta" / f"{with_sidecar}.jsonl"
    _write_jsonl(transcript, _claude_transcript(with_sidecar, cwd="/Users/me/beta"))
    _write_jsonl(
        claude_backend.sidecar_of(transcript) / "subagents" / "agent-deadbeef.jsonl",
        _claude_transcript(str(uuid.uuid4()), cwd="/Users/me/beta"),
    )

    # A stray sub-agent transcript at project level: excluded by name.
    _write_jsonl(
        projects / "-Users-me-beta" / "agent-cafebabe.jsonl",
        _claude_transcript(str(uuid.uuid4()), cwd="/Users/me/beta"),
    )

    # Sessions opened and abandoned without saying anything — what `E` sweeps.
    for _ in range(3):
        session_id = str(uuid.uuid4())
        _write_jsonl(
            projects / "-tmp-empty-project" / f"{session_id}.jsonl",
            [
                {"type": "mode", "mode": "normal", "sessionId": session_id},
                {"type": "permission-mode", "permissionMode": "default", "sessionId": session_id},
            ],
        )
    return home


async def test_claude_mutations(home: Path) -> None:
    print("\n[6] Claude 变更 / 临时 CLAUDE_CONFIG_DIR")
    backend = ClaudeBackend(home)
    app = SessionCleanerApp(backend)
    async with app.run_test(size=(120, 40)) as pilot:
        await pilot.pause()
        await pilot.pause(0.4)
        total = len(app._visible)
        check(total > 0, f"启动后列出 {total} 个 session")

        # 归档相关的键在 Claude 模式下必须消失
        archive_keys = (
            ("a", "archive"),
            ("u", "unarchive"),
            ("v", "toggle_view"),
            ("D", "delete_archived"),
        )
        for key, action in archive_keys:
            if app.check_action(action, ()) is not False:
                check(False, f"{key} 键（{action}）在 Claude 模式下应当被隐藏")
                break
        else:
            check(True, "a / u / v / D 在 Claude 模式下被隐藏")

        # s 也没有意义：Claude 什么都不折叠，空会话照样列出来
        check(app.check_action("toggle_sources", ()) is False, "s 键在 Claude 模式下被隐藏")
        check(backend.noise_label is None and app._all_sources, "Claude 默认显示全部会话")
        empty_rows = [s for s in app._visible if s.noise]
        check(bool(empty_rows), f"空会话也在列表里（{len(empty_rows)} 个）")
        check(app.check_action("delete_empty", ()) is not False, "E 键在 Claude 模式下可用")

        before = len(app._sessions)
        await pilot.press("a")
        await _settle(pilot, app)
        check(len(app._sessions) == before, "按 a 不会有任何效果")

        # 带 sidecar 的那条：删除时目录要一起消失
        sidecar_rows = [
            s for s in app._visible if claude_backend.sidecar_of(s.path).is_dir()
        ]
        check(bool(sidecar_rows), f"素材里有 {len(sidecar_rows)} 个带 sidecar 的 session")
        target = sidecar_rows[0]
        sidecar = claude_backend.sidecar_of(target.path)
        nested = list(sidecar.rglob("*.jsonl"))
        check(bool(nested), f"sidecar 里有 {len(nested)} 个子代理 transcript")

        listing = app.query_one("#sessions")
        listing.index = app._visible.index(target)
        await pilot.pause()
        await pilot.press("d")
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "d 弹出二次确认框")
        await pilot.press("y")
        await _settle(pilot, app)
        check(not target.path.exists(), f"transcript 已删除：{_status(app)}")
        check(not sidecar.exists(), "同名 sidecar 目录一并删除")
        check(len(app._visible) == total - 1, "列表刷新")

        # session id 不匹配时必须拒绝
        survivor = app._selected()
        tampered = survivor.__class__(
            **{**survivor.__dict__, "session_id": "00000000-dead-beef-0000-000000000000"}
        )
        result = await backend.delete(tampered)
        check(
            not result.ok and result.message and survivor.path.exists(),
            f"session id 对不上时拒绝删除：{result.message}",
        )

        # E: 一键清除空会话
        empty_before = [s for s in app._sessions if s.noise]
        await pilot.press("E")
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "E 弹出确认框")
        await pilot.press("escape")
        await pilot.pause()
        check(all(s.path.exists() for s in empty_before), "取消后空会话都还在")

        await pilot.press("E")
        await pilot.pause()
        await pilot.press("y")
        await _settle(pilot, app)
        check(
            not any(s.path.exists() for s in empty_before),
            f"确认后清除全部 {len(empty_before)} 个空会话：{_status(app)}",
        )
        check(not any(s.noise for s in app._sessions), "列表里不再有空会话")
        await pilot.press("E")
        await pilot.pause()
        check("没有空会话" in _status(app), "已经没有空会话时给出提示")

        # 危险模式在 Claude 侧同样生效
        await pilot.press("exclamation_mark")
        await pilot.pause()
        await pilot.press("y")
        await pilot.pause(0.3)
        check(app._danger, "Claude 模式下也能进入危险模式")
        doomed = app._selected()
        await pilot.press("d")
        await pilot.pause(0.2)
        check(not isinstance(app.screen, ConfirmScreen), "危险模式下不弹确认框")
        await _settle(pilot, app)
        check(not doomed.path.exists(), "危险模式下直接删除")

        # Esc 也能退出危险模式
        check(app._danger, "此时仍在危险模式")
        await pilot.press("escape")
        await pilot.pause()
        check(not app._danger, "Esc 退出危险模式")
        await pilot.press("d")
        await pilot.pause()
        check(isinstance(app.screen, ConfirmScreen), "退出后 d 恢复确认")
        await pilot.press("escape")
        await pilot.pause()


# ================================================================ [7] 回归项


def test_parsing_regressions(claude_home: Path) -> None:
    """Things that were wrong, expressed so they cannot go wrong again."""
    print("\n[7] 解析回归")

    # A long preamble used to push the opening message out of the scan window,
    # which marked a real conversation as empty — and `E` deletes those.
    backend = ClaudeBackend(claude_home)
    sessions = backend.discover()
    buried = [s for s in sessions if s.cwd == "/Users/me/beta"]
    long_preamble = [s for s in buried if s.title == "第 1 个问题"]
    check(
        bool(long_preamble) and not any(s.noise for s in long_preamble),
        f"前言超过 {claude_backend.HEAD_SCAN_LINES} 行的会话不会被当成空会话",
    )
    check(
        all(s.cwd and s.created_at for s in long_preamble),
        "这类会话的目录和开始时间也照样读得到",
    )
    check(
        any(s.title == "起好的标题" for s in sessions),
        "ai-title 覆盖首条用户消息作为标题",
    )
    empty = [s for s in sessions if s.noise]
    check(len(empty) == 3 and all(s.title == "(无消息)" for s in empty),
          f"空会话仍然识别为空（{len(empty)} 个）")

    # Only text blocks reach the reader: thinking, tool_use and tool results
    # are the agent talking to itself.
    talkative = next(s for s in sessions if not s.noise)
    texts = [m.text for m in backend.load_messages(talkative)]
    check(
        texts == ["第 1 个问题", "第 1 个回答", "第 2 个问题", "第 2 个回答"],
        f"只保留人看的内容：{texts}",
    )

    # Codex metadata and the opening message now come from one pass.
    rollout = Path(tempfile.mkdtemp(prefix="asc-rollout-"))
    session_id = str(uuid.uuid4())
    path = rollout / f"rollout-2026-03-04T05-06-07-{session_id}.jsonl"
    _write_jsonl(
        path,
        [
            {
                "type": "session_meta",
                "payload": {
                    "session_id": session_id,
                    "cwd": "/Users/me/gamma",
                    "originator": "codex_vscode",
                    "source": "vscode",
                    "cli_version": "0.9.9",
                },
            },
            {"type": "event_msg", "payload": {"type": "user_message", "message": "开场白"}},
            {"type": "event_msg", "payload": {"type": "agent_message", "message": "回话"}},
        ],
    )
    try:
        meta, opening = codex_backend._read_head(path)
        check(
            meta.get("cwd") == "/Users/me/gamma" and opening == "开场白",
            "Codex 一次读取同时拿到元数据和开场白",
        )
        parsed = codex_backend.load_session(path, archived=False, names={})
        check(
            parsed.title == "开场白" and parsed.client == "vscode" and parsed.version == "0.9.9",
            f"解析结果：{parsed.title} / {parsed.client} / v{parsed.version}",
        )
        check(
            parsed.created_at == datetime(2026, 3, 4, 5, 6, 7),
            "创建时间取自文件名里的本地时间戳",
        )
        named = codex_backend.load_session(path, archived=False, names={session_id: "我起的名字"})
        check(named.title == "我起的名字" and named.named, "用户命名优先于开场白")
    finally:
        shutil.rmtree(rollout, ignore_errors=True)


def test_day_labels() -> None:
    print("\n[7.1] 日期列")
    today = date(2026, 8, 5)
    cases = {
        datetime(2026, 8, 5, 9, 0): "今天",
        datetime(2026, 8, 4, 9, 0): "昨天",
        datetime(2026, 3, 4, 9, 0): "03-04",
        datetime(2025, 12, 31, 9, 0): "25-12-31",
    }
    for when, expected in cases.items():
        got = app_module._day_label(when, today)
        check(got == expected, f"{when:%Y-%m-%d} 显示为「{got}」")
    check(
        app_module._day_label(datetime(2025, 8, 5), today)
        != app_module._day_label(datetime(2026, 8, 5), today),
        "去年的同一天不会和今年混淆",
    )


async def test_ui_regressions(claude_home: Path) -> None:
    print("\n[7.2] 界面回归")
    app = SessionCleanerApp(ClaudeBackend(claude_home))
    async with app.run_test(size=(120, 40)) as pilot:
        await pilot.pause()
        await pilot.pause(0.5)
        listing = app.query_one("#sessions")

        # 反向搜索停在原地时不该说"已回绕"
        app._query = "问题"
        matches = app._matches()
        check(len(matches) > 1, f"搜索「问题」命中 {len(matches)} 条")
        listing.index = matches[1]
        await pilot.pause()
        app._search_direction = -1
        app._jump(matches[1], -1, inclusive=True)
        await pilot.pause()
        check(
            "已回绕" not in _status(app) and listing.index == matches[1],
            f"反向搜索停在原地时不谎报回绕：{_status(app)}",
        )
        app._jump(matches[0], -1, inclusive=False)
        await pilot.pause()
        check("已回绕" in _status(app), "真正绕回末尾时才提示")
        app._query = ""
        app._apply_highlight()

        # 删除进行中不能退出，否则批量删除会停在一半
        app._busy = True
        await pilot.press("q")
        await pilot.pause(0.2)
        check(app.is_running and "退出" in _status(app), f"处理中按 q 不退出：{_status(app)}")
        app._busy = False

        # 空列表提示只提到这个 agent 真的有的键
        check(app._filter_hint() == "", "Claude 什么都没隐藏，就不提任何按键")

    codex = SessionCleanerApp(CodexBackend(claude_home))  # 空目录，只看提示逻辑
    async with codex.run_test(size=(120, 40)) as pilot:
        await pilot.pause()
        await pilot.pause(0.4)
        hint = codex._filter_hint()
        check("v" in hint and "s" in hint, f"Codex 隐藏了东西时才提示按键：{hint.strip()}")
        codex._show_archived = True
        codex._all_sources = True
        check(codex._filter_hint() == "", "全部显示出来之后不再提示")
        placeholder = str(codex.query_one(".placeholder").visual)
        check("没有会话" in placeholder, f"空目录给出说明：{placeholder.strip()}")

    # 批量删除对话框不能提到当前用不上的按键
    fake = [
        Session(
            backend="codex",
            path=Path(f"/x/{i}.jsonl"),
            session_id=str(i),
            title=f"会话 {i}",
            client="cli",
            updated_at=datetime(2026, 1, 1),
            size=0,
            archived=True,
        )
        for i in range(9)
    ]
    dialog = app_module.confirm_bulk(fake, "已归档的会话", hidden=4)
    check("按 s" not in dialog._body, f"批量删除提示不提按键：{dialog._body.splitlines()[-1]}")
    check("4 个" in dialog._body, "说明有多少个是看不见的")
    check(
        f"还有 {len(fake) - app_module.BULK_PREVIEW_LIMIT} 个" in str(dialog._subject),
        "标题列表过长时折叠",
    )


async def test_footer_fits(codex_home: Path, claude_home: Path) -> None:
    """The footer is the only place the keys are advertised, so it has to fit.

    Textual lays the keys out in binding order and simply runs off the edge of
    a narrow terminal, which used to take `q 退出` with it.
    """
    print("\n[7.2b] 底栏宽度")
    for label, backend in (
        ("Codex", CodexBackend(codex_home)),
        ("Claude Code", ClaudeBackend(claude_home)),
    ):
        for width in (80, 100):
            app = SessionCleanerApp(backend)
            async with app.run_test(size=(width, 25)) as pilot:
                await pilot.pause()
                await pilot.pause(0.3)
                keys = list(app.query_one("Footer").query("FooterKey"))
                visible = [str(k.render()).strip() for k in keys if k.region.right <= width]
                needed = max(k.region.right for k in keys)
                if width == 100:
                    check(
                        needed <= width,
                        f"{label} 在 {width} 列下按键全部显示（需要 {needed} 列）",
                    )
                check(
                    any(v.startswith("q ") for v in visible),
                    f"{label} 在 {width} 列下退出键仍然可见",
                )


async def test_picker_feedback() -> None:
    print("\n[7.3] 选择界面反馈")
    empty = Path(tempfile.mkdtemp(prefix="asc-none-"))
    try:
        picker = AgentPicker({"codex": empty / "no", "claude": empty / "no2"})
        async with picker.run_test(size=(100, 20)) as pilot:
            await pilot.pause()
            await pilot.pause(0.3)
            await pilot.press("x")
            await pilot.pause(0.2)
            hint = str(picker.query_one("#picker-hint").visual)
            check(
                picker.is_running and "还没有会话记录" in hint,
                f"按下没有记录的 agent 会说明原因：{hint.strip()}",
            )
            await pilot.press("q")
    finally:
        shutil.rmtree(empty, ignore_errors=True)


def test_clipboard_helper() -> None:
    print("\n[7.4] 剪贴板")
    scratch = Path(tempfile.mkdtemp(prefix="asc-clip-"))
    saved = os.environ["PATH"]
    try:
        sink = scratch / "pasted.txt"
        fake = scratch / "pbcopy"
        # Written in Python rather than shell: PATH is about to point at this
        # directory alone, so the stand-in cannot rely on finding `cat`.
        fake.write_text(
            f"#!{sys.executable}\n"
            "import pathlib, sys\n"
            f"pathlib.Path({str(sink)!r}).write_bytes(sys.stdin.buffer.read())\n",
            encoding="utf-8",
        )
        fake.chmod(0o755)
        os.environ["PATH"] = str(scratch)
        check(clipboard.copy("你好 world"), "找到原生工具时复制成功")
        check(sink.read_text(encoding="utf-8") == "你好 world", "内容原样送出去")

        os.environ["PATH"] = str(scratch / "nothing")
        check(clipboard.copy("x") is False, "没有原生工具时如实返回 False")

        fake.write_text(f"#!{sys.executable}\nraise SystemExit(3)\n", encoding="utf-8")
        os.environ["PATH"] = str(scratch)
        check(clipboard.copy("x") is False, "工具报错时不谎报成功")
    finally:
        os.environ["PATH"] = saved
        shutil.rmtree(scratch, ignore_errors=True)


def test_packaging() -> None:
    print("\n[7.5] 打包")
    from importlib.metadata import PackageNotFoundError, version

    root = Path(__file__).resolve().parents[1]
    pyproject = (root / "pyproject.toml").read_text(encoding="utf-8")
    check("dynamic = [\"version\"]" in pyproject, "版本号只写在包里，构建时读取")
    check(f'"{__version__}"' not in pyproject, f"pyproject 里没有第二份版本号（{__version__}）")
    try:
        installed = version("agent-session-cleaner")
    except PackageNotFoundError:
        skip("没有安装成包，跳过版本一致性检查")
    else:
        check(installed == __version__, f"安装后的版本与源码一致：{installed}")
    readme = (root / "README.md").read_text(encoding="utf-8")
    keys = [line for line in readme.splitlines() if line.startswith("| `")]
    check(len(keys) > 10, f"README 列出了 {len(keys)} 个按键")


def main() -> int:
    # Sections that only read run against synthetic homes so the suite behaves
    # the same everywhere; the ones that delete get their own copy, because
    # they leave the tree in pieces.
    temporary: list[Path] = []

    def fixture(build) -> Path:
        home = build()
        temporary.append(home)
        return home

    try:
        test_codex_data()
        test_claude_data()

        codex_fixture = fixture(make_codex_fixture)
        claude_fixture = fixture(make_claude_home)
        asyncio.run(test_wording_and_missing_deps(codex_fixture, claude_fixture))
        asyncio.run(test_copy_resume(codex_fixture, claude_fixture))
        asyncio.run(test_search(codex_fixture))
        asyncio.run(test_picker(codex_fixture, claude_fixture))

        codex_home = make_codex_home()
        if codex_home is None:
            print("\n[5] Codex 变更 / 临时 CODEX_HOME")
            skip("没有 codex 命令行工具或可用素材，跳过变更测试")
        else:
            temporary.append(codex_home)
            asyncio.run(test_codex_mutations(codex_home))

        asyncio.run(test_claude_mutations(fixture(make_claude_home)))

        regression_home = fixture(make_claude_home)
        test_parsing_regressions(regression_home)
        test_day_labels()
        asyncio.run(test_ui_regressions(regression_home))
        asyncio.run(test_footer_fits(codex_fixture, regression_home))
        asyncio.run(test_picker_feedback())
        test_clipboard_helper()
        test_packaging()
    finally:
        for home in temporary:
            shutil.rmtree(home, ignore_errors=True)

    failed = [label for ok, label in checks if not ok]
    tail = f"，跳过 {len(skipped)} 项" if skipped else ""
    print(f"\n{len(checks) - len(failed)}/{len(checks)} 通过{tail}")
    for label in skipped:
        print(f"  SKIPPED: {label}")
    for label in failed:
        print(f"  FAILED: {label}")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
