# Deep Review Findings

**Date:** 2026-11-10
**Branch:** 004-corgi-integration
**Rounds:** 2 fix rounds (each followed by a narrowed re-review)
**Gate Outcome:** PASS
**Invocation:** quality-gate (ship pipeline, review-code stage)

## Summary

| Severity | Found | Fixed | Remaining |
|----------|-------|-------|-----------|
| Critical | 0 | 0 | 0 |
| Important | 13 | 13 | 0 |
| Minor | 31 | 6 | 25 |
| Notable | 0 | - | 0 |
| **Total** | **44** | **19** | **25** |

**Agents completed:** 5/6 (+ 2 external tools: CodeRabbit, Codex)
**Agents skipped:** Goal Alignment (no PR found for the branch)
**External tools:** CodeRabbit completed (1 finding, merged into FINDING-1; re-reviews of both fix rounds: 0 findings). Codex completed (3 findings). Copilot disabled in config.

Fix commits: f2808ce, 4e2bfe6, b3cebe9, 5621836, 97b246f, 78ec0b9 (round 1); 32076e7, 0a5dc37, 601f7e3 (round 2).

## Findings

### FINDING-1
- **Severity:** Important
- **Confidence:** 88
- **File:** internal/launch/apply.go:55-78 (root cause internal/launch/doc.go:95-103)
- **Category:** correctness
- **Source:** correctness-agent (also reported by: coderabbit)
- **Round found:** 1
- **Resolution:** fixed (round 1, f2808ce)

**What is wrong:**
`PlanWith` adds the files the launch record lists so that placed entries are removed after a harnesses.d override drops or moves `folder_launch`, but `Apply` and `Status` built the Doc with `ReadDoc(def, configDir)`, which reads only the files the definition names. A record-only file came back as a zero `FileDoc`, so the plan emitted remove steps without changing the file, and the record was deleted.

**Why this matters:**
The placed `env` entries and `apiKeyHelper` stayed in `settings.json` (and beat the wrapper's values), the Codex `env_key` was never restored, and aprofile lost track of entries it owned (FR-005, FR-009).

**How it was resolved:**
New `ReadDocFor(def, configDir, rec)` reads the definition's files plus every safe relative file the record lists; `Apply` (both reads) and `Status` use it. Regression test `TestApplyRemovesEntriesWhenDefinitionDropsFolderLaunch` goes through the real read path.

**External tool analysis (CodeRabbit):**
> Read the files the record lists, not only the files the definition names. With `folder_launch: null`, `ReadDoc` returns an empty `Files` map ... sync prints `removed env ANTHROPIC_MODEL` but `settings.json` keeps the old entries; the record is gone, so aprofile no longer owns these entries.

### FINDING-2
- **Severity:** Important
- **Confidence:** 85
- **File:** internal/launch/carrier.go:94-107
- **Category:** correctness
- **Source:** correctness-agent
- **Round found:** 1
- **Resolution:** fixed (round 1, 4e2bfe6)

**What is wrong:**
`DecideWith` emitted the model, endpoint and extra variables without de-duplicating by name. A profile with `--model X` and `--env ANTHROPIC_MODEL=Y` produced two desired env entries for one variable; every plan restored one and then the other.

**Why this matters:**
Every `sync` rewrote `settings.json` and `list`/`show`/`doctor` always reported the folder as stale (FR-011, SC-005, FR-020a); `APROFILE_SET_VARS` listed the name twice.

**How it was resolved:**
`DecideWith` keeps one setting per variable; a later setting of the same variable (extra over model or endpoint, hook over extra) replaces the earlier one in place, matching the old wrapper's last-export-wins. Test `TestApplyExtraNamedLikeModelVarIsIdempotent`.

### FINDING-3
- **Severity:** Important
- **Confidence:** 80
- **File:** internal/launch/apply.go:82-107
- **Category:** production-readiness
- **Source:** production-agent (also reported by: correctness-agent, codex)
- **Round found:** 1
- **Resolution:** fixed (round 1, f2808ce)

**What is wrong:**
`Apply` wrote the harness files first and saved the record afterwards. A failed record save (or a failed second file write) left placed entries unrecorded; for Codex the `env_key` the auth block replaced was lost for good, because the next plan saw no `env_key` to capture.

**Why this matters:**
Removing the auth block later would leave the provider with neither `auth` nor `env_key` (FR-004, FR-009); JSON entries would be misreported as "replaced user entry".

**How it was resolved:**
Write-ahead record: before any harness file is written, `Apply` saves the old entries (updated from the plan) plus the new entries that replace a setting (round 2 narrowed this from a full union, see FINDING-16); after the writes it saves the final record. A partial failure self-heals on the next sync. Test `TestApplyRecordsReplacedSettingBeforeWriting` (read-only config dir).

**External tool analysis (Codex):**
> [P2] Roll back earlier writes when a later launch-file write fails: this loop may replace one file before a later write fails; it then returns without saving the new record. sync reports failure while leaving a partially updated, untracked profile folder.

### FINDING-4
- **Severity:** Important
- **Confidence:** 78
- **File:** internal/cli/edit.go:462-468
- **Category:** production-readiness
- **Source:** production-agent
- **Round found:** 1
- **Resolution:** fixed (round 1, b3cebe9)

**What is wrong:**
`changeShare` dropped the launch record entries after the level change was already applied to the config dir; an unreadable record made it return an error, so the profile store was never saved with the new level.

**Why this matters:**
A corrupt bookkeeping file turned a completed, irreversible share change into a half-applied state (FR-008a).

**How it was resolved:**
The record update failure is now a warning on stderr ("launch record not updated ... run: aprofile sync"); the planner never records linked files, so the next sync cleans the record anyway. Test `TestLevelChangeCorruptLaunchRecordKeepsLevel`.

### FINDING-5
- **Severity:** Important
- **Confidence:** 80
- **File:** internal/launch/carrier.go:201-219, internal/launch/plan.go:196-211
- **Category:** architecture
- **Source:** architecture-agent
- **Round found:** 1
- **Resolution:** fixed (round 1, 4e2bfe6)

**What is wrong:**
The FR-006a "supplied" set and the cleared-source key rule were re-derived from `p.ExtraEnv` and missed exec-hook variables, unlike the wrapper's model.

**Why this matters:**
A hook variable on the auth-clearing list got an empty folder entry, which beats the value the wrapper sets (Claude applies the env block over the launching environment).

**How it was resolved:**
Both now use the decided settings (every variable setting, hook variables included). Test `TestPlanHookEnvVariableNotNeutralized`.

### FINDING-6
- **Severity:** Important
- **Confidence:** 85
- **File:** internal/wrapper/wrapper.go:93-115, internal/wrapper/env.go:105-116
- **Category:** architecture
- **Source:** architecture-agent (related: codex)
- **Round found:** 1
- **Resolution:** fixed (round 1, 5621836)

**What is wrong:**
The "never clear from an inherited APROFILE_SET_VARS" rule was written twice (shell `case` list and Go `protectedFromInherited`).

**Why this matters:**
`aprofile login` (Environ) and the real wrapper could clear different variables after a one-sided change.

**How it was resolved:**
The protected names are computed once in the wrapper's environment model (`m.protected`); `renderCleanup` renders it and `Environ` tests against it.

### FINDING-7
- **Severity:** Important
- **Confidence:** 75
- **File:** internal/wrapper/wrapper.go:103
- **Category:** external
- **Source:** codex
- **Round found:** 1
- **Resolution:** fixed (round 1, 5621836)

**What is wrong:**
The cleanup skipped every `APROFILE_*` name, so a profile's own extra variable such as `APROFILE_TOKEN` (listed in its `APROFILE_SET_VARS`) leaked into another profile's launch.

**Why this matters:**
Violates FR-014 and SC-006 (no launch variable of one profile reaches another profile's harness); the value could be a resolved secret.

**How it was resolved:**
Only the exact `APROFILE_*` contract variables are protected now; the wrapper's own temporaries keep the `_aprofile_*` protection. `TestWrapperCleanupInherited` checks both the shell wrapper and `Environ`.

**External tool analysis (Codex):**
> [P1] Clear inherited user variables in reserved prefixes. When a profile supplies an extra variable such as `_aprofile_TOKEN` or `APROFILE_TOKEN`, its name is included in `APROFILE_SET_VARS`, but this exemption skips clearing it in both the generated wrapper and `Environ`.

### FINDING-8
- **Severity:** Important
- **Confidence:** 85
- **File:** tests/integration/launch_daily_test.go:12-134
- **Category:** test-quality
- **Source:** test-quality-agent
- **Round found:** 1
- **Resolution:** fixed (round 1, 97b246f), partially by design

**What is wrong:**
SC-001's test checked only file shape and the key command; it never checked the FR-006a neutralizing entries that make a folder-only launch from a dirty environment use the folder's key.

**How it was resolved:**
The Claude branch now emulates a folder-only launch from a hostile environment (exported key, endpoint, model, provider switches) with the verified harness facts, and asserts the neutralizers, endpoint and model. Running the real harnesses against a stub endpoint stays a manual walkthrough step, because tests never run real `claude`/`codex` binaries (Constitution Principle VII).

### FINDING-9
- **Severity:** Important
- **Confidence:** 80
- **File:** tests/integration/launch_leak_test.go:33-61
- **Category:** test-quality
- **Source:** test-quality-agent
- **Round found:** 1
- **Resolution:** fixed (round 1, 97b246f)

**What is wrong:**
SC-006's first-session environments were hand-written literals, and OpenCode pairs were skipped.

**How it was resolved:**
Each session is now built from the wrapper's real output plus the Claude folder's env block; the folder's `APROFILE_SET_VARS` is asserted; an `opencode` stub is installed so all ordered pairs run.

### FINDING-10
- **Severity:** Important
- **Confidence:** 80
- **File:** internal/launch/plan_test.go
- **Category:** test-quality
- **Source:** test-quality-agent
- **Round found:** 1
- **Resolution:** fixed (round 1, 97b246f)

**What is wrong:**
FR-005 branches for `apiKeyHelper` (replace-user, restore after edit or delete), the Codex `auth` block (replace-user, restore), a deleted extra variable, and an unplaced Codex auth block (FR-020b) had no tests.

**How it was resolved:**
Added `TestPlanKeyCommandReplaceUserAndRestore`, `TestPlanExtraVariableDeleted`, `TestPlanCodexAuthReplaceUserRestoreUnplaced`.

### FINDING-11
- **Severity:** Important
- **Confidence:** 75
- **File:** tests/integration/launch_report_test.go:53-165
- **Category:** test-quality
- **Source:** test-quality-agent
- **Round found:** 1
- **Resolution:** fixed (round 1, 97b246f)

**What is wrong:**
FR-020 "information, not a problem" was not asserted; the `no-provider` and `no-support` reasons and doctor's env-dependent / cleared-source notes were untested.

**How it was resolved:**
Added `codex/noprov` and `opencode/oc` fixtures, their reasons, doctor info findings for envkey/cleared/noprov/oc, and an assertion that no folder information is reported as a problem.

### FINDING-12
- **Severity:** Important
- **Confidence:** 75
- **File:** tests/integration/settings_pull_test.go:64-91
- **Category:** test-quality
- **Source:** test-quality-agent
- **Round found:** 1
- **Resolution:** fixed (round 1, 97b246f)

**What is wrong:**
FR-010 was tested only with an oauth-login profile and a default setup without conflicting launch entries.

**How it was resolved:**
`TestSettingsPullRestoresKeyCommandOverDefault`: api-key profile, default setup with its own `apiKeyHelper` and endpoint; after merge and replace pulls the profile's key command, endpoint, model and neutralizer win, and a merge keeps the user's `MY_TRACE`.

### FINDING-13
- **Severity:** Minor
- **Confidence:** 75
- **File:** tests/integration/level_change_test.go:291-336
- **Category:** test-quality
- **Source:** test-quality-agent
- **Round found:** 1
- **Resolution:** fixed (round 1, b3cebe9)

**What is wrong / how resolved:**
After a change to `all`, the test now asserts `settings.json` is a link and the default setup's `settings.json` got no `apiKeyHelper` or launch env entries (FR-008).

### FINDING-14
- **Severity:** Minor
- **Confidence:** 85
- **File:** docs/guide/docs/user/corgi.md:54-63
- **Category:** architecture
- **Source:** architecture-agent
- **Round found:** 1
- **Resolution:** fixed (round 1, 78ec0b9)

**What is wrong / how resolved:**
The wrapper-only table claimed every setting at share level `all` stays with the wrapper, which is wrong for Codex (`config.toml` is never linked); exec-hook variables and harnesses without folder support were missing. Table corrected.

### FINDING-15
- **Severity:** Important
- **Confidence:** 88
- **File:** internal/launch/apply.go:40-43, 67-73, 159-170
- **Category:** correctness
- **Source:** re-review agent (round 1 re-review)
- **Round found:** 2
- **Resolution:** fixed (round 2, 32076e7)

**What is wrong:**
After round 1 made the key decision depend on exec-hook variables, Apply and Status still fetched the hook's env output only when the folder carried a launch variable. A profile whose key is `env:HOOK_KEY` (set by the hook) and that has no folder variable was decided folder-carried by sync/show and wrapper-carried by the wrapper.

**Why this matters:**
Both carriers supplied the key (FR-012, FR-015), and show/doctor named the wrong carrier.

**How it was resolved:**
`HookEnv` runs whenever the definition declares `env` and `folder_launch`; Apply and Status always use it, the same condition the wrapper uses. Test `TestApplyStatusUseHookEnvForTheKey` with a hook script on a hermetic PATH.

### FINDING-16
- **Severity:** Minor
- **Confidence:** 78
- **File:** internal/launch/apply.go:96-131
- **Category:** correctness
- **Source:** re-review agent (round 1 re-review)
- **Round found:** 2
- **Resolution:** fixed (round 2, 32076e7)

**What is wrong / how resolved:**
The round 1 write-ahead record (full union) recorded replace-user entries before the write; after a failed write a later sync could remove the user's own value. `writeAhead` now adds only new entries that carry a replaced setting. Test `TestApplyFailedWriteKeepsUserEntryUnowned`.

### FINDING-17
- **Severity:** Minor
- **Confidence:** 80
- **File:** internal/launch/plan_test.go
- **Category:** test-quality
- **Source:** re-review agent (round 1 re-review)
- **Round found:** 2
- **Resolution:** fixed (round 2, 32076e7)

**What is wrong / how resolved:**
The round 1 test fed a hook variable on the auth-clearing list, which `RunEnvHook` rejects, so it covered an impossible input. Replaced by `TestDecideKeyFromHookVariable` and the Apply/Status-level hook test.

### FINDING-18
- **Severity:** Minor
- **Confidence:** 75
- **File:** internal/cli/edit.go:464-470
- **Category:** correctness
- **Source:** re-review agent (round 1 re-review)
- **Round found:** 2
- **Resolution:** fixed (round 2, 0a5dc37)

**What is wrong / how resolved:**
The share-change warning advised "run: aprofile sync", which keeps failing on an unreadable record. The warning now names the record to fix or remove; the test removes it and runs sync.

### FINDING-19
- **Severity:** Minor
- **Confidence:** 85
- **File:** internal/launch/apply.go:58-62
- **Category:** correctness
- **Source:** re-review agent (round 2 re-review)
- **Round found:** 3
- **Resolution:** fixed (601f7e3)

**What is wrong / how resolved:**
Apply's doc comment still described the full-union write-ahead record; corrected.

## Remaining Findings (all Minor)

| # | Source | File:line | Description | Suggested fix | Kind |
|---|--------|-----------|-------------|---------------|------|
| M1 | security | internal/wrapper/env.go:64-89, profile.go:187 | An extra variable named `PATH` or `HOME` (or `PATH` in a user `launch_clear`) is folder-carried and cleared by the wrapper, so the launch loses PATH/HOME | Reject reserved names (PATH, HOME, config_env, APROFILE_*, _aprofile_*) for extra env and launch_clear, or keep such extras wrapper-carried | judgment call (validation change) |
| M2 | security | internal/launch/plan.go:479-494, record.go:111 | A harnesses.d `replaces` list naming a literal-token key (e.g. `experimental_bearer_token`) would copy that token into the record (mode 0644) | Refuse secret-looking replaced values, validate `replaces` names, write the record 0600 | judgment call |
| M3 | codex | internal/launch/doc.go:108 | If the profile config dir itself is a symlink, launch files are treated as unlinked and written through it | Lstat the config dir and treat a symlinked root as linked or refuse | judgment call |
| M4 | codex | internal/wrapper/env.go | Extras named `_aprofile_*` are still protected from an inherited `APROFILE_SET_VARS` | Reject `_aprofile_` names for profile variables | judgment call (pathological names) |
| M5 | production | internal/cli/list_show.go:116-121, 227, 448 | `list`/`show` run the exec hook env operation 2-3 times per profile (only harnesses.d definitions with an env hook and folder_launch.env) | Compute hook env once per profile and pass it to wrapper and folder status | clear-cut refactor |
| M6 | production | internal/sync/sync.go:590-594 | The "comments are dropped" note for a re-encoded `config.toml` is shown only under `--dry-run` | Emit it on a real sync too, or back up before the first rewrite | clear-cut (spec requires only dry run) |
| M7 | production | list_show.go:119, record.go:144, doctor.go:643 | A corrupt launch record is reported inconsistently (list text silent, LoadPlaced returns nil, doctor drops info findings) | Show `folder unknown`, keep info findings, name the record path in the error | clear-cut |
| M8 | production | internal/config/atomic.go:26-38 | `WriteFileAtomic` does not fsync before rename | `tmp.Sync()` and fsync the directory | clear-cut |
| M9 | architecture | internal/cli/edit.go:475-510 | `launchLevelEffects`/`keyCommandFile` duplicate `launch.linked`/`launch.files` | Export `launch.LinkedAt` and `launch.Files` | clear-cut refactor |
| M10 | architecture | internal/launch/apply.go:40-48 | Hook context built twice (launch and wrapper) | One constructor | clear-cut refactor |
| M11 | architecture | plan.go:41, doc.go:45, definition.go:303 | Dead code: `Step.Mutating`, `FileDoc.Raw`, `Definition.FolderKeyFor` (prod) | Remove or use | clear-cut |
| M12 | architecture | internal/sync/sync.go:585-618 | `LaunchStepText` lives in sync; `sync --json` puts the location into `note` and drops the step note | `launch.Step.String()/Action()`; emit the note | clear-cut |
| M13 | architecture | report.go:118-123, list_show.go:489, sync.go:603 | Hardcoded `"env."` prefix instead of `FolderLaunch.EnvKey()` | Shared `EntryName` helper | clear-cut |
| M14 | architecture | internal/launch/report.go:128-131 | Linked-file doctor fix text differs from FR-020b wording ("remove the entry or set it on the profile") | Align the text or document the deviation | judgment call (wording) |
| M15 | architecture | internal/launch/keycmd.go:17-52 | FR-006 read script duplicated in two quoting styles | One template | clear-cut |
| M16 | architecture | definition.go:323-333, plan.go:661 | Two TOML string escapers | Share one | clear-cut |
| M17 | architecture | internal/wrapper/fit.go:69-73 | Sentinel `"model_arg"` passed as a variable name | Use `def.CarriesModel()` | clear-cut |
| M18 | correctness | internal/sync/sync.go:572-582 | On a launch Apply error, steps already applied are not printed or counted | Report applied steps before the problem | clear-cut |
| M19-M24 | test-quality | carrier_test.go:213, keycmd_test.go:39, launch_claude_test.go:232/99, launch_codex_test.go:74, canary_test.go:131 | Minor test gaps: no-support test can pass vacuously; key command tests lack absolute expected values; FR-009a only for Claude and without a healthy neighbor profile; Codex second-sync idempotence by mtimes; Claude dry-run lines not asserted; canary sweep skips state/cache dirs and the edit/pull/rename paths | Add the listed assertions | clear-cut |

| M25 | re-review (round 2) | internal/launch/apply.go:167-174, report.go:37-57 | When the exec hook's env operation fails, Status falls back to a decision without hook variables and show/doctor may print "self-contained" (rep.Err is not shown) | Report the folder status as unknown or print rep.Err in show/doctor when the hook fails | judgment call (adds a status value to the show --json contract) |

## Post-Fix Spec Coverage

The fix rounds removed no requirement code (`ownLaunchVar` was rewritten, the duplicated protected list was replaced by a shared one, and the unused `folderCarriesVariable` gate was removed). All FR-001 to FR-024 implementations were rechecked: carrier decision (`launch.DecideWith`), planner and record (`launch.PlanWith`, `Apply`), key commands, wrapper clearing and model argument, registry `model`, show/doctor/list reporting, docs and constitution 2.0.0. All spec requirements verified after the fix loop.

## Test Suite Results

| Round | Test Command | Exit Code | Failures | Status |
|-------|-------------|-----------|----------|--------|
| 0 (before) | go test -count=1 ./... | 0 | 0 | passed |
| 1 | go test -count=1 ./... | 0 | 0 | passed |
| 2 | go test -count=1 ./... | 0 | 0 | passed |

`go vet ./...` clean, `gofmt -l .` empty in every round.
