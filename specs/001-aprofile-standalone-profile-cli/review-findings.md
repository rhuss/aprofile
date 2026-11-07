# Deep Review Findings

**Date:** 2026-11-06
**Branch:** 001-aprofile-standalone-profile-cli
**Scope:** uncommitted Phase 11 gap fixes (105 source files)
**Rounds:** 3 (plus one fix after the loop)
**Gate Outcome:** FAIL by protocol (one Important finding remained after round 3). It was fixed after the loop and covered by regression tests, but that fix was not re-reviewed by an agent.
**Invocation:** quality-gate (after `/speckit-implement`)

## Summary

| Severity | Found | Fixed | Remaining |
|----------|-------|-------|-----------|
| Critical | 2 | 2 | 0 |
| Important | 21 | 21 | 0 |
| Minor | 25 | 13 | 12 |
| Notable | 3 | - | 3 |
| **Total** | **51** | **36** | **15** |

Counts are after deduplication. Each Critical was a regression, found and fixed within the loop: the pi `!` command (round 1) and the URL placeholder panic (round 2). The Important count includes the regressions that the re-reviews found in rounds 1 and 2 (empty values dropped, template URLs only partly checked, OpenCode key names) and the unparseable-URL fallback found in round 3.

**Agents completed:** 5/5 internal (Goal Alignment skipped: no PR), plus CodeRabbit and Codex. Each fix round was re-reviewed by a narrowed correctness and security agent, and round 1 also by Codex.
**Agents failed:** none

## Findings (Critical and Important)

### FINDING-1 (M1)
- **Severity:** Critical
- **Confidence:** 92
- **File:** internal/mcp/template.go, internal/mcp/pi.go
- **Category:** security / correctness
- **Source:** correctness-agent (also security-agent, test-quality-agent)
- **Round found:** 1
- **Resolution:** fixed (round 1)

**What is wrong:** The neutral MCP record could not tell a pi `!command` from a literal that starts with `!`. `render` wrote such a literal to pi unchanged, so a Claude env value `"!echo pwned"` became a command that pi runs. An escaped pi literal `$!foo` also became a command after a pi to pi `mcp copy`. In the other direction, a pi command leaked into other harnesses as literal text.

**Why this matters:** A `copy` turned data into code execution in the target harness.

**How it was resolved:** pi commands now have their own neutral form (`cmd:`), which only the pi decoder sets. A plain literal that starts with `!` is always written to pi as `$!...`. Non-pi targets drop pi commands and report them as unrepresentable. Literals that look like neutral markers stay literal. Tests: TestPiCommandNeutralForm, TestNoBareBangFromNonPiSource, TestNeutralMarkerLiteralsStayLiteral.

### FINDING-2 (round 2 regression)
- **Severity:** Critical
- **Confidence:** 95
- **File:** internal/mcp/refs.go (urlShape)
- **Category:** correctness
- **Source:** re-review agent, round 2
- **Round found:** 2
- **Resolution:** fixed (round 3)

**What is wrong:** The URL placeholder regex was greedy, and the tag used only the letters a to y. So adjacent references such as `${A}${B}` merged into one match. `RedactURL` then panicked with an out-of-range index, which broke `mcp list` and `copy`, and other inputs silently lost their references.

**How it was resolved:** Placeholders are now framed by a tag that starts and ends with `z`, which the index (a to y, at most 6 letters) never contains. A reference in the port position gets a numeric placeholder. If `back` cannot restore a placeholder, the url is dropped and reported instead of being corrupted. Tests: TestTemplateURLShapes, TestTemplateURLShapeSecrets.

### Important findings, all fixed

| ID | Area | Problem | Fix (round) |
|----|------|---------|-------------|
| M2 | mcp encoders | A remote server whose url was dropped was still written (Codex read it back as an empty stdio server) and counted as Converted | Server skipped and reported (1) |
| M3 | mcp OpenCode | `{file:path}` substitution in values, and in env, header and server names, went unchecked | Any `{word:...}` treated as a substitution; names checked through a `keys` syntax (1, 2) |
| M4 | mcp URL | Fragment and path segments not checked; template URLs got only the prefix check | Full checks for literal and template URLs through placeholders (1, 2, 3) |
| R2-1 | mcp | The round-trip check dropped every empty env, header or args value, which shifted the remaining args | Empty values kept; a stdio server that loses any arg is skipped (2) |
| R3-1 | mcp URL | The unparseable-URL fallback (stray `%`, bad port) let a long token through in the query, fragment, userinfo or path | Each piece checked like a parsed url (after the loop) |
| M5 | registry | Omarchy parser accepted trailing data and dropped it on rewrite (Codex P2) | Requires `io.EOF` (1) |
| M6 | importer | Writing the imported `.claude.json` blocked later seeding | Starts from the seed for the share level, then adds credentials and owned keys (1) |
| M7 | profile | The new color/icon validation made existing profiles fail to load | Repaired on load with a warning; still strict on save and input (1) |
| M8 | login | `loginEnv` was a hand-written copy of the wrapper env (no env hook, drifted) | Shared env model `wrapper.Environ`; env hook called (1) |
| M9 | cli/sync | Four wrapper-name clash implementations | One helper, `Definition.WrapperName` (1) |
| M10 | secret | Divergent secret heuristics in mcp, layer, cli and importer | One value and one key-name classifier in `internal/secret` (1) |
| M11 | layer/importer | Importer reimplemented mixed-key merging; a non-JSON mixed file was seeded raw | `MergeMixedKeys` and `SeedMixed`; non-JSON seeded empty (1) |
| M12 | hooks | A timeout left orphaned grandchild processes | Process group killed (1) |
| M13 | hooks | Every call to a failing hook cost about 11 s | Timeout memo per profile, harness-wide after 2 timeouts, 60 s TTL (1, 2) |
| M14 | importer | One failing profile aborted the import and left it half-written | Failures recorded per profile, run continues, rollback, non-zero exit (1) |
| M15 | doctor | Silent about declared but missing or broken hooks | Findings once per harness and per profile (1) |
| M16 | tests | cc-deck credential branches untested | Table test for every branch (1) |
| M17 | tests | Overhead benchmark flaky under load | Interleaved pairs, 25th percentile, retry; still 50 ms (1) |
| copy | cli/sync | Same-harness copy and the rc writer replaced a target they could not read | Fails on any error except "not found" (1) |

## Remaining Minor Findings (not gate-relevant)

- Arch5: the MCP `location` (`file#key`) is parsed in four places, and `ParseDoc`/`MergeInto` hardcode the in-file key per flavor.
- Arch6: `copy --overwrite` across harnesses does not print replaced servers in text mode, and `runMCPCopy` duplicates `convertMCP`.
- Arch7: the copy `--json` schema has parallel fields (`plugins_skipped`/`skipped_plugins`, three secret-warning lists).
- Arch9: `ccBackends` in the importer holds Claude Code variable names in Go. It is keyed by cc-deck backend, not by harness, but a definition field would fit Principle I better.
- Arch10: the color name table exists twice (profile and tui), kept in step by a test.
- importer `storedKeys` still duplicates the cli helper.
- Prod5: a failed `copy` into a new target leaves a prepared config dir, and the retry then needs `--adopt`.
- Prod6: Ctrl-C leaves `.aprofile-tmp-*` files, which are later reported as unclassified entries.
- Codex P2 (round-1 re-review): in sandboxes where process environments cannot be read, five integration tests hit the new "cannot tell whether it runs" prompt.
- Settings secret warnings are prefix-only by design (a length check would flag permission rules), and the merged key list now warns on Codex `env_key`.
- An empty MCP `command` is written as `""`.
- `--flag=value` drops keep the server (by design, and reported).

## Notable Observations

### NOTABLE-1
- **File:** internal/importer/importer.go (fileRef)
- **Category:** security
- **Source:** security-agent
- **Description:** `import cc-deck` copies whatever absolute credential file cc-deck's config names, from any location, into a 0600 copy.
- **Rationale:** This matches cc-deck's own behavior, and the trust boundary is the user's config. `--dry-run` could list files outside cc-deck's own folders.

### NOTABLE-2
- **File:** internal/mcp/caps.go (argPositionLost)
- **Category:** correctness
- **Source:** re-review agent, round 2
- **Description:** Dropping a whole `--flag=value` element keeps the server, which then starts without that option. It is reported, but users only see it in the drop list.

### NOTABLE-3
- **File:** internal/sync/sync.go
- **Category:** production-readiness
- **Source:** production-agent
- **Description:** Problems are now reported once per harness, but sync's summary line still says "0 changes" next to "completed with problems".

## Post-Fix Spec Coverage

The fix loop removed code: duplicate helpers, the old heuristics, `copyWrapperClash`. No FR lost its implementation. The compliance agent (50/52, then the deviations fixed) and the re-review agents traced every requirement to code, and the full suite (500+ tests) covers the affected paths.

## Test Suite Results

| Round | Test Command | Result |
|-------|-------------|--------|
| 1 | go test -count=1 ./... | passed (the overhead benchmark failed once under load average 5-10; the wrapper was byte-identical before and after) |
| 1b | go test -count=1 ./... | passed (497) |
| 2 | go test -count=1 ./... | passed (504) |
| 3 + after the loop | go test -count=1 ./... and go test -race on mcp, harness, cli | passed |

## External Tools

- CodeRabbit 0.7.6: 1 minor finding (sync keepHarness), fixed. The CLI is outdated and cannot update itself; run `brew upgrade coderabbit`. It has no `--files` flag, so new files were included with `git add -N`, undone afterwards.
- Codex 0.167.0: 1 Important finding (Omarchy trailing data), fixed. In the round-1 re-review, 1 minor finding (sandbox process scan).
- Copilot: disabled in config.

## Follow-ups (2026-11-07)

The remaining Minor findings and Notable-1/2/3 were addressed as Phase 12 tasks T084 to T097 in `tasks.md` (branch `001-review-followups`): MCP location parsing in one place with the definition's key honored and validated at load; one copy/`mcp copy` conversion path that reports replaced servers; one `secret_warnings` list in the copy JSON; cc-deck backends as definition data; one color table; one stored-profile helper; rollback of a failed copy into a new target; aprofile's temp files ignored and cleaned up (an interrupted swap is restored, not deleted); a build-tag test seam for the running-process check; no secret warning for env-var names under keys that name a variable; empty commands skipped; dropped `--flag=value` options named; external credential files listed in the import dry run; problem counts in the sync summary. A review of that change set found one Important regression in the env-var exemption (all-caps secrets no longer warned), fixed with negative tests, and three Minor issues, all fixed. Full suite green with and without `-tags aprofile_testseam`.
