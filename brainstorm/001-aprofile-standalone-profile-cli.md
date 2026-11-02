# Brainstorm: aprofile, a Standalone Harness Profile CLI

**Date:** 2026-11-02
**Status:** active
**Origin:** cc-deck brainstorm 099 (`brainstorm/099-aprofile-standalone-profile-cli.md` in github.com/cc-deck/cc-deck), copied here as the seed for slice one.

## Problem Framing

cc-deck grew a profile feature (brainstorms 093 and 098, spec 087) that lets one user run several accounts of the same coding harness side by side. `claude-work`, `claude-private` and `codex-private` are generated wrappers that point the harness at its own config directory and credentials. The feature lives in `internal/profile` (about 5000 lines with tests) and knows three harnesses: Claude Code, Codex and OpenCode. It is tied to cc-deck in several places: the wrapper env contract that the sidebar and hooks read, hook installation into per-profile config dirs, provisioning to SSH and OpenShell workspaces, snapshot restore by wrapper name, and the Kubernetes deploy path.

Profiles are useful without cc-deck. The user wants them in a dedicated CLI in a new project, named **aprofile** (agent profile), that:

- manages the startup wrappers and puts them on `PATH` through a `sync` command;
- defines labels, colors and icons per profile;
- holds a detailed alternative configuration per harness, for Claude Code, Codex, OpenCode and pi, including common knobs such as the endpoint URL (`ANTHROPIC_BASE_URL` for Claude Code), model and auth;
- is extensible to new harnesses through an SPI that does not require touching the tool's core;
- keeps each profile's state apart: sessions, transcripts, history, logs, auto-memory, and therefore usage and cost;
- can copy a profile together with its MCP servers, skills and plugins, and convert MCP servers between harnesses;
- offers a flexible, colorful UI and plain command-line management;
- runs on Linux and macOS;
- plays nicely with herdr and corgi without modifying either, and lets cc-deck be updated easily to consume its artifacts.

**Success** looks like this: from any shell the user types `claude-work` or `codex-private` and lands in the right account with the right model, endpoint and MCP servers, and nothing from that session leaks into another profile's history or bill. Adding pi or a future harness is a definition file, not a release. cc-deck, corgi and herdr can tell which profile a pane runs by reading what aprofile publishes.

**Failure, even if it technically works:**

- a second Claude-only tool next to cc-setup;
- a tool that must be edited every time a harness ships a new flag;
- wrappers that embed secrets;
- a tool that fights the harness by regenerating files the harness writes at runtime;
- a tool that needs changes inside herdr or corgi to be useful.

Three related decisions were made during the session and are recorded here:

1. **cc-deck is completely decoupled from aprofile.** The cc-deck side (removing `internal/profile`, consuming aprofile's artifacts, remote provisioning) is a separate brainstorm. The coupling points are listed below so that session has a starting point.
2. **cc-setup is absorbed by aprofile over time**, not extended into a multi-harness tool. Slice one of aprofile is the profile side; slice two ports cc-setup's per-project MCP activation, plugin toggling and permission presets onto the same harness definitions, after which cc-setup is frozen with a pointer to aprofile.
3. **Claude Code auto-memory follows the profile.** It lives in `projects/<project>/memory/` next to the transcripts and is treated as per-profile state.

## Current State

**cc-deck `internal/profile`.** A `Translator` interface per harness (`ConfigDirEnv`, `IsolatedEntries`, `AuthVars`, `Backends`, `DefaultConfigSubdir`, `Render`, `PrepareConfigDir`, `ProviderType`). Most methods return constants; the only real logic is wrapper rendering and the config dir merge, and that logic is the same for every harness. Config dirs are prepared with a share-everything-but-auth model: every entry of the default config dir that is not in the isolated list is symlinked. On this machine the `private` profile's Claude dir therefore symlinks `projects`, `sessions`, `history.jsonl`, `plans`, `todos`, `logs` and `debug` back to `~/.claude`, so transcripts and memory of all profiles land in one pile. Wrappers export `CC_DECK_PROFILE`, `CC_DECK_PROFILE_COLOR` and `CC_DECK_PROFILE_ICON`, then `exec` the harness binary. Config dir layout is `<dataHome>/cc-deck/profiles/<name>/<harness>`, credentials live in `<configHome>/cc-deck/profiles/<harness>/<name>`. Profiles are grouped under harness keys in `config.yaml` (brainstorm 098). There is no pi adapter.

**cc-setup** (`github.com/cc-deck/cc-setup`, Go, cobra, bubbletea, huh, lipgloss). Claude Code only. A central MCP registry at `~/.config/cc-setup/mcp.json`, per-project activation that writes `.mcp.json` and `~/.claude.json`, inherited `.mcp.json` detection, health checks, OAuth reuse, plugin enable and disable through `enabledPlugins` and the Claude plugin cache, permission presets (called "profiles" there, a naming clash). Everything is under `internal/`, so nothing is importable by another module. It does not know `CLAUDE_CONFIG_DIR`.

**corgi** (user's own project, Bun and Zig on Ghostty). Has a native account concept: an account is a config folder selected through `CLAUDE_CONFIG_DIR` or `CODEX_HOME` (pi through a configurable env name, OpenCode through `XDG_CONFIG_HOME`). It discovers folders it created, folders an agent signed in from, and a read-only registry in Omarchy's format at `$XDG_STATE_HOME/omarchy/agents/accounts/<provider>.json` with `{accounts:[{id,label,home,primary}]}`. The account name is derived from the folder basename minus a provider prefix. Agents are detected by executable basename or known script paths; an exec wrapper is transparent. Colors are per provider, not per account.

**herdr** (Rust, user has a fork). Detects agents by the pane's foreground process plus screen manifests; exec wrappers are transparent. Non-exec wrappers need a `HERDR_AGENT=<agent>` hint, and the docs warn against exporting it globally. Panes inherit `HERDR_ENV`, `HERDR_PANE_ID`, `HERDR_TAB_ID`, `HERDR_WORKSPACE_ID`, `HERDR_SOCKET_PATH`. Labels exist for tabs, workspaces and machines through the CLI. No account or per-pane color notion.

## Approaches Considered

### Relationship to cc-setup

- **Extend cc-setup to multiple harnesses.** Rejected. Its name says Claude Code, its model is per project, and Claude paths sit in every layer. Making it multi-harness means a harness abstraction under all of it, which is a rewrite of the core under the old name.
- **Two tools side by side** (aprofile for profiles, cc-setup for MCP and plugins). Rejected. Both write the same files through the same per-harness knowledge. One harness definition can carry both the launch knobs and the setup knobs.
- **New tool that absorbs cc-setup over time.** Chosen. The TUI patterns, the MCP registry shape, OAuth reuse and health checks port over cheaply because it is the same stack and the same owner.

### A: Port the cc-deck profile package into its own module

Lift `internal/profile` as it stands, rename the env contract, add a pi translator, put a colored CLI on top.

- Pros: fastest path, proven code with tests, the cc-deck migration becomes a dependency swap.
- Cons: harness knowledge stays Go code, so a new harness means a pull request. There is no owned layer, so a profile copy duplicates auth settings but not MCP servers or skills. Slice two would reshape the core anyway.

### B: Data-driven harness definitions with a four-layer config dir (Chosen)

A harness definition is data: binary, config dir variable, env map for model, base URL and auth kinds, entry classification lists, file patches for TOML and JSON, MCP file location and schema flavor, skills directory, plugin mechanism. Built-ins ship as data, users override or add in a `harnesses.d` directory, and an exec hook covers what data cannot express. Each profile config dir has four layers. **Credentials** are isolated and never copied. The **owned** layer holds MCP servers, skills, enabled plugins, and model and endpoint settings, and travels with a profile copy. **State** holds sessions, transcripts, history, logs, auto-memory and caches; it is per profile, created empty, never symlinked and never copied. **Shared** entries are symlinked to the default setup. Wrappers stay exec-style with a neutral env prefix and defaults the caller can override. The tool writes a machine-readable registry next to the wrappers for corgi, herdr and cc-deck.

- Pros: a new harness is a file. Slice two lands on the same definitions. Copy and conversion have a home in the owned layer. Integrates with corgi and herdr without touching them. Separate state per profile by construction.
- Cons: more schema design upfront, especially merge semantics for config patches. Generality leaks at the edges (Codex native profiles, OpenCode file-versus-dir config, OpenCode session storage outside the config dir) and leans on the exec hook. A new codebase, though wrapper and sync logic port over.

### C: Canonical store that materializes whole config dirs

The agentsync model: the tool owns canonical MCP, skills, plugins and settings and regenerates each profile dir from scratch on every sync, no symlinks.

- Pros: strongest portability; conversion is the normal path.
- Cons: harnesses write trust decisions, OAuth refreshes and plugin caches into their own config dirs at runtime, so regeneration fights live state. The default setup becomes second class. Large surface before anything useful ships, and agentsync already exists.

### Dropped without full treatment

- Plugin runtimes for the SPI (go-plugin, Lua, WASM): heavy for a configuration tool; the exec hook covers the same ground with a `key=value` protocol.
- Keeping the feature inside cc-deck: contradicts the request.

## Decision

Approach B, under the name **aprofile**. Written in Go (same stack as cc-setup, single static binary for Linux and macOS, lets cc-setup's bubbletea and huh code port over). Delivered in two slices, each its own spec:

- **Slice one (this brainstorm):** profiles, harness definitions, four-layer config dirs, wrappers and sync, registry, profile copy with the owned layer, MCP conversion, CLI and TUI, one-time import of the existing cc-deck profiles.
- **Slice two (separate spec):** per-project MCP activation, plugin toggling and permission presets ported from cc-setup onto the harness definitions; cc-setup frozen with a pointer to aprofile.

The cc-deck decoupling is a third, separate brainstorm. agentsync stays a reference for projection logic, not a dependency.

Why B over A: the cc-deck translator is already a data record wearing method clothes, so making it data is the natural next step rather than a leap. The owned layer is the only place where copy and slice two can live. The shared layer leaves runtime-written state alone, which C cannot.

## Key Requirements

Items without a marker were stated by the user. Items marked *proposed* were filled in during the session and need confirmation in the spec phase; the reason is given with each.

### Profiles

- A profile belongs to one harness and has a name, an auth kind, an optional model, an optional endpoint, extra environment variables, a color, an icon and a label.
- Supported harnesses in slice one: Claude Code, Codex, OpenCode, pi.
- Common per-harness options are first-class on the command line, for example the endpoint URL for Claude Code, rather than only reachable through raw env entries.
- *Proposed:* profile identity is the pair `(harness, name)` as decided in brainstorm 098, so `work` can exist for Claude and for Codex. Reason: wrapper names already include the harness.
- *Proposed:* the per-profile config dir basename equals the profile name, for example `.../profiles/claude/work`. Reason: corgi derives account names from the basename; cc-deck's current `<name>/<harness>` order is the wrong way around for it.

### Harness definitions (the SPI)

- Adding a harness must not require changing aprofile's core. The user asked for cross research on how to shape this; the result is in the research notes below.
- *Proposed:* a harness definition is a data record. Reason: every method of cc-deck's `Translator` except rendering returns constants. Fields:
  - binary name, config dir environment variable, default config subdir;
  - env mapping for model and base URL;
  - auth kinds with the variables each kind sets and clears;
  - entry classification lists (credentials, owned, state, shared);
  - config file patches (TOML or JSON merge);
  - MCP file location and schema flavor, skills directory, plugin mechanism;
  - capability flags (login supported, OAuth token supported, native profiles).
- *Proposed:* built-in definitions ship as data inside the binary; the same format is loadable from `~/.config/aprofile/harnesses.d/*.toml` with override semantics, and a `show` command prints the effective definition. Reason: mise and asdf precedent; lets a user patch a harness the day it changes.
- *Proposed:* an exec hook `aprofile-harness-<name>` in the git-credential-helper style (operations such as `env`, `pre-launch`, `write-config`, `auth-status`, `login`; `key=value` lines on stdin and stdout; unknown operations ignored) for behavior data cannot express. Reason: avoids a plugin runtime while keeping the door open.

### Config dir layers

- Credentials and login state are isolated per profile and never copied.
- Sessions, transcripts, history, logs and Claude auto-memory are per profile, never shared and never copied. This is what keeps usage and cost separable.
- MCP servers, skills, the enabled plugin set, and model and endpoint settings belong to the profile and travel with a copy.
- Everything else is shared with the default setup by symlink, so the default setup stays the single place to maintain shared configuration.
- *Proposed:* the classification is part of the harness definition, so a user can move an entry between layers without a release. Reason: harnesses add files between versions.

### Wrappers and sync

- `aprofile sync` writes one wrapper per profile into a bin directory, ensures that directory is on `PATH` through the shell rc, removes stale wrappers, and prepares the config dirs.
- Wrappers never embed secret values; they reference environment variables or files.
- *Proposed:* wrapper name `<harness-command>-<profile>` as in cc-deck, exec-style so herdr and corgi see the real harness process. Reason: existing muscle memory and transparent detection.
- *Proposed:* wrappers set their variables with `${VAR:-default}` so a caller can override model or endpoint for one run, and clear the auth variables of every kind other than the profile's own. Reason: Nix `makeWrapper` precedent; cc-deck research R16.
- *Proposed:* a neutral env contract (`APROFILE_NAME`, `APROFILE_HARNESS`, `APROFILE_COLOR`, `APROFILE_ICON` or similar) replaces `CC_DECK_PROFILE*`. Reason: the tool is independent of cc-deck.

### Registry and integration contracts (pull-only)

- aprofile must not modify herdr or corgi. Integration is through what aprofile publishes.
- *Proposed:* a machine-readable registry file next to the wrappers listing every profile with harness, name, label, color, icon, config dir and wrapper path, plus `--json` on `list` and `show`. Reason: corgi already reads Omarchy's registry; cc-deck can read the same file instead of importing a package.
- *Proposed:* optional per-profile `HERDR_AGENT` only when a wrapper cannot exec. Reason: herdr documents this hint and warns against exporting it globally.
- Per-account color in corgi and per-pane labels in herdr are changes on their side and out of scope.

### Copy and conversion

- `copy` duplicates a profile with its owned layer, leaves credentials empty and starts with empty state.
- MCP servers convert between harnesses; plugins are harness-specific and are not converted.
- *Proposed:* a canonical MCP record projected into each harness's native schema, with secrets held only as env references. Fields: name, transport, command, args, env, cwd, url, headers, auth as env reference, enabled, timeout, tool allow and deny lists, description, per-harness escape fields. Skills are stored once and materialized per harness. Plugins give up their skills and MCP servers when converting. Reason: the portability research shows this is the only part that converts cleanly.

### UI

- Colored command-line output and an interactive way to add, edit and browse profiles.
- *Proposed:* lipgloss-colored tables with `--json` everywhere, huh forms for add and edit, a bubbletea browse view ported from cc-setup. Reason: same stack, code already written.

### Platform and migration

- Linux and macOS.
- *Proposed:* one-time import of the user's existing cc-deck profiles and their credential files, no permanent compatibility with cc-deck's config schema. Reason: brainstorm 098 took the same stance for one user.
- *Proposed:* secrets stay as env or file references; no keychain integration in slice one. Reason: scope.

## Follow-up: cc-deck decoupling (separate brainstorm)

Recorded so that session can start from a list rather than a search. Each of these currently depends on `internal/profile` or on its artifacts:

- `internal/profile` itself and the `profile add/list/show/use/delete/sync` subcommands in `internal/cmd/profile.go`; docs in `harness-profiles.adoc`, `cli.adoc`, `configuration.adoc`.
- The wrapper env contract `CC_DECK_PROFILE*` read by `hook.go`, `claude_bg_attribution.go`, `codex_attribution.go` and the sidebar edge bar. Switch to aprofile's contract or read the registry.
- Hook installation: `Sync` installs cc-deck hooks into every per-profile config dir through `agent.InstallHooksAt`. With aprofile owning the dirs, cc-deck needs another trigger (read the registry and install, or a hook mechanism aprofile offers).
- `Provision(cfg, Target)` for SSH and OpenShell workspaces and the credential transport. Either aprofile grows an export, or cc-deck renders remote wrappers from the registry.
- Snapshot restore replays the wrapper name (spec 087); OpenShell provider list derived from profiles (FR-026); Kubernetes deploy path with `secret` credential sources; `default_profile`.
- Session restore and attribution read transcripts; with per-profile state they must look in the profile's state dir.
- Profile grouping in `config.yaml` (brainstorm 098) becomes unnecessary once profiles live in aprofile.

## Slice two: absorbing cc-setup (separate spec)

- Port the MCP registry, per-project activation, inherited `.mcp.json` detection, health checks and OAuth reuse onto the harness definitions.
- Plugin toggling and permission presets per harness where the harness has the concept; the definition says where it does not, and the UI greys it out.
- Rename cc-setup's permission "profiles" to presets to avoid the clash.
- Freeze cc-setup with a pointer to aprofile. Before touching that repository, check whether `.mcp.json` is tracked there; it is present in the repo root.

## Research Notes

Two research passes were run during the session. Their reports were kept in a temporary location and are condensed here.

### SPI shapes in comparable tools

- asdf plugins are directories of scripts. mise has three tiers: data-driven core tools, a tool registry, and script or Lua plugins. vfox uses Lua.
- git credential helpers use an exec protocol with `key=value` lines. kubectl plugins and gh extensions are executables found by name prefix.
- aqua and ubi are fully declarative registries. go-plugin runs gRPC subprocesses.
- Nix `makeWrapper` uses `--set-default` for overridable variables. distrobox-export generates exec wrappers.
- Synthesis: per-harness knowledge is mostly data, so the tiers above (built-in data, user data files, exec escape hatch) cover it without a plugin runtime.
- Existing tools in the space: cc-switch (Tauri GUI, twelve tools including pi), cc-switch-cli (Rust TUI), ccs, ccman, clauth, aiwitch, prodex, bridle (Rust TUI "config manager for agentic harnesses"), claude-code-router, claudeup, agentsync, rulesync, Vercel skills CLI, mcpm, mcpx. None combines per-profile wrappers, a four-layer config dir and multi-harness MCP projection.

### First-party knobs per harness

- **Claude Code:** `CLAUDE_CONFIG_DIR`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_MODEL`, `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_OAUTH_TOKEN`, Vertex and Bedrock and Foundry switches. Auth precedence: cloud provider flags, then `ANTHROPIC_AUTH_TOKEN`, then `ANTHROPIC_API_KEY`, then `apiKeyHelper`, then `CLAUDE_CODE_OAUTH_TOKEN`, then the Anthropic profile in `~/.config/anthropic` selected by `ANTHROPIC_PROFILE`, then `/login`. `~/.claude.json` holds account, MCP servers and per-project trust in one file; whether it moves with `CLAUDE_CONFIG_DIR` is confirmed by cc-deck's observed behavior.
- **Codex:** `CODEX_HOME` (must exist). Native profiles are now `$CODEX_HOME/<name>.config.toml` selected by `--profile` (since 0.134.0; `[profiles.x]` tables are no longer read). `[model_providers.<id>]` with `base_url` and `env_key`. Auth in `auth.json` or the keyring. Sessions, `history.jsonl`, a SQLite state file and `log/` live under `CODEX_HOME`.
- **OpenCode:** `OPENCODE_CONFIG` (a file), `OPENCODE_CONFIG_DIR`, `provider.<id>.options.baseURL`, `apiKey` with `{env:VAR}`. Auth in `~/.local/share/opencode/auth.json`, sessions in `~/.local/share/opencode/opencode.db` (`OPENCODE_DB` override), logs under the same data dir. Redirecting `XDG_DATA_HOME` to isolate accounts is community-confirmed only.
- **pi:** `PI_CODING_AGENT_DIR` (default `~/.pi/agent`), `models.json` with `baseUrl`, `auth.json`, `settings.json` with `defaultProvider`, `defaultModel` and `sessionDir`; `PI_CODING_AGENT_SESSION_DIR` moves sessions. No native profiles. Reads `~/.agents/skills` and its own `mcp.json`, not `.mcp.json`.

### Storage classification

Per harness the entries fall into credential, configuration, session, cache and extension classes. For Claude Code:

- credentials: `.credentials.json` and the account part of `.claude.json`;
- session state: `projects/` (transcripts and `memory/`), `history.jsonl`, `sessions/`, `file-history/`, `plans/`, `todos/`, `session-env/`, `jobs/`, `daemon/`, `usage-data/`;
- cache: `cache/`, `shell-snapshots/`, `backups/`, `debug/`, `logs/`, `statsig/`;
- extensions: `skills/`, `plugins/`, `agents/`, `commands/`, `hooks/`.

Codex and pi keep sessions inside their home directory. OpenCode keeps them in its data directory outside the config file. Codex, OpenCode and pi read skills from `~/.agents/skills`, which lies outside any harness home.

### Portability

- MCP stdio servers: portable with conversion (Claude `${VAR}` interpolation; Codex TOML `[mcp_servers.x]` with `bearer_token_env_var` and `env_http_headers`, no interpolation; OpenCode `mcp` block with a `command` array, `environment` and `{env:VAR}`; pi `mcp.json` with `mcpServers`, `exposure`, `${VAR}` and `!cmd`).
- Remote MCP servers: convertible, auth differs per harness.
- Skills: portable, paths change (Claude reads `~/.claude/skills`, the other three read `~/.agents/skills`).
- Plugins, hooks, permissions, auth: harness-specific. Model and provider settings: convertible.
- Copy guidance: copy configuration and extensions, regenerate credentials by logging in, leave sessions and caches behind.

### Name

Candidates checked for collisions on npm, crates.io, Homebrew, PyPI and GitHub: `livery` (rejected, the German Livree picture), `callsign`, `alterego`, `aka`, `loadout`, `tackroom`, `agentry`, `deckname`, `mandant`, `rufname`, `reiter`, `gespann`. `aprofile` was chosen: free on every registry, says what it does, keeps the word cc-deck users know. Docs must introduce it once as "agent profile" and distinguish an aprofile from Codex's native profiles and cc-setup's permission presets.

### Related idea inbox entries (not consumed)

`profile-wrappers-podman-backends`, `profile-wrappers-kubernetes-backend`, `per-profile-remote-secret-scope`, `env-override-warning-in-validate`, `derived-color-palette-shift`, `anthropic-auth-token-precedence`. They concern cc-deck's remote paths and belong to the decoupling brainstorm.

## Open Questions

- Configuration format and layout for profile definitions (TOML or YAML, one file or one file per profile) and where the bin directory lives (`~/.local/share/aprofile/bin` or `~/.local/bin`).
- Harness definition schema: exact fields, override semantics between built-in and `harnesses.d`, merge rules for TOML and JSON patches, and the operation list of the exec hook.
- Claude Code: `.claude.json` mixes account, MCP servers and project trust in one file. How does the owned layer split that file, given that cc-deck merges it today? Does `CLAUDE_CONFIG_DIR` also separate the macOS Keychain entry for OAuth, or do two profiles share one Keychain item?
- Codex: separate `CODEX_HOME` per profile (needed for separate `auth.json`) versus native `<name>.config.toml` profiles inside one home. Can both be combined so a user can still pass `--profile`?
- OpenCode: which lever isolates sessions and auth per profile, `OPENCODE_DB` plus an auth path, or `XDG_DATA_HOME` for the whole data dir? Needs verification against a current release.
- Skills for Codex, OpenCode and pi live in `~/.agents/skills`, outside any profile dir. Per-profile skills for those harnesses need either a per-profile `.agents` location or acceptance that skills are shared there.
- Cost tracking: is "separate" satisfied by separate state dirs, or should aprofile report per-profile usage itself?
- Registry format: adopt Omarchy's schema verbatim so corgi reads it unchanged, or an own schema plus an Omarchy export?
- Repository location and module path (for example under the `cc-deck` GitHub organization or the user's account), and distribution (Homebrew tap, GoReleaser).
- Which cc-deck hooks, if any, aprofile should offer a generic mechanism for (a per-harness "install these hooks into every profile dir" setting) so the decoupling brainstorm has an option other than reading the registry.
