# Brainstorm: Neutralize Provider Switches for Login Profiles

**Date:** 2026-11-10
**Status:** active

Seeded from the idea inbox (`neutralize-provider-switches-for-login-profiles`). Related: [003 Corgi integration](003-corgi-integration.md), spec `004-corgi-integration` FR-006a.

## Problem Framing

A profile launched from its config folder alone (Corgi, or any tool that only sets `CLAUDE_CONFIG_DIR` or `CODEX_HOME`) bypasses the wrapper, and with it the wrapper's auth clearing. Spec 004 FR-006a gives the folder its own counterpart: empty `env` entries in `settings.json` for the harness's auth-clearing list (auth variables of every kind plus provider switches such as `CLAUDE_CODE_USE_VERTEX`). But the folder writes them only when it carries the auth key (`internal/launch/plan.go`, `desiredState` returns before the neutralizing step when there is no folder key). Login profiles, `oauth-token` profiles and api-key profiles whose key stays in the wrapper therefore inherit whatever the launcher exports.

Observed 2026-11-10: Corgi's daemon still carried `CLAUDE_CODE_USE_VERTEX=1` from an old shell, and the `claude/private` profile (Claude Max login) started on Google Vertex AI.

Finding during the session: `claude/private` is at share level `all`, so its `settings.json` is the linked default setup, which aprofile never writes (FR-008). The neutralizing entries alone would not have prevented the incident. Only the new `show`/`doctor` warning would have pointed at it.

**Success:** every profile with its own settings file runs on the provider it was set up for when launched from the folder alone, whatever its auth kind. Profiles that cannot be protected are named as such.

**Anti-goals:**
- Breaking ordinary wrapper launches. An empty folder entry beats the wrapper's value, so blanking a name the wrapper supplies would silently drop the profile's own token, key or variable.
- Writing anything into the linked default setup at share level `all`.
- A permanent `doctor` warning for the primary profile in the normal case.

## Approaches Considered

### A: Neutralize whenever the profile has its own settings file (chosen)
Drop the "only with a folder key" gate for the neutralizing entries. Keep and widen the existing exclusion of names the profile supplies through the wrapper.
- Pros: one rule for every auth kind; covers login, `oauth-token` and wrapper-key api-key profiles; reuses the placed-entry record, so `sync` places, restores and removes the entries like other launch entries.
- Cons: existing profiles show stale folders until their next `sync`; more aprofile-owned entries in each profile's `settings.json`.

### B: Neutralize only for login kinds
- Pros: narrowest change to FR-006a.
- Cons: `oauth-token` and wrapper-key api-key folders keep the same leak; a second condition that `show`, dry runs and `sync` must agree on.

### C: Warn only, write nothing
- Pros: no new placed entries.
- Cons: aprofile cannot see a daemon's environment, so the warning is guesswork; the incident shows stale shell exports linger.

### Dropped: fix it in Corgi
Having Corgi launch the wrapper instead of the folder belongs to the Corgi project, not aprofile.

## Decision

Approach A. Profiles at share level `all` stay uncovered; `show` and `doctor` warn about it for interactive login kinds only.

## Key Requirements

- For every profile whose settings file is its own (share level `content` or `none`), the folder carries empty `env` entries for the harness's auth-clearing list (`UnsetAuthVars()`), independent of auth kind and of whether the folder carries the key. They are placed entries, recorded and maintained by `sync` as in spec 004.
- Names the profile supplies through the wrapper are never blanked: the auth variable of a wrapper-carried key (`oauth-token`, or an `api-key`/`auth-token` key that stays wrapper-only, for example a cleared `env:` source), and extra or exec-hook variables, as today. *(My proposal: an empty folder entry beats the wrapper's value and would break wrapper launches.)*
- Share level `all`: no entries are written (FR-008 unchanged).
- `show` and `doctor` report, for a profile at share level `all` with an interactive login kind (`oauth-login`, `chatgpt-login`), that a launch from the folder alone inherits provider switches and keys the launcher exports. No warning for other `all` profiles. *(User decision. Treating `oauth-token` as excluded is my call: its token is wrapper-only, and `show` already reports that folder as not self-contained.)*
- Codex: no change unless verification shows a `chatgpt-login` profile honors an exported `OPENAI_API_KEY` or `OPENAI_BASE_URL`. If it does, record it as a known gap, since `config.toml` has no env block. *(My proposal.)*
- FR-006a in spec 004 is amended accordingly (neutralizing no longer tied to a folder key).

## Open Questions

- Does a ChatGPT-login Codex profile honor an exported `OPENAI_API_KEY` or `OPENAI_BASE_URL`? Needs verification on a current Codex release.
- Does a login profile with neutralizing entries and no other launch settings count as "self-contained" in `show`/`list`, or does the folder status stay unchanged?
- Wording and fix hint of the `show`/`doctor` warning for `all` login profiles (for example, suggest `aprofile edit --share content`).
