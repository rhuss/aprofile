# Research: Self-Contained Profile Folders

All harness behavior below was verified on 2026-11-09 against Claude Code 2.1.319 and Codex
0.167.0 with a local logging stub server, a fresh config folder and an empty environment
(`env -i`), never with real keys or endpoints.

## R1. Claude Code reads launch settings from `settings.json` (FR-001, FR-002)

- Decision: carry model, endpoint and literal extra variables as entries of the `env` block of the
  profile's `settings.json`, and the key as `apiKeyHelper`.
- Evidence:
  - With only `CLAUDE_CONFIG_DIR` set, `GET /v1/models` (gateway discovery) and every
    `POST /v1/messages` went to the block's `ANTHROPIC_BASE_URL`.
  - `apiKeyHelper` output was sent as `x-api-key` and as `Authorization: Bearer`, so it serves
    both the `api-key` and the `auth-token` auth kinds. `oauth-token` (`CLAUDE_CODE_OAUTH_TOKEN`)
    is a different mechanism and stays wrapper-only.
  - `--model haiku` sent the block's `ANTHROPIC_DEFAULT_HAIKU_MODEL`.
- Alternatives: keeping launch settings wrapper-only (the status quo) fails the feature's goal; a
  separate per-profile settings file would need a launch flag, which config-folder-only tools
  cannot pass.

## R2. Precedence in Claude Code (FR-012, FR-013, US4)

- The `env` block beats the launching environment for `ANTHROPIC_MODEL` and `ANTHROPIC_BASE_URL`.
- The block's `ANTHROPIC_MODEL` beats the `model` key that `/model` writes, so a Claude folder
  starts every launch on the profile's model even without the wrapper.
- An exported `ANTHROPIC_API_KEY` beats the helper in `x-api-key`; an exported
  `ANTHROPIC_AUTH_TOKEN` beats it in `Authorization`. Empty entries for both in the block restore
  the helper's key in both headers (FR-006a).
- An exported `CLAUDE_CODE_USE_VERTEX=1` sends requests to Vertex even with the block's endpoint;
  an empty `CLAUDE_CODE_USE_VERTEX` entry in the block sends them back to the block's endpoint.
  So the folder neutralizes the whole auth-clearing list (`UnsetAuthVars()`) with empty entries.
- Consequence: environment overrides cannot work once a variable is in the block, which is why
  FR-013 removes them and one-run changes go through `--model`.

## R3. Codex key by command (FR-009)

- Decision: write `[model_providers.<selected>.auth]` with `command = "sh"` and `args` that run
  the same `read` as the wrapper, the reference passed as a positional argument (no quoting of the
  path inside the script).
- Evidence: with `OPENAI_API_KEY` unset, Codex sent the command's output as `Bearer` on
  `GET /v1/models` and `POST /v1/responses`.
- Codex refuses `auth` together with `env_key` in the same provider block ("provider auth cannot be
  combined with env_key"), with or without the variable set. Replacing `env_key` is required, and
  restoring it on removal (FR-005) keeps the user's block working after a change of auth kind.
- The selected provider is the top-level `model_provider` key. When absent or not defined under
  `model_providers` in the same file, the key stays wrapper-only (built-in providers cannot take an
  `auth` block from this file in a way we verified).
- Codex has no `env` block, so FR-006a (neutralizing exported variables) does not apply; with
  `env_key` removed, an exported `OPENAI_API_KEY` is not read by the custom provider.

## R4. Codex model by launch argument (FR-016, FR-023)

- Decision: the wrapper runs `codex -c 'model="<toml-escaped model>"' "$@"`.
- Evidence: `/model` saves `model` (and `model_reasoning_effort`) into `config.toml` by default
  ("enter default · s session"), so writing `model` into the file would fight the harness.
  `-c model=…` beats the file. A caller's `-m` (top level or after `exec`) or later
  `-c model=…` wins over it; a second top-level `-m` would fail ("cannot be used multiple
  times"). An unquoted `-c model=1.5` fails ("invalid type: floating point"); the quoted TOML
  string works.
- The model value is escaped as a TOML basic string (backslash and double quote escaped, control
  characters rejected by profile validation) and the whole argument is shell-quoted.

## R5. Key command reproduces the wrapper's reference semantics (FR-006)

- The wrapper resolves `file:` references with `IFS= read -r VAR < path` and fails closed when the
  result is empty (`internal/secret/render.go` `ResolveShell`). `env:` references read the
  variable.
- Decision: both harnesses run the key command through `/bin/sh`, so the command uses the same
  builtin and gets the same bytes (backslashes kept, trailing whitespace and a carriage return kept
  exactly as `read -r` keeps them; `IFS=` disables trimming).
  - Claude `apiKeyHelper` (a shell string):
    `k=; IFS= read -r k < '<path>' || [ -n "$k" ]; printf '%s' "$k"` with the path single-quoted by
    the existing `ShellQuote`; `env:` form: `printf '%s' "${NAME:-}"` with `NAME` validated by
    `secret.ValidEnvVarName`.
  - Codex `auth` (`command`/`args`, no shell string from aprofile values): `command = "sh"`,
    `args = ["-c", 'k=; IFS= read -r k < "$1" || [ -n "$k" ]; printf "%s" "$k"', "aprofile-key", "<path>"]`.
- The Go-side `secret.Resolve` strips `\r` (bufio.Scanner); it is not used for the folder, so the
  folder matches the wrapper, not the Go resolver. Tests compare the command's output with the
  wrapper's resolved value for files with backslashes, trailing spaces, CRLF and no final newline.

## R6. Record of placed entries (FR-004, FR-005)

- Decision: `<XDG_DATA_HOME>/aprofile/managed-launch/<harness>/<name>.json`, beside the existing
  `managed-hooks` record, with the same load/save rules (missing file is empty, write only on
  change, delete when empty, atomic). Rename and `remove --purge` move or delete it like the hooks
  record.
- Contents: format version, and per placed entry the file, the location (`env.<NAME>`,
  `apiKeyHelper`, `model_providers.<p>.auth`), and for a replaced setting its original raw value
  (`env_key` of the provider block). No key values: the key command contains a reference path or
  variable name only.
- Unlike hook entries (a user edit is "diverged" and left alone), a placed launch entry the user
  changed is restored (FR-005), because the profile is the single source.

## R7. Write safety and formatting (FR-004, FR-009a)

- Reuse the config-patch encoders: `decodeJSONObject`/`EncodeJSON` (untouched values byte for
  byte, sorted keys and indentation normalized) and go-toml v2 for `config.toml` (re-encoded,
  comments lost, key order may change; dry run says so).
- Unlike `ApplyConfigPatches`, the launch writer re-reads the file right before writing and
  re-applies its plan to the fresh content, then writes atomically, because Claude Code (`/model`)
  and Codex (`/model`) write the same files at runtime.
- A parse error, an `env` that is not an object, or a selected provider entry that is not a table
  stops that profile: no write, wrapper and registry entry left unchanged (`sync` `continue`s
  before the wrapper write; today patch problems do not), report naming profile, file and key,
  exit non-zero.

## R8. Leaks across sessions (FR-014)

- Claude Code exports the `env` block into processes it starts (its shell tool), so a wrapper
  started from such a shell inherits the previous profile's launch variables.
- Decision: every wrapper clears, unconditionally, the definition's `launch_clear` names, every
  name in an inherited `APROFILE_SET_VARS`, and its own profile's launch variables, then sets its
  wrapper-carried ones. When the folder carries launch variables, it also carries
  `APROFILE_SET_VARS` (a placed entry), so sessions started without the wrapper still pass the list
  on. Names that are not valid variable names, `PATH`, `HOME`, the config-folder variable and
  `APROFILE_*` contract names are skipped when clearing from the inherited list.

## R9. Shared settings paths that must know about placed entries (FR-008a, FR-010, FR-020b)

- `layer.differsFromDefault` ignores the hook key only; it must ignore placed entries too, or a
  `content` profile always reads "differs from the default setup".
- `settings pull` excludes top-level keys only (`env` holds user and placed entries), so the
  launch entries are re-placed after the pull is applied, in the same command.
- `edit --share` to `all`: the record is dropped (the file becomes a link that aprofile never
  writes); from `all`: the next `sync` places the entries. Both appear in the level change's dry
  run beside the hook preview.
- Unplaced launch entries (FR-020b): an `env` entry whose name is in `launch_clear`, the auth
  clearing list or the profile's extra variable names, or an `apiKeyHelper`, that the record does
  not list.

## R10. Test approach (SC-001 to SC-006, Principle VII)

- No HTTP stub exists in the repo. Real harness binaries are not available in CI, so SC-001 is
  verified at the boundary aprofile controls: tests read the written `settings.json` and
  `config.toml`, run the key command with `/bin/sh` and compare its output with the wrapper's
  resolution, and check every launch variable and the neutralizing entries. The verified harness
  behavior (R1 to R4) is recorded here and re-checked manually in the walkthrough.
- The existing stub harness (`tests/fixtures/stub`) is installed as `claude` and, new, as `codex`
  to observe wrapper environments and arguments (US4, FR-016).
- Canary sweep (`canary_test.go`) is extended to the new files and record (SC-003).
