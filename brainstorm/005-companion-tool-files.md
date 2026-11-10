# Brainstorm: Companion Tool Files in Profile Folders

**Date:** 2026-11-10
**Status:** active

Starting brief, written from a session in the corgi repository after `aprofile sync` reported unclassified entries. Related: [003 Corgi integration](003-corgi-integration.md).

## Problem Framing

Other tools write into profile config folders: Corgi installs its hooks into every account folder it knows, and newer harness releases add files the built-in classification has not seen. aprofile treats an unknown entry safely (left in place, never linked or copied) but reports it on every `sync`:

```
aprofile: note: config-dir claude/deepseek: report .../claude/deepseek/.corgi-hooks.lock (unclassified: left in place, never linked or copied; classify it with: aprofile harness classify claude .corgi-hooks.lock --as <category>)
```

The notes are noise for files that are understood, and they will grow as soon as brainstorm 003 registers more profiles with Corgi, because Corgi then installs hooks into each of them.

Anti-goals:

- A catch-all rule. Codex's classification deliberately has none (FR-001); unknown files must stay visible.
- Requiring changes to Corgi (FR-032).

## Inventory (2026-11-10)

| Entry | Found in | Written by | What it is |
|---|---|---|---|
| `.corgi-hooks.lock` | `claude/deepseek`, `claude/work-deepseek`, `codex/glm`, `~/.claude`, `~/.codex` | Corgi | Empty `flock` file Corgi holds while it edits the hook settings. Removed when Corgi's hooks are uninstalled. |
| `settings.json.corgi-backup` | `claude/deepseek`, `claude/work-deepseek`, `~/.claude` | Corgi | Copy of `settings.json` from before Corgi's first edit, created once (`wx`), mode 0600. Deleted on uninstall, not restored. |
| `config.toml.corgi-backup`, `hooks.json.corgi-backup` | `~/.codex` | Corgi | Same for Codex. Corgi reads the `config.toml` backup on uninstall to restore the original title setting. |
| `auth.json.lock` | `codex/private` | Codex | Codex's lock beside `auth.json`. The built-in definition lists `auth.lock` (verified with Codex 0.166.0), so this is a newer name. |

The removed `claude/vertex-old` folder also has a lock file and a backup; it is no longer a profile.

Source in Corgi: `src/providers/hooks_file.ts` (`edit`, `cleanup`), `src/providers/codex/config.ts`, `src/providers/codex/integration.ts`.

## Proposed classification

- `.corgi-hooks.lock` as `state`: per-folder, never linked or copied.
- `*.corgi-backup` as `state`: per-folder and private. Never linked, so a profile at share level `all` does not see the default setup's backup. A copied profile gets no backup, and Corgi makes its own on first install.
- `auth.json.lock` as `credentials`, the same as `auth.lock`.

Classification entries accept globs (Codex uses `*.sqlite`), so one `*.corgi-backup` rule covers all three backup names.

## Approaches for where the rules live

### A: Built-in definitions

Add the rules to `internal/harness/builtin/{claude,codex}.yaml` (Corgi's files to both, `auth.json.lock` to Codex).

- Pros: works for every user of aprofile and Corgi without setup. `auth.json.lock` is a harness fact and belongs in the built-in definition anyway.
- Cons: the built-ins name a third-party tool. If Corgi renames its files, aprofile follows behind.

### B: User rules through `aprofile harness classify`

Run `aprofile harness classify claude .corgi-hooks.lock --as state` and similar into `~/.config/aprofile/harnesses.d/`.

- Pros: no code change, available today.
- Cons: each user repeats it; nothing documents why.

### C: Companion tool fragments

A small definition per companion tool (for example `companions/corgi.yaml`) that adds its classification rules to each harness it touches, shipped built-in but separable.

- Pros: keeps third-party knowledge apart from harness definitions; room for other tools (herdr, cc-deck) later.
- Cons: a new concept and merge path for a handful of rules.

## Writers on shared files

Classification only settles the side files. The edited files themselves have more than one writer:

- Claude `settings.json` (layer `settings`): Corgi adds its `hooks` entries; brainstorm 003 plans for aprofile to write `env`, `apiKeyHelper` and `model` there. At share level `all` the file is a link to `~/.claude/settings.json`, which Corgi edits in place.
- Codex `config.toml` (layer `owned`, patched by aprofile): Corgi changes the TUI title setting there, and aprofile's config patches rewrite the file.

Corgi guards its own edits: it takes `flock` on `.corgi-hooks.lock`, rereads the file, refuses with `HookSettingsChanged` if the file changed under it, and writes atomically. aprofile could take the same lock when it writes these files, so the two tools never interleave.

## Open Questions

- A, B or C? A for `auth.json.lock` seems clear regardless.
- Should aprofile take `.corgi-hooks.lock` when writing `settings.json` or `config.toml`, and is depending on that file name acceptable under FR-032?
- Does `aprofile sync` rewrite a patched `config.toml` in a way that drops Corgi's title setting, and does Corgi then fight back on its next start?
- `aprofile remove --purge` deletes the folder with Corgi's files. Should it warn that Corgi still lists the folder as an account (`corgi account remove` forgets it)?
- Should `doctor` know about companion tools, for example to report a folder with Corgi hooks that Corgi no longer lists?
