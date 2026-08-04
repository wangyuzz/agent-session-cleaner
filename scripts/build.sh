#!/usr/bin/env bash
# Build the binary for whatever machine (or container) this runs in.
#
#   ./scripts/build.sh              on macOS, for this Mac's architecture
#   ./scripts/build.sh              inside the Linux image, for its architecture
#
# The build environment is thrown away and rebuilt every time so that a stale
# dependency in someone's .venv can never end up baked into a release.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

if ! command -v uv >/dev/null 2>&1; then
    echo "需要先安装 uv：https://github.com/astral-sh/uv" >&2
    exit 1
fi

# 3.12 rather than the newest Python: PyInstaller's support trails each release
# by a few months, and a build tool is the wrong place to be an early adopter.
venv="$root/build/venv"
rm -rf "$venv"
uv venv --python 3.12 --quiet "$venv"
VIRTUAL_ENV="$venv" uv pip install --quiet --python "$venv" . pyinstaller

"$venv/bin/python" scripts/build_binary.py "$@"
