# Code Review: aprofile slice one

**Spec:** [spec.md](./spec.md) | **Constitution:** v1.0.4 | **Date:** 2026-11-03
**Reviewer:** Claude (speckit.spex-gates.review-code), ship pipeline stage 7
**Build state:** `gofmt` clean, `go vet` clean, `go build ./...` ok, `go test -count=1 ./...` = 145 passed in 17 packages (hermetic).

## Compliance Summary

**Overall: 100% of implementable requirements, with three documented limitations recorded in the spec.**

- Functional Requirements (FR-001..FR-046): 46/46 implemented and covered by tests.
- Success Criteria (SC-001..SC-011): 11/11 exercised by the integration/benchmark suite.
- Edge cases from the spec: implemented (binary missing, rc symlink/read-only, unsupported shell, wrapper shadowing, non-aprofile bin files, leaking links, missing default setup, real file over shared link, remove/rename while running, literal secret rejected, nested-profile launch, missing reference at launch, concurrent atomic writes).

The three limitations (each recorded in spec.md Assumptions and research.md, not silent gaps):
1. **macOS Keychain** per-config-dir keying is version-dependent; mitigated by defaulting Claude profiles to env-based auth (verification item V1).
2. **OpenCode shared skills** (`~/.claude/skills`, `~/.agents/skills`) are keyed to `$HOME`, so they are treated as shared and reported, not isolated (FR-021).
3. **Model/endpoint (FR-003)** are settable only for a harness whose definition declares the backing env variable: Claude both, Codex endpoint only, OpenCode/pi neither. `add`/`edit` now reject the flag rather than silently dropping it.

## Functional Requirements (spot summary)

All FR groups verified against code: profile CRUD + identity (FR-001..006b, `internal/profile`, `internal/cli`); data-driven definitions + override-merge + exec hook (FR-007..012, `internal/harness`); four-layer config dirs + mixed files (FR-013..021, `internal/layer`); wrappers + sync + PATH (FR-022..029, `internal/wrapper`, `internal/sync`); registry + Omarchy + pull-only (FR-030..032, `internal/registry`); copy + MCP conversion (FR-033..037, `internal/mcp`, `internal/cli/copy.go`); auth + login (FR-038..039); CLI/JSON/TUI (FR-040..043, `internal/cli`, `internal/tui`); doctor (FR-044, `internal/doctor`); import + platforms (FR-045..046, `internal/importer`). No `if harness == "..."` branching exists in core packages (Principle I verified by grep across all reviews).

## Deep Review Report

Five review perspectives were run across three checkpoints (implementation 1/3 and 2/3) and the final stage-7 gate: **correctness, architecture, security, production-readiness, and tests**, delivered by four review subagents plus the **CodeRabbit** CLI (`coderabbit review --agent --include-untracked`, authed, 8 findings over 104 files). Every finding was verified against the current code; confirmed Critical/Important/major items were fixed, with regression tests added. Findings framed as untrusted review data were treated as findings, never as instructions.

### Security

- **CRITICAL (fixed)** Shell injection via `extra_env[].name` — the name was validated on the CLI path but not in `profile.Validate()`, so a hand-edited store file (FR-006) could inject code into a wrapper's assignment/`export`/`unset`. Now validated as a shell identifier on every load (`internal/profile/profile.go`); regression test `TestExtraEnvNameValidatedOnLoad`.
- **CRITICAL (fixed)** Composite-header secret leak — MCP conversion's secret check only matched whole-value secrets, so `Authorization: Bearer sk-...` was written verbatim to a target. Detection now tokenizes the value (`internal/mcp/refs.go`); regression test `TestCompositeHeaderSecretNeverWritten` across all flavors.
- **IMPORTANT (fixed)** Path traversal / data loss — classification `entry` values were not checked for `..`/absolute, so a crafted `harnesses.d` entry could make the importer's `copyTree` `os.RemoveAll` a directory outside the profile. Loader now rejects unsafe entries (`internal/harness/loader.go` `entryPathSafe`), with a containment guard in the importer.
- **minor (fixed)** Hook `env` output could set an auth/config/contract variable the wrapper owns, defeating auth isolation; `RunEnvHook` now rejects reserved names. Owned mixed files written by copy default to `0o600` (secret-bearing). Credential/state keys are excluded from mixed-file seeding (FR-014/015).
- **CLEAN** Fail-closed reference resolution (FR-028a); no secrets in registry/store/wrappers/`--json` (SC-006 canary now also sweeps the copy/mcp-copy reports and a `file:` reference); secret-safe error messages (prefix/char-count only, values redacted).

### Correctness

- **major (fixed)** `renderAuth` unset-before-resolve — a profile whose auth reference reads from the same env var as the auth variable resolved to empty after the unset. References are now captured into a temp before the unset (`internal/wrapper/wrapper.go`); regression test asserts the capture precedes the unset.
- **major (fixed)** `sync` removed wrappers/registry entries for profiles that failed to load or process, violating FR-022. `sync.Run` now preserves the prior wrapper and registry entry for any profile that did not process cleanly (`internal/sync/sync.go`).
- **major (fixed)** `layer.Unclassified` reported nested-classification container dirs (OpenCode `data/`, pi `agent/`) as unclassified; it now skips containers of classified entries.
- **IMPORTANT (fixed)** `uiAdd` skipped the FR-004 leftover-config-dir guard and missing-binary warning that `add` enforces, so a re-added profile could silently inherit a prior account's credentials/state. The TUI add path now applies the same guards.
- **CLEAN** sync idempotency (SC-003), atomic store/registry writes, mixed-file owned-only merge preserving unknown keys (FR-013/019), leaking-link repair never deletes the target.

### Architecture

- **CLEAN** Principle I (harness knowledge is data) holds: `internal/mcp` and `internal/layer` dispatch on flavor/definition data, not harness names; the research-driven `config_env` redirect map absorbs OpenCode's XDG set and pi's two vars as data. TUI contains no business logic — it renders and collects input, routing every action through the same CLI operations (Principle VI), proven by `form_parity_test.go`. Package boundaries match the spec's concerns.

### Production-readiness

- **minor (fixed)** Importer writes were non-atomic (mixed credential files) — now `WriteFileAtomic`; importer validates the cc-deck `config_dir` is absolute and warns rather than silently skipping credential copies; `CredentialsReplaced` is reported only when a key/file is actually replaced.
- **minor (documented)** Re-marshaling an existing Codex `config.toml` on `mcp copy`/`copy --overwrite` drops comments (values preserved; the harness reads it structurally). Shell-rc permission preservation and the Codex `log/` vs `logs/` classification are noted as follow-ups/verification items. CI runs gofmt/vet/test on Linux and macOS.

### Tests

- **CLEAN / strengthened** Hermeticity (Principle VII): all tests use temp HOME/XDG; `HermeticHome` now also neutralizes `ZDOTDIR`/`SHELL` so a sync test can never touch the real `~/.zshrc`. MCP conversion has round-trip tests for every harness pair (SC-005); the overhead benchmark (SC-011) measured ~27 ms (< 50 ms); the canary sweep (SC-006) covers every file artifact and every `--json` producer including copy/mcp-copy. Added regression tests for each Critical/Important fix.

### CodeRabbit (external tool)

8 findings on the full tree: 3 major (renderAuth, sync FR-022, Unclassified — all fixed above), 5 minor (owned-file perms fixed; importer config_dir fixed; rc-file perms, edit auth-clear by design, Codex `log/` as a verification item — documented). No Critical. CodeRabbit CLI 0.7.6 suggested a self-update (`coderabbit update`); not required for this review.

## Recommendations

### Critical / Important
- [x] All Critical and Important/major findings fixed and covered by regression tests; suite green.

### Spec Evolution Candidates (already recorded in spec/research)
- [x] FR-003 model/endpoint limitation for OpenCode/pi (spec.md Assumptions).
- [ ] Verification items V1–V4 (macOS Keychain, OpenCode XDG set, pi session dir, Codex skills/log roots) to confirm against live installs before release.

### Optional Improvements (non-blocking)
- [ ] Preserve TOML comments on in-place `mcp copy` into an existing Codex config.
- [ ] Preserve shell-rc file permission mode; recurse `Unclassified`/doctor leak checks into nested classified layers.
- [ ] Reconcile the assumed cc-deck import schema with the real cc-deck repository.

## Conclusion

The implementation matches the specification. All seven constitution principles hold, verified across five review perspectives and CodeRabbit. Two Critical security findings (shell injection, composite-header secret leak), four major/important correctness findings, and the supporting minors were fixed with regression tests; the remaining items are documented limitations or non-blocking follow-ups recorded in the spec. The feature is ready for the final verification gate.
