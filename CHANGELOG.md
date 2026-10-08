# Changelog

## Unreleased

- Add `--export-dir` and `ASC_EXPORT_DIR` to save Markdown exports in an existing directory, with English and Chinese command help.
- Prevent concurrent exports from overwriting existing files, and remove incomplete output after an error or cancellation.
- Sanitize session IDs as well as titles in file names, handle Windows reserved names, and bound Unicode file-name lengths.
- Write combined exports one session at a time instead of assembling every session into one large buffer.
- Create exports with owner-only permissions on Unix systems.
- Mark messages truncated by the session preview so an export does not imply that omitted text was preserved.
- Document this fork's origin, export behavior, and contribution checks in English and Chinese.
- Make real-session integration tests opt-in with `ASC_TEST_REAL_SESSIONS=1`, keeping normal test runs independent of private local history.

## 0.12.0

- Copy ready-to-run resume commands and export session previews as Markdown.

## 0.11.0

- Add Grok session browsing and deletion.

## 0.10.1

- Allow Windows updates while the previous executable is running.

## 0.10.0

- Add Windows native keyboard controls and improve PowerShell installation compatibility.
