# CLI Contract Changes

Additive only; no format version changes.

## `aprofile show <id>` (text)

For an interactive-login Claude profile at share level `all`:

```text
  folder:         not self-contained
                  wrapper-only: auth-clearing (share level all: settings.json is linked)
```

When the profile also has other wrapper-only settings (for example `model`), all items are listed
as today, `auth-clearing` among them.

## `aprofile show <id> --json`

`folder.wrapper_only` gains an item; `folder.status` follows the existing rule:

```json
"folder": {
  "status": "not-self-contained",
  "wrapper_only": [
    {"setting": "auth-clearing", "variable": "", "reason": "share-all"}
  ]
}
```

## `aprofile doctor`

Information finding (exit status unaffected), next to the existing "profile folder: ..." line:

```text
info  claude/private  auth-clearing is wrapper-only: share level all: settings.json is linked
      fix: start the profile through its wrapper claude-private, or give it its own settings: aprofile edit claude/private --share content
```

`doctor --json` carries the same finding with `severity: "info"`.

## `aprofile sync` / `sync --dry-run`

No new output shapes. For `content` and `none` profiles without a folder key, the first `sync`
after the upgrade lists the new `env.<NAME>` entries as additions, and names any user entry it
replaces (spec 004 FR-004). `list` is unchanged apart from its existing stale marker until that
`sync`.
