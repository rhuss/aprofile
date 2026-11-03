# aprofile

Agent profiles for coding harnesses. Run several accounts or configurations of Claude Code, Codex, OpenCode and pi side by side, each with its own credentials, history, memory and settings, reachable through a short wrapper command on your `PATH`.

An "aprofile" (agent profile) is one account or configuration of one harness, identified by the pair (harness, name). It is distinct from Codex's own native profiles and from cc-setup's permission presets.

## Why

A single machine often needs more than one login for the same tool: a work account and a personal account for Claude Code, a client account for Codex. Pointing each one at a separate configuration directory by hand is fiddly and easy to get wrong, and a mistake mixes transcripts, memory or billing across accounts. aprofile keeps each profile's credentials and state fully separate, shares the settings you want shared, and gives every profile a launcher like `claude-work` that starts the harness already pointed at the right place.

## Install

Build from source (Go 1.24 or newer):

```sh
git clone https://github.com/cc-deck/aprofile
cd aprofile
make build          # writes ./bin/aprofile
# or:
go build -o bin/aprofile ./cmd/aprofile
```

Release packaging is out of scope for this slice; building from source is the supported path.

## Quick start

```sh
# 1. Create a work profile for Claude Code.
aprofile add claude work --label "Work" --color blue --icon W \
  --auth-kind oauth-token --auth-key env:WORK_TOKEN

# 2. Write the wrapper, prepare the config dir, publish the registry, and put
#    aprofile's bin dir on PATH.
aprofile sync

# 3. Open a new shell so the PATH change applies, then start the profile.
claude-work
```

`claude-work` replaces itself with the real `claude` binary, so process-based tools see the harness directly. Every argument you pass is forwarded unchanged.

## Core commands

| Command | What it does |
|---------|--------------|
| `aprofile add <harness> <name>` | Create a profile. Flags: `--model`, `--endpoint`, `--auth-kind`, `--auth-key`, `--env`, `--env-literal`, `--color`, `--icon`, `--label`, `--primary`, `--herdr-agent`, `--adopt`. |
| `aprofile list` | List profiles grouped by harness (`--json` for a stable schema). |
| `aprofile show <harness>/<name>` | Show one profile, including whether it has credentials (never the values). |
| `aprofile edit <harness>/<name>` | Change a profile's fields. |
| `aprofile rename <harness>/<name> <new-name>` | Rename a profile, moving its file and config dir together. |
| `aprofile remove <harness> <name>` | Remove a profile. `--purge` also deletes its config dir (after confirmation). |
| `aprofile sync` | Write one wrapper per profile, prepare each config dir by layer, publish the registry, and manage the PATH block. Idempotent. |
| `aprofile login <harness>/<name>` | Run the harness's own login flow with the profile's environment. |
| `aprofile copy <h>/<n> <h>/<n>` | Copy a profile within or across harnesses, converting MCP servers. |
| `aprofile mcp list <h>/<n>` / `mcp copy <from> <to> [server...]` | Inspect or copy MCP servers. |
| `aprofile harness list` / `harness show <name>` | Inspect the effective harness definitions and the origin of each field. |
| `aprofile registry path` / `registry export --format omarchy` | Print the registry location, or export Omarchy accounts files. |
| `aprofile import cc-deck` | One-time import of existing cc-deck profiles (never modifies cc-deck files). |
| `aprofile doctor` | Check for configuration problems (see below). |
| `aprofile ui` | Browse and edit profiles interactively. |

Every operation is available as a non-interactive command. The interactive UI is a convenience layer over those same operations, so anything you can do in the UI you can also script. Commands that list or show data support `--json`. Colored, icon-rich output is used on a terminal and falls back to plain text when stdout is not a terminal or `NO_COLOR` is set.

Only `sync` writes or removes wrappers. After `add`, `edit`, `rename`, `copy`, `remove` or `import`, the affected wrappers are stale until the next `sync`; `list`, `show` and `doctor` report them as such.

## Built-in harnesses

Four harnesses ship built in. Each is described entirely by data, using the same format a user definition uses.

| Harness | Binary | Wrapper prefix | Config dir lever | Model / endpoint | Default auth |
|---------|--------|----------------|------------------|------------------|--------------|
| Claude Code | `claude` | `claude-` | `CLAUDE_CONFIG_DIR` | `ANTHROPIC_MODEL` / `ANTHROPIC_BASE_URL` | auth token (env based) |
| Codex | `codex` | `codex-` | `CODEX_HOME` | model via `--model`; endpoint `OPENAI_BASE_URL` | ChatGPT login |
| OpenCode | `opencode` | `opencode-` | the XDG base set (`XDG_DATA_HOME`, `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME`) | not carried in this slice | provider login |
| pi | `pi` | `pi-` | `PI_CODING_AGENT_DIR` (plus a session pin) | not carried in this slice | login |

Model and endpoint are settable only for a harness whose definition declares the backing environment variable. In this slice Claude declares both, Codex declares the endpoint (its model is a pass-through CLI flag), and OpenCode and pi declare neither; `add` and `edit` reject `--model` or `--endpoint` for a harness that cannot carry them rather than silently dropping the value.

## The layer model

Every entry of a profile's config dir belongs to one layer, declared by the harness definition:

- **credentials**: per profile, created by the harness or `login`, never linked, never copied, never written to the registry or wrappers.
- **state** (sessions, transcripts, history, logs, caches, auto-memory): per profile, created empty, never linked, never copied.
- **owned** (MCP servers, skills, the enabled plugin set, model and endpoint settings): per profile, carried over by `copy`.
- **shared**: everything else, symlinked to the harness's default setup so settings you keep there (for example a shared `CLAUDE.md` or keybindings) apply to every profile.

A structured file may instead be **mixed**, with a layer per top-level key. Claude's `.claude.json`, for example, holds account keys (credentials), `mcpServers` (owned) and project trust (state) in one file. A mixed file is always a real per-profile file; its shared keys are seeded once from the default setup and then belong to the profile. An entry the definition does not classify is left in place and reported, never silently shared.

aprofile does not regenerate, overwrite or delete files a harness writes at runtime, except for the explicit, confirmed operations `remove --purge`, `copy --overwrite` and `import --overwrite-credentials`. Config patches merge into existing files and preserve keys aprofile does not own. `sync` is idempotent: a second run with no changes writes zero files.

## Wrappers and the environment contract

A wrapper is a small POSIX-sh launcher. It is self-contained: at launch it needs only a POSIX shell and the harness binary, and it invokes nothing else except the harness's exec hook when the definition declares a `pre-launch` operation. It always sets the profile's config dir variable and a neutral contract that other tools can read:

```
APROFILE_HARNESS  APROFILE_NAME  APROFILE_LABEL
APROFILE_COLOR    APROFILE_ICON  APROFILE_CONFIG_DIR
```

Model, endpoint and extra variables use `${VAR:-default}`, so a caller can override them for one run. Authentication variables are always set from the profile and are never caller-overridable; the wrapper clears every other auth kind's variables. Integration with other tools is pull-only: aprofile publishes a machine-readable registry at `<data dir>/registry.json` and the `APROFILE_*` contract, and requires no changes in, and writes nothing into, those tools.

## Secret references

aprofile never writes a secret value into any artifact it generates (wrappers, the registry, the profile store, harness definitions, command output or `--json`). Secrets are stored as references and resolved only when a wrapper runs:

- `env:NAME` reads the secret from an environment variable at launch.
- `file:/absolute/path` reads the first line of a file at launch.

Auth options accept only references. A value that looks like a secret (a known token prefix such as `sk-`, `ghp_` or `xox`, or a long value with no spaces) is rejected with a message asking for a reference. If a reference cannot be resolved at launch, the wrapper stops with a message naming the profile and the reference, and never starts the harness.

The only files aprofile writes that hold secret values are credentials-layer files and the backups `import` makes. Never commit an MCP configuration file that embeds a token; keep such files out of version control and use references instead.

## Harness definitions and the exec hook

All per-harness knowledge lives in definition records, not in code that branches on a harness name. Built-in definitions are embedded in the binary; you can add a harness or override individual fields of a built-in by dropping a file into `<config dir>/aprofile/harnesses.d/`. A file whose `name` matches a built-in overrides only the fields it sets: maps merge by key, scalars and lists replace, and an explicit null removes an inherited entry.

For behavior a definition cannot express, aprofile calls an executable named `aprofile-harness-<name>` on `PATH` with the operation as its only argument, passing `key=value` lines on standard input and reading `key=value` lines from standard output. Every call receives `harness`, `profile` and `config_dir`.

| Operation | Called by | Extra input | Output |
|-----------|-----------|-------------|--------|
| `env` | `sync`, when rendering the wrapper | `auth_kind`, `model`, `endpoint` | any `VAR=value` (secrets must be references) |
| `pre-launch` | the wrapper, before it replaces itself | none | none (exit status only) |
| `write-config` | `sync`, after preparing the config dir | `auth_kind`, `model`, `endpoint` | `changed=true` or `changed=false` |
| `auth-status` | `show`, `doctor` | `auth_kind` | `status=` one of `ok`, `missing`, `unknown`, plus optional `detail` |
| `login` | `login` | `auth_kind` | none (interactive) |

aprofile calls only the operations the definition lists. Exit status 64 means "not supported" and is treated as a no-op. Any other non-zero exit, output that is not `key=value` lines, or a timeout (ten seconds, except `login` and `pre-launch`) aborts the command with an error naming the hook and operation.

## doctor

`aprofile doctor` (with `--json`) checks: the bin dir is on `PATH`; wrapper shadowing by a same-named executable earlier on `PATH`; stale or missing wrappers; missing harness binaries; unclassified config-dir entries; state or credentials entries that are links; invalid definitions; references that cannot be resolved now; the exec-hook `auth-status` result where a hook implements it; and seeded mixed-file keys that drift from the default setup. Each finding names the affected profile, if any, and a fix.

## Platforms

aprofile runs on Linux and macOS and follows the XDG base directories with the usual fallbacks (`~/.config`, `~/.local/share`, `~/.local/state`) on both. The test suite runs on both platforms and never reads or writes the real home directory of the user running it.
