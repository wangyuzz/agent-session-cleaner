"""Entry point: ``agent-session-cleaner [codex|claude|opencode]``."""

from __future__ import annotations

import argparse
import os
from pathlib import Path

from . import __version__
from .i18n import argparse_text, t


def main() -> None:
    os.environ.setdefault("TEXTUAL_COLOR_SYSTEM", "truecolor")
    # argparse localizes its own structural labels through this module-level
    # hook. Point it at the same locale as the rest of the application so an
    # explicit language override also covers usage and parser errors.
    argparse._ = argparse_text

    from .app import SessionCleanerApp
    from .backends import BACKEND_CLASSES, BACKEND_IDS, build
    from .picker import AgentPicker

    parser = argparse.ArgumentParser(
        prog="agent-session-cleaner",
        description=t("cli_description"),
        add_help=False,
    )
    parser.add_argument("-h", "--help", action="help", help=t("cli_help"))
    parser.add_argument(
        "agent",
        nargs="?",
        choices=BACKEND_IDS,
        help=t("cli_agent"),
    )
    # One --<agent>-home per backend, in the order the chooser lists them.
    for backend_id in BACKEND_IDS:
        parser.add_argument(
            f"--{backend_id}-home",
            type=Path,
            default=None,
            metavar=t("cli_directory"),
            help=t(f"cli_{backend_id}_home"),
        )
    parser.add_argument(
        "--version",
        action="version",
        version=f"%(prog)s {__version__}",
        help=t("cli_version"),
    )
    args = parser.parse_args()

    homes = {
        backend_id: home.expanduser() if (home := getattr(args, f"{backend_id}_home")) else None
        for backend_id in BACKEND_IDS
    }

    agent = args.agent
    if agent is None:
        agent = AgentPicker(homes).run()
        if agent is None:  # quit from the picker
            return

    backend = build(agent, homes.get(agent))
    if not backend.home.is_dir():
        label = BACKEND_CLASSES[agent].label
        parser.error(t("cli_missing_home", agent=label, path=backend.home))
    # Say now if this directory is one the agent could never be pointed at,
    # rather than listing sessions that nothing may then be done to.
    problem = backend.home_problem()
    if problem:
        parser.error(problem)

    SessionCleanerApp(backend).run()


if __name__ == "__main__":
    main()
