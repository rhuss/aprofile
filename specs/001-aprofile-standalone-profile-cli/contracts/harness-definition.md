# Contract: harness definition file format (public interface)

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

config_patches:                 # FR-019, merge-only
  - { file: "settings.json", merge: { ... } }

exec_hook:                      # optional, FR-011
  operations: [env, auth-status]
```

Rules:
- An invalid definition is reported with file and field and MUST NOT stop other harnesses from
  loading (FR-012).
- `classification` entries not covered leave the entry unclassified: reported, never linked/copied
  (FR-018).
- A `mixed` file lists a layer per top-level key; uncovered keys are reported like unclassified
  entries (FR-013).
- `skills_location.scope: shared` means `copy` does not write the shared location and reports which
  source skills exist/are missing there (FR-021, FR-036).
- The concrete field *values* for the four built-in harnesses are fixed in
  [research.md](../research.md).
