# Brainstorm Overview

Last updated: 2026-11-13

## Sessions

| # | Date | Topic | Status | Spec | Issue |
|---|------|-------|--------|------|-------|
| 001 | 2026-11-02 | aprofile-standalone-profile-cli | active | - | - |
| 002 | 2026-11-07 | history-groups | active | - | - |
| 003 | 2026-11-09 | corgi-integration | active | - | - |
| 005 | 2026-11-10 | companion-tool-files | active | - | - |
| 006 | 2026-11-10 | neutralize-provider-switches | active | - | - |
| 007 | 2026-11-13 | profile-features | active | - | - |

## Open Threads

- Configuration format and layout for profile definitions, and where the wrapper bin directory lives. (from #001)
- Harness definition schema: fields, override semantics between built-in and `harnesses.d`, merge rules for TOML and JSON patches, exec hook operations. (from #001)
- Claude Code: how does the owned layer split `.claude.json`, and does `CLAUDE_CONFIG_DIR` separate the macOS Keychain OAuth entry per profile? (from #001)
- Codex: separate `CODEX_HOME` per profile versus native `<name>.config.toml` profiles, and whether both can be combined with `--profile`. (from #001)
- OpenCode: which lever isolates sessions and auth per profile, `OPENCODE_DB` or `XDG_DATA_HOME`? Needs verification on a current release. (from #001)
- Skills for Codex, OpenCode and pi live in `~/.agents/skills`, outside any profile dir. Per-profile location or accepted as shared? (from #001)
- Is separate cost tracking satisfied by separate state dirs, or should aprofile report per-profile usage itself? (from #001)
- Distribution: Homebrew tap and GoReleaser. (from #001)
- Slice two, absorbing cc-setup, is a separate spec; the cc-deck decoupling is a separate brainstorm in the cc-deck repository. (from #001)
- Exact history entry list per harness: which entries are shared history and which are per-process runtime state? (from #002)
- Can OpenCode's history be grouped when sessions, auth and other state share one SQLite database? (from #002)
- Does Codex tolerate a linked `state_5.sqlite` and session index used by profiles running different Codex versions? (from #002)
- Where do non-`home` group stores live in aprofile's data directory, and how are they backed up? (from #002)
- Command and flag syntax for group management and membership. (from #002)
- Fate of a group store when its last member leaves. (from #002)
- How `copy` treats group membership. (from #002)
- How a profile leaves `home` to stop sharing with the default setup. (from #002)
- Codex: `auth` command block versus `env_key` in the same provider block. (from #003)
- Launch settings for a profile at share level `all` (linked default settings): refuse, warn, or require `content`? (from #003)
- Removing owned `env` entries when launch settings or the auth kind change. (from #003)
- Corgi default accounts (`claude/private` via the daemon, `~/.codex`): hide or keep? (from #003)
- Where rules for companion tool files live: built-in definitions, user `harnesses.d`, or companion fragments? (from #005)
- Should aprofile take Corgi's `.corgi-hooks.lock` when writing `settings.json` or `config.toml`? (from #005)
- Does a ChatGPT-login Codex profile honor an exported `OPENAI_API_KEY` or `OPENAI_BASE_URL`? (from #006)
- Does a login profile with only neutralizing entries count as "self-contained" in `show`/`list`? (from #006)
- Wording and fix hint of the `show`/`doctor` warning for `all` login profiles. (from #006)
- Exact TOML encoding of Codex `[otel]` exporters, to verify against a current release. (from #007)
- Secret-bearing feature parameters (OTLP headers, auth) and the ref/literal distinction. (from #007)
- `doctor` warnings for features depending on undocumented, version-dependent mechanisms. (from #007)
- `features` interaction with `settings pull`. (from #007)

## Parked Ideas

- None yet.
