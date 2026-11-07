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
