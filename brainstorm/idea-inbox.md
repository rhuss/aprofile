# Idea Inbox

Out-of-scope ideas captured from reviews and conversations, waiting for a brainstorm.

### remove-cc-deck-import
- **Source**: conversation
- **Date**: 2026-11-04
- **Reference**: 002-profile-isolation-levels (clarify session)
- **Summary**: Remove `aprofile import cc-deck` (slice one FR-045, about 700 lines in
  `internal/importer/` and `internal/cli/import.go`). There is a single cc-deck installation,
  which can be ported by hand with `aprofile add <harness> <name> --adopt --share <level>`.
  Removal touches the code and tests, retires FR-045 and the `import --overwrite*` exceptions
  (constitution Principle II, slice one FR-019), the "one-time cc-deck import" scope line in the
  constitution's Technical Constraints, and the README.
> Spec 002 keeps `import` working and only records that it picks a share level the same way `add`
> does (global default or `--share`), so the cleanup can happen independently.

### intermediate-symlink-checks

- **Source**: deep-review
- **Date**: 2026-11-04
- **Reference**: 002-profile-isolation-levels
- **Summary**: Link checks look only at the final path component; a symlinked intermediate directory (for example a hand-linked `config/opencode`) is not detected.

> Writes beneath such a directory would reach the default setup; a helper that checks every component between the config dir and the entry would close this.

### shared-treatment-decision-table

- **Source**: deep-review
- **Date**: 2026-11-04
- **Reference**: 002-profile-isolation-levels
- **Summary**: `layer.Treatments` reimplements the planner's per-category decision table instead of sharing one function.

> Two copies of the table are the most likely place for `show`, dry runs and `sync` to drift apart (SC-006).

### split-event-groups-planner

- **Source**: deep-review
- **Date**: 2026-11-04
- **Reference**: 002-profile-isolation-levels
- **Summary**: `planEventGroups` holds several record-state transitions in one long function.

> Splitting it into classify, resolve divergence and assemble steps would make each transition testable on its own.

### importer-external-credential-paths

- **Source**: deep-review
- **Date**: 2026-11-06
- **Reference**: 001-aprofile-standalone-profile-cli
- **Summary**: `import cc-deck` copies any absolute credential file that cc-deck's config names, from any location, into a private copy.

> This matches cc-deck's own behavior, and the trust boundary is the user's config. Listing files outside cc-deck's own folders in `--dry-run` would make that visible.

### flag-value-drop-keeps-server

- **Source**: deep-review
- **Date**: 2026-11-06
- **Reference**: 001-aprofile-standalone-profile-cli
- **Summary**: When an MCP conversion drops a whole `--flag=value` argument, the server is still written and starts without that option.

> It is reported in the drop list, but a server that loses a credential flag may fail at runtime; a stronger signal (or a per-server skip option) could help.

### sync-summary-with-problems

- **Source**: deep-review
- **Date**: 2026-11-06
- **Reference**: 001-aprofile-standalone-profile-cli
- **Summary**: sync's summary says "0 changes" next to "completed with problems".

> The summary line could count problems too, so a run with a missing hook doesn't read as fully up to date.

### history-store-permissions

- **Source**: deep-review
- **Date**: 2026-11-07
- **Reference**: 003-history-groups
- **Summary**: Group stores and set-aside dirs are 0755 like profile dirs, yet they collect transcripts of several profiles.

> Tighten to 0700 together with profile dirs if those ever change.

### former-history-link-record

- **Source**: deep-review
- **Date**: 2026-11-07
- **Reference**: 003-history-groups
- **Summary**: Nothing records which links aprofile created as history links, so a home member's former history entry reclassified to a private category cannot be told apart from a hand-made leak.

> A small per-profile record of history links would let sync detach former history links safely without weakening the spec 002 --fix-links contract.

### join-removes-emptied-tree-principle

- **Source**: deep-review
- **Date**: 2026-11-07
- **Reference**: 003-history-groups
- **Summary**: Constitution Principle II does not name the join's removal of an emptied entry tree explicitly.

> Consider naming it in the next constitution amendment.

### neutralize-anthropic-base-url

- **Source**: deep-review
- **Date**: 2026-11-10
- **Reference**: 005-neutralize-provider-switches
- **Summary**: Folder launches do not neutralize `ANTHROPIC_BASE_URL` (in `launch_clear`, not the auth-clearing list), so an exported base URL can redirect a login profile's OAuth bearer.

> Same threat class as the Vertex incident, with a credential leak instead of re-billing; spec 005 FR-001 scopes only the auth-clearing list.

### settings-pull-ignore-placed-entries

- **Source**: deep-review
- **Date**: 2026-11-10
- **Reference**: 005-neutralize-provider-switches
- **Summary**: `settings pull --replace` lists "removed: env" for every Claude profile at content because the diff ignores the launch record.

> Never reaches "nothing to change"; prompt and backup on every run; short window without neutralizing entries.

### empty-vs-unset-child-env

- **Source**: deep-review
- **Date**: 2026-11-10
- **Reference**: 005-neutralize-provider-switches
- **Summary**: Child processes of a folder launch see the auth-clearing names set to empty rather than unset.

> Tools that distinguish empty from unset may behave differently; a troubleshooting note would help.

### launch-record-version-skew

- **Source**: deep-review
- **Date**: 2026-11-10
- **Reference**: 005-neutralize-provider-switches
- **Summary**: An older aprofile binary's sync removes the neutralizing entries and a newer one re-adds them.

> The record format version did not change, so mixed binaries flip the folder between protected and unprotected.

### replaced-values-may-hold-secrets

- **Source**: deep-review
- **Date**: 2026-11-13
- **Reference**: 006-profile-features
- **Summary**: Capturing a hand-written Codex exporter subtree in the launch record's Replaced field can store authentication headers in a 0644 file.

> Spec 006's FR-012 mandates replace-and-restore and its Assumptions defer secret-bearing params, so this is a recorded tension; revisit when an OTLP-headers parameter lands (refuse secret-bearing subtrees, or store recovery data outside the record).

### endpoint-url-embedded-credentials

- **Source**: deep-review
- **Date**: 2026-11-13
- **Reference**: 006-profile-features
- **Summary**: The telemetry endpoint is a free string, so a URL with embedded user:pass credentials would land in the profile-private settings.json.

> V1 defers secret-bearing params; the ref/literal question the spec defers should also cover endpoint URLs when it is picked up.
