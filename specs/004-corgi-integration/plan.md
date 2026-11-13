# Implementation Plan: Self-Contained Profile Folders

**Branch**: `004-corgi-integration` | **Date**: 2026-11-09 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/004-corgi-integration/spec.md`

## Summary

aprofile writes each profile's launch settings into the harness's own files so a tool that only
sets the config-folder variable (Corgi) launches the profile correctly: Claude Code gets `env`
entries and an `apiKeyHelper` in `settings.json`, Codex gets an `auth` command block in the
selected provider of `config.toml`. A per-profile record in aprofile's data directory tracks the
entries aprofile placed. Wrappers drop `${VAR:-default}` overrides, clear inherited launch
variables unconditionally, stop exporting what the folder carries, and pass the Codex model as
`-c model="…"`. The registry gains `model`; `show`, `doctor` and `list` report self-containment
and stale or unplaced entries. The constitution moves to 2.0.0.

## Technical Context

**Language/Version**: Go 1.24 (module `github.com/cc-deck/aprofile`)

**Primary Dependencies**: existing only: cobra, go-toml/v2 (already used for `config.toml`
patches and MCP), lipgloss/huh/bubbletea. No new module.

**Storage**: files: harness `settings.json` (JSON) and `config.toml` (TOML) in profile config dirs;
new record files `<data>/managed-launch/<harness>/<name>.json`; `registry.json`.

**Testing**: `go test ./...`; table-driven unit tests; integration tests under `tests/integration`
with `testutil.HermeticHome` and the stub harness binary (installed as `claude` and `codex`).

**Target Platform**: Linux and macOS; key commands run under `/bin/sh` of the harness.

**Project Type**: CLI (single binary).

**Performance Goals**: `sync` stays dominated by file I/O; one extra JSON or TOML read and write per
profile when something changes, none otherwise (SC-005).

**Constraints**: no literal secret in any written file (Principle IV); never write through a link
or into the default setup (Principle II); idempotent `sync`; tests never touch the real home.

**Scale/Scope**: tens of profiles; two harnesses gain folder-carried settings (Claude Code, Codex);
OpenCode and pi only lose caller overrides.

## Global Constraints

- Harness differences are definition data (Principle I): new definition fields, no
  `if harness == …` in core. The two write forms (`env-object` + `shell-command` for Claude,
  `provider-auth` for Codex) are named forms like the existing hook forms.
- The launch planner is a pure function of (profile, definition, share level, file content,
  record); writers apply its plan. `sync`, `--dry-run`, `show`, `doctor` and `list` use the same
  planner so they cannot drift (spec 002 SC-006 lesson).
- The shell wrapper (`wrapper.RenderWith`) and its Go mirror (`wrapper.Environ`, used by
  `aprofile login`) change together; `TestEnvironMatchesRenderedWrapper` stays the guard.
- Values are never printed: reports name entries, files and keys only.
- Supported harness versions for folder-carried settings: Claude Code 2.1.319 or later, Codex
  0.167.0 or later (spec Assumptions).
- Package dependencies: `launch` depends on `harness`, `profile`, `secret`, `config` and `layer`
  (JSON/TOML helpers, exported as needed); `wrapper` depends on `launch`. `ShellQuote` moves from
  `wrapper` to `secret` (wrapper keeps a thin alias) so `launch` never imports `wrapper`. `layer`
  never imports `launch`: callers pass placed keys in through options.

## Interfaces

- **Harness definition** (contracts/harness-definition.md): `folder_launch` block (`env`,
  `key_command`), `launch_clear` list, `model_arg` list, `folder_key` flag per auth kind.
- **Registry** (contracts/registry-and-env.md): `model` string field in every entry; env contract
  changes for `APROFILE_SET_VARS`.
- **Wrapper behavior** (contracts/registry-and-env.md): unconditional clearing and setting,
  clear-only, Codex model argument.
- **CLI output** (contracts/cli.md): `show` and `doctor` self-containment, stale folder, unplaced
  entries; `sync --dry-run` launch steps; `edit --share --dry-run` effects; `--model` for Codex.
- **Go interfaces of `internal/launch`** (all take `*profile.Profile`):

  ```go
  // Doc is the parsed content of the files the definition's folder_launch names, read once.
  type Doc struct {
      Files map[string]FileDoc // keyed by profile-relative file name
  }
  type FileDoc struct {
      Exists, Linked bool
      JSON  map[string]json.RawMessage // settings.json-style files
      TOML  map[string]any             // config.toml-style files
      Err   error                      // parse error, reported with file and key
  }
  func ReadDoc(def *harness.Definition, configDir string) Doc
  func Decide(p *profile.Profile, def *harness.Definition, doc Doc) []Setting           // pure
  func Plan(p *profile.Profile, def *harness.Definition, doc Doc, rec Record) (Result, error) // pure
  func Apply(p *profile.Profile, def *harness.Definition, configDir, recPath string, dryRun bool) (Result, error)
  func Status(p *profile.Profile, def *harness.Definition, configDir, recPath string) (StatusReport, error)
  type StatusReport struct {
      Settings []Setting; Sync string /* synced|stale|unknown */; Unplaced []Entry; Err error
  }
  ```

  `wrapper.Expected`, `wrapper.RenderWith` and `wrapper.Environ` call `launch.ReadDoc` and
  `launch.Decide` for the profile, so the wrapper's carrier choice and the folder's always agree;
  a wrapper therefore becomes stale when the file content changes the decision (Codex
  `model_provider`), which `sync` resolves.
- **Step mapping to sync actions**: `set` and `restore` of an edited entry → `launch-set`;
  `remove` → `launch-remove` (with note `restored <key>` when a replaced key comes back);
  `replace-user` → `launch-replace-user`.

## Constitution Check

*Pre-design and post-design: PASS with the amendment this feature makes (FR-024, version 2.0.0).*

| Principle | Assessment |
|---|---|
| I. Harness knowledge is data | Pass. Folder locations, key command form, launch variables to clear, model argument and per-kind `folder_key` are definition fields, loadable and overridable from `harnesses.d`; the amendment adds them to the principle's list. |
| II. Never fight the harness | Pass after amendment. aprofile writes only the entries it placed (recorded), merges into existing files, re-reads before writing, never writes through a link, never touches the `model` key Claude Code or Codex write; idempotent. The amendment lists placed launch entries among allowed writes. |
| III. Isolation by construction | Pass after amendment. Placed entries exist only in a profile's own settings file; a file linked at `all` carries none and stays linkable. Credentials stay private (the key file is referenced, never copied). |
| IV. No secrets in artifacts | Pass. Key commands hold a path or variable name; the record holds locations and the replaced `env_key` name; dry run and reports name entries only; canary sweep extended (SC-003). |
| V. Transparent wrappers, pull-only | Pass after amendment. Wrappers stay exec-style; caller-override rule replaced by the single-source rule; registry gains an additive field with a note; aprofile never touches Corgi. |
| VI. Scriptable first | Pass. All new reporting is in `--json` too; `--model` works for Codex via flags. |
| VII. Tests never touch the real home | Pass. Hermetic homes; harness behavior verified once manually (research.md) and covered by file-level tests. |

## Project Structure

### Documentation (this feature)

```text
specs/004-corgi-integration/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── harness-definition.md
│   ├── registry-and-env.md
│   └── cli.md
└── tasks.md            # /speckit-tasks
```

### Source Code (repository root)

```text
internal/harness/definition.go        # FolderLaunch, LaunchClear, ModelArg, AuthKind.FolderKey, CarriesModel()
internal/harness/loader.go            # validate new fields, allFieldNames, auth_clears rule update
internal/harness/builtin/claude.yaml  # folder_launch (settings.json#env, apiKeyHelper), launch_clear, folder_key flags
internal/harness/builtin/codex.yaml   # folder_launch (provider-auth), model_arg, folder_key on api-key
internal/launch/                      # NEW: carrier decision, planner, key commands, record, writers
  carrier.go                          #   per-setting carrier + reason (pure)
  plan.go                             #   desired entries vs file vs record -> steps, stale, unplaced
  keycmd.go                           #   shell and provider-auth key commands (R5)
  record.go                           #   managed-launch record (load/save like hooksets/record.go)
  apply.go                            #   re-read, re-plan, atomic write; JSON env-object and TOML provider-auth
internal/config/config.go             # ManagedLaunchPath
internal/wrapper/wrapper.go, env.go   # unconditional clearing/setting, clear-only, key check, model arg
internal/wrapper/fit.go               # model fit uses CarriesModel()
internal/sync/sync.go                 # syncLaunch step, continue on launch error, counters, registry Model
internal/registry/registry.go         # Entry.Model
internal/layer/treatment.go           # differsFromDefault ignores placed entries
internal/cli/settings.go              # re-place launch entries after settings pull
internal/cli/edit.go                  # share change effects + record drop; rename/purge record; --model for Codex
internal/cli/add.go, copy.go, importer # CarriesModel() instead of ModelVar != ""
internal/cli/list_show.go             # folder status, self-containment, unplaced entries (text + JSON)
internal/doctor/doctor.go             # launchFindings
internal/cli/harness.go               # show new definition fields
tests/integration/                    # self-contained folder, wrapper env, codex args, canary, idempotency
docs/README.md, README.md, docs/guide # wrappers, registry, Corgi registration page
.specify/memory/constitution.md       # 2.0.0
specs/001-…, specs/002-…              # superseding notes
```

**Structure Decision**: one new package, `internal/launch`, owns everything about folder-carried
settings so `sync`, `show`, `doctor`, `edit` and `settings pull` call one planner. It depends on
`harness`, `profile`, `secret`, `config` and the JSON/TOML helpers in `layer`; nothing in `layer`
depends on it except the `differsFromDefault` hook, which takes the set of placed keys as input.

## Design Decisions

1. **Carrier per setting** (FR-001): for each launch setting the planner returns `folder` or
   `wrapper` plus a reason code (`reference-value`, `auth-kind`, `share-all`, `no-provider`,
   `harness-rewrites`, `no-support`). The wrapper renderer and the folder writer both consume this
   one result, so a setting can never be carried twice or not at all.
2. **Desired state, not diffs**: the planner computes the full desired set of placed entries; the
   writer sets those, removes recorded entries no longer desired, restores a recorded replaced
   `env_key` when its `auth` block goes, and saves the new record. User-changed placed entries are
   overwritten (FR-005).
3. **First placement over a user entry**: when a desired entry exists but is not in the record, it
   is replaced and reported as `replaced user entry <name>` (never the value). The record marks it
   placed from then on.
4. **Neutralizing entries** (FR-006a): when the folder carries the key and the definition declares
   an `env` location, the desired set includes `<VAR>: ""` for every name in `UnsetAuthVars()`
   except variables the profile itself supplies (any extra variable, whichever carrier), so a
   deliberate `CLAUDE_CODE_USE_VERTEX=1` or a pass-through `CLOUD_ML_REGION=env:…` is never
   blanked.
5. **`APROFILE_SET_VARS`** (FR-014): its value lists the profile's launch variable names in a fixed
   order (model, endpoint, extra variables in profile order, then exec-hook `env` variables;
   neutralizing names are not listed). Exec-hook variables are always wrapper-carried (reason
   `hook-env`) and set unconditionally.
   Carrier is the folder when any launch variable is folder-carried.
6. **Wrapper shape**: capture the sources of every `env:` reference into private temporaries
   (so a self-referencing pass-through such as `CLOUD_ML_REGION=env:CLOUD_ML_REGION` survives) →
   cleanup block (unconditional) → config env → contract → auth (clear all;
   set wrapper-carried key; check folder-carried key without exporting) → herdr → wrapper-carried
   launch variables set unconditionally → `APROFILE_SET_VARS` when wrapper-carried → guard →
   `exec <binary> <model_arg…> "$@"`.
7. **Stale folder** (FR-020a): the planner's step list for the current file and record is
   non-empty. `list`/`show` show `folder: stale`, `doctor` a problem with `run: aprofile sync`.
8. **Errors** (FR-009a): planner errors (parse, shape) become a profile problem; `sync` `continue`s
   before the wrapper write so wrapper and registry entry are preserved by the existing
   `preserve()` path.
9. **Codex `--model`** (FR-023): `Definition.CarriesModel()` is true when `model_var` or `model_arg`
   is set; every `ModelVar != ""` check uses it.
10. **Constitution and notes** (FR-024): edit `constitution.md` directly with a Sync Impact Report
    in the file's existing format (version 2.0.0), and add superseding notes to specs 001 and 002.

11. **Key referenced through a cleared variable** (C1): an `env:` key whose variable is in
    `UnsetAuthVars()` or `launch_clear` is wrapper-only (`cleared-source`); the wrapper captures
    it before clearing, as today.
12. **Missing files**: a `folder_launch` file that is absent and not linked is created holding only
    the placed entries (as `sync` creates absent mixed files today); an absent `config.toml` means
    no selected provider, so the Codex key is wrapper-only (`no-provider`).
13. **Parse errors without anything to place**: when `Decide` puts every setting in the wrapper
    (login profiles, no-support harnesses), a parse error in the file is a note, not a problem, and
    the wrapper is still written.

## Review Focus

- Carrier agreement: the wrapper and the folder must take the same `Decide` result; look for any
  path that renders a wrapper without reading the `Doc`.
- Secrets: the record stores replaced keys only for names in `key_command.replaces`
  (`env_key`); a provider's `experimental_bearer_token` or any other key is never recorded or
  printed.
- Neutralizing exceptions and the `env:` capture order (pass-through variables).
- Write races with the harness: re-read before write, atomic replace.
- Idempotency: `sync` twice writes nothing; JSON key-order normalization happens once.

## Verification of SC-001

A table-driven integration test runs over nine fixtures shaped like the daily API-key profiles and
checks the written files and the key command's output (automated). Launching the real harnesses
against a stub endpoint is done manually in the walkthrough (`/speckit-spex-walkthrough`), with the
procedure in research.md.

## Migration and compatibility

- First `sync` after upgrade places launch entries into every eligible profile folder and reports
  each replaced user entry; it rewrites every wrapper (new shape) and the registry (new field).
- Existing `config.toml` files of Codex API-key profiles lose `env_key` in the selected provider
  (recorded, restored if the auth kind changes) and lose comments on that first write; dry run says
  so.
- Users relying on `ANTHROPIC_MODEL=x claude-…` must switch to `--model x` (README note).
- Codex profiles can now take `--model`; existing Codex profiles have none until set.

## Complexity Tracking

No unjustified violations. The constitution amendment is the feature's own deliverable (FR-024),
not an exception.
