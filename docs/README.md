# aprofile project details

For setup and everyday use, start with the [user guide](../README.md):

- [Build and launch your first profile](guide/docs/user/getting-started.md)
- [Choose a share level](guide/docs/user/share-levels.md)
- [Manage profiles](guide/docs/user/manage-profiles.md)
- [Troubleshoot profiles](guide/docs/user/troubleshooting.md)
- [Command reference](guide/docs/reference/commands.md)

This document covers configuration internals, integration, and extending aprofile.

## Built-in harnesses

Four harnesses ship built in. Each is described entirely by data, using the same format a user definition uses.

| Harness | Binary | Wrapper prefix | Config dir lever | Model / endpoint | Default auth |
|---------|--------|----------------|------------------|------------------|--------------|
| Claude Code | `claude` | `claude-` | `CLAUDE_CONFIG_DIR` | `ANTHROPIC_MODEL` / `ANTHROPIC_BASE_URL` | auth token (env based) |
| Codex | `codex` | `codex-` | `CODEX_HOME` | model via `--model`; endpoint `OPENAI_BASE_URL` | ChatGPT login |
| OpenCode | `opencode` | `opencode-` | the XDG base set (`XDG_DATA_HOME`, `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME`) | not carried in this slice | provider login |
| pi | `pi` | `pi-` | `PI_CODING_AGENT_DIR` (plus a session pin) | not carried in this slice | login |

Model and endpoint are settable only for a harness whose definition declares the backing environment variable. In this slice Claude declares both, Codex declares the endpoint (its model is a pass-through CLI flag), and OpenCode and pi declare neither; `add` and `edit` reject `--model` or `--endpoint` for a harness that cannot carry them rather than silently dropping the value.

## History groups

Some profiles should continue each other's conversations, others should only share among themselves, and some must stay apart. A history group is a set of profiles of one harness that share one conversation history: a conversation started in one member can be resumed in another with the harness's own resume (`claude-lab --resume <id>`), and it then runs with that member's login, endpoint and model. There is only one copy of the conversation on disk; the members' history entries are links to the group's store.

- `home` is built in for every harness that supports groups. Its store is your default setup, so its members share history with the plain `claude` (or `codex`, `pi`). Join it with `--group home`.
- Your own groups are made with `aprofile group create claude/work`. Their history lives in aprofile's data directory (`<data dir>/aprofile/groups/<harness>/<group>/`), apart from the default setup. Join with `--group work`.
- A profile in no group is isolated: its history is its own. That is the default for new profiles.

Logins, settings and all other runtime state stay private in every group, and membership does not depend on the share level: a `none` profile can join `home` and then shares only its history.

| # | Use case | Group | Why |
|---|----------|-------|-----|
| H1 | Personal aliases (other endpoints or accounts) continue each other's conversations and those of the plain default setup | `--group home` | Each alias links its history to the default setup; logins, endpoints and models stay per profile |
| H2 | Work profiles continue each other's conversations, never personal ones | `--group work` | aprofile group create claude/work first; work history lives apart from the default setup |
| H3 | A client profile that must never see other history or memory, and whose history no other profile sees | `no group` | The profile's history is its own (isolated), as without history groups |
| H4 | A demo profile with a clean history | `no group` | Nothing from other conversations shows up |

```sh
aprofile group create claude/work
aprofile add claude acme --auth-kind oauth-login --group work
aprofile add claude lab --endpoint https://lab.example.com --group home
aprofile edit claude/personal --group home --dry-run   # what a join moves and sets aside
aprofile edit claude/client --no-group                 # back to its own, fresh history
aprofile group list
```

Naming a group that does not exist fails with exit code 2 and lists the harness's groups, so a typo never starts a new empty history. Joining with existing history moves each conversation file into the group's store where no file of that name exists yet, and sets everything else aside (prompt history, databases, colliding project memory) under `<data dir>/aprofile/set-aside/<harness>/<name>/<timestamp>/` with a manifest; nothing in the store is overwritten and nothing is deleted. `edit` lists the plan first, asks once (or needs `--yes`) when anything moves or is set aside, and refuses while the profile runs. Leaving a group removes only the links; the group's history stays and the profile starts with fresh, empty history. `group delete` refuses while a group has members and keeps the store unless you pass `--purge`; `home` cannot be created, renamed or deleted.

Running the same conversation in two profiles at the same time is not prevented and can interleave its transcript, so resume a conversation in one profile at a time. Shared conversations also show which account wrote them, because transcripts carry account identifiers. OpenCode keeps sessions and logins in one database, so it does not support history groups; `aprofile harness show <name>` lists each harness's history entries and notes such as pi's session format migration.

`doctor` reports a member whose history entry became a real file or directory, a link to the wrong store, a missing store and a profile of a group that does not exist; `sync` recreates missing links but never moves data or writes into your default setup, and `sync --relink` restores a replaced link after a backup.

## The layer model

Every entry of a profile's config dir belongs to one category, declared by the harness definition:

- **credentials**: per profile, created by the harness or `login`, never linked, never copied, never written to the registry or wrappers.
- **state** (sessions, transcripts, history, logs, caches, databases with their `-wal`/`-shm` companions, locks): per profile, created empty, never linked, never copied. The one exception is opt-in: the entries a definition marks as history (`history: true`) are linked to the group store for a member of a history group.
- **owned** (MCP servers, model and endpoint settings): per profile, carried over by `copy`.
- **content** (instructions such as `CLAUDE.md`, skills, agents, commands, prompts, themes, keybindings), **settings** (permissions, enabled plugins, status line), the **plugin store** (installed plugin code) and the **hook location**: what the share level decides about, as described in the [share-level comparison](guide/docs/user/share-levels.md).

A structured file may instead be **mixed**, with a layer per top-level key. Claude's `.claude.json`, for example, holds account keys (credentials), `mcpServers` (owned) and project trust (state) in one file. A mixed file is always a real per-profile file and is never linked. When aprofile creates it (not at `none`, where it starts minimal), it copies the settings-like keys and the owned keys once from the default setup, and they belong to the profile from then on; state and credentials keys are never copied. A key the definition does not declare is copied unless its name looks like a credential (token, secret, password, API key, auth or account data, or a name ending in "key"). An entry the definition does not classify is left in place and reported, never linked or copied. The built-in definitions have no catch-all rule.

aprofile does not regenerate, overwrite or delete files a harness writes at runtime, except for explicit operations that ask first and back up what they replace: `remove --purge` (asks; it deletes, it does not back up), `copy --overwrite`, a level change to `all`, `settings pull`, `sync --relink` and `hook-sets reapply`. `import --overwrite-credentials` backs up what it replaces but does not ask: the flag itself is the explicit request. Config patches merge into existing files and preserve keys aprofile does not own. `sync` adds, updates and removes the hook entries of a profile's hook sets in the profile's own hook location without asking, because it only ever touches the entries it placed itself; every other entry stays as it is. `sync` is idempotent: a second run with no changes writes zero files.

## Wrappers and the environment contract

A wrapper is a small POSIX-sh launcher. It is self-contained: at launch it needs only a POSIX shell and the harness binary, and it invokes nothing else except the harness's exec hook when the definition declares a `pre-launch` operation. It always sets the profile's config dir variable and a neutral contract that other tools can read:

```
APROFILE_HARNESS  APROFILE_NAME  APROFILE_LABEL
APROFILE_COLOR    APROFILE_ICON  APROFILE_CONFIG_DIR
APROFILE_SHARE    APROFILE_HISTORY_GROUP
```

`APROFILE_HISTORY_GROUP` is the profile's history group (empty when isolated), so a tool such as a session browser can tell which profiles share a history; the registry carries it as `history_group`.

Model, endpoint and extra variables use `${VAR:-default}`, so a caller can override them for one run. Authentication variables are always set from the profile and are never caller-overridable; the wrapper clears every other auth kind's variables and the variables the definition lists in `auth_clears`. For Claude Code these are the provider switches and their settings (`CLAUDE_CODE_USE_VERTEX`, `CLAUDE_CODE_USE_BEDROCK`, `CLAUDE_CODE_USE_FOUNDRY`, `ANTHROPIC_VERTEX_PROJECT_ID`, `CLOUD_ML_REGION` and similar), so a provider exported in your shell cannot redirect a profile; a profile that should use a cloud provider sets the switch itself with `--env`, either as a value or as a pass-through such as `--env CLOUD_ML_REGION=env:CLOUD_ML_REGION`, which reads the caller's value from before the clear. `aprofile login` builds the same environment as the wrapper, including the exec hook's `env` variables. Integration with other tools is pull-only: aprofile publishes a machine-readable registry at `<data dir>/registry.json` and the `APROFILE_*` contract, and requires no changes in, and writes nothing into, those tools.

IDE extensions write lock files into the config dir of the session that started them. An IDE launched from a shell that runs a profile wrapper inherits that profile; one started outside a wrapper talks to the default setup only. To have an IDE use a profile, start it from such a shell.

## Registry and command-line integration

| Command | What it does |
|---------|--------------|
| `aprofile add <harness> <name>` | Create a profile. Flags: `--share`, `--group`, `--hook-sets`, `--exclude-hook-sets`, `--model`, `--endpoint`, `--auth-kind`, `--auth-key`, `--env`, `--env-literal`, `--color`, `--icon`, `--label`, `--primary`, `--herdr-agent`, `--adopt`. |
| `aprofile list` | List profiles grouped by harness (`--json` for a stable schema). |
| `aprofile show <harness>/<name>` | Show one profile: its share level, every config-dir entry with its treatment and reason, hook sets, and whether it has credentials (never the values): credential files in the config dir, whether each auth reference resolves now, and the exec hook's `auth-status` answer, which covers logins kept in the macOS Keychain. |
| `aprofile edit <harness>/<name>` | Change a profile's fields. `--share` applies a level change and `--group <group>` or `--no-group` a history group change, each with `--dry-run` and `--yes`. |
| `aprofile group list\|show\|create\|delete` | Manage history groups (`--json`; `delete --purge` also removes the store). |
| `aprofile rename <harness>/<name> <new-name>` | Rename a profile, moving its file, config dir and imported credential files together (references into that dir follow). Refused while the profile runs; where aprofile cannot check that, it asks first. `--force` skips both. |
| `aprofile remove <harness> <name>` | Remove a profile. `--purge` also deletes its config dir and the credential files `import` copied for it (after confirmation). |
| `aprofile sync` | Write one wrapper per profile, prepare each config dir for its share level, place hook sets, publish the registry, and manage the PATH block. Idempotent. Removes temp entries (`.aprofile-tmp-*`, `.aprofile-new-*`) an interrupted aprofile write left in a config dir once they (and, for a directory, everything in it) are older than a minute, and counts each removal as a change; an entry that an interrupted directory swap left only in its `.<entry>.aprofile-new-*-old` temp is renamed back instead, and a temp entry it cannot remove or restore is a problem. The summary counts problems (`0 changes, 2 problems (see above)`); a run with problems is never reported as up to date. `--fix-links` repairs leaking links, `--relink` restores links a harness replaced with a real file. |
| `aprofile login <harness>/<name>` | Run the harness's own login flow with the profile's environment: the definition's `login_command` (`claude auth login`, `codex login`, `opencode auth login`; pi starts a session where you run `/login`), or the exec hook's `login` operation when the definition declares one. |
| `aprofile copy <h>/<n> <h>/<n>` | Copy a profile within or across harnesses, converting MCP servers. `--overwrite` replaces an existing target's copied parts: it backs up what it replaces under `<data dir>/backups/` and asks first, and in a non-interactive run it requires `--yes` and exits with code 3 without it. `--adopt` reuses a leftover config dir; `--auth-key env:NAME` supplies the key a cross-harness target's auth kind needs. The target is in no history group unless `--group` names one of its harness; copy never carries history. |
| `aprofile mcp list <h>/<n>` / `mcp copy <from> <to> [server...]` | Inspect or copy MCP servers. |
| `aprofile harness list` / `harness show <name>` | Inspect the effective harness definitions, the origin of each field and rule, and what each share level shares. |
| `aprofile harness classify <harness> <entry> --as <category>` | Classify one entry in `harnesses.d`, merged with the built-in rules. |
| `aprofile settings pull <h>/<n> [key...]` | Refresh a `content` profile's settings from the default setup (merge, or `--replace`). |
| `aprofile hook-sets list\|show\|import\|reapply` | Manage the hook-set library. |
| `aprofile help sharing` | The share-level guide in the terminal. |
| `aprofile registry path` | Print the registry location. |
| `aprofile import cc-deck` | One-time import of existing cc-deck profiles (never modifies cc-deck files). The report, `--dry-run` included, names each credential file it copies from outside cc-deck's own folders (path only). A cc-deck `backend` maps to environment variables through the harness definition's `backends` block; a profile with a backend the definition does not declare is skipped. |
| `aprofile doctor` | Check for configuration problems (see below). |
| `aprofile ui` | Browse and edit profiles interactively. |

Every operation is available as a non-interactive command. The interactive UI is a convenience layer over those same operations, so anything you can do in the UI you can also script. Commands that list or show data support `--json`, and so does `sync` (with or without `--dry-run`); every JSON document carries a `format_version`. Colored, icon-rich output is used on a terminal and falls back to plain text when stdout is not a terminal or `NO_COLOR` is set.

Only `sync` writes or removes wrappers. After `add`, `edit`, `rename`, `copy`, `remove` or `import`, the affected wrappers are stale until the next `sync`; `list`, `show` and `doctor` report them as such. After `remove` or `rename` the old wrapper stays on `PATH` until the next `sync` removes it, and both commands say so. `list` prints one line about wrappers that carry the aprofile marker but belong to no profile, and `doctor` reports each of them as a problem with the fix `aprofile sync`.

Wrapper names (`<wrapper_prefix>-<name>`) are unique: `add`, `rename`, `copy` and `import` refuse a profile whose wrapper name another profile already has and name that profile. Two harness definitions that declare the same `wrapper_prefix` are reported as a definition error.

## Secret references

aprofile never writes a secret value into any artifact it generates (wrappers, the registry, the profile store, harness definitions, command output or `--json`). Secrets are stored as references and resolved only when a wrapper runs:

- `env:NAME` reads the secret from an environment variable at launch.
- `file:/absolute/path` reads the first line of a file at launch.

Auth options accept only references. A value that looks like a secret (a known token prefix such as `sk-`, `ghp_` or `xox`, or a long value with no spaces) is rejected with a message asking for a reference. If a reference cannot be resolved at launch, the wrapper stops with a message naming the profile and the reference, and never starts the harness.

aprofile copies secret values only verbatim and with a warning: into credentials-layer files, into a profile's own settings copy or seeded MCP file (`config.toml`, pi's `agent/mcp.json`) when the default setup's file holds them, into the target of a same-harness `copy`, which carries the owned layer verbatim (including tokens a harness wrote into MCP server env or headers) and names each secret-looking value in its report (a cross-harness `copy` and `mcp copy` drop such values instead), and into the backups that `import`, `copy --overwrite`, level changes, `settings pull`, `sync --relink` and `hook-sets reapply` make. Backups are written with private permissions under `<data dir>/backups/`. Never commit an MCP configuration file that embeds a token; keep such files out of version control and use references instead.

## Harness definitions and the exec hook

All per-harness knowledge lives in definition records, not in code that branches on a harness name. Built-in definitions are embedded in the binary; you can add a harness or override individual fields of a built-in by dropping a file into `<config dir>/aprofile/harnesses.d/`. A file whose `name` matches a built-in overrides only the fields it sets: maps merge by key, scalars and lists replace, and an explicit null removes an inherited entry. The `classification` list is the exception: it merges by `entry`, so one added rule does not need a copy of the built-in list, and `{ entry: X, layer: null }` removes a rule. The slice one layer `shared` still loads in user files, read as `content`, and `doctor` reports it.

A definition's `config_patches` list sets keys in a JSON or TOML file of the profile config dir on every `sync`: `{ file: settings.json, merge: { env: { FOO: "1" } } }`. A patch merges nested maps key by key and leaves every other key as it was; it never removes a key, never creates an absent file, and never writes through a link, so a file linked to the default setup is reported and left alone. A file is rewritten only when a patched value differs (keeping its mode), and `sync` lists each patch it applied with the keys it set. TOML files are re-encoded when a patch changes them, so their comments are lost.

A definition's `backends` block tells `import cc-deck` which environment each cc-deck model backend implies, for example Claude Code's `vertex: { env: { CLAUDE_CODE_USE_VERTEX: "1", ANTHROPIC_VERTEX_PROJECT_ID: "{project}", CLOUD_ML_REGION: "{region}" }, credentials_var: GOOGLE_APPLICATION_CREDENTIALS }`. Values may use the `{project}` and `{region}` placeholders, filled from the cc-deck profile; `credentials_var` receives the profile's credentials file. To import a backend the built-in does not know, add it in `harnesses.d`.

For behavior a definition cannot express, aprofile calls an executable named `aprofile-harness-<name>` on `PATH` with the operation as its only argument, passing `key=value` lines on standard input and reading `key=value` lines from standard output. Every call receives `harness`, `profile` and `config_dir`.

| Operation | Called by | Extra input | Output |
|-----------|-----------|-------------|--------|
| `env` | `sync`, when rendering the wrapper; `login`, for the same environment; `list`, `show` and `doctor`, to tell whether the wrapper is stale | `auth_kind`, `model`, `endpoint` | any `VAR=value` (secrets must be references) |
| `pre-launch` | the wrapper, before it replaces itself | none | none (exit status only) |
| `write-config` | `sync`, after preparing the config dir | `auth_kind`, `model`, `endpoint` | `changed=true` or `changed=false` |
| `auth-status` | `show`, `doctor` | `auth_kind` | `status=` one of `ok`, `missing`, `unknown`, plus optional `detail` |
| `login` | `login` | `auth_kind` | none (interactive) |

The extra inputs `auth_kind`, `model` and `endpoint` are sent only when the profile sets them; a missing key means unset. aprofile calls only the operations the definition lists. The `login` hook's output goes straight to the terminal (it reads keyboard input from `/dev/tty`); when it exits 64 or is not installed, `aprofile login` falls back to the definition's `login_command`. Exit status 64 means "not supported" and is treated as a no-op. Any other non-zero exit, output that is not `key=value` lines or more than 1 MiB of it, or a timeout (ten seconds, except `login` and `pre-launch`) aborts the command with an error naming the hook and operation. A timeout kills the hook with every process it started, and an operation that timed out is not called again in the same command, so `list` or `doctor` over many profiles waits for a hanging hook once. `list` shows the wrapper status `unknown` when the `env` operation fails and prints the reason once per harness on standard error.

When the definition declares an operation but `aprofile-harness-<name>` is not on `PATH`: for `env`, `sync` reports a problem once for the harness and keeps the old wrappers of its profiles, and `login` fails; for `pre-launch`, the wrapper stops before the harness starts (exit 127) and `sync` notes the missing hook once per harness. For `write-config`, `sync` notes it once per harness and carries on; `show` reports `auth-status` as not installed; `login` falls back to `login_command`. `write-config` is not called under `sync --dry-run`, and only `changed=true` counts as a change.

## Authoring hook sets

Hook sets keep hooks in one place and let each profile pick the ones it uses. Each set is one file in `<config dir>/aprofile/hooks.d/<name>.yaml` with a body per harness, in that harness's own hook format (aprofile never translates hooks between harnesses):

```yaml
name: jira
description: Link Jira keys in tool output
every_profile: false
bodies:
  claude:
    PostToolUse:
      - matcher: Bash
        hooks: [{ type: command, command: ~/.local/share/hooks/jira-link.sh }]
  opencode:
    files: [jira-link.ts]      # in hooks.d/files/jira/, linked into the profile
```

```sh
aprofile hook-sets import claude --name base       # start from your existing hooks
aprofile edit claude/work --hook-sets base,jira    # attach by name
aprofile edit claude/work --exclude-hook-sets rtk  # skip an every-profile set
aprofile sync                                      # places, updates and removes managed entries
aprofile hook-sets list
```

A set marked `every_profile: true` applies to every `content` profile of the harnesses it has a body for; a `none` profile gets only the sets it names, and an `all` profile uses the hooks of the default setup and cannot attach sets. `sync` records a fingerprint of every hook entry it places, so it updates and removes only those. Entries you or another tool add are never touched. When you edit a managed entry by hand, `sync` leaves it alone and reports it; `aprofile hook-sets reapply <profile>` restores the set's version: `--dry-run` shows the change, and the command backs the file up and asks before writing.

Two alternatives need no hook set:

- Guard inside the hook script on `APROFILE_NAME` or `APROFILE_SHARE`, for example `[ "$APROFILE_NAME" = work ] || exit 0`. Every wrapper sets both.
- For larger bundles, enable a plugin per profile. At `content` and `none` the enabled plugin set lives in each profile's own settings, so enabling a plugin affects that profile only; at `all` it is shared with the default setup.

## doctor

`aprofile doctor` (with `--json`) checks: the bin dir is on `PATH`; wrapper shadowing by a same-named executable earlier on `PATH`; stale or missing wrappers; missing harness binaries; unclassified config-dir entries (aprofile's own temp entries from an interrupted write are not entries and are never reported); state or credentials entries that are links; linked entries a harness replaced with a real file; managed hook entries edited by hand; hook sets that are referenced but missing, and scripts they reference that do not exist; deprecated `shared` rules and catch-all rules in user definitions; invalid definitions and hook sets; references that cannot be resolved now; a declared exec hook that is not on `PATH` (once per harness); a failing exec-hook `env` operation; the exec-hook `auth-status` result where a hook implements it; wrappers that carry the aprofile marker but belong to no profile (fix: `aprofile sync`); a profile whose auth kind its harness definition does not declare; and a model or endpoint on a harness that has no variable for it. As information it lists `content` profiles whose settings differ from the default setup, seeded mixed-file keys that differ from the default setup, undeclared mixed-file keys of the default setup (once per harness, names only, never values), `$HOME` escapes, and default-setup entries that no profile borrows because they are unclassified. Each finding names the affected profile, if any, its severity (`problem` or `info`) and a fix. The exit status counts problems only.

## Platforms

aprofile runs on Linux and macOS and follows the XDG base directories with the usual fallbacks (`~/.config`, `~/.local/share`, `~/.local/state`) on both. Inside a profile session whose wrapper redirects an XDG variable into the profile (OpenCode does), aprofile ignores that redirected value and uses the fallback, so its own config stays where it was. The test suite runs on both platforms and never reads or writes the real home directory of the user running it.
## Maintaining the guide

Edit the guide introduction in [guide/docs/README.md](guide/docs/README.md). The repository's root README is generated from that page with adjusted links and a link to this document. From the repository root, refresh it with:

```sh
make -C docs/guide readme
```

The guide's `make check` verifies that the generated README is up to date.

## Licensing

No project license is currently declared in this repository.
