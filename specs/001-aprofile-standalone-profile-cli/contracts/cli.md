# Contract: CLI command surface

**Feature**: 001-aprofile-standalone-profile-cli. Derived from spec FRs. Every operation is
available non-interactively (FR-040); the TUI is a convenience layer over the same operations
(Constitution VI).

## Conventions

- Profile reference syntax: `<harness>/<name>` where a command takes a single profile, or
  `<harness> <name>` as two positional args for `add`/`remove` (see each command).
- `--json` is accepted by read commands and reports; output follows a documented, versioned schema
  (FR-041). All `--json` output omits secret values (FR-028).
- Exit codes: `0` success; non-zero on any error. Specific non-zero cases called out below
  (partial import, path setup failure with `--require-path`, unresolved references at launch).
- Color/icons on a TTY; plain text when not a TTY or `NO_COLOR` is set (FR-042).
- Secret inputs accept only `env:`/`file:` references; literals are rejected (FR-028), except
  `--env-literal` for non-secret extra variables.

## Commands

### Profile management (FR-001, FR-002, FR-003, FR-004, FR-006a)

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile add <harness> <name>` | `--model`, `--endpoint`, `--auth-kind`, `--auth-key env:… \| file:…`, `--env NAME=ref`, `--env-literal NAME=VALUE`, `--color`, `--icon`, `--label`, `--primary`, `--herdr-agent`, `--adopt` | Create a profile file. Warns if the harness binary is missing. `--adopt` attaches a leftover config dir. Opens a form if required values are missing and stdin+stdout are TTYs (FR-043). |
| `aprofile list` | `--json` | List profiles grouped by harness. |
| `aprofile show <harness>/<name>` | `--json` | Show one profile; reports whether credentials exist without revealing them (FR-039). |
| `aprofile edit <harness>/<name>` | same value flags as `add` | Update fields; form when no flags and TTY. Does not write wrappers (FR-006b). |
| `aprofile rename <harness>/<name> <new-name>` | `--force` | Move profile file + config dir together (FR-006a). Refused while the harness runs unless `--force`. |
| `aprofile remove <harness> <name>` | `--purge` | Remove the profile file. Config dir retained unless `--purge` (confirmed). |

### Sync and launch (FR-022..FR-027)

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile sync` | `--dry-run`, `--require-path`, `--remove-path`, `--fix-links` | Write one wrapper per profile, prepare config dirs by layer, remove stale wrappers, write registry, ensure bin dir on PATH. Idempotent (FR-025). `--dry-run` lists changes only. Exits non-zero on load failures, and on PATH-setup failure only with `--require-path`. |
| `aprofile login <harness>/<name>` | — | Run the harness login flow with the profile's environment (FR-039). |
| `<wrapper-prefix>-<name> [args...]` | (harness args) | Generated wrapper; execs the harness with the profile environment (FR-026). |

### Harness definitions (FR-007..FR-012)

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile harness list` | `--json` | List harnesses with source (built-in / file path). |
| `aprofile harness show <name>` | `--json` | Effective definition; marks overridden vs built-in fields. |

### Registry and integration (FR-030..FR-032)

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile registry path` | — | Print the registry file location. |
| `aprofile registry export --format omarchy` | `--output-dir` | Write Omarchy accounts files per provider, opt-in (FR-031). |

### Copy and MCP (FR-033..FR-037)

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile copy <h>/<n> <h>/<n>` | `--overwrite`, `--auth`, `--label`, `--color`, `--icon` | Copy a profile. Same-harness carries all settings + owned layer verbatim (warns on secret-looking owned values); cross-harness converts MCP, drops auth/model/endpoint. Target gets empty credentials + state. `--overwrite` replaces only what the copy writes. `--json` report. |
| `aprofile mcp list <h>/<n>` | `--json` | List a profile's MCP servers. |
| `aprofile mcp copy <from> <to> [server...]` | `--overwrite`, `--json` | Convert named servers (or all); leaves existing target servers unless `--overwrite`. |

### Migration, UI, diagnostics (FR-043..FR-045)

| Command | Key flags | Behavior |
|---------|-----------|----------|
| `aprofile import cc-deck` | `--dry-run`, `--overwrite`, `--overwrite-credentials`, `--json` | Create profiles from cc-deck config + credentials; never modifies cc-deck files. Skips existing `(harness,name)` and exits non-zero; reports wrapper-name collisions on PATH. |
| `aprofile ui` | — | Browse view with add/edit/copy/remove via forms (FR-043). |
| `aprofile doctor` | `--json` | Run all checks in FR-044; each finding names the profile (if any) and a fix. |
