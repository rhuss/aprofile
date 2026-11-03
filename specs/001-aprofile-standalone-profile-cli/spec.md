# Feature Specification: aprofile Slice One, Standalone Harness Profiles

**Feature Branch**: `001-aprofile-standalone-profile-cli`

**Created**: 2026-11-02

**Status**: Draft

**Input**: Brainstorm `brainstorm/001-aprofile-standalone-profile-cli.md` (slice one only): a standalone
CLI that manages side-by-side profiles for Claude Code, Codex, OpenCode and pi, with data-driven
harness definitions, four-layer config dirs, exec-style wrappers synced onto `PATH`, a
machine-readable registry, profile copy with MCP conversion, a colored CLI with `--json` and a
TUI, and a one-time import of cc-deck profiles. Linux and macOS.

## Terminology

- **Harness**: a coding agent CLI (Claude Code, Codex, OpenCode, pi, or one a user defines).
- **Profile** (an "aprofile", short for agent profile): one account or configuration of one
  harness, identified by the pair (harness, name). Distinct from Codex's native profiles and from
  cc-setup's permission presets.
- **Harness definition**: a data record describing how aprofile drives one harness.
- **Wrapper**: a small generated launcher on `PATH`, such as `claude-work`, that starts the
  harness with a profile's environment.
- **Default setup**: the harness's own configuration in its usual location (for example
  `~/.claude`), used when the harness is started without a wrapper.
- **Layers**: every entry of a profile config dir is either in one layer (credentials, owned,
  state or shared), a mixed file with a layer per top-level key, or unclassified.

## Clarifications

### Session 2026-11-02

- Q: When the calling shell already exports an auth variable (for example a global
  `ANTHROPIC_API_KEY`), may it override the profile's credentials for one run? → A: No. Auth
  variables always come from the profile (set from its reference, or unset); only model,
  endpoint and extra variables are caller-overridable.
- Q: Do wrappers call aprofile at launch time, or are they self-contained? → A: Self-contained.
  A wrapper needs only a POSIX shell and the harness binary; it invokes nothing else at launch
  except the harness's exec hook when the definition declares a `pre-launch` operation.
- Q: How are profiles stored for hand editing? → A: One file per profile under
  `<aprofile config dir>/profiles/<harness>/<name>`, plus one global settings file; harness
  definitions stay in `harnesses.d`.
- Q: What happens when `copy`, `rename` or `import` targets a profile that already exists? → A:
  `copy` and `rename` fail without changes and name the existing profile; `import` skips that
  profile, imports the rest and exits non-zero. `copy --overwrite` replaces only what the copy
  writes and `import --overwrite` replaces settings and owned layer; neither touches credentials
  or state; only
  `import --overwrite-credentials` replaces credentials, after a backup. `rename` never
  overwrites.
- Q: How much may a wrapper add to the harness's start time? → A: At most 50 ms on a typical
  developer laptop when no exec hook is declared.

### Spec review 2026-11-02

Decisions taken on the review-spec findings (all recommendations accepted by the user):
mixed files with per-key layers (FR-013); wrappers started inside another profile discard the
inherited profile values (FR-027); `env:`/`file:` secret references and token-pattern rejection
(FR-028); fail-closed reference resolution (FR-028a); exec hook protocol table (FR-011); no
writes to shared skills locations (FR-036); same-harness versus cross-harness copy rules
(FR-033a, FR-033b); `mcp` capability flag (FR-007, FR-035); fixed registry path (FR-030); rename
refused while running (FR-006a); only `sync` writes wrappers (FR-006b); Omarchy export details
(FR-031); override merge rules (FR-009); shell startup files (FR-024); color and icon formats
(FR-001).

Second round, also accepted: `copy --overwrite` replaces only what the copy writes (FR-033);
nested wrappers unset the inherited `APROFILE_SET_VARS` (FR-027); constitution v1.0.2 lists the
writes Principle II allows; `add --adopt` for leftover config dirs (FR-004); default auth kind
per harness and `copy --auth` (FR-007, FR-033b); `--env-literal` opt-in (FR-028); the primary
flag moves (FR-001).

Third round (final verification): delete rules name the confirmed purge, `copy --overwrite` and
`import --overwrite-credentials` exceptions (FR-019; constitution v1.0.3 Principle II);
`import --overwrite-credentials` backs up a mixed file and replaces only its credential keys
(FR-045). Secret-value checks split into a prefix check and a length check; `--env-literal` values
are marked literal in the store and skip the length check on load, and hook `env` output is subject
to the prefix check only (FR-028, FR-011). Owned-layer files a harness wrote may hold literal
secrets: a same-harness `copy` carries them verbatim and warns (FR-033a), a cross-harness
conversion drops literal secrets it cannot express as a reference and reports them (FR-035);
constitution v1.0.4 Principle IV records the exception.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Create a profile and launch it from any shell (Priority: P1)

A developer with a personal and a work account for Claude Code creates a `work` profile with a
label, color, icon, model and endpoint, runs `aprofile sync`, opens a new shell, types
`claude-work`, logs in once, and from then on lands in the work account with the work model and
endpoint every time. They can list, show, edit and remove profiles from the command line.

**Why this priority**: this is the core value. Without a working wrapper on `PATH` nothing else
matters.

**Independent Test**: on a clean machine with Claude Code installed, add one profile, sync, open
a new shell and start the wrapper; the harness starts with the profile's config dir, model and
endpoint, and `aprofile list` shows the profile.

**Acceptance Scenarios**:

1. **Given** no profiles exist, **When** the user runs `aprofile add claude work --model X
   --endpoint Y --color blue --icon W --label "Work"` and then `aprofile sync`, **Then** a wrapper
   named `claude-work` exists in aprofile's bin directory, that directory is on `PATH` in new
   shells, and the profile's config dir exists.
2. **Given** a synced profile, **When** the user runs `claude-work --version` (or any harness
   argument), **Then** the arguments pass through unchanged to the real harness binary, which
   replaces the wrapper process.
3. **Given** a synced profile with a default model, **When** the user runs
   `ANTHROPIC_MODEL=other claude-work`, **Then** the harness sees `other` for this run only.
4. **Given** a profile was removed with `aprofile remove claude work`, **When** the user runs
   `aprofile sync`, **Then** the `claude-work` wrapper is gone and the profile's config dir is
   still on disk unless `--purge` was given and confirmed.
5. **Given** `sync` has run once, **When** it runs again with no changes, **Then** it reports zero
   changes and modifies no file.

---

### User Story 2 - Keep history, memory, credentials and cost apart per profile (Priority: P1)

The developer uses `claude-work` and `claude-private` in the same project on the same day. Work
transcripts, history, auto-memory and logs never appear in the private profile or the default
setup, and the reverse. Each profile has its own login. Settings the user maintains in the
default setup (for example a shared `CLAUDE.md` or keybindings) keep applying to every profile.

**Why this priority**: separation of history and billing is the reason the tool exists, and a
leak is hard to undo once transcripts are mixed.

**Independent Test**: create two profiles of the same harness, run a short session in each, and
verify that each session's transcript and history appear only in that profile's config dir, that
the credential files differ, and that a shared entry edited in the default setup is visible in
both.

**Acceptance Scenarios**:

1. **Given** two synced profiles of one harness, **When** the user runs a session in each,
   **Then** the state entries (sessions, transcripts, history, logs, auto-memory, caches) of each
   profile are real directories or files inside that profile's config dir, not links.
2. **Given** a profile, **When** the user logs in through its wrapper, **Then** the credentials
   are stored for that profile only and logging in through another profile does not change them.
3. **Given** an entry classified as shared, **When** the user changes it in the default setup,
   **Then** every profile of that harness sees the change without another sync.
4. **Given** the harness has created an entry in a profile config dir that its definition does
   not classify, **When** the user runs `sync` or `doctor`, **Then** aprofile reports the entry by
   name, does not share or copy it, and leaves it in place.

---

### User Story 3 - Other tools can tell which profile a pane runs (Priority: P2)

A terminal multiplexer or agent dashboard (corgi, herdr, cc-deck) wants to show which account a
pane uses. It reads aprofile's registry file or the environment of the harness process and finds
the profile's harness, name, label, color, icon, config dir and wrapper path, with no change on
aprofile's side and no aprofile code linked into the tool.

**Why this priority**: integration without modifying other tools is an explicit success
condition, but the tool is useful before any consumer reads the registry.

**Independent Test**: sync two profiles, read the registry file with a JSON parser and verify
every field; start a wrapper and inspect the harness process environment for the contract
variables.

**Acceptance Scenarios**:

1. **Given** synced profiles, **When** a tool reads the registry file, **Then** it finds one entry
   per profile with harness, name, label, color, icon, config dir and wrapper path, and a format
   version field.
2. **Given** a running harness started by a wrapper, **When** a tool reads its environment,
   **Then** it finds `APROFILE_HARNESS`, `APROFILE_NAME`, `APROFILE_LABEL`, `APROFILE_COLOR`,
   `APROFILE_ICON` and `APROFILE_CONFIG_DIR`.
3. **Given** a process-based detector, **When** a wrapper starts a harness, **Then** the detector
   sees the real harness executable as the foreground process.
4. **Given** the user opts in with `aprofile registry export --format omarchy`, **When** the
   export runs, **Then** an Omarchy-format accounts file is written per provider with one entry
   per profile, and entries aprofile did not write are preserved.

---

### User Story 4 - Copy a profile with its MCP servers, skills and plugins (Priority: P2)

The developer wants a `client-x` Claude profile set up like `work`, and later a Codex profile
with the same MCP servers. `aprofile copy claude/work claude/client-x` gives a new profile with
the same MCP servers, skills, enabled plugins, model and endpoint, an empty login and empty
history. `aprofile copy claude/work codex/work` converts the MCP servers into Codex's format and
reports what could not be converted.

**Why this priority**: copying is a frequent setup step, and MCP conversion is the part of
portability that works reliably across harnesses.

**Independent Test**: copy a profile with three MCP servers (one stdio with env references, one
remote with a header from an env variable, one disabled) to the same harness and to each other
harness, then compare the target's native MCP configuration with the expected projection.

**Acceptance Scenarios**:

1. **Given** a source profile, **When** the user copies it within the same harness, **Then** the
   target has the source's owned layer, an empty credentials layer and an empty state layer.
2. **Given** a source profile with MCP servers, **When** the user copies it to another harness,
   **Then** each server appears in the target harness's native MCP configuration with equivalent
   transport, command, arguments, URL, environment references and enabled state.
3. **Given** a server field the target harness cannot represent, **When** the copy converts it,
   **Then** the copy completes, the field is dropped, and the report names the server and field.
4. **Given** a source with secrets held as environment references, **When** the copy converts
   them, **Then** the target holds references in the target harness's syntax and no secret value
   appears in any file aprofile writes.
5. **Given** a cross-harness copy, **When** the source has plugins, **Then** plugins are not
   carried over and the report lists them.
6. **Given** two existing profiles, **When** the user runs `aprofile mcp copy <from> <to>
   [server...]`, **Then** only the named servers (or all) are converted into the target, and
   servers already in the target are left alone unless `--overwrite` is given.

---

### User Story 5 - Support a new or changed harness without a release (Priority: P2)

A harness ships a new state directory, or the user wants profiles for a harness aprofile does not
know. The user drops a definition file into `harnesses.d`, or overrides one field of a built-in
definition, and aprofile uses it on the next command. For behavior a definition cannot express,
an executable named `aprofile-harness-<name>` on `PATH` is called with a simple line protocol.

**Why this priority**: a tool that must be edited every time a harness changes is a stated
failure. Built-in definitions for the four harnesses cover day one, so this matters second.

**Independent Test**: add a definition file for a fictional harness whose binary is a test
script, add a profile for it, sync, and run the wrapper; then override one classification entry
of a built-in definition and confirm `aprofile harness show` reflects it.

**Acceptance Scenarios**:

1. **Given** the built-in definitions, **When** the user runs `aprofile harness list`, **Then**
   Claude Code, Codex, OpenCode and pi are listed with their source (built-in or file path).
2. **Given** a definition file in `harnesses.d` for a new harness, **When** the user adds a
   profile for it and syncs, **Then** a working wrapper is generated with no change to aprofile.
3. **Given** a file in `harnesses.d` whose `name` field equals a built-in's, **When** the user runs
   `aprofile harness show <name>`, **Then** the effective definition shows the overridden fields
   from the file and all other fields from the built-in, and marks which is which.
4. **Given** an invalid definition file, **When** any command loads definitions, **Then** the
   error names the file and the field, and the other harnesses keep working.
5. **Given** an exec hook that answers the `env` operation, **When** a wrapper for that harness is
   generated, **Then** the variables it returns are included; aprofile calls only the operations
   the definition lists for the hook, and a hook answering exit status 64 is treated as a no-op.

---

### User Story 6 - Move existing cc-deck profiles over once (Priority: P3)

The developer has profiles created by cc-deck (`claude-work`, `claude-private`, `codex-private`).
`aprofile import cc-deck` creates equivalent aprofile profiles, carries over model, endpoint,
auth kind, color, icon and credential files, and creates a fresh state layer, without changing
anything on cc-deck's side.

**Why this priority**: it is a one-time step for an existing user; new users do not need it.

**Independent Test**: point the import at a fixture copy of a cc-deck config and credential
directory, run the import, and compare the resulting profiles and credential files.

**Acceptance Scenarios**:

1. **Given** cc-deck profiles exist, **When** the user runs `aprofile import cc-deck --dry-run`,
   **Then** aprofile lists the profiles it would create and changes nothing.
2. **Given** the same, **When** the user runs the import, **Then** each profile exists in aprofile
   with its settings and credentials, and no cc-deck file is modified or deleted.
3. **Given** an aprofile profile with the same (harness, name) already exists, **When** the
   import runs, **Then** that profile is skipped and reported, the others are imported, and the
   command exits non-zero; with `--overwrite` the settings are replaced and the existing
   credentials are kept, and only `--overwrite-credentials` replaces them (after a backup).
4. **Given** cc-deck wrappers with the same names remain on `PATH`, **When** the import finishes,
   **Then** aprofile reports which wrapper wins on the current `PATH` and how to remove the old
   ones.

---

### User Story 7 - Browse and edit profiles interactively (Priority: P3)

The developer runs `aprofile ui` and sees all profiles grouped by harness, in their colors and
icons, with sync status. From there they add, edit, copy or remove a profile through forms.
`aprofile add` without the required flags on a terminal opens the same form.

**Why this priority**: convenience on top of commands that already work.

**Independent Test**: on a terminal, open the browse view, add a profile through the form, and
confirm `aprofile list --json` shows it with the entered values.

**Acceptance Scenarios**:

1. **Given** profiles exist, **When** the user opens `aprofile ui`, **Then** profiles appear
   grouped by harness with label, color, icon and whether the wrapper is in sync.
2. **Given** the browse view, **When** the user adds or edits a profile through a form, **Then**
   the result is the same as the equivalent command with flags.
3. **Given** stdin or stdout is not a terminal, **When** a command needs input it was not given, **Then**
   it fails with a message naming the missing flags instead of opening a form.

### Edge Cases

- The harness binary is not installed: `add` warns, `sync` still writes the wrapper, and the
  wrapper prints a clear error naming the missing binary when started.
- The user's shell rc file is a symlink or read-only: `sync` does not replace it and prints the
  line to add by hand (FR-024).
- Unsupported shell (neither bash, zsh nor fish): `sync` prints the bin directory and the export
  line for manual setup.
- A wrapper name collides with an unrelated executable already on `PATH` before aprofile's bin
  directory: `sync` and `doctor` report the shadowing.
- A file in aprofile's bin directory was not written by aprofile: `sync` never deletes it.
- A profile config dir contains a link for a state entry left by an older tool (for example a
  cc-deck profile dir that links `projects/` to `~/.claude`): `doctor` reports the leak and
  `sync --fix-links` replaces it with an empty real directory after confirmation, never deleting
  the link target.
- The default setup does not yet exist (the harness has never run): shared links are created
  lazily on the next sync after the target appears, and the wrapper still starts.
- A shared entry would be created as a link but a real file with that name already exists in
  the profile dir: the real file is kept and reported, not overwritten.
- A profile is removed while its harness is running: the session keeps running against the
  retained config dir; the next sync removes the wrapper. A rename is refused while the profile's
  harness runs, unless `--force` is given (FR-006a).
- A secret is passed on the command line as a literal value (for example `--auth-key sk-...`):
  aprofile rejects it and asks for an `env:` or `file:` reference (FR-028).
- A wrapper is started inside another profile's session (for example `claude-work` from a
  `claude-private` shell): it discards the inherited profile's model, endpoint and extra
  variables and applies its own (FR-027).
- A referenced secret is missing at launch: the wrapper stops with a message naming the profile
  and reference (FR-028a).
- Concurrent `aprofile` invocations: writes to the profile store and registry are atomic, so a
  reader never sees a half-written file.
- The harness changes its config file format between versions: patches merge only the keys
  aprofile owns and leave unknown keys as they are.

## Requirements *(mandatory)*

### Functional Requirements

**Profiles**

- **FR-001**: Users MUST be able to create, list, show, edit, rename and remove profiles. A
  profile has a harness, a name, an auth kind with its secret references, an optional model, an
  optional endpoint, extra environment variables, a label, a color, an icon, and an optional
  `primary` flag (at most one primary profile per harness; marking a profile primary moves the
  flag from the harness's previous primary profile and reports the change). A color MUST be an ANSI color name
  or `#rrggbb`; an icon MUST be a single Unicode grapheme. Invalid values are rejected.
- **FR-002**: Profile identity MUST be the pair (harness, name); the same name MAY exist for
  different harnesses. Profile names and harness names MUST match
  `^[a-z][a-z0-9-]{0,62}[a-z0-9]$` (start with a letter, no trailing hyphen, at most 64
  characters). Wrapper prefixes MUST match the same pattern and be unique across all effective
  harness definitions. Wrapper names (`<prefix>-<name>`) MUST be unique: `add`, `rename`, `copy`
  and `import` MUST reject a profile whose wrapper name equals another profile's and name that
  profile; if definition overrides create a clash, `sync` reports it and skips both wrappers.
- **FR-003**: Model, endpoint and auth kind MUST be settable through dedicated command-line
  options (`--model`, `--endpoint`, `--auth`) for every built-in harness, not only through raw
  environment entries. "Endpoint" is the canonical term for the API base URL throughout.
- **FR-004**: Removing a profile MUST keep its config dir unless the user passes `--purge` and
  confirms (or passes `--yes`); credentials and state are never deleted silently. `add` (and
  `copy` to a new target) MUST fail and name the directory when a config dir for that
  (harness, name) already exists without a profile, unless `--adopt` is given, which reuses the
  directory with its credentials and state.
- **FR-005**: The aprofile config dir is `$XDG_CONFIG_HOME/aprofile` and the aprofile data dir is
  `$XDG_DATA_HOME/aprofile` (XDG fallbacks `~/.config` and `~/.local/share` on both platforms).
  Each profile's config dir MUST be located at `<aprofile data dir>/profiles/<harness>/<name>`, so
  its directory name equals the profile name.
- **FR-006**: The profile store MUST be one human-readable file per profile at
  `<aprofile config dir>/profiles/<harness>/<name>.<ext>` (the extension is fixed by the chosen
  file format), plus one global settings file. Store files and config dirs MUST NOT collide even
  when both XDG variables point to the same directory. Changes made
  by hand MUST be picked up by the next command and validated with errors that name the profile
  and field.
- **FR-006a**: `rename` MUST move the profile file and config dir together, and MUST fail without
  changes when the new (harness, name) already exists. It MUST refuse while a harness process
  started for that profile is running, unless `--force` is given.
- **FR-006b**: Only `sync` writes or removes wrappers. `add`, `edit`, `rename`, `copy`, `remove`
  and `import` change the profile store and config dirs; the affected wrappers are then stale
  until the next `sync`, and `list`, `show` and `doctor` report them as stale.

**Harness definitions**

- **FR-007**: All per-harness behavior MUST come from harness definitions. A definition declares
  at least: binary name, wrapper command prefix, config dir environment variable, default config
  location, environment variables for model and endpoint, auth kinds with the variables each
  sets and clears, a default auth kind, the layer of every known config dir entry, config file patches, MCP file
  location and format, skills location, plugin mechanism, an Omarchy provider name, and
  capability flags (at least: `login`, `oauth-token`, `native-profiles`, `mcp`), and, when it
  declares an exec hook, the list of hook operations it implements.
- **FR-008**: Definitions for Claude Code, Codex, OpenCode and pi MUST ship built in.
- **FR-009**: Users MUST be able to add harnesses and override individual fields of built-in
  definitions with files in `<aprofile config dir>/harnesses.d/`. A user file whose `name` field
  equals a built-in's name overrides only the fields it sets, merged as follows: maps merge by
  key, scalars and lists replace the built-in value, and an explicit null removes an inherited
  entry. Two user files with the same `name` are an FR-012 error naming both files.
- **FR-010**: `aprofile harness list` and `aprofile harness show <name>` MUST show the effective
  definitions and the origin of each field.
- **FR-011**: When a definition declares an exec hook, aprofile MUST call the executable
  `aprofile-harness-<name>` with the operation name as its only argument, `key=value` lines on
  standard input and `key=value` lines expected on standard output. Every call receives
  `harness`, `profile` and `config_dir`. Protocol rules:

  | Operation | Called by | Extra input keys | Output keys |
  |-----------|-----------|------------------|-------------|
  | `env` | `sync`, when rendering the wrapper | `auth_kind`, `model`, `endpoint` | any `VAR=value`; values that carry secrets MUST use the reference syntax of FR-028 |
  | `pre-launch` | the wrapper, before replacing itself | none | none (exit status only) |
  | `write-config` | `sync`, after preparing the config dir | `auth_kind`, `model`, `endpoint` | `changed=true\|false` |
  | `auth-status` | `show`, `doctor` | `auth_kind` | `status=ok\|missing\|unknown`, optional `detail` |
  | `login` | `login` | `auth_kind` | none (interactive, inherits the terminal) |

  aprofile calls only the operations the definition lists for the hook. Exit status 64 means
  "operation not supported" and is treated as a no-op. Any other non-zero exit, a timeout (10
  seconds, except `login` and `pre-launch`, which have none), or output that is not `key=value`
  lines aborts the command with an error naming the hook and operation; a failing `pre-launch`
  stops the wrapper before the harness starts. Values returned by `env` are rendered like profile
  values (FR-028b) and are subject to FR-028's prefix check but not its length check, so a hook may
  emit long paths and URLs yet never a known secret prefix.
- **FR-012**: An invalid definition MUST be reported with file and field and MUST NOT prevent
  other harnesses from working.

**Config dir layers**

- **FR-013**: Every entry of a profile config dir MUST belong to one layer: credentials, owned,
  state or shared, as declared by the harness definition. A structured file (JSON or TOML) MAY
  instead be declared **mixed**, with a layer assigned per top-level key (for example Claude
  Code's `.claude.json`: account keys are credentials, `mcpServers` is owned, project trust is
  state). A mixed file is always a real per-profile file; `copy` and `mcp copy` merge only its
  owned keys into the target and leave all other keys untouched. Keys a mixed file declares as
  shared cannot be linked; they are seeded from the default setup's file when the profile's file
  is first created and are never rewritten afterwards. `sync` creates each declared mixed file
  that is absent, seeding from the default setup's file when it exists and otherwise creating it
  with only the keys aprofile owns, and reports a skipped seed. Keys of a mixed file not covered
  by the declaration are left alone and reported like unclassified entries (FR-018).
- **FR-014**: Credentials MUST be per profile and MUST NOT be linked, copied by `copy`, or written
  to the registry, wrappers or command output.
- **FR-015**: State entries MUST be per profile, created empty, never linked and never copied.
- **FR-016**: Owned entries MUST be per profile and MUST be carried over by `copy`.
- **FR-017**: Shared entries MUST be links to the corresponding entry of the default setup.
- **FR-018**: Entries not classified by the definition MUST NOT be linked or copied and MUST be
  reported by `sync` and `doctor`.
- **FR-019**: aprofile MUST NOT overwrite, regenerate or delete entries the harness writes at
  runtime, except for the writes Constitution Principle II allows, which include the explicit,
  confirmed operations `remove --purge` (FR-004), `copy --overwrite` (FR-033) and
  `import --overwrite-credentials` (FR-045). Config patches MUST merge into existing files and
  preserve keys aprofile does not own.
- **FR-020**: For harnesses that keep sessions or credentials outside their config dir (OpenCode),
  the definition MUST redirect those locations into the profile so FR-014 and FR-015 still hold.
- **FR-021**: For harnesses that read skills from a location outside any config dir and offer no
  per-profile override, skills MUST be treated as shared for that harness and `harness show` and
  `copy` MUST say so.

**Wrappers and sync**

- **FR-022**: `aprofile sync` MUST write one wrapper per profile named
  `<wrapper prefix>-<profile name>` into aprofile's bin directory, prepare every profile config
  dir according to its layers, remove wrappers for profiles that no longer exist, and write the
  registry. When a profile file or harness definition fails to load, `sync` MUST leave that
  profile's wrapper and registry entry unchanged, report it, and exit non-zero; a profile that
  fails validation (including FR-028) MUST NOT get a newly rendered wrapper.
- **FR-023**: `sync` MUST only remove files in the bin directory that carry aprofile's wrapper
  marker.
- **FR-024**: `sync` MUST ensure the bin directory is on `PATH` by adding a marked block,
  idempotently, to the startup file of the shell named in `$SHELL`: `${ZDOTDIR:-$HOME}/.zshrc`
  for zsh,
  `~/.bashrc` for bash on Linux, `~/.bash_profile` for bash on macOS, and
  `<fish config dir>/conf.d/aprofile.fish` for fish. It MUST print manual instructions when it
  cannot, and exit non-zero in that case only when `--require-path` is given. A global setting
  MUST disable all shell file edits, and `sync --remove-path` MUST remove the marked block.
- **FR-025**: `sync` MUST be idempotent: a second run with no changes MUST report zero changes and
  write no files. `sync --dry-run` MUST list the changes without making them.
- **FR-026**: Wrappers MUST start the harness by replacing their own process, pass all arguments
  through unchanged, and always set (never caller-overridable) the profile's config dir variable
  and the contract variables `APROFILE_HARNESS`, `APROFILE_NAME`, `APROFILE_LABEL`,
  `APROFILE_COLOR`, `APROFILE_ICON`, `APROFILE_CONFIG_DIR`. Wrappers MUST be self-contained: at
  launch they require only a POSIX shell and the harness binary, use only shell builtins
  otherwise, and invoke nothing else except the harness's exec hook when the definition declares
  a `pre-launch` operation. Before starting the harness, a wrapper MUST check with a shell
  builtin that its profile config dir exists; if it does not (for example after a `rename`
  before the next `sync`), it MUST exit non-zero and tell the user to run `aprofile sync`.
- **FR-027**: Wrappers MUST set model, endpoint and extra variables only when the caller has not
  already set them. When the calling environment carries an `APROFILE_NAME` or `APROFILE_HARNESS`
  that differs from the wrapper's own profile (a wrapper started inside another profile's
  session), the wrapper MUST first unset every variable named in the inherited
  `APROFILE_SET_VARS` and the harness's model and endpoint variables, then apply its own values;
  inside another profile's session, caller overrides of model and endpoint are therefore not
  honored (documented limitation). Every wrapper MUST export `APROFILE_SET_VARS`, a
  space-separated list of the model, endpoint and extra variable names it set. Auth variables are not caller-overridable: the wrapper MUST set the
  profile's own auth variables from their references and MUST unset the auth variables of every
  other auth kind, regardless of what the calling shell exports.
- **FR-028**: Artifacts aprofile authors (wrappers, the registry, the profile store, harness
  definitions, command output and `--json` output) MUST NOT contain secret values aprofile itself
  derived from a profile setting; the only files aprofile writes that hold such secrets are
  credentials-layer files and their backups created by `import`. Secrets are written as references,
  `env:NAME` (read from the named environment variable, `NAME` matching `[A-Za-z_][A-Za-z0-9_]*`)
  or `file:/absolute/path` (first line of the file, path absolute and without newlines), and
  resolved when the wrapper runs. Two checks guard supplied values: a **prefix check** rejecting a
  known secret prefix (such as `sk-`, `ghp_`, `xox`) and a **length check** rejecting a value of 32
  or more characters without spaces. Auth options MUST accept only references and reject anything
  else. A value passed with `--env` that fails either check MUST be rejected unless given as a
  reference. `--env-literal NAME=VALUE` stores a value as a literal and records it as literal in the
  profile store; the length check is skipped for it, but the prefix check still applies. Both checks
  apply when a value is first supplied through command options or forms, and to a hand-edited store
  value that is not marked literal; on load, a value the store marks literal skips the length check
  but is still subject to the prefix check. Hook `env` output is subject to the prefix check only,
  so a hook may emit long paths and URLs. A violation on any of these paths is a validation error
  naming the profile and field. Owned-layer files a harness maintains may already hold literal
  secrets aprofile did not author; `copy` handles those under FR-033a and FR-035, not here.
- **FR-028a**: When a reference cannot be resolved at launch (variable unset or empty, file
  missing, unreadable or empty), the wrapper MUST exit non-zero with a message naming the profile
  and the reference, and MUST NOT start the harness. File references are read with the shell's
  `read` builtin, so only single-line secrets are supported.
- **FR-028b**: Wrappers MUST render reference values as launch-time resolution and every other
  value as a shell-quoted literal, so no profile, definition or hook value can inject shell
  code.
- **FR-029**: A profile MAY set a `HERDR_AGENT` hint; aprofile MUST set it only for profiles that
  declare it, never globally.

**Registry and integration**

- **FR-030**: `sync` MUST write the registry to the fixed path
  `<aprofile data dir>/registry.json`, independent of the configurable bin directory, listing
  every profile with harness, name, label, color, icon, auth kind, primary flag, config dir and
  wrapper path, plus a format version. The registry MUST NOT contain timestamps or other values
  that change without a profile change, so an unchanged `sync` rewrites nothing. Writes MUST be
  atomic. `aprofile registry path` MUST print the registry's location.
- **FR-031**: `aprofile registry export --format omarchy` MUST, only on explicit request, write
  one Omarchy-format accounts file per provider (`{accounts:[{id,label,home,primary}]}`) to
  `$XDG_STATE_HOME/omarchy/agents/accounts/<provider>.json` (fallback `~/.local/state`,
  overridable with `--output-dir`). The provider comes from the harness definition, `home` is the
  profile config dir, `primary` comes from the profile's primary flag, and every entry aprofile
  writes has the id `aprofile:<harness>/<name>`. Entries without the `aprofile:` prefix MUST be
  preserved unchanged; `aprofile:` entries for profiles that no longer exist are removed.
- **FR-032**: aprofile MUST NOT require changes to herdr, corgi or cc-deck and MUST NOT write into
  their files.

**Copy and MCP conversion**

- **FR-033**: `aprofile copy <harness>/<name> <harness>/<name>` MUST create the target with an
  empty credentials layer and an empty state layer. If the target exists, `copy` MUST fail
  without changes unless `--overwrite` is given. `--overwrite` replaces only what the copy writes
  (FR-033a, FR-033b); every target setting the copy does not carry, the target's primary flag,
  its credentials and its state stay as they were.
- **FR-033a**: A same-harness copy MUST carry every profile setting (auth kind and references,
  model, endpoint, extra environment, label, color, icon; not the primary flag) and the whole
  owned layer verbatim, including the owned keys of mixed files. Because a harness may have written
  literal secrets into its own owned files (for example tokens in `mcpServers` env or headers), the
  copy carries those values unchanged (the target is another of the user's own profiles) and MUST
  warn in its report about each owned value matching a secret pattern (the prefix or length check of
  FR-028), naming the server or field, so the user knows a literal secret was duplicated.
- **FR-033b**: A cross-harness copy MUST carry label, color and icon and the converted MCP servers,
  MUST NOT carry model, endpoint, auth kind, auth references or extra environment, and MUST
  report each dropped setting and every owned entry it could not convert. A new cross-harness
  target gets the target harness's default auth kind unless `--auth` is given. Label, color and
  icon are overridable by flags in both cases.
- **FR-034**: When source and target harness differ, `copy` MUST convert MCP servers through a
  harness-neutral record holding name, transport, command, arguments, environment, working dir,
  URL, headers, auth as environment reference, enabled state, timeout, tool allow and deny lists,
  description and per-harness extra fields.
- **FR-035**: Conversion MUST keep secrets held as references in the target harness's syntax and
  MUST report every field or server it could not represent. A literal value in a source owned entry
  that matches a secret pattern (the prefix or length check of FR-028) MUST NOT be written to the
  target: it is dropped and reported, naming the server and field, so conversion never authors a
  literal secret into the target. When the target harness lacks the `mcp` capability, every server
  MUST be reported as skipped.
- **FR-036**: Cross-harness `copy` MUST carry skills to the target's skills location only when
  that location is per profile. When the target's skills are shared (FR-021), `copy` MUST NOT
  write to the shared location and MUST report which source skills already exist there and which
  are missing. `copy` MUST NOT carry plugins across harnesses; skipped plugins are reported,
  together with any skills and MCP servers they contain that the user can copy separately.
- **FR-037**: `aprofile mcp list <profile>` and `aprofile mcp copy <from> <to> [server...]` MUST be
  available; `mcp copy` MUST leave existing target servers unchanged unless `--overwrite` is
  given.

**Auth**

- **FR-038**: Each harness definition MUST declare its auth kinds; slice one covers interactive
  login, API key, auth token and OAuth token for Claude Code, and the equivalents each other
  built-in harness supports.
- **FR-039**: `aprofile login <profile>` MUST run the harness's login flow with the profile's
  environment, and `aprofile show` MUST report whether the profile has credentials without
  revealing them.

**CLI, output and UI**

- **FR-040**: Every operation MUST be available as a non-interactive command.
- **FR-041**: `list`, `show`, `harness list`, `harness show`, `mcp list`, `doctor`, `sync
  --dry-run`, `import --dry-run`, and the conversion reports of `copy` and `mcp copy` MUST support
  `--json` with a documented, versioned schema.
- **FR-042**: Human-readable output MUST use profile colors and icons on a terminal and MUST fall
  back to plain text when stdout is not a terminal or `NO_COLOR` is set.
- **FR-043**: `aprofile ui` MUST offer a browse view with add, edit, copy and remove; `add` and
  `edit` without required values MUST open a form when stdin and stdout are both terminals.
- **FR-044**: `aprofile doctor` MUST check: bin directory on `PATH`, wrapper shadowing, stale or
  missing wrappers, missing harness binaries, unclassified entries, state entries that are links,
  credentials that are links, invalid definitions, references that cannot be resolved (FR-028a),
  the hook `auth-status` result where the hook implements it, and seeded keys of mixed files that
  differ from the default setup; each finding names the affected profile, if any, and a fix.

**Migration and platforms**

- **FR-045**: `aprofile import cc-deck` MUST create aprofile profiles from cc-deck's profile
  configuration and credential files, supports `--dry-run`, creates fresh state layers, and MUST
  NOT modify any cc-deck file. A cc-deck profile whose (harness, name) already exists in aprofile
  MUST be skipped and reported while the others are imported, and the command MUST then exit
  non-zero. `--overwrite` replaces the existing profile's settings and owned layer and keeps its
  credentials and state; `--overwrite-credentials` additionally replaces its credentials with the
  imported ones after backing up the old files to
  `<aprofile data dir>/backups/<harness>/<name>/<timestamp>/`. For a mixed file, the whole file
  is backed up and only its credentials-layer keys are replaced from the imported file; its owned
  and state keys stay as they were.
- **FR-046**: aprofile MUST run on Linux and macOS and MUST use the XDG base directories with the
  standard fallbacks on both.

### Key Entities

- **Profile**: harness, name, label, color, icon, primary flag, auth kind and its secret
  references (`env:` or `file:`), model, endpoint, extra environment, optional `HERDR_AGENT` hint.
  Belongs to one harness definition.
- **Harness definition**: binary, wrapper prefix, config dir variable, default config location,
  model and endpoint variables, auth kinds, entry classification (including mixed files with
  per-key layers), config patches, MCP location and format, skills location, plugin mechanism,
  Omarchy provider name, capability flags, optional exec hook. Origin is built-in, user file, or
  both merged.
- **Profile config dir**: the per-profile directory, split into credentials, owned, state and
  shared entries, plus mixed files.
- **Wrapper**: the generated launcher for one profile, carrying an aprofile marker.
- **Registry**: versioned list of all profiles with the fields other tools need.
- **MCP server record**: harness-neutral description of one MCP server used for conversion.
- **Conversion report**: per copy, the servers, fields, skills and plugins that were converted,
  dropped or skipped.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A user with a harness installed can create a profile and start it from a new shell
  in under 2 minutes, using at most three commands before the harness's own login.
- **SC-002**: After sessions in two profiles of the same harness, 0 transcript, history, memory or
  log entries of one profile are reachable through the other profile's config dir or the default
  setup.
- **SC-003**: A second `sync` with no changes writes 0 files and reports 0 changes.
- **SC-004**: A new harness can be supported by adding one definition file, with 0 changes to the
  released tool.
- **SC-005**: MCP conversion between every pair of built-in harnesses that have the `mcp`
  capability preserves 100% of the fields both sides can represent, and every dropped field is
  named in the report.
- **SC-006**: 0 secret values appear in any wrapper, registry, profile store, definition or
  `--json` output across the full test suite, checked with planted canary secrets.
- **SC-007**: A tool that only reads the registry file can show label, color and icon for every
  configured profile without any change to aprofile.
- **SC-008**: Every capability of the interactive UI can also be performed with commands alone.
- **SC-009**: The full test suite passes on Linux and macOS and never reads or writes the real home
  directory of the user running it.
- **SC-010**: The user's existing cc-deck profiles import with 0 manual edits and 0 changes to
  cc-deck's files.
- **SC-011**: A wrapper without an exec hook adds at most 50 ms to the harness's start time,
  measured as the median of 20 runs of the wrapper against a no-op stub binary compared with 20
  direct runs of the same stub.

## Assumptions

- Slice one only. Per-project MCP activation, plugin toggling and permission presets
  (absorbing cc-setup) are slice two. Changes to cc-deck, corgi and herdr are out of scope.
- Separate state dirs satisfy cost separation; aprofile does not report usage or cost itself.
- aprofile's bin directory is its own directory under its data dir (not `~/.local/bin`), so
  stale-wrapper removal can never touch files of other tools; the location is configurable.
- Claude Code: `CLAUDE_CONFIG_DIR` relocates `.claude.json` into the profile dir; that file is
  declared mixed (FR-013): account data is credentials, MCP servers are owned and project trust
  is state, so `copy` carries over only the MCP part. Claude's `settings.json` is likewise mixed:
  model and enabled plugins are owned. A single file cannot be half linked, so its other keys
  (hooks, permissions) are seeded from the default setup when the profile is created and belong
  to the profile from then on; `doctor` reports when they differ from the default setup. Whether the macOS Keychain entry is
  separated per config dir must be verified during planning; if not, Claude profiles on macOS use
  a per-profile token kind or the definition documents the limitation.
- Codex: each profile gets its own `CODEX_HOME`; Codex's native `--profile` keeps working inside it
  because wrapper arguments pass through.
- Model and endpoint are settable (FR-003) only for a harness whose definition declares the backing
  environment variable. In slice one Claude declares both, Codex declares the endpoint only (model is
  passed through as a CLI argument), and OpenCode and pi declare neither; `add` and `edit` reject
  `--model`/`--endpoint` for a harness that cannot carry them rather than silently storing a value the
  wrapper would drop. Adding those variables (or a config-file mechanism) for OpenCode and pi is
  deferred to a later slice.
- OpenCode: the lever that moves sessions and credentials into the profile (database path override
  or data home redirect) is chosen during planning against a current release.
- Codex, OpenCode and pi read skills from `~/.agents/skills`; unless a per-profile override is
  found during planning, skills are shared for these harnesses (FR-021).
- No keychain integration: secrets stay as environment variable or file references.
- No permanent compatibility with cc-deck's configuration schema; the import is one-time.
- Release packaging (Homebrew tap, release automation) is outside this spec; building from source
  is sufficient.
- A single user per machine account; no multi-user or shared-machine scenarios.
