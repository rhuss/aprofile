# Contract: registry format and environment contract (public interfaces)

These are public interfaces (Constitution V, FR-030, FR-032). Breaking changes require a bumped
`format_version` and a migration note.

## Registry file

- Location: `<aprofile data dir>/registry.json` (fixed, independent of the bin directory). Printed
  by `aprofile registry path`.
- Written atomically by `sync`. Contains no timestamps or volatile fields: an unchanged `sync`
  rewrites nothing (FR-030). Contains no secret values (FR-014, FR-028).

```jsonc
{
  "format_version": 1,
  "profiles": [
    {
      "harness": "claude",
      "name": "work",
      "label": "Work",
      "color": "blue",
      "icon": "W",
      "auth_kind": "oauth-token",
      "primary": true,
      "config_dir": "/abs/path/to/profile/config/dir",
      "wrapper_path": "/abs/path/to/bin/claude-work",
      "share": "content"
    }
  ]
}
```

Field notes: `color`/`icon` are the display values from the profile. `config_dir` is the directory
the wrapper points the harness at. `wrapper_path` is the generated launcher. No credentials, no
references.

**Addition (feature 002, `specs/002-profile-isolation-levels`)**: `share` (`all`, `content` or
`none`) was added additively at `format_version` 1; a reader that ignores unknown fields is
unaffected. Profiles without a stored level are published as `all`.

**Addition (feature 004, `specs/004-corgi-integration`)**: `model` (the profile's configured model
from the profile store, `""` when unset) was added additively at `format_version` 1, after
`auth_kind`; a reader that ignores unknown fields is unaffected.

## Omarchy export (opt-in, FR-031)

Written only on `aprofile registry export --format omarchy`, one file per provider at
`$XDG_STATE_HOME/omarchy/agents/accounts/<provider>.json` (fallback `~/.local/state`, override
`--output-dir`):

```jsonc
{ "accounts": [ { "id": "aprofile:claude/work", "label": "Work", "home": "/abs/config/dir", "primary": true } ] }
```

Entries whose `id` lacks the `aprofile:` prefix are preserved unchanged; `aprofile:` entries for
profiles that no longer exist are removed. `provider` comes from the harness definition.

Merge rules (T074):

- Every existing `<provider>.json` in the output dir is visited, not only providers that
  currently have profiles, so a provider whose last profile was removed loses its stale
  `aprofile:` entries too.
- Foreign entries, unknown top-level keys and unknown entry fields are kept as raw JSON, in
  their order. An `aprofile:` entry of a current profile is updated in place (`id`, `label`,
  `home`, `primary`), keeping fields another tool added; new entries are appended sorted by id.
- An `aprofile:` entry of a profile that exists but cannot be exported (its file or its harness
  definition failed to load) is kept unchanged.
- A harness whose definition has no `omarchy_provider` has no Omarchy entries. Each of its
  profiles is skipped with `aprofile: profile <harness>/<name> has no Omarchy provider; skipped`
  on stderr; the export does not fail, and an `aprofile:` entry for such a profile found in an
  existing file is removed like any other stale entry. All four built-in harnesses declare a
  provider (`anthropic`, `openai`, `opencode`, `pi`).
- A file is never deleted; one left without entries keeps `"accounts": []`. A file with nothing
  to change is not rewritten (aprofile never reformats a file it has no entries in).
- Every file is parsed before any is written; an unparseable file aborts the export with
  nothing written.

## Environment contract (APROFILE_*)

Every wrapper sets these, never caller-overridable (FR-026):

| Variable | Value |
|----------|-------|
| `APROFILE_HARNESS` | harness id |
| `APROFILE_NAME` | profile name |
| `APROFILE_LABEL` | profile label |
| `APROFILE_COLOR` | profile color |
| `APROFILE_ICON` | profile icon |
| `APROFILE_CONFIG_DIR` | the profile config dir |
| `APROFILE_SHARE` | the profile's share level: `all`, `content` or `none` (added by feature 002) |
| `APROFILE_SET_VARS` | space-separated names of every launch variable the profile supplies (model, endpoint, extra variables, exec-hook `env` variables), whether the folder or the wrapper carries it; carried by the folder's `env` block when the folder carries any launch variable, otherwise exported by the wrapper (feature 004) |

The wrapper also sets the harness's own config-dir variable (e.g. `CLAUDE_CONFIG_DIR`) and the
profile's auth variables, and unsets other auth kinds' variables and the definition's `auth_clears`
variables (provider switches such as `CLAUDE_CODE_USE_VERTEX`), so a shell export cannot override
the profile's auth kind. An extra variable may pass a cleared variable through on purpose
(`--env CLOUD_ML_REGION=env:CLOUD_ML_REGION`): its reference reads the caller's value captured before
the clear. `aprofile login` builds the same environment (one shared model, `wrapper.Environ`),
including the nested-session cleanup, the exec hook's `env` variables and `APROFILE_SET_VARS`; a
declared `env` operation that fails or is not installed stops the login. Consumers (corgi, herdr, cc-deck) read the registry file or these
variables; aprofile requires no change in them (FR-032).

**Wrapper behavior since feature 004** (`specs/004-corgi-integration/contracts/registry-and-env.md`):
model, endpoint and extra variables are no longer caller-overridable. Every wrapper first clears,
unconditionally, the definition's `launch_clear` names, every name in an inherited
`APROFILE_SET_VARS` and its own profile's launch variables; settings the profile folder carries
are only cleared (the harness reads them from its folder), wrapper-only settings are set
unconditionally, and a Codex model is passed as `-c model="…"` before the caller's arguments.
One-run changes use the harness's own flags (`--model`, `-m`).

**aprofile inside a profile session.** A harness isolated through the XDG base directories
(its `config_env` names `XDG_*` variables, as OpenCode's does) runs with those variables
pointing inside the profile config dir. When aprofile itself runs in such a session (from the
harness's shell or a hook), it resolves its own config, data and state dirs with one generic rule:
an `XDG_CONFIG_HOME`, `XDG_DATA_HOME` or `XDG_STATE_HOME` value equal to or below an absolute
`APROFILE_CONFIG_DIR` is treated as unset, so that base falls back to its `$HOME` default
(`~/.config`, `~/.local/share`, `~/.local/state`). Values outside the profile config dir are
honored as usual. The rule looks at paths only, never at the harness name. Limitation: the
wrapper does not record the caller's pre-redirect values, so a user who runs with a custom
`XDG_*_HOME` and a harness that redirects that same variable gets the `$HOME` default inside the
session.

## Exec hook protocol (FR-011)

Executable `aprofile-harness-<name>` on PATH. Invoked with the operation name as the sole argument;
`key=value` lines on stdin; `key=value` lines expected on stdout. Every call receives `harness`,
`profile`, `config_dir`.

| Operation | Called by | Extra input keys | Output keys |
|-----------|-----------|------------------|-------------|
| `env` | `sync` (wrapper render), `login` (same environment), `list`/`show`/`doctor` (stale check) | `auth_kind`, `model`, `endpoint` | any `VAR=value`; secret values MUST use `env:`/`file:` syntax; output is subject to FR-028's prefix check only |
| `pre-launch` | the wrapper, before exec | none | none (exit status only) |
| `write-config` | `sync`, after preparing config dir | `auth_kind`, `model`, `endpoint` | `changed=true\|false` |
| `auth-status` | `show`, `doctor` | `auth_kind` | `status=ok\|missing\|unknown`, optional `detail` |
| `login` | `login` | `auth_kind` | none (interactive) |

Exit 64 = operation unsupported (no-op). Any other non-zero, a timeout (10 s, except `login` and
`pre-launch`), non-`key=value` output or more than 1 MiB of standard output aborts the command naming
the hook and operation (standard error is quoted up to 4 KiB). The hook runs in its own process
group; a timeout kills the group, and the timed-out operation of that executable is not called
again in the same process (later calls fail at once with the same error). `list` reports an
`unknown` wrapper status with the `env` error once per harness on stderr; `show` runs `env` and
`auth-status` concurrently.

A missing executable for a declared operation: `env` aborts the sync of every profile of the harness
(wrappers and registry entries are kept; reported once per harness) and fails `login`; `pre-launch` makes the wrapper exit 127 before the harness starts;
`write-config` is a sync note (pre-launch and write-config notes once per harness); `auth-status` shows as `not-installed` in `show`; `login` falls back
(below). `write-config` is not called by `sync --dry-run` (the plan lists it as a non-mutating
`write-config` step); its output must contain `changed=true` or `changed=false`, anything else is a
protocol error, and only `changed=true` counts as a sync change. `show` calls `auth-status` with the
10 s timeout; a failing call is reported in `credentials.auth_status`, never fails `show`.

`login` runs with the profile's login environment; its stdout and stderr go to the terminal
unparsed, and a hook that needs keyboard input reads `/dev/tty` (stdin carries the input lines).
Exit 64 or a missing executable falls back to the definition's `login_command`; without one,
`aprofile login` fails naming both.
