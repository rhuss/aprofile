# Contract: harness definition file format (public interface)

> **Superseded in part by feature 002** (`specs/002-profile-isolation-levels/contracts/harness-definition.md`): the `*` catch-all with layer `shared`, the `shared` layer itself and `skills_location.scope` below are slice one examples. Since 002 the built-ins have no catch-all, the former shared layer is split into `content`, `settings`, `plugin-store` and `hook-location`, skills are content, and locations outside the config dir are `home_escapes`. `default_locations` (per-entry default paths, e.g. Claude `.claude.json` at `~/.claude.json`) is documented there too.

Built-in definitions ship embedded in the binary; users add or override harnesses by dropping files
with the same format into `<aprofile config dir>/harnesses.d` (FR-007, FR-008, FR-009). A user file
whose `name` equals a built-in's overrides only the fields it sets; all other fields come from the
built-in, and `aprofile harness show` marks which is which (FR-009).

Format is a structured file (YAML or JSON; the loader accepts the project's chosen format, decided
in the plan). Schema:

```yaml
name: claude                    # harness id (required)
binary: claude                  # executable to exec (required)
wrapper_prefix: claude          # wrapper name is "<wrapper_prefix>-<profile>"
config_env:                     # env vars that redirect the harness into the profile (FR-020)
  CLAUDE_CONFIG_DIR: "."        #   path relative to the profile root; "." = the root
# OpenCode would instead list the XDG set; pi lists PI_CODING_AGENT_DIR + PI_CODING_AGENT_SESSION_DIR.
# APROFILE_CONFIG_DIR is always the profile root. Concrete values per harness: see research.md.
default_config_location: "~/.claude" # link source for shared entries
model_var: ANTHROPIC_MODEL      # optional
endpoint_var: ANTHROPIC_BASE_URL # optional
omarchy_provider: anthropic     # optional, for registry export
capabilities: [login, oauth-token, native-profiles, mcp]  # at least these keys recognized

auth_kinds:                     # FR-038
  - name: oauth-token
    vars: { ANTHROPIC_AUTH_TOKEN: ref }   # var -> expects a reference
  - name: api-key
    vars: { ANTHROPIC_API_KEY: ref }
auth_clears:                    # optional, FR-007/FR-027: variables every auth kind clears
  - CLAUDE_CODE_USE_VERTEX      #   (provider switches and their settings), except a kind
  - CLOUD_ML_REGION             #   that sets the variable itself

login_command:                  # optional, FR-039: how `aprofile login` runs the login flow
  args: [auth, login]           #   arguments after `binary`; [] starts the bare binary
  note: "..."                   #   optional hint printed before the launch

classification:                 # FR-013; one rule per entry or glob
  - { entry: "projects/",       layer: state }
  - { entry: "statsig/",        layer: state }
  - { entry: ".credentials.json", layer: credentials }
  - entry: ".claude.json"       # a mixed file: layer per top-level key
    layer: mixed
    keys:
      mcpServers: owned
      numStartups: state
      projects: state
      oauthAccount: credentials
  - { entry: "*",               layer: shared }   # default for everything else

mcp:                            # absent when no mcp capability
  location: ".claude.json#mcpServers"
  flavor: claude-json
skills_location:                # FR-021
  path: "<config_dir>/skills"
  scope: per-profile            # or: shared
plugin_mechanism: "enabledPlugins in settings.json"

config_patches:                 # FR-019, merge-only (applied by sync)
  - { file: "settings.json", merge: { env: { FOO: "1" } } }   # .json or .toml

exec_hook:                      # optional, FR-011
  operations: [env, auth-status]
```

Rules:
- An invalid definition is reported with file and field and MUST NOT stop other harnesses from
  loading (FR-012).
- `wrapper_prefix` MUST be unique across the effective definitions (FR-002). A prefix declared by
  more than one harness is a load error naming every harness that declares it; the definitions
  stay loaded, and wrapper-name clashes between their profiles are refused by `add`, `rename`,
  `copy` and `import` and skipped by `sync`.
- `classification` entries not covered leave the entry unclassified: reported, never linked/copied
  (FR-018).
- A `mixed` file lists a layer per top-level key; uncovered keys are seeded like settings keys
  unless their name looks like a credential (never seeded), and `doctor` reports them once per
  harness as information (FR-013).
- `auth_clears`: the wrapper and `aprofile login` unset these together with every auth kind's
  variables, then set the selected kind's own, so a clear applies to every kind except the one
  that sets the variable. A profile can still opt in with an extra variable (`--env`). An exec
  hook's `env` operation may not set them. Names must be valid identifiers and must not be a
  `config_env` variable, `model_var`, `endpoint_var` or an `APROFILE_*` variable.
- `aprofile login` calls the exec hook's `login` operation when the hook declares it; otherwise,
  or when the hook exits 64 or is not installed, it execs `binary` with `login_command.args` and
  the profile's environment. A definition with neither fails with an error naming both (FR-039).
  Built-ins: claude `auth login`, codex `login`, opencode `auth login`, pi `[]` (pi signs in
  with `/login` inside a session).
- `config_patches` (FR-019): `sync` applies each patch in order after preparing the config dir.
  `file` is a relative path inside the profile dir ending in `.json` or `.toml`; `merge` is a
  non-empty map without null values (a load error names `config_patches` otherwise). A patch
  merges nested maps key by key and replaces any other value; it never removes a key, never
  creates an absent file, and never writes through a link anywhere on the path (a linked entry
  belongs to the default setup), reporting those as a note. A file is rewritten only when a
  patched value differs, atomically and with its mode kept; JSON values the patch does not touch
  keep their exact content, TOML files are re-encoded (comments are lost on a changing write). A
  file that is not a valid JSON object or TOML document is a problem and stays untouched. `sync`
  reports each applied patch as a `patch` step naming the keys it set; a second `sync` changes
  nothing.
- A declared exec-hook operation whose executable `aprofile-harness-<name>` is not on PATH:
  `env` is an error (the profile's sync fails and its wrapper is kept), `pre-launch` stops the
  wrapper with exit 127 before the harness starts (sync adds a note), `write-config` is a sync
  note, `auth-status` is reported by `show` as `not-installed` (doctor reports nothing), and
  `login` falls back to `login_command`. A definition without `exec_hook`, or one that does not
  list the operation, never looks for the executable.
- `skills_location.scope: shared` means `copy` does not write the shared location and reports which
  source skills exist/are missing there (FR-021, FR-036).
- The concrete field *values* for the four built-in harnesses are fixed in
  [research.md](../research.md).
