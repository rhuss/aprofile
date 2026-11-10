# Deep Review Findings

**Date:** 2026-11-10
**Branch:** 005-neutralize-provider-switches
**Rounds:** 1
**Gate Outcome:** PASS (0 unambiguous Critical/Important remaining; 1 Important left open as ambiguous, see FINDING-2)
**Invocation:** quality-gate (ship pipeline, ask=smart)

## Summary

| Severity | Found | Fixed | Remaining |
|----------|-------|-------|-----------|
| Critical | 0 | 0 | 0 |
| Important | 2 | 1 | 1 (ambiguous) |
| Minor | 12 | 6 | 6 |
| Notable | 6 | - | 6 |
| **Total** | **20** | **7** | **13** |

Severity of FINDING-1 was raised from Minor to Important at merge time because it breaks a spec 005 edge case ("Changing to `all` drops the record").

**Agents completed:** 5/5 internal (Goal Alignment skipped: no PR), Codex completed (0 findings), CodeRabbit failed (authentication), Copilot skipped (disabled in config).

## Findings

### FINDING-1
- **Severity:** Important
- **File:** internal/cli/edit.go:476-487
- **Category:** correctness
- **Source:** correctness-agent
- **Resolution:** fixed (round 1, commit 60dd564)

**What is wrong:** `launchLevelEffects` returned early when `launch.Decide` yields no settings. A plain `oauth-login` profile has no launch settings, but after FR-001 it carries recorded neutralizing entries. So `edit --share all` neither dropped its launch record nor printed the level-change lines.

**Why this matters:** Spec 005 edge case: "Changing to `all` drops the record". With the stale record, going back to `content` before a `sync` could label an overwrite of a user entry as `restore` instead of `replace-user`.

**How it was resolved:** The gate now also counts a definition with a folder env location and a non-empty auth-clearing list. New integration test `TestLevelChangeLoginNeutralizing` fails without the fix and passes with it.

### FINDING-2
- **Severity:** Important
- **File:** internal/launch/plan.go:191-211, internal/sync/sync.go:584-589
- **Category:** production-readiness
- **Source:** production-readiness-agent (related: correctness-agent Notable on lost replaced values)
- **Resolution:** remaining, ambiguous (design/requirement interpretation)

**What is wrong:** On upgrade, a `content` Claude profile whose once-copied `settings.json` holds a provider switch (for example Vertex from the default setup) is moved to the default provider by the first `sync` (replace-user step). Before the sync, the spec 004 FR-020b "sets env X, which this profile does not set" problem disappears (the name is now wanted), so only "folder stale" is reported. The replace line is one among ~25 `set env` lines, and the replaced value is not recorded or backed up.

**Why this matters:** Provider choice decides billing and data routing; the change is easy to miss.

**Why not auto-fixed:** The spec edge case explicitly accepts the replacement ("The profile's empty entry replaces it, `sync` names the entry ... not restored later"), and `sync` does name it. Making it louder (separate warning, backup, doctor preview, upgrade note) is a design decision for the author.

### FINDING-3 (user-hinted open item)
- **Severity:** Minor
- **File:** internal/cli/settings.go:175-258
- **Category:** production-readiness
- **Source:** production-readiness-agent (also: architecture, security, correctness, test-quality)
- **Resolution:** remaining, ambiguous (deferral candidate)

**What is wrong:** `planSettingsPull` diffs the default `settings.json` against the profile's without ignoring entries recorded in the launch record. Every Claude profile at `content` (pull is refused at `none`) now has a placed `env` block, so `settings pull --replace` always lists `removed: env`, prompts, backs up and rewrites, and `launch.Apply` then places the entries again.

**Why this matters:** No data loss and no FR broken (the end state is correct and spec 004 FR-010 holds), but the command never reaches "nothing to change", its listing is misleading, and there is a short window (or, if `launch.Apply` fails, a persistent state until `sync`) where the folder lacks the neutralizing entries. Spec 004 had this for key-carrying folders only; spec 005 FR-001 widens it to nearly every Claude profile.

**Recommendation:** Fix in a follow-up: load the launch record in `planSettingsPull` and leave recorded `env.<NAME>` locations out of the diff (or plan the launch entries in memory and write the combined result once, skipping when equal). Until then, record it as a known limitation in spec 005. A test now asserts that the entries are back after a real pull.

### FINDING-4
- **Severity:** Minor
- **File:** internal/launch/carrier.go:163-168, internal/launch/plan.go:167-171
- **Category:** architecture
- **Resolution:** fixed (commit 649d8d3)

`canCarryEnv` claimed "The planner and Status share this rule" (Status uses `envCarrier` via `authClearingGap`), and `desiredState`'s parameter shadowed the function. Comment corrected, parameter renamed to `envOK`.

### FINDING-5
- **Severity:** Minor
- **File:** internal/launch/report.go:27-28, 76-77
- **Category:** architecture
- **Resolution:** fixed (commit 649d8d3)

`WrapperOnly` and `SettingName` docs did not mention the `auth-clearing` item. Docs updated.

### FINDING-6
- **Severity:** Minor
- **File:** internal/doctor/doctor.go:651-661
- **Category:** architecture
- **Resolution:** fixed partially (commit 649d8d3)

The special doctor fix (`--share content`) keyed on the setting only; it now also requires reason `share-all`. The message wording itself ("auth-clearing is wrapper-only: share level all: settings.json is linked") does not say a folder launch inherits exported switches and keys; left as is (wording is a spec-level choice; FR-005 wording is met).

### FINDING-7
- **Severity:** Minor
- **File:** tests/integration/review_gaps_test.go:92-101
- **Category:** test-quality
- **Resolution:** fixed (commit 649d8d3)

`TestHandEditedLevel` weakened from an exact-content check to `m["own"] == true`. Now asserts exactly `own` plus a neutralizing-only `env`.

### FINDING-8
- **Severity:** Minor
- **File:** tests/integration/review_gaps_test.go:128-153
- **Category:** test-quality
- **Resolution:** fixed (commit 649d8d3)

No direct check that `settings pull --replace` places the neutralizing entries again. Assertion added.

### FINDING-9
- **Severity:** Minor
- **File:** internal/launch/plan_test.go
- **Category:** test-quality
- **Resolution:** remaining

No unit test toggles an already placed listed name through an extra variable (`""` to `"1"` and back), nor auth kind `oauth-token` to `oauth-login` (entry placed again). Code paths were checked by reading and behave correctly.

### FINDING-10
- **Severity:** Minor
- **File:** tests/integration/launch_claude_test.go:303-349
- **Category:** test-quality
- **Resolution:** remaining

US1 scenario 4 (hand-deleted entry reported stale by `list`, `show`, `doctor`) and FR-005a `show` output are covered at unit level only.

### FINDING-11
- **Severity:** Minor
- **File:** internal/sync/sync.go:584-589
- **Category:** production-readiness
- **Resolution:** remaining

The first `sync` prints one line per neutralizing entry (~25 per Claude profile), hiding the few lines that need attention. Suggest collapsing set/restore of neutralizing entries into one line per profile in text output.

### FINDING-12
- **Severity:** Minor
- **File:** internal/launch/plan.go:129-139
- **Category:** production-readiness
- **Resolution:** remaining (spec-accepted)

An unparseable `settings.json` now makes `sync` fail for every login profile (previously a note), holding back an unrelated wrapper update. The spec edge case accepts this; the error text "cannot place launch settings" could name the neutralizing entries.

### FINDING-13
- **Severity:** Minor
- **File:** internal/launch/apply.go:40-56
- **Category:** architecture
- **Resolution:** remaining

`authClearingGap` repeats the folder-env guard that `envCarrier` already does and lives in apply.go instead of carrier.go. Cosmetic.

## Notable Observations

### NOTABLE-1
- **File:** internal/launch/plan.go:191-211
- **Category:** security
- **Description:** `ANTHROPIC_BASE_URL` (in `launch_clear`, not the auth-clearing list) is not neutralized. A login profile at `content` launched from the folder alone with an exported base URL could send its OAuth bearer to that endpoint. Outside spec 005 scope (FR-001 names the auth-clearing list); worth a follow-up spec or a RUNBOOK check.

### NOTABLE-2
- **File:** internal/launch/plan.go:195-211
- **Category:** production-readiness
- **Description:** Child processes of a folder launch see ~25 names set to empty instead of unset (spec Assumptions). A troubleshooting note in the Corgi guide would help.

### NOTABLE-3
- **File:** internal/launch/plan.go:191-211
- **Category:** production-readiness
- **Description:** An older aprofile binary's `sync` removes the neutralizing entries and the new one re-adds them (record format version unchanged). Release note or version bump.

### NOTABLE-4
- **File:** internal/launch/plan.go:298-311
- **Category:** correctness
- **Description:** JSON env replace-user steps record no replaced value; overwritten user switches cannot be recovered (relates to FINDING-2).

### NOTABLE-5
- **File:** internal/cli/settings.go
- **Category:** test-quality
- **Description:** `TestSettingsPullApplies` now asserts the `removed: env` listing, locking in the FINDING-3 behavior; update it when FINDING-3 is fixed.

### NOTABLE-6
- **File:** tests/integration/launch_report_test.go
- **Category:** test-quality
- **Description:** US3 scenario 3 for `oauth-token` at `all` is covered at unit level only; manual-only items are FR-004a/SC-001 real login and US4 Codex verification (recorded in research.md and RUNBOOK.md).

## Post-Fix Spec Coverage

The fix round removed no implementation code (only a widened guard and comment edits). All FR-001 to FR-010 remain implemented; see REVIEW-CODE.md compliance matrix.

## Test Suite Results

| Round | Test Command | Exit Code | Failures | Status |
|-------|-------------|-----------|----------|--------|
| baseline | GOFLAGS=-count=1 make test | 0 | 0 | passed |
| 1 | GOFLAGS=-count=1 make test | 0 | 0 | passed |

`go vet ./...` clean, `gofmt -l` empty.

## External Tools

- CodeRabbit: failed (`--files` option unsupported by the installed CLI; retry with `--committed --base main` failed authentication: run `coderabbit auth login`).
- Codex (`codex review --base main`): completed, no actionable findings.
- Copilot: skipped (disabled in config).
