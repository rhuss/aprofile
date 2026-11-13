# Review Guide: Neutralize Provider Switches for Every Profile Folder

**Generated**: 2026-11-10 | **Spec**: [spec.md](spec.md)

## Why This Change

Spec 004 lets a profile folder blank inherited auth variables and provider switches (empty `env` entries in `settings.json`), but only when the folder carries the auth key. Login profiles, `oauth-token` profiles and API-key profiles whose key stays in the wrapper get no protection when a tool such as Corgi launches them from the folder alone. On 2026-11-10 a Claude Max login profile started on Google Vertex AI because Corgi's daemon still exported `CLAUDE_CODE_USE_VERTEX=1`.

## What Changes

Every Claude profile with its own settings file (share level `content` or `none`) gets empty `env` entries for the whole auth-clearing list after `sync`, whatever its auth kind, except for names the profile supplies through its wrapper. Interactive-login profiles at share level `all`, whose settings file is the linked default setup and is never written, are now reported as not self-contained: `show` lists `auth-clearing` as a wrapper-only item and `doctor` reports an information finding with a fix. Codex was verified to ignore exported `OPENAI_API_KEY` and `OPENAI_BASE_URL` for login profiles, so Codex behavior is unchanged. Existing `content`/`none` profiles without a folder key show as stale until the next `sync`; no other migration.

## How It Works

- **Planner** (`internal/launch/plan.go`): `PlanWith` passes "the folder can carry env entries" (the existing `envCarrier` rule in `carrier.go` returns no reason) into `desiredState`. The neutralizing block runs on that condition instead of only when a key is folder-carried, and the early return for "no folder key" moves below it.
- **Supplied names**: the exclusion set grows from "every variable setting" (model, endpoint, extra, exec-hook) to also include the variable of every key setting that stays wrapper-carried (`oauth-token`, `api-key` with reason `auth-kind` or `cleared-source`). A folder-carried key's own variable is still blanked, as in spec 004.
- **Reporting** (`apply.go` `Status`, `report.go` `Folder()`): a new `StatusReport` field marks the share-level-`all` login gap; `Folder()` appends `WrapperOnly{Setting: "auth-clearing", Variable: "", Reason: "share-all"}`, which makes the folder `not-self-contained` under the existing rule. `DecideWith` is untouched, so the wrapper and the planner never see a pseudo setting.
- **Doctor** (`internal/doctor/doctor.go` `launchFindings`): the `auth-clearing` item gets its own fix text (wrapper or `aprofile edit <id> --share content`), severity info.
- **Codex**: a comment in `internal/harness/builtin/codex.yaml` records the research R2 result.
- **Docs**: amendment notes in spec 004 (FR-001, FR-006a, FR-020) and the Corgi user guide.

## When It Applies

**Applies when**:
- A harness definition declares a folder `env` location (today only Claude Code) and the profile's settings file is its own (`content` or `none`).
- A Claude interactive-login profile is at share level `all` (report only, nothing written).

**Does not apply when**:
- The profile is at share level `all` with any auth kind other than an interactive login (their wrapper-only key is already reported).
- The harness has no folder `env` location (Codex, OpenCode, pi); Codex was checked and needs nothing.
- Corgi or other launchers would need changes (out of scope, Constitution Principle V).
- A user adds empty entries to their default setup by hand to protect an `all` profile (out of scope; spec 004 FR-020b keeps reporting such entries).

## Key Decisions

1. **Neutralize for every own settings file, not only login kinds.** Gating on auth kind was rejected in the brainstorm; one rule covers login, wrapper-token and wrapper-key profiles alike.
2. **Never blank a supplied name.** An empty folder entry beats the value the wrapper sets, so blanking `CLAUDE_CODE_OAUTH_TOKEN` for an `oauth-token` profile would break ordinary wrapper launches. The exclusion now covers wrapper-carried key variables.
3. **Reuse `envCarrier` for the linked-file test** instead of checking `p.ShareLevel()`, so the planner, the carrier decision and the report agree on when the folder can carry entries.
4. **Report the `all` gap as a wrapper-only item, not a new field.** Alternative: a separate `warnings` array in `show --json`. Rejected because it adds a second place to look; the wrapper-only list already means "the folder alone does not do this". The item lives only in `Folder()`, never in `DecideWith`.
5. **Doctor reports the gap as info.** A primary profile at `all` would otherwise make `doctor` fail permanently.
6. **Codex unchanged.** Research R2 showed Codex ignores the exported variables for login profiles.

## Areas Needing Attention

- **Folder vs wrapper launch difference**: a folder launch exports the neutralized names as empty strings to child processes, where a wrapper launch leaves them unset. The spec assumes tools treat empty as unset.
- **FR-004a evidence is partial**: research R1 shows empty entries turn Vertex off and drop the exported key, but was run without a real login ("Not logged in"). The real Claude Max check is a manual quickstart step (section 4).
- **Broken settings files for login profiles**: login profiles now always want entries in `settings.json`, so an unparseable file makes `sync` fail for them (spec 004 FR-009a), where it was a note before. The spec accepts this; reviewers should confirm it is the intended user experience.
- **Replacing user entries**: a non-empty `CLAUDE_CODE_USE_VERTEX` the user put into the profile's own settings is replaced and not restored later (spec 004 FR-004). A profile that wants a provider switch must set it with `--env`.
- **Test blast radius**: many existing tests create `oauth-login` Claude profiles at `content`; their expectations about `settings.json` content and sync output change.
- **Nil definition path**: `PlanWith` keeps going when a definition override removed `folder_launch` but the record has entries; the moved early return in `desiredState` must not dereference a nil `FolderLaunch`.

## Open Questions

No open questions identified.

## Review Checklist

- [ ] Key decisions are justified
- [ ] Breaking changes are documented with migration guidance
- [ ] Scope matches the stated boundaries
- [ ] Success criteria are achievable
- [ ] No unstated assumptions
- [ ] No supplied name (wrapper-carried key, extra or exec-hook variable) is ever blanked
- [ ] Nothing is written into a linked settings file at share level `all`
- [ ] Messages and `--json` name variables only, never values

---

<!-- Code phase sections are appended below this line by the phase-manager command -->
