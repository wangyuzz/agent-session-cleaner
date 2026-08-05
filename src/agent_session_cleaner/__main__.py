"""Entry point: `agent-session-cleaner [codex|claude]`."""

from __future__ import annotations

import argparse
import os
from pathlib import Path

from . import __version__


def main() -> None:
    os.environ.setdefault("TEXTUAL_COLOR_SYSTEM", "truecolor")

    from .app import SessionCleanerApp
    from .backends import BACKEND_CLASSES, BACKEND_IDS, build
    from .picker import AgentPicker

    parser = argparse.ArgumentParser(
        prog="agent-session-cleaner",
        description="浏览和清理 Codex、Claude Code 的历史会话。",
        add_help=False,
    )
    parser.add_argument("-h", "--help", action="help", help="显示帮助并退出")
    parser.add_argument(
        "agent",
        nargs="?",
        choices=BACKEND_IDS,
        help="要清理的会话来源；省略时打开选择界面",
    )
    parser.add_argument(
        "--codex-home",
        type=Path,
        default=None,
        metavar="目录",
        help="Codex 会话数据目录（默认：~/.codex）",
    )
    parser.add_argument(
        "--claude-home",
        type=Path,
        default=None,
        metavar="目录",
        help="Claude Code 会话数据目录（默认：~/.claude）",
    )
    parser.add_argument(
        "--version",
        action="version",
        version=f"%(prog)s {__version__}",
        help="显示版本号并退出",
    )
    args = parser.parse_args()

    homes = {
        "codex": args.codex_home.expanduser() if args.codex_home else None,
        "claude": args.claude_home.expanduser() if args.claude_home else None,
    }

    agent = args.agent
    if agent is None:
        agent = AgentPicker(homes).run()
        if agent is None:  # quit from the picker
            return

    backend = build(agent, homes.get(agent))
    if not backend.home.is_dir():
        label = BACKEND_CLASSES[agent].label
        parser.error(f"找不到 {label} 会话目录：{backend.home}")

    SessionCleanerApp(backend).run()


if __name__ == "__main__":
    main()
