<!--
Sync Impact Report
- Version change: (unversioned template) → 1.0.0 → 1.0.1 → 1.0.2 → 1.0.3 → 1.0.4
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
  kinds, entry classification, config patches, MCP location and schema flavor, skills directory,
  plugin mechanism, capability flags) MUST live in harness definition records, not in Go code
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
  owned (and owned keys of mixed files); symlinks for shared entries; empty state entries at
  creation; one-time seeding of the shared keys of a mixed file when aprofile creates that file;
  credential files an explicit `import` brings in (after a backup); on explicit request,
  replacing a leaking link with an empty real entry (never touching the link target); and
  deleting the whole profile config dir on a confirmed purge. Config patches MUST merge into
  existing files and preserve keys aprofile does not own.
- The user's default harness setup stays first class and is never modified by `sync`, except for
  explicitly requested operations such as import.
- Every write operation MUST be idempotent: running `sync` twice yields the same state as once.

Rationale: harnesses keep live state in their config dirs; regenerating them corrupts that state.

### III. Profile Isolation by Construction

Each profile config dir has four layers, and their rules are non-negotiable:

- **Credentials**: isolated per profile, never copied, never symlinked, never exported.
- **State** (sessions, transcripts, history, logs, Claude auto-memory, caches): per profile,
  created empty, never symlinked, never copied.
- **Owned** (MCP servers, skills, enabled plugin set, model and endpoint settings): per profile,
  travels with `copy`.
- **Shared**: everything else, symlinked to the default setup.

"Everything else" means every other entry the harness definition classifies as shared. An entry
the definition does not classify MUST default to a non-leaking treatment (neither linked nor
copied) and be reported, never silently shared. Profile identity is the pair `(harness, name)`.

Rationale: separate history, memory and billing per account is the reason the tool exists.

### IV. No Secrets in Generated Artifacts

- Wrappers, the registry, harness definitions, the profile store and `--json` output MUST NOT
  contain secret values aprofile derived from a profile setting; they reference environment
  variables or files. aprofile MUST NOT author a literal secret into any artifact it generates.
- An owned-layer file a harness maintains may already hold literal secrets aprofile did not write.
  A same-harness `copy` MAY carry that file verbatim and MUST warn about each value matching a
  secret pattern; a cross-harness conversion MUST drop any literal secret it cannot express as a
  reference and report it.
- Canonical MCP records hold secrets only as references; conversion between harnesses MUST preserve
  references and MUST NOT emit a new literal secret.
- Log and error output MUST NOT print credential contents.

Rationale: wrappers that embed secrets are a stated failure, and generated files get shared.

### V. Transparent Wrappers, Pull-Only Integration

- Wrappers MUST be exec-style so process-based detection (herdr, corgi) sees the real harness.
- Non-auth wrapper variables (model, endpoint, extra variables) use `${VAR:-default}` so a caller
  can override them for one run. Auth variables, the config dir variable and the `APROFILE_*`
  contract variables are always set by the wrapper and are not caller-overridable; each wrapper
  clears the auth variables of every auth kind other than its profile's own.
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
  Per-project MCP activation, plugin toggling and permission presets belong to slice two.
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

**Version**: 1.0.4 | **Ratified**: 2026-11-02 | **Last Amended**: 2026-11-03
