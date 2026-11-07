# Data Model: Profile Share Levels and Hook Sets

**Feature**: 002-profile-isolation-levels. Builds on slice one's data model; only changes and
additions are listed.

## Profile (store file `<config>/aprofile/profiles/<harness>/<name>.yaml`)

| Field | Type | Rules |
|-------|------|-------|
| `share` | `all \| content \| none` | Optional in the file. Absent means `all` for files written before this feature (FR-033); `add`, `copy`, `import` always write it. Loading never rewrites the file, and saving keeps an absent level absent until the user sets one with `--share` (`ShareImplicit`). |
| `hook_sets` | list of set names | The commands that attach sets (`add`, `edit`, `copy`, TUI) require each name to exist in the library with a body for the harness and refuse sets for `share: all` (FR-025, FR-027). Order is irrelevant; stored sorted. |
| `exclude_hook_sets` | list of set names | Only meaningful at `content`; names of `every_profile` sets to skip (FR-026). |

Validation runs on load and save (slice one `Validate`) and checks the share level and the name
format of the set lists only, so a deleted set or a hand-edited file never hides the profile (spec
Edge Cases). A named set that is missing from the library is reported by `sync`, `show` (reason
`missing`) and `doctor` (problem); sets named by a hand-edited `share: all` profile are ignored and
reported by `doctor` as information.

## Global settings (`<config>/aprofile/settings.yaml`)

| Field | Type | Rules |
|-------|------|-------|
| `default_share` | `all \| content \| none` | Default `content` (FR-007). |

## Harness definition additions

See contracts/harness-definition.md: borrowable layers (`content`, `settings`, `plugin-store`),
`share_policy`, `hooks {location, form}`, `home_escapes`, companion expansion, keyed
classification overrides.

## Hook set (`<config>/aprofile/hooks.d/<name>.yaml`)

| Field | Type | Rules |
|-------|------|-------|
| `name` | string | Matches the file name; profile-name regex. |
| `description` | string | Optional, one line. |
| `every_profile` | bool | Default false. Applies to `content` profiles of covered harnesses unless excluded; never to `none` (FR-026). |
| `bodies` | map harness name to body | Body shape follows the harness's `hooks.form`: `event-groups` = `{Event: [group...]}`; `files` = `{files: [relative paths]}` resolved under `hooks.d/files/<name>/`. |

Validation: the body shape matches the form; referenced files exist in the library (a missing
script referenced by a command is a doctor problem, not a load error); literals pass the secret
prefix check.

Applicability of set S to profile P:

```text
applies(S, P) = S has body for P.harness
            and P.share != all
            and ( S.name in P.hook_sets
                  or (S.every_profile and P.share == content and S.name not in P.exclude_hook_sets) )
```

## Managed hook record (`<data>/aprofile/managed-hooks/<harness>/<name>.json`)

```json
{
  "format_version": 1,
  "entries": [
    { "set": "jira", "form": "event-groups", "event": "PostToolUse", "fingerprint": "sha256:..." },
    { "set": "rtk",  "form": "files", "path": "plugins/rtk.ts", "target": "<config>/aprofile/hooks.d/files/rtk/rtk.ts" }
  ]
}
```

States of a recorded entry during `sync`:

| Found in profile | State | Action |
|------------------|-------|--------|
| unchanged (fingerprint or link target matches) | managed | update if the set changed, remove if no longer applies |
| missing or changed | diverged | leave as is, report; `hook-sets reapply` replaces after backup |

Entries not in the record are never touched (FR-028).

## Entry treatment (computed, shown by `show`)

| Field | Values |
|-------|--------|
| `entry` | path relative to the profile config dir |
| `category` | `credentials`, `state`, `owned`, `mixed`, `content`, `settings`, `plugin-store`, `hook-location`, `unclassified`, `home-escape` |
| `treatment` | `private`, `linked`, `own-copy`, `managed-by-hook-set`, `unclassified`, `home-escape` |
| `reason` | plain-language sentence from a fixed catalog, e.g. "runtime state: always private" |
| `status` | `ok`, `missing-link-target` (lazy: the default setup has none yet; `show` lists these apart from linked entries), `not-synced` (the next sync links or unlinks it), `link-replaced-by-file`, `leaking-link`, `differs-from-default`, `kept-from-upgrade` (a profile without a stored level keeps its own entry where `all` would link one; information, FR-033) |

`show --json` (top-level `format_version`, slice one's contracts/cli.md) adds to slice one's
`profileJSON`: `share`, `share_description`, `entries[]` (above),
`hook_sets[] {name, reason: named|every-profile|excluded|missing|failed-to-load}` (`missing`: a
named set absent from the library; `failed-to-load`: a named set whose file exists but cannot be
parsed), `settings_differ` (bool),
`home_escapes[]`, `unborrowed_default_entries[]` (FR-001a).

## Change plan (computed; `--dry-run`, `edit --share`, `settings pull`, `sync`)

| Field | Values |
|-------|--------|
| `kind` | `link`, `unlink`, `mkdir`, `copy-once`, `create` (minimal settings entry), `seed-mixed`, `replace-with-link`, `fix-link` (sync --fix-links), `merge-settings`, `replace-settings`, `place-hook`, `remove-hook`, `report` |
| `path` | profile-relative path |
| `source` | default-setup path or library path, when relevant |
| `confirm` | true when the step replaces profile data (backup first) |
| `backup` | backup path, filled when applied |
| `note` | reason shown to the user |

Level transitions (applied by `edit --share`; `sync` runs only steps with `confirm: false`):

| From to | Content | Settings | Plugin store | Hook location |
|---------|---------|----------|--------------|---------------|
| all to content | keep links | `unlink` + `copy-once` (hooks excluded; dry run lists default hooks and suggests `hook-sets import`) | keep link | `unlink` (separate file) or key dropped from the copy; then `place-hook` |
| content to none | `unlink` (own entries kept) | keep own | `unlink`, `mkdir` | `remove-hook` for every-profile sets not named |
| none to content | `link` where no real entry exists; an empty own directory (as `none` creates for skills) becomes the link; other real entries kept and reported as `link-replaced-by-file` | keep own | `link` if absent; an empty own dir becomes the link; a non-empty one is kept and reported | `place-hook` for every-profile sets |
| content or none to all | `link` as above | `replace-with-link` (confirm) | `link` | `replace-with-link` (confirm); record cleared |

## Doctor finding (changed)

| Field | Values |
|-------|--------|
| `profile` | profile id or empty |
| `severity` | `problem` or `info` (new) |
| `message`, `fix` | as slice one |

Exit status counts only `problem` findings. New problem checks: link replaced by real file,
diverged managed hook, missing referenced set, missing hook script, deprecated `shared` alias in a
user definition, user catch-all rule. New info checks: settings differ from default (content),
`$HOME` escapes, unborrowed unclassified default entries.

## Registry entry (changed)

`share` added (string). `format_version` stays 1.
