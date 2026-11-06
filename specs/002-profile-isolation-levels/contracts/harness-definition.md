# Contract: Harness definition changes

**Feature**: 002-profile-isolation-levels. Extends
`specs/001-aprofile-standalone-profile-cli/contracts/harness-definition.md`. Same YAML format for
built-ins (embedded) and `harnesses.d` files.

## Classification

```yaml
classification:
  - { entry: ".credentials.json", layer: credentials }
  - { entry: "*.sqlite",          layer: state }        # companions -wal, -shm, -journal, .lock implied
  - { entry: "settings.json",     layer: settings }
  - { entry: "CLAUDE.md",         layer: content }
  - { entry: "skills/",           layer: content }
  - { entry: "plugins/",          layer: plugin-store }
  - { entry: ".claude.json", layer: mixed, keys: { mcpServers: owned, oauthAccount: credentials } }
```

- `layer` values: `credentials`, `state`, `owned`, `mixed`, `content`, `settings`,
  `plugin-store`. (`hook-location` is declared in its own block, below.)
- `shared` is accepted only in user files, as a deprecated alias for `content`; `doctor` reports
  each use. Built-ins MUST NOT use it and MUST NOT contain a glob rule whose layer is borrowable.
- A rule whose entry ends in `.sqlite`, `.db` or is declared `database: true` covers its companion
  files (research R2).
- Overrides in `harnesses.d` merge `classification` **by `entry`**: a rule with the same entry
  replaces the built-in rule; a new entry is appended; `{ entry: X, layer: null }` removes it. This
  is the one list that merges by key (FR-001a); all other lists still replace.

## Level policy

Built-ins use this default; a definition may override single cells.

```yaml
share_policy:
  content:      { all: link, content: link,      none: own }
  settings:     { all: link, content: copy-once, none: minimal }
  plugin-store: { all: link, content: link,      none: own }
  hook-location:{ all: link, content: managed,   none: managed }
```

Treatments: `link` (symlink to the default setup), `own` (real per-profile entry, never copied
from the default setup, travels with `copy`), `copy-once` (copied from the default setup when
aprofile creates it, hook location excluded), `minimal` (created with only aprofile-owned keys),
`managed` (hook sets placed by aprofile, user entries untouched).

A settings entry that is `mixed` or `owned` is never linked, whatever the policy says (FR-009a);
`harness show` prints the effective treatment per level.

## Hook location

```yaml
hooks:
  location: "settings.json#hooks"   # file#key for event-groups; a directory for files
  form: event-groups                # event-groups | files
```

- `event-groups`: JSON object `{Event: [{matcher, hooks: [...]}]}` at the key path. If the file is
  a settings entry, managed groups live inside the profile's own copy of that file.
- `files`: a directory; managed entries are links into `hooks.d/files/<set>/`.
- A definition without `hooks` does not support hook sets (FR-024).

## Default locations

```yaml
default_locations:
  "config/opencode": "~/.config/opencode"
  "data/opencode": "~/.local/share/opencode"
```

Optional (added during implementation). Maps a profile-relative prefix to its default-setup
location for a harness whose default setup spans several directories (OpenCode's XDG set). The
longest matching prefix wins; an entry without a match lives under `default_config_location`.
Without it, OpenCode content and settings could not be found in `~/.config/opencode`.

## `$HOME` escapes

```yaml
home_escapes:
  - path: "~/.agents/skills"
    what: "skills"
```

Listed by `harness show`, `show` and `doctor` (information severity) (FR-020, FR-022).

## Removed or changed fields

- `skills_location.scope` is ignored (skills are content); `skills_location.path` still tells
  `copy` and `show` where skills live.
- `plugin_mechanism` stays a free-text description; the plugin store is now a classification.
- `allFieldNames` gains `share_policy`, `hooks`, `home_escapes` so `harness show` reports origins.
