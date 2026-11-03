# Implementation Plan: aprofile slice one (standalone profile CLI)

**Branch**: `001-aprofile-standalone-profile-cli` | **Date**: 2026-11-03 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/001-aprofile-standalone-profile-cli/spec.md`

## Summary

aprofile is a single-binary Go CLI that manages side-by-side profiles (separate credentials, state,
owned config, shared config) for Claude Code, Codex, OpenCode and pi. All per-harness knowledge lives
in data-driven harness definitions (built-in + `harnesses.d` overrides + an exec hook for the rest).
Each profile gets a config dir organized into four layers; `sync` prepares those dirs, writes
exec-style POSIX-sh wrappers onto `PATH`, and publishes a machine-readable registry plus an
`APROFILE_*` environment contract for pull-only integration (corgi, herdr, cc-deck). `copy` carries
the owned layer and converts MCP servers between harnesses through a neutral record. A colored CLI
(with `--json`) and a bubbletea/huh TUI sit over the same operations, and a one-time `import cc-deck`
brings existing profiles across. Research (Phase 0) fixed the per-harness isolation levers and drove
one design refinement: definitions redirect a *set* of environment variables into the profile
(`config_env`), because the harnesses have no common home variable.

## Technical Context

**Language/Version**: Go 1.23 (single static binary), module `github.com/cc-deck/aprofile`.

**Primary Dependencies**: cobra (commands); lipgloss, bubbletea, huh (output, browse, forms);
`gopkg.in/yaml.v3` (profile store + harness definitions); `github.com/pelletier/go-toml/v2` (Codex
config); `encoding/json` (registry, Claude/OpenCode/pi config). Each third-party module is justified
in [research.md](./research.md) (constitution requires a reason per dependency).

**Storage**: Plain files. Profile store: one YAML file per profile at
`$XDG_CONFIG_HOME/aprofile/profiles/<harness>/<name>`. Harness definitions: embedded built-ins +
`$XDG_CONFIG_HOME/aprofile/harnesses.d/*`. Registry: JSON at `$XDG_DATA_HOME/aprofile/registry.json`.
Wrappers: `$XDG_DATA_HOME/aprofile/bin` (configurable). Backups: `$XDG_DATA_HOME/aprofile/backups/...`.

**Testing**: Go `testing`, table-driven; `t.TempDir()` + `t.Setenv()` for hermetic HOME/XDG;
round-trip MCP conversion tests per harness pair; a no-op stub harness binary for wrapper + overhead
tests. Test-first (Principle VII).

**Target Platform**: Linux and macOS; both in CI. Platform-specific code (shell rc selection, macOS
Keychain notes) behind build tags or a small interface.

**Project Type**: Single Go CLI project (cmd + internal packages).

**Performance Goals**: Wrapper adds <= 50 ms vs a direct harness launch with no exec hook, median of
20 runs (SC-011). `sync` with no changes writes 0 files (SC-003).

**Constraints**: No secret values in any generated artifact (SC-006, canary-tested); tests never
touch the real home (SC-009); wrappers self-contained (POSIX sh + harness binary only); every
capability reachable from non-interactive commands (Principle VI).

**Scale/Scope**: Slice one only (per spec Assumptions). Small scale: a handful of harnesses, tens of
profiles, single machine user.

## Constitution Check

*GATE: checked before Phase 0 and re-checked after Phase 1 design. Constitution v1.0.4.*

| Principle | Status | How the design satisfies it |
|-----------|--------|------------------------------|
| **I. Harness Knowledge Is Data** | PASS | All per-harness knowledge is in definition records (`config_env` redirects, `classification` incl. mixed files, `mcp`, `skills_location`, `auth_kinds`, `capabilities`, `exec_hook`). Built-ins are embedded and use the same format as `harnesses.d`. The research-driven `config_env` map absorbs OpenCode's four XDG vars and pi's two vars as data; behavior data cannot express goes through the exec hook. No `if harness == "..."` in core packages. |
| **II. Never Fight the Harness** | PASS | Writes limited to the Principle II list: owned entries + owned keys of mixed files, symlinks for shared, empty state at creation, one-time seeding of mixed-file shared keys, `import` credential files (after backup), `--fix-links` replacing a leaking link with an empty entry, and `--purge` deleting the config dir. Config edits decode to `map[...]RawMessage`/`map[string]any`, change only owned keys, re-encode, preserving unknown keys. `sync` is idempotent. |
| **III. Profile Isolation by Construction** | PASS (caveats documented) | Four layers enforced via `config_env` redirects into the profile root: credentials/state never linked or copied, owned carried by `copy`, everything else classified shared (symlinked) or reported if unclassified. Research-confirmed levers: Claude `CLAUDE_CONFIG_DIR`, Codex `CODEX_HOME`, pi `PI_CODING_AGENT_DIR` (+ session pin), OpenCode XDG set. Non-isolable globals (Claude Anthropic-Console creds in `~/.config/anthropic`; OpenCode `~/.claude`/`~/.agents` skills) are classified shared and reported, never silently shared, exactly as Principle III requires. Env-based auth is the default Claude kind so credentials isolate independent of the Keychain. |
| **IV. No Secrets in Generated Artifacts** | PASS | Wrappers, registry, store, definitions and `--json` carry only `env:`/`file:` references; prefix + length checks on every load/write path (literal-marked values skip the length check; hook `env` gets the prefix check only). Same-harness `copy` carries harness-authored owned files verbatim and warns on secret-looking values; cross-harness conversion drops literal secrets it cannot express as a reference and reports them. SC-006 canary test across all artifacts. |
| **V. Transparent Wrappers, Pull-Only Integration** | PASS | Wrappers `exec` the real harness so process detection works; non-auth vars use `${VAR:-default}`, auth vars always set and competing auth kinds unset. Integration is pull-only via the registry file and `APROFILE_*` contract; no imports from or writes into herdr/corgi/cc-deck. Registry + contract are versioned public interfaces. |
| **VI. Scriptable First, Colorful Second** | PASS | Every operation is a non-interactive cobra command; read commands and reports support `--json` with a versioned schema; color/icons degrade when not a TTY or `NO_COLOR`; endpoint/model/auth-kind are first-class flags. TUI is a layer over the same operations. |
| **VII. Tests Never Touch the Real Home** | PASS | Hermetic temp HOME/XDG via `t.TempDir()`/`t.Setenv()`; table-driven tests for definitions, wrapper rendering, classification, patch merging and MCP conversion; round-trip conversion tests per harness pair; test-first. |

**Result**: all gates pass; no violations. Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/001-aprofile-standalone-profile-cli/
├── plan.md              # This file
├── research.md          # Phase 0: the four research items + tech choices
├── data-model.md        # Phase 1: entities, layers, validation, transitions
├── quickstart.md        # Phase 1: validation scenarios mapped to SC-xxx
├── contracts/           # Phase 1: public-interface contracts
│   ├── cli.md           #   command surface + flags + --json conventions
│   ├── registry-and-env.md  # registry JSON, Omarchy export, APROFILE_*, exec hook
│   └── harness-definition.md # definition file schema
└── tasks.md             # Phase 2 (/speckit-tasks, not this command)
```

### Source Code (repository root)

```text
cmd/
└── aprofile/
    └── main.go               # cobra root wiring

internal/
├── config/                   # XDG paths, aprofile dirs, global settings (YAML)
├── secret/                   # env:/file: reference parse, prefix+length checks, launch-render
├── harness/                  # definition model, loader, built-in+file merge, exec hook client
│   └── builtin/              # embedded claude/codex/opencode/pi definition files (go:embed)
├── profile/                  # profile model, YAML store (one file per profile), validation
├── layer/                    # classification, config-dir preparation, mixed-file merge (json/toml)
├── mcp/                      # neutral MCP record + per-harness conversion (round-trip tested)
├── wrapper/                  # POSIX-sh rendering + shell quoting
├── registry/                 # registry JSON + atomic write + Omarchy export
├── sync/                     # orchestration: dirs, wrappers, registry, PATH (shell rc edit)
├── importer/                 # one-time cc-deck import
├── doctor/                   # FR-044 checks
├── cli/                      # cobra command implementations (thin over internal packages)
├── tui/                      # bubbletea/huh browse + forms
└── testutil/                 # hermetic HOME/XDG helpers, stub harness

tests/
├── integration/             # end-to-end per quickstart scenarios (hermetic)
└── fixtures/                # harnesses.d samples, cc-deck fixture, stub harness
```

Unit tests live beside each package as `*_test.go` with package `testdata/`. Integration tests under
`tests/integration` drive the built binary against the stub harness.

**Structure Decision**: single Go module, standard `cmd/` + `internal/` layout (the spec template's
"single project" option). Packages are split by the spec's concern boundaries (harness definitions,
profile store, layers, wrappers, sync, registry, mcp, import, doctor, cli, tui) so core logic stays
free of harness-name branching (Principle I) and each unit is table-testable (Principle VII).

## Complexity Tracking

No constitution violations; no entries. (The `config_env` generalization reduces complexity relative
to per-harness special casing and is itself required by Principle I.)

## Phase outputs

- **Phase 0** -> [research.md](./research.md): all four deferred items resolved with decisions,
  rationale and the concrete per-harness definition values; four non-blocking live-verification items
  (V1-V4) with safe defaults already chosen.
- **Phase 1** -> [data-model.md](./data-model.md), [contracts/](./contracts/),
  [quickstart.md](./quickstart.md). Post-design Constitution Check re-run above: still all PASS.

Next command: `/speckit-tasks` to generate the dependency-ordered `tasks.md`.
