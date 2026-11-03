# Phase 0 research: aprofile slice one

**Feature**: 001-aprofile-standalone-profile-cli
**Date**: 2026-11-03
**Inputs**: spec.md Assumptions (deferred-to-planning items), constitution v1.0.4.

Each decision is stated as Decision / Rationale / Alternatives, followed by the concrete per-harness
values the built-in definitions will carry. Facts were gathered from official docs and project
repositories on 2026-11-03; items that are version-dependent are listed under "Open verification
items" and must be confirmed with a quick live check during implementation (not blockers).

## Cross-cutting decision: harness definitions redirect a *set* of env vars (not one)

**Decision**: The harness definition does not carry a single `config_dir_var`. It carries
`config_env`: an ordered map of environment variable -> path (relative to the profile config-dir
root, or `.` for the root itself). `sync` and the wrapper set every entry; `APROFILE_CONFIG_DIR` is
always the profile root. Layer classification (FR-013) operates on paths under that root.

**Rationale**: Research shows the four harnesses do not share one "home" variable. Claude and Codex
have a single dir var; pi has one dir var plus a separate session override; OpenCode has no home var
at all and is isolated through the XDG base-directory set. Modeling this as data (a var->path map)
keeps Constitution Principle I intact: supporting OpenCode's four variables or pi's two is a
definition change, not a core-code change. FR-020 already requires exactly this ("the definition
MUST redirect those locations into the profile").

**Alternatives considered**: (a) a single `config_dir_var` with special-case code for OpenCode -
rejected, violates Principle I; (b) repointing `$HOME` per profile - rejected, too invasive and
breaks shared shell/tooling. See the per-harness `config_env` values below.

---

## Research item 1 - Claude Code macOS Keychain separation for `CLAUDE_CONFIG_DIR`

**Decision**: Isolate Claude profiles with `CLAUDE_CONFIG_DIR = <profile root>`. Per the current
official authentication docs this relocates `settings.json`, `projects/` (session history), plugins,
`.claude.json` and `.credentials.json`, and keys the macOS Keychain credential entry to the config
dir, so subscription-OAuth login, API keys and the file fallback are all per-profile. Default new
Claude profiles to an **env-based auth kind** (`ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN`), which
is inherently per-profile and Keychain-independent; offer subscription-OAuth login as a second auth
kind that relies on the `CLAUDE_CONFIG_DIR` keying.

**Rationale**: Env-based auth needs no Keychain and satisfies Principle III by construction on both
platforms, so it is the safe default for a credential-isolation tool. The Keychain keying covers the
OAuth-login case per the docs. Two caveats are recorded in the definition and `doctor`:
1. Anthropic Console keyless/federation sign-ins are stored outside the config dir (in
   `~/.config/anthropic`) and are **not** isolated by `CLAUDE_CONFIG_DIR`. aprofile documents this as
   an unsupported auth type for isolation (the user picks an env-based or OAuth-login kind instead).
2. A reported bug deletes the shared Keychain entry when `CLAUDE_CODE_OAUTH_TOKEN` is set as an env
   var and the process exits. aprofile therefore never mixes an env OAuth-token kind with a
   Keychain login in the same profile, and `doctor` warns if both are present.

**Alternatives considered**: `apiKeyHelper` script (more moving parts); repointing `$HOME` (too
invasive). **Status**: the per-config-dir Keychain keying is version-dependent (docs reflect
up to v2.1.299) -> Open verification item V1.

Claude built-in definition values: `config_env = {CLAUDE_CONFIG_DIR: "."}`; `.claude.json` is a
**mixed** file (`oauthAccount`/account keys = credentials, `mcpServers` = owned, `projects` and
`numStartups` = state); `.credentials.json` = credentials; `projects/`, `statsig/`, history/logs =
state; `skills/` = owned; everything else shared. MCP: JSON, `mcpServers` object under `.claude.json`
(user scope), stdio `command`/`args`/`env`, remote `type: sse|http`/`url`/`headers`. Auth kinds:
`api-key` (`ANTHROPIC_API_KEY`), `auth-token` (`ANTHROPIC_AUTH_TOKEN`), `oauth-login` (Keychain/file
via login flow), `oauth-token` (`CLAUDE_CODE_OAUTH_TOKEN`, flagged against Keychain mixing).

## Research item 2 - OpenCode isolation lever (`OPENCODE_DB` vs `XDG_DATA_HOME`)

**Decision**: Isolate OpenCode with the **XDG base-directory set**, not `OPENCODE_DB`.
`config_env = {XDG_DATA_HOME: "data", XDG_CONFIG_HOME: "config", XDG_CACHE_HOME: "cache",
XDG_STATE_HOME: "state"}`. `XDG_DATA_HOME` relocates auth (`auth.json`) and sessions (the
`opencode.db` SQLite database) together; `XDG_CONFIG_HOME` relocates `opencode.json` (config + MCP).

**Rationale**: `OPENCODE_DB` moves only the database file and leaves `auth.json` shared, so it fails
credential isolation (Principle III). There is no single `OPENCODE_HOME` (it is an open upstream
request), so the XDG set is the supported lever. The data-dir default `~/.local/share/opencode` is
exactly `$XDG_DATA_HOME/opencode`, confirming the mapping.

**Alternatives considered**: `OPENCODE_DB` alone (rejected: leaves auth shared);
`OPENCODE_CONFIG_DIR` (an augmenting overlay, not a clean relocation). **Caveat**: OpenCode merges
config across layers, and its skills lookup includes shared `~/.claude/skills` and `~/.agents/skills`
(see item 4). **Status**: exact completeness of the XDG set is Open verification item V2
(`opencode debug paths`).

OpenCode built-in definition values (relative to the profile root): `data/opencode/auth.json` =
credentials; `data/opencode/opencode.db` and other data-dir state = state; `config/opencode/opencode.json`
= mixed (the `mcp` key = owned, rest shared-seeded); `config/opencode/skills/` = owned. MCP: JSON,
`mcp` key; local `type: local`/`command[]`/`environment`/`cwd`/`enabled`/`timeout`; remote
`type: remote`/`url`/`headers`/`oauth`/`enabled`; secret refs `{env:NAME}`.

## Research item 3 - pi MCP support and config location

**Decision**: pi supports MCP. Isolate pi with `config_env = {PI_CODING_AGENT_DIR: "agent",
PI_CODING_AGENT_SESSION_DIR: "agent/sessions"}`. The agent dir holds `auth.json`, `settings.json`,
`mcp.json`, `mcp-auth.json`, `models.json`, `skills/`, `prompts/` and (by default) sessions; pinning
the session dir guarantees session isolation even if an extension overrides it.

**Rationale**: pi (earendil-works/pi, pi.dev) isolates the most cleanly of the four through a single
agent dir, and skills/MCP all live under it. `PI_CODING_AGENT_DIR` points at the `agent` leaf dir,
not a `.pi` parent, so the definition maps it to `agent` under the profile root. MCP is first-class
(`pi mcp add/list/...`).

**Alternatives considered**: relying on the agent-dir default for sessions (rejected: an override can
escape it; pinning is safer). **Status**: whether the default session dir sits under the agent dir or
needs the explicit pin is Open verification item V3.

pi built-in definition values (relative to the profile root): `agent/auth.json` and
`agent/mcp-auth.json` = credentials; `agent/sessions/`, logs = state; `agent/mcp.json`,
`agent/settings.json` (owned keys), `agent/skills/`, `agent/prompts/` = owned. MCP: JSON,
`mcpServers` object (Claude-compatible) in `agent/mcp.json`; stdio `command`/`args`/`env`/`cwd`;
http `url`/`headers`/`oauth`; `type` in `stdio|http|streamable-http` (SSE rejected); `timeout`
(seconds); refs `${VAR}` and `!command`.

## Research item 4 - per-profile skills vs shared `~/.agents/skills`

**Decision**: Skills are **per-profile (owned, FR-016)** for all four harnesses via each harness's
config/home dir; the global shared fallbacks are classified **shared and reported (FR-021)**:

| Harness | Per-profile skills (owned) | Shared/global skills (reported, not isolated) |
|---------|----------------------------|-----------------------------------------------|
| Claude | `$CLAUDE_CONFIG_DIR/skills` | project `.claude/skills` (repo-scoped) |
| Codex | `$CODEX_HOME/skills` | `$HOME/.agents/skills` (global) |
| OpenCode | `$XDG_CONFIG_HOME/opencode/skills` | `~/.claude/skills`, `~/.agents/skills` (global) |
| pi | `<agent-dir>/skills` | (none; pi has no `~/.agents` lookup) |

**Rationale**: This refines the spec Assumption (which expected skills to be shared via
`~/.agents/skills`): a genuine per-profile location exists for every harness, so skills travel with
`copy` as owned. Codex and OpenCode additionally read a global `~/.agents/skills` (and OpenCode
`~/.claude/skills`) keyed to `$HOME`; those cannot be isolated without repointing `$HOME`, so they
are treated as shared and `harness show`/`copy`/`doctor` report them (FR-021, FR-036). No spec change
is required: FR-016 covers the owned per-profile skills and FR-021 covers the shared global fallback.

**Alternatives considered**: forcing all skills through `~/.agents/skills` as shared (rejected:
loses per-profile skills that `copy` should carry); repointing `$HOME` (rejected: invasive).
**Status**: Codex's exact skills-root precedence is in flux upstream -> Open verification item V4.

## Technology choices

- **Language/runtime**: Go 1.23 (single static binary), module `github.com/cc-deck/aprofile`
  (constitution). **Rationale**: constitution-mandated; static binary suits a PATH wrapper manager.
- **CLI/TUI**: cobra (commands), lipgloss + bubbletea + huh (output, browse, forms) - constitution
  stack, shared with cc-setup. **Rationale**: enables later cc-setup code reuse.
- **Config formats**: aprofile's own profile store and harness definitions in **YAML**
  (`gopkg.in/yaml.v3`) for hand-editing; the registry in **JSON** (stdlib `encoding/json`) for
  machine consumers (FR-030). Harness config files are read/written in their native format: JSON via
  stdlib, TOML via `github.com/pelletier/go-toml/v2`. **Rationale**: readability for human-edited
  files, native fidelity for harness files.
- **Merge-preserving edits** (FR-013, FR-019): decode harness JSON files to
  `map[string]json.RawMessage` (and TOML to `map[string]any`), modify only owned keys, re-encode, so
  keys aprofile does not own are preserved byte-for-byte where possible. **Rationale**: never fight
  the harness (Principle II). **Alternative rejected**: full struct models of each harness file (too
  brittle; would drop unknown keys).
- **Wrappers**: generated POSIX `sh` scripts using only builtins plus `exec` (FR-026); references
  resolved at launch, other values shell-quoted (FR-028b). **Rationale**: self-contained, near-zero
  overhead meets SC-011.
- **Testing**: Go `testing`, table-driven; `t.TempDir()` + `t.Setenv()` for hermetic HOME/XDG
  (Principle VII); round-trip MCP conversion tests per harness pair (FR via SC-005); a no-op stub
  harness binary for wrapper/overhead tests (SC-011). **Rationale**: constitution VII.
- **Atomic writes**: write-temp-then-rename for the store and registry (FR-030, edge case:
  concurrent invocations).

## Open verification items (implementation-time, not blockers)

- **V1 (Claude/macOS)**: on a real Mac, confirm two `CLAUDE_CONFIG_DIR` values produce two distinct
  Keychain entries on the installed version; capture the item/service name. If keying is not
  per-dir on the target version, Claude profiles on macOS fall back to env-based auth only and the
  definition documents the limitation (spec Assumption already allows this).
- **V2 (OpenCode)**: run `opencode debug paths` to confirm the current XDG var set the release reads.
- **V3 (pi)**: confirm whether the default session dir is under `PI_CODING_AGENT_DIR` or needs the
  explicit `PI_CODING_AGENT_SESSION_DIR` pin.
- **V4 (Codex)**: confirm which skills roots the installed version scans (`$CODEX_HOME/skills` vs
  `$HOME/.agents/skills` precedence).

Each item has a safe default already chosen above, so none blocks implementation; they are confirmed
against a live install when the corresponding built-in definition is finalized.
