# Brainstorm Overview

Last updated: 2026-11-02

## Sessions

| # | Date | Topic | Status | Spec | Issue |
|---|------|-------|--------|------|-------|
| 001 | 2026-11-02 | aprofile-standalone-profile-cli | active | - | - |

## Open Threads

- Configuration format and layout for profile definitions, and where the wrapper bin directory lives. (from #001)
- Harness definition schema: fields, override semantics between built-in and `harnesses.d`, merge rules for TOML and JSON patches, exec hook operations. (from #001)
- Claude Code: how does the owned layer split `.claude.json`, and does `CLAUDE_CONFIG_DIR` separate the macOS Keychain OAuth entry per profile? (from #001)
- Codex: separate `CODEX_HOME` per profile versus native `<name>.config.toml` profiles, and whether both can be combined with `--profile`. (from #001)
- OpenCode: which lever isolates sessions and auth per profile, `OPENCODE_DB` or `XDG_DATA_HOME`? Needs verification on a current release. (from #001)
- Skills for Codex, OpenCode and pi live in `~/.agents/skills`, outside any profile dir. Per-profile location or accepted as shared? (from #001)
- Is separate cost tracking satisfied by separate state dirs, or should aprofile report per-profile usage itself? (from #001)
- Registry format: adopt Omarchy's schema verbatim for corgi, or an own schema plus an Omarchy export? (from #001)
- Distribution: Homebrew tap and GoReleaser. (from #001)
- Slice two, absorbing cc-setup, is a separate spec; the cc-deck decoupling is a separate brainstorm in the cc-deck repository. (from #001)

## Parked Ideas

- None yet.
