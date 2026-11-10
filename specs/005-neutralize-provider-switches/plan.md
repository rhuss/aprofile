# Implementation Plan: Neutralize Provider Switches for Every Profile Folder

**Branch**: `005-neutralize-provider-switches` | **Date**: 2026-11-10 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/005-neutralize-provider-switches/spec.md`

## Summary

Widen spec 004 FR-006a: every Claude profile whose settings file is its own gets empty `env`
entries for the auth-clearing list, whatever its auth kind, minus the names the profile supplies
through the wrapper (now including a wrapper-carried key's own variable). Interactive-login
profiles at share level `all` are reported as not self-contained, with `auth-clearing` as a
wrapper-only item in `show` and an information finding in `doctor`. Codex was verified to ignore
exported `OPENAI_API_KEY` and `OPENAI_BASE_URL` for login profiles (research R2), so Codex is
unchanged apart from a definition comment.

## Technical Context

**Language/Version**: Go 1.24 (module), built with the local Go toolchain

**Primary Dependencies**: existing packages only: `internal/launch` (carrier decision, planner,
status), `internal/doctor`, `internal/cli`, `internal/harness` (definitions)

**Storage**: unchanged placed-entry record in aprofile's data directory (spec 004)

**Testing**: `go test ./...` (`make test`); unit tests in `internal/launch`, `internal/cli`,
`internal/doctor`; integration tests in `tests/integration` with a throwaway `HOME`

**Target Platform**: macOS and Linux CLI

**Project Type**: CLI

**Performance Goals**: n/a (a handful of extra JSON entries per profile)

**Constraints**: no values in output (Principle IV); idempotent `sync` (Principle II); wrapper
output unchanged (Principle V)

**Scale/Scope**: about 22 names on Claude's auth-clearing list; one planner rule, one report item

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Check | Result |
|---|---|---|
| I. Harness knowledge is data | The list comes from the definition (`auth_kinds`, `auth_clears`); no names hard-coded in Go | Pass |
| II. Never fight the harness | Writes only placed `env` entries in the profile's own `settings.json`, recorded per profile, re-read before write; never into the linked default file; idempotent | Pass: neutralizing entries are launch entries aprofile places, as spec 004 FR-006a already treats them |
| III. Isolation by construction | At `all`, the linked file carries nothing (unchanged); the gap is reported in plain language | Pass |
| IV. No secrets | Entries are empty strings; reports name variables only | Pass |
| V. Transparent wrappers, pull-only | Wrapper rendering untouched (`DecideWith` unchanged); no change in Corgi required | Pass |
| VI. Scriptable first | Additive `show --json` item, `doctor --json` finding; no format version change | Pass |
| VII. Tests never touch the real home | New tests use `testutil` temp homes | Pass |

Post-design re-check: unchanged, all pass. No constitution amendment needed; an optional PATCH
naming "neutralizing entries" in Principles II and III is left out to keep scope tight.

## Project Structure

### Documentation (this feature)

```text
specs/005-neutralize-provider-switches/
├── spec.md
├── plan.md
├── research.md        # R1 Claude empty entries, R2 Codex, R3 planner rule, R4 report item
├── data-model.md
├── quickstart.md
├── contracts/cli.md   # show, show --json, doctor
└── tasks.md           # /speckit-tasks
```

### Source Code (repository root)

```text
internal/launch/
├── plan.go            # desiredState: neutralize whenever the folder can carry env; supplied names include wrapper-carried key vars
├── carrier.go         # expose the "folder can carry env" decision (envCarrier) to the planner and status
├── apply.go           # Status: compute the share-level-all login gap
├── report.go          # Folder(): append the auth-clearing wrapper-only item; SettingName/ReasonText for it
├── plan_test.go       # login, oauth-token, wrapper-key, extra-var, all, idempotence cases
└── apply_test.go      # folder status and wrapper-only item
internal/doctor/doctor.go       # launchFindings: special fix text for auth-clearing
internal/cli/list_show.go       # show text: nothing new beyond the wrapper-only line (verify)
internal/harness/builtin/codex.yaml  # comment recording research R2
tests/integration/              # launch tests for login profiles at content/none/all
specs/004-corgi-integration/spec.md  # amendment notes on FR-001, FR-006a, FR-020
docs/guide/docs/user/corgi.md   # widened neutralization, the all-level gap
README.md                       # short note where folder launches are described
```

**Structure Decision**: single Go module; the change stays inside `internal/launch` with a small
reporting hook in `internal/doctor`, following spec 004's layout.

## Design Notes

1. **Planner rule (FR-001, FR-002, FR-004).** `PlanWith` passes `canCarryEnv :=
   envCarrier(p, def, doc) == ""` into `desiredState`. The neutralizing block runs when
   `canCarryEnv` (it implies a folder `env` location; `def.FolderLaunch` may be nil when an
   override removed it), no longer only when a key is folder-carried. Supplied names are
   the variables of `IsVariable()` settings plus the variable of each `KindKey` setting whose
   carrier is `CarrierWrapper`. The early `return` for `key == nil` moves below the neutralizing
   block. Because the env file is not linked whenever entries are desired, the planner's
   `fd.Linked` branch and `unplaced` keep today's behavior at `all`.
2. **Report item (FR-005, FR-006, FR-005a).** `Status` sets a `StatusReport` field (for example
   `AuthClearing Reason`) to `share-all` when `def.FolderLaunch.Env != ""`, the env carrier reason
   is `share-all`, the auth kind's `VarNames()` is empty and `def.UnsetAuthVars()` is not empty.
   `Folder()` appends `WrapperOnly{Setting: "auth-clearing", Reason: share-all}`. `SettingName()`
   returns `auth-clearing`. Status of folders with only neutralizing entries stays
   `self-contained`, because those entries are not settings.
3. **Doctor (FR-005).** `launchFindings` keeps its loop; for the `auth-clearing` item the fix is
   "start the profile through its wrapper <wrapper>, or give it its own settings: aprofile edit
   <id> --share content". Severity stays info.
4. **Codex (FR-008, FR-009).** Comment in `codex.yaml` with the R2 result; no code change.
5. **Docs (FR-010).** Amendment notes in spec 004; Corgi guide section on what the folder
   neutralizes and the `all` gap; README note if it describes folder launches.
6. **Migration.** None needed: the next `sync` places the entries; until then the folder is stale
   (spec 004 FR-020a). Existing tests asserting that login folders hold no entries are updated.

## Complexity Tracking

No constitution violations.
