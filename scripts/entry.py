"""Entry point for the frozen binary.

PyInstaller runs its target script as ``__main__``, which breaks the relative
imports inside ``agent_session_cleaner/__main__.py``. Handing it this stub
instead keeps the package importable under its own name.
"""

from agent_session_cleaner.__main__ import main

main()
