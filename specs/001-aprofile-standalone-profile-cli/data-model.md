# Data Model: aprofile slice one

**Feature**: 001-aprofile-standalone-profile-cli
**Source**: [spec.md](./spec.md) Key Entities + Functional Requirements
**Date**: 2026-11-03

This model describes the in-memory and on-disk structures aprofile manipulates. Concrete
per-harness field *values* (config dir variable names, auth kinds, MCP paths, skills locations)
live in [research.md](./research.md) and in the built-in harness definitions, not here. This file
defines the *schema* and the rules that hold across all harnesses.

## Entity overview

```text
HarnessDefinition 1 ──< Profile >── 1 ProfileConfigDir
       │                   │              ├── Credentials layer
       │                   │              ├── Owned layer
       │                   │              ├── State layer
       │                   │              ├── Shared layer (links)
       │                   │              └── Mixed files (per-key layers)
       │                   └──> Wrapper (generated)
       └──> ExecHook (optional)

Registry 1 ──< RegistryEntry (one per Profile)
MCPServerRecord (neutral) ──> per-harness projection (conversion)
ConversionReport (per copy / mcp copy)
GlobalSettings (one per installation)
```

## 1. Profile

The user-facing unit. One human-readable file per profile at
`<aprofile config dir>/profiles/<harness>/<name>` (FR-006).

| Field | Type | Rules |
|-------|------|-------|
| `harness` | string | Must name a loadable harness definition. Part of identity. |
| `name` | string | `^[a-z][a-z0-9-]{0,62}[a-z0-9]$`. Part of identity. |
| `label` | string | Human label; defaults to name. |
| `color` | string | Terminal color (named or hex, per FR-001). |
| `icon` | string | Short glyph/emoji (per FR-001). |
| `primary` | bool | At most one primary per harness; setting it moves the flag (FR-001). |
| `auth_kind` | enum | One of the harness definition's declared auth kinds (FR-038). |
| `auth_refs` | map[string]Reference | Secret references for the auth kind; never literals (FR-028). |
| `model` | string? | Optional; rendered as the harness's model variable, caller-overridable (FR-027). |
| `endpoint` | string? | Optional; rendered as the harness's endpoint variable, caller-overridable. |
| `extra_env` | []ExtraEnvVar | Extra environment variables (FR-027). |
| `herdr_agent` | string? | Optional `HERDR_AGENT` hint; set only when present (FR-029). |

**Identity**: the pair `(harness, name)` (FR-002). The same `name` may exist under different
harnesses.

### 1a. ExtraEnvVar

| Field | Type | Rules |
|-------|------|-------|
| `name` | string | `[A-Za-z_][A-Za-z0-9_]*`. |
| `value` | Reference \| Literal | A reference, or a literal value. |
| `literal` | bool | True when stored via `--env-literal`; recorded in the store. On load, a literal value skips the length check but is still subject to the prefix check (FR-028). |

### 1b. Reference (value object)

A secret or value that resolves at wrapper launch, never stored as a plaintext secret.

| Form | Meaning | Validation |
|------|---------|-----------|
| `env:NAME` | Read from environment variable `NAME` at launch. | `NAME` matches `[A-Za-z_][A-Za-z0-9_]*`. |
| `file:/abs/path` | First line of the file at launch. | Path absolute, no newlines. |

Resolution is fail-closed: unset/empty/missing/unreadable aborts the wrapper before the harness
starts (FR-028a). File references read a single line via the shell `read` builtin.

**Validation checks on supplied values** (FR-028):
- **prefix check**: reject a known secret prefix (`sk-`, `ghp_`, `xox`, ...). Always applies, on
  every load/write path including hook `env` output.
- **length check**: reject a value of 32+ characters without spaces. Applies when first supplied
  (command options, forms) and to a hand-edited store value not marked literal. Skipped for
  values the store marks literal, and not applied to hook `env` output.
- Auth references accept only `env:`/`file:` and reject anything else.

## 2. HarnessDefinition

Data record describing one harness (FR-007). Built-in definitions ship embedded in the binary and
use the same format users drop into `harnesses.d` (FR-008). Origin is built-in, user file, or both
merged (FR-009); `harness show` marks which fields were overridden.

| Field | Type | Notes |
|-------|------|-------|
| `name` | string | Harness id (e.g., `claude`, `codex`, `opencode`, `pi`). |
| `binary` | string | Executable name to exec. |
| `wrapper_prefix` | string | Wrapper is `<wrapper_prefix>-<profile name>` (FR-022). |
| `config_env` | ordered map[var]path | Environment variables that redirect the harness into the profile, each mapped to a path relative to the profile root (`.` = the root). One entry for Claude (`CLAUDE_CONFIG_DIR: .`) and Codex (`CODEX_HOME: .`); two for pi (`PI_CODING_AGENT_DIR`, `PI_CODING_AGENT_SESSION_DIR`); the XDG set for OpenCode (FR-020). `APROFILE_CONFIG_DIR` is always the profile root. See [research.md](./research.md). |
| `default_config_location` | path | Where the harness's default setup lives (link source for shared). |
| `model_var` | string? | Env var carrying the model. |
| `endpoint_var` | string? | Env var carrying the endpoint. |
| `auth_kinds` | []AuthKind | Declared auth kinds with their variable/reference shape (FR-038). |
| `classification` | []EntryRule | Per-entry layer rules; a structured file may be `mixed` with per-key layers (FR-013). |
| `config_patches` | []ConfigPatch | Merge-only patches into existing files (FR-019). |
| `mcp` | MCPLocation? | File path + schema flavor; absent when no `mcp` capability. |
| `skills_location` | SkillsLocation? | Path + whether per-profile or shared (FR-021). |
| `plugin_mechanism` | string? | How plugins are enabled (owned set); not carried across harnesses (FR-036). |
| `omarchy_provider` | string? | Provider name for Omarchy export (FR-031). |
| `capabilities` | set | At least `login`, `oauth-token`, `native-profiles`, `mcp` (FR-007). |
| `exec_hook` | ExecHook? | Declared operations for `aprofile-harness-<name>` (FR-011). |

### 2a. EntryRule

| Field | Type | Values |
|-------|------|--------|
| `entry` | string/glob | Path within the config dir, or a structured file name. |
| `layer` | enum | `credentials` \| `owned` \| `state` \| `shared` \| `mixed`. |
| `keys` | map[string]layer | Only for `mixed`: top-level key → layer. Keys not covered are reported as unclassified (FR-013, FR-018). |

### 2b. ExecHook

Protocol: executable `aprofile-harness-<name>`, operation name as sole argument, `key=value` on
stdin and stdout (FR-011). Operations: `env`, `pre-launch`, `write-config`, `auth-status`,
`login`. Exit 64 = unsupported (no-op); 10 s timeout except `login`/`pre-launch`. `env` output is
subject to FR-028's prefix check only.

## 3. ProfileConfigDir and layers

The per-profile directory the harness's `config_dir_var` points at. Every entry belongs to exactly
one layer (FR-013):

| Layer | Treatment | Carried by `copy`? | Rule |
|-------|-----------|--------------------|------|
| Credentials | Per profile, real, isolated | No | Never linked/copied/exported (FR-014). |
| State | Per profile, real, created empty | No | Sessions, transcripts, history, logs, auto-memory, caches (FR-015). |
| Owned | Per profile, real | Yes | MCP servers, skills, enabled plugins, model/endpoint settings (FR-016). |
| Shared | Symlink to default setup | n/a | Everything else classified shared (FR-017). |
| Unclassified | Left in place, reported | No | Never linked/copied; reported by `sync`/`doctor` (FR-018). |

**Mixed file**: a JSON/TOML file with a layer per top-level key. Always a real per-profile file.
`copy`/`mcp copy` merge only its owned keys. Shared keys are seeded once from the default setup
when the profile file is first created and never rewritten; `doctor` reports drift (FR-013).

## 4. Wrapper

Generated POSIX-sh launcher for one profile, carrying an aprofile marker (FR-023). Rendered only
by `sync` (FR-006b). Self-contained: needs only a POSIX shell and the harness binary (plus the
exec hook when a `pre-launch` op is declared) (FR-026).

Always-set (never caller-overridable): the profile's `config_env` variables, `APROFILE_HARNESS`,
`APROFILE_NAME`, `APROFILE_LABEL`, `APROFILE_COLOR`, `APROFILE_ICON`, `APROFILE_CONFIG_DIR`, the
profile's auth variables (and unset of other auth kinds' variables). Caller-overridable
(`${VAR:-default}`): model, endpoint, extra variables, unless started inside another profile's
session (FR-027). Exports `APROFILE_SET_VARS` listing the model/endpoint/extra names it set.
Reference values render as launch-time resolution; every other value renders shell-quoted (FR-028b).

## 5. Registry and RegistryEntry

Written by `sync` to the fixed path `<aprofile data dir>/registry.json` (FR-030). Atomic writes.
No timestamps or volatile fields (an unchanged sync rewrites nothing).

RegistryEntry per profile: `harness`, `name`, `label`, `color`, `icon`, `auth_kind`, `primary`,
`config_dir`, `wrapper_path`. Top-level `format_version`. No secret values (FR-014, FR-028).

**Omarchy export** (opt-in, FR-031): per-provider accounts file
`{accounts:[{id,label,home,primary}]}` with `id = aprofile:<harness>/<name>`; non-aprofile entries
preserved, stale `aprofile:` entries removed.

## 6. MCPServerRecord (neutral) and ConversionReport

Harness-neutral MCP record for conversion (FR-034): `name`, `transport`, `command`, `args`,
`env` (references), `working_dir`, `url`, `headers`, `auth` (env reference), `enabled`, `timeout`,
`tool_allow`, `tool_deny`, `description`, `extra` (per-harness passthrough).

Conversion rules (FR-035): references convert to the target's syntax; a literal value matching a
secret pattern is dropped and reported (never written to the target); unrepresentable fields and,
when the target lacks `mcp`, whole servers are reported as skipped.

ConversionReport (per `copy` / `mcp copy`): lists converted / dropped / skipped servers, fields,
skills and plugins, plus same-harness owned values flagged as looking like secrets (FR-033a).

## 7. GlobalSettings

One settings file at `<aprofile config dir>` (FR-006). Holds: bin directory location (default
under the data dir, configurable; FR-023 assumption), whether shell-rc editing is disabled
(FR-024), and other installation-wide options. No secrets.

## State transitions

- **Profile lifecycle**: `add` → (file written, no wrapper) → `sync` (wrapper + config dir
  prepared + registry) → `edit`/`rename` (file/dir moved; wrapper rewritten only by next `sync`,
  FR-006b) → `remove` (file gone; config dir retained unless `--purge`, FR-004) → `sync` (wrapper
  removed).
- **rename** is refused while the harness runs unless `--force` (FR-006a).
- **copy**: creates target with empty credentials + empty state; same-harness carries all settings
  + owned layer verbatim (FR-033a); cross-harness carries label/color/icon + converted MCP, drops
  auth/model/endpoint (FR-033b).
- **import**: never modifies cc-deck; skips existing `(harness, name)` and exits non-zero;
  `--overwrite` / `--overwrite-credentials` with backup (FR-045).
