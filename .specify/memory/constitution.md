<!--
Sync Impact Report
- Version change: (unversioned template) → 1.0.0 → 1.0.1 → 1.0.2 → 1.0.3 → 1.0.4 → 1.1.0 → 1.2.0
  → 2.0.0
- 2.0.0 (MAJOR, spec 004-corgi-integration FR-024): Principle V's caller-override rule
  (`${VAR:-default}`) is removed and replaced by the single-source rule: a profile's settings are
  fixed for every launch, the folder (or, where it cannot hold a setting or the harness rewrites
  it, the wrapper) is the single source, one-run changes go through the harness's own flags, and
  wrappers clear the launch variables a profile session may have set. Principle I lists as
  definition data the folder locations of launch settings, how the wrapper passes a setting as a
  launch argument, the per-auth-kind `folder_key` flag and the launch variable names wrappers
  clear. Principle II allows aprofile-placed launch entries in a harness's settings files
  (recorded in aprofile's data directory, the rest of the file preserved, re-read before every
  write). Principle III states that launch entries are owned only where the folder carries them;
  a settings file linked at share level `all` carries none, so it stays linkable.
  Modified principles: I, II, III, V (titles unchanged). Added/removed sections: none.
  Follow-up: spec 001 (FR-006, FR-026, FR-027, FR-030) and spec 002 (FR-009a, FR-010, FR-017,
  FR-018) carry notes pointing to spec 004; the Claude Code definition's `settings.json`
  classification comment is updated.
- 1.2.0 (MINOR, spec 003-history-groups FR-025): Principle III gains an opt-in exception for
  history groups: the history entries of a member of an explicitly chosen history group are
  linked to the group store; credentials and all other state stay private, and isolated profiles
  keep the previous rule. Principle II's allowed writes gain adding, replacing and removing
  history links of group members, creating fresh empty history entries when a profile leaves a
  group or an entry stops being history, and, on an explicit, confirmed join, moving mergeable
  history into the group store and the rest into set-aside history; the default-setup rule gains
  the writes of an explicit `home` join (creating a missing history directory empty, moving
  mergeable session files in), never by `sync`.
  Modified principles: II, III (titles unchanged). Added/removed sections: none.
  Follow-up: spec 002's level table and FR-008 carry a note pointing to this exception.
- 1.1.0 (MINOR, spec 002-profile-isolation-levels FR-035): Principle III now has share levels
  (`all`, `content`, `none`) and splits the former shared layer into content, settings, plugin
  store and hook location; skills move from owned to content; credentials and state stay private
  at every level; the enabled plugin
  set is per profile except at `all`; unclassified entries are never linked by built-in
  definitions. Principle II adds the writes share levels and hook sets need (one-time settings
  copy, `settings pull`, managed hook entries, links to hook-set files, link changes on a confirmed
  level change, relinking after backup). Principle I lists share-level data, plugin store, hook
  location and `$HOME` escapes as definition data. Technical Constraints: slice two scope names
  share levels and hook sets. Principle IV extends the owned-file secret exception to the
  settings copy and `settings pull` (warn per secret-looking value).
  Modified principles: I, II, III, IV (titles unchanged). Added/removed sections: none.
  Follow-up: README and slice one spec text that call the enabled plugin set always per profile
  are superseded by spec 002 (FR-012, FR-032).
- 1.0.4 (PATCH, final spec review of 001): Principle IV exception for owned-layer files a harness
  wrote: same-harness copy carries literal secrets verbatim with a warning, cross-harness
  conversion drops them.
- 1.0.3 (PATCH, final spec review of 001): Principle II exceptions for confirmed purge and import backup.
- 1.0.2 (PATCH, from second spec review of 001): Principle II lists every write aprofile may
  make in a profile config dir.
- 1.0.1 (PATCH, from spec review of 001): clarified Principle III "everything else" means
  entries classified as shared; clarified Principle V that only non-auth variables are
  caller-overridable.
- Modified principles: none (initial ratification)
- Added principles:
  I. Harness Knowledge Is Data
  II. Never Fight the Harness
  III. Profile Isolation by Construction
  IV. No Secrets in Generated Artifacts
  V. Transparent Wrappers, Pull-Only Integration
  VI. Scriptable First, Colorful Second
  VII. Tests Never Touch the Real Home
- Added sections: Technical Constraints, Development Workflow, Governance
- Removed sections: none
- Source: brainstorm/001-aprofile-standalone-profile-cli.md (decision record, failure criteria)
- Deferred TODOs: none
-->

# aprofile Constitution

aprofile ("agent profile") manages side-by-side profiles for coding harnesses (Claude Code,
Codex, OpenCode, pi). An aprofile is distinct from Codex's native profiles and from cc-setup's
permission presets; documentation MUST make that distinction the first time the term appears.

## Core Principles

### I. Harness Knowledge Is Data

- Per-harness knowledge (binary, config dir variable, env mapping for model and endpoint, auth
  kinds, entry classification and its treatment per share level, config patches, MCP location and
  schema flavor, skills directory, plugin mechanism and plugin store, hook location and form,
  `$HOME` escapes, the folder locations where the harness reads launch settings, how the wrapper
  passes a setting as a launch argument, whether the folder can carry each auth kind's key
  (`folder_key`), the launch variable names every wrapper clears (`launch_clear`), capability
  flags) MUST live in harness definition records, not in Go code
  branching on harness names.
- Built-in definitions ship as data embedded in the binary and use the same format that users
  load from `harnesses.d`. A user definition MUST be able to override or add a harness without a
  release.
- Behavior data cannot express goes through the exec hook protocol (`key=value` lines, unknown
  operations ignored). A plugin runtime (go-plugin, Lua, WASM) MUST NOT be introduced.
- Test: adding a harness, or moving an entry between layers, requires no change to core
  packages. A change that adds `if harness == "..."` to core logic violates this principle.

Rationale: a tool that must be edited every time a harness ships a new flag is a stated failure.

### II. Never Fight the Harness

- aprofile MUST NOT regenerate, overwrite or delete files a harness writes at runtime (trust
  decisions, OAuth refreshes, plugin caches, session state), except through the explicit,
  confirmed operations listed below.
- Writes into a profile config dir are limited to: entries the harness definition classifies as
  owned (and owned keys of mixed files); symlinks for entries the profile's share level links;
  empty state entries at creation; one-time seeding of the shared keys of a mixed file when
  aprofile creates that file; a one-time copy of the default setup's settings (without the hook
  location) when aprofile creates a profile's own settings; managed hook entries and links to
  hook-set files (only entries aprofile placed, never other hook entries); launch entries in a
  harness's settings files (the launch variables, key command and provider `auth` block aprofile
  placed, recorded per profile in aprofile's data directory, never marked inside the harness's
  files; the rest of the file is preserved and the file is re-read immediately before every
  write); credential files an
  explicit `import` brings in (after a backup); on explicit request, replacing a leaking link with
  an empty real entry (never touching the link target); on an explicit, confirmed request and
  after a backup, `settings pull`, a share level change that replaces profile data with a link,
  and relinking an entry the harness turned into a real file; and deleting the whole profile
  config dir on a confirmed purge. For members of a history group: adding, replacing and removing
  links for history entries; creating fresh empty history entries when a profile leaves a group or
  an entry stops being history; and, on an explicit, confirmed join, moving mergeable history into
  the group store (never overwriting a store file) and moving the rest to set-aside history in
  aprofile's data directory. Adding or removing a link for a share level change needs no
  confirmation, because it never deletes profile data or a link target. Config patches MUST merge
  into existing files and preserve keys aprofile does not own.
- The user's default harness setup stays first class and is never modified by `sync`, except for
  explicitly requested operations such as import. An explicit join of the `home` history group
  may create a missing history directory empty in the default location and, after confirmation,
  move mergeable session files into it; it never replaces or rewrites an existing default-setup
  entry, and `sync` never does either.
- Every write operation MUST be idempotent: running `sync` twice yields the same state as once.

Rationale: harnesses keep live state in their config dirs; regenerating them corrupts that state.

### III. Profile Isolation by Construction

Each profile config dir has layers, and their rules are non-negotiable at every share level:

- **Credentials**: isolated per profile, never copied, never symlinked, never exported.
- **State** (sessions, transcripts, history, logs, auto-memory, caches, databases, locks): per
  profile, created empty, never symlinked, never copied. Exception (opt-in history groups): the
  entries a harness definition declares as history are linked to the group store for a member of
  an explicitly chosen history group; credentials and all other state stay private, and a profile
  in no group (isolated) keeps this rule unchanged.
- **Owned** (MCP servers, model and endpoint settings): per profile, travels with `copy`.
- **Borrowable** entries, which the definition declares as one of: **content** (read-mostly
  material such as instructions, skills, agents, commands, themes), **settings** (configuration the
  harness also writes, including the enabled plugin set), **plugin store** (installed plugin
  code), or **hook location**.

Each profile has one share level that decides how borrowable entries are treated:

- `all`: content, settings, plugin store and hook location are linked to the default setup.
- `content`: content and plugin store are linked; settings are the profile's own, copied once
  from the default setup; hooks come from hook sets.
- `none`: nothing is linked or copied from the default setup; settings and skills are the
  profile's own and start minimal; hooks come only from hook sets the profile names.

A settings file that also holds owned or credentials keys MUST NOT be linked at any level. Launch
entries (model, endpoint, extra variables, key command) are owned only where the profile folder
carries them; a settings file linked to the default setup at `all` carries none, so it stays
linkable, and those settings stay with the wrapper. The
enabled plugin set is therefore per profile at `content` and `none`, and shared at `all`. An
entry the definition does not classify MUST default to a non-leaking treatment (neither linked
nor copied) and be reported, never silently shared; built-in definitions MUST NOT use a
catch-all rule that links unclassified entries. The share levels and what each one links for a
harness MUST be explained to users in plain language (README, in-tool help, `show`). Profile
identity is the pair `(harness, name)`.

Rationale: separate history, memory and billing per account is the reason the tool exists.

### IV. No Secrets in Generated Artifacts

- Wrappers, the registry, harness definitions, the profile store and `--json` output MUST NOT
  contain secret values aprofile derived from a profile setting; they reference environment
  variables or files. aprofile MUST NOT author a literal secret into any artifact it generates.
- An owned-layer file a harness maintains may already hold literal secrets aprofile did not write.
  A same-harness `copy` MAY carry that file verbatim and MUST warn about each value matching a
  secret pattern; a cross-harness conversion MUST drop any literal secret it cannot express as a
  reference and report it. The same applies to the one-time settings copy and `settings pull` from
  the user's default setup into a profile: values are carried verbatim, with a warning per value
  that matches a secret pattern (naming the key, never the value).
- Canonical MCP records hold secrets only as references; conversion between harnesses MUST preserve
  references and MUST NOT emit a new literal secret.
- Log and error output MUST NOT print credential contents.

Rationale: wrappers that embed secrets are a stated failure, and generated files get shared.

### V. Transparent Wrappers, Pull-Only Integration

- Wrappers MUST be exec-style so process-based detection (herdr, corgi) sees the real harness.
- A profile's settings are fixed for every launch: the folder (or, where the folder cannot hold a
  setting or the harness rewrites it, the wrapper) is the single source. One-run changes go
  through the harness's own flags. Wrappers clear the launch variables a profile session may have
  set, so inherited values never leak into a launch. The config dir variable and the `APROFILE_*`
  contract variables are always set by the wrapper; each wrapper clears the auth variables of
  every auth kind other than its profile's own.
- Integration with herdr, corgi and cc-deck is pull-only: aprofile publishes a machine-readable
  registry and a neutral env contract (`APROFILE_*`). aprofile MUST NOT require changes in those
  tools, and MUST NOT import from or write into them.
- The registry format and env contract are public interfaces: breaking changes require a
  versioned format and a migration note.

Rationale: needing changes inside herdr or corgi to be useful is a stated failure.

### VI. Scriptable First, Colorful Second

- Every capability MUST be reachable from plain CLI commands with flags; the TUI is a
  convenience layer over the same operations, never the only path.
- Commands that list or show data MUST support `--json` with a stable schema.
- Colored output MUST degrade cleanly when stdout is not a TTY or `NO_COLOR` is set.
- Common per-harness options (endpoint, model, auth kind) are first-class flags, not only raw
  env entries.

Rationale: other tools and scripts consume aprofile; humans get color and forms on top.

### VII. Tests Never Touch the Real Home

- Tests MUST run against temporary `HOME` and XDG directories and MUST NOT read or write the
  developer's real harness config, credentials or shell rc files.
- Harness definitions, wrapper rendering, layer classification, config patch merging and MCP
  conversion MUST have table-driven tests; MCP conversion MUST have round-trip tests per harness
  pair.
- New behavior is developed test-first: a failing test precedes the implementation.

Rationale: a bug in this tool damages live accounts; tests are the only safe place to find it.

## Technical Constraints

- Language: Go, module path `github.com/cc-deck/aprofile`, shipped as a single static binary.
- Platforms: Linux and macOS. Platform-specific code MUST sit behind build tags or a small
  interface, with both platforms covered in CI.
- Stack: cobra for commands; lipgloss, huh and bubbletea for output, forms and browse views
  (shared with cc-setup so its code can be ported).
- Paths follow XDG conventions (`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`) with the
  usual fallbacks on both platforms.
- Dependencies: prefer the standard library; each new third-party module needs a reason in the
  plan.
- Scope discipline: slice one covers profiles, harness definitions, layered config dirs,
  wrappers and sync, registry, copy, MCP conversion, CLI and TUI, and a one-time cc-deck import.
  Slice two covers share levels and hook sets (spec 002), then per-project MCP activation, plugin
  toggling and permission presets in their own specs.
  Keychain integration and permanent cc-deck schema compatibility are out of scope until a spec
  says otherwise.
- `.mcp.json` and any file holding credentials MUST be gitignored and never committed.

## Development Workflow

- Work follows Spec Kit with spex: brainstorm, then `/speckit-specify`, `/speckit-plan`,
  `/speckit-tasks`, `/speckit-implement`. Non-trivial features MUST have a spec before code.
- The spex gates (review-spec, review-plan, review-code, verify) run at their phase boundaries;
  a failed gate blocks the next phase until resolved or the deviation is recorded in the spec.
- Every plan MUST include a Constitution Check that walks Principles I to VII and justifies any
  exception in its Complexity Tracking section.
- `go vet`, `gofmt` and the full test suite MUST pass before a change is merged.
- Items marked *proposed* in a brainstorm are not decisions until a spec confirms them.

## Governance

- This constitution supersedes other practices in this repository. Where a spec, plan or task
  conflicts with it, the constitution wins until amended.
- Amendments are made through `/speckit-constitution`, state the reason, and update dependent
  specs or plans that the change invalidates.
- Versioning follows semantic versioning: MAJOR for removing or redefining a principle, MINOR
  for adding a principle or section or materially expanding guidance, PATCH for clarifications.
- Compliance is reviewed at each spex gate and in code review. Violations are either fixed or
  recorded as justified exceptions in the plan's Complexity Tracking section.

**Version**: 2.0.0 | **Ratified**: 2026-11-02 | **Last Amended**: 2026-11-09
