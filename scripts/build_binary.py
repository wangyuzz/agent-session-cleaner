"""Freeze the app into one self-contained executable, then prove it runs.

Run through ``scripts/build.sh``, which prepares an isolated environment first.
The same script builds on macOS and inside the Linux container, so the two
platforms cannot drift apart.

The result lands in ``dist/agent-session-cleaner-<os>-<arch>``: a single file
with no Python required on the machine that runs it.
"""

from __future__ import annotations

import fcntl
import hashlib
import json
import os
import platform
import select
import shutil
import struct
import subprocess
import sys
import tempfile
import termios
import time
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
NAME = "agent-session-cleaner"

# What `platform.machine()` reports, mapped to the names used in the release
# assets. Two spellings of the same chip would send people to the wrong file.
ARCH_NAMES = {
    "arm64": "arm64",
    "aarch64": "arm64",
    "x86_64": "x86_64",
    "amd64": "x86_64",
}
OS_NAMES = {"Darwin": "macos", "Linux": "linux"}


def target_name() -> str:
    system = platform.system()
    machine = platform.machine().lower()
    if system not in OS_NAMES or machine not in ARCH_NAMES:
        raise SystemExit(f"暂不支持在 {system}/{machine} 上构建")
    return f"{NAME}-{OS_NAMES[system]}-{ARCH_NAMES[machine]}"


def freeze(target: Path) -> None:
    work = ROOT / "build" / "pyinstaller"
    subprocess.run(
        [
            sys.executable,
            "-m",
            "PyInstaller",
            "--onefile",
            "--console",
            "--noconfirm",
            "--clean",
            "--name",
            target.name,
            # The .tcss files sit beside the modules that name them, and
            # Textual resolves CSS_PATH relative to those modules.
            "--collect-data",
            "agent_session_cleaner",
            # Textual reaches for drivers and widgets by name at runtime, so
            # static analysis alone leaves holes.
            "--collect-all",
            "textual",
            "--copy-metadata",
            "textual",
            # Nothing here draws a window or runs a test suite.
            "--exclude-module",
            "tkinter",
            "--exclude-module",
            "_tkinter",
            "--exclude-module",
            "pytest",
            "--distpath",
            str(ROOT / "dist"),
            "--workpath",
            str(work),
            "--specpath",
            str(work),
            str(ROOT / "scripts" / "entry.py"),
        ],
        check=True,
        cwd=ROOT,
    )


def sample_home(root: Path) -> Path:
    """A one-session Claude Code home, so the smoke test has something to draw."""
    home = root / "claude"
    project = home / "projects" / "-tmp-demo"
    project.mkdir(parents=True)
    session_id = str(uuid.uuid4())
    records = [
        {
            "type": "user",
            "sessionId": session_id,
            "cwd": "/tmp/demo",
            "version": "2.0.0",
            "timestamp": "2026-01-01T08:00:00.000Z",
            "message": {"role": "user", "content": "冒烟测试"},
        },
        {
            "type": "assistant",
            "sessionId": session_id,
            "timestamp": "2026-01-01T08:00:01.000Z",
            "message": {"role": "assistant", "content": [{"type": "text", "text": "收到"}]},
        },
    ]
    lines = [json.dumps(record, ensure_ascii=False) for record in records]
    (project / f"{session_id}.jsonl").write_text("\n".join(lines) + "\n", encoding="utf-8")
    return home


def run_in_terminal(binary: Path, home: Path, timeout: float = 40.0) -> tuple[int, str]:
    """Start the TUI on a pseudo-terminal, let it draw, then press `q`.

    A binary that merely answers `--version` proves very little: the interesting
    failures of a frozen bundle — a stylesheet that was left behind, a widget
    module that never made it in — only show up once the app actually paints.
    """
    master_fd, slave_fd = os.openpty()
    fcntl.ioctl(slave_fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
    env = dict(os.environ, TERM="xterm-256color", COLUMNS="120", LINES="40")
    process = subprocess.Popen(
        [str(binary), "claude", "--claude-home", str(home)],
        stdin=slave_fd,
        stdout=slave_fd,
        stderr=slave_fd,
        env=env,
        close_fds=True,
    )
    os.close(slave_fd)

    output = bytearray()
    deadline = time.monotonic() + timeout
    quit_at = time.monotonic() + 3.0
    quit_sent = False
    try:
        while time.monotonic() < deadline:
            readable, _, _ = select.select([master_fd], [], [], 0.2)
            if readable:
                try:
                    chunk = os.read(master_fd, 65536)
                except OSError:  # the child closed the terminal
                    break
                if not chunk:
                    break
                output += chunk
            if not quit_sent and time.monotonic() >= quit_at:
                os.write(master_fd, b"q")
                quit_sent = True
            if process.poll() is not None and not readable:
                break
        try:
            code = process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            code = process.wait()
            output += "\n[按下 q 之后没有退出]".encode()
    finally:
        os.close(master_fd)
    return code, output.decode("utf-8", errors="replace")


def smoke_test(binary: Path) -> None:
    version = subprocess.run(
        [str(binary), "--version"], capture_output=True, text=True, timeout=60
    )
    if version.returncode != 0 or NAME not in version.stdout:
        raise SystemExit(f"--version 没有正常输出：{version.stdout!r} {version.stderr!r}")
    print(f"  --version  {version.stdout.strip()}")

    with tempfile.TemporaryDirectory(prefix="asc-smoke-") as tmp:
        home = sample_home(Path(tmp))
        code, screen = run_in_terminal(binary, home)
    if code != 0:
        tail = screen[-2000:]
        raise SystemExit(f"启动后没有正常退出（退出码 {code}）：\n{tail}")
    for expected in ("冒烟测试", "退出"):
        if expected not in screen:
            tail = screen[-2000:]
            raise SystemExit(f"界面里没有出现「{expected}」：\n{tail}")
    print("  终端里跑通了：会话列表、详情、按 q 退出")


def main() -> None:
    target = ROOT / "dist" / target_name()
    if target.exists():
        target.unlink()

    print(f"构建 {target.name}")
    freeze(target)
    if not target.is_file():
        raise SystemExit(f"没有生成 {target}")
    target.chmod(0o755)

    print("冒烟测试")
    smoke_test(target)

    digest = hashlib.sha256(target.read_bytes()).hexdigest()
    size = target.stat().st_size / 1024 / 1024
    print(f"\n{target}  {size:.1f} MiB\nsha256  {digest}")

    # The build tree is a few hundred megabytes of intermediate objects and is
    # of no use once the binary is verified.
    shutil.rmtree(ROOT / "build" / "pyinstaller", ignore_errors=True)


if __name__ == "__main__":
    main()
