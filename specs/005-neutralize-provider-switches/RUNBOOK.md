---
runbook_version: 1
feature: 005-neutralize-provider-switches
status: drafted
mode: light
steps_total: 9
step_results: pending,pending,pending,pending,pending,pending,pending,pending,pending
drafted_at: 9594e66
drafted_fingerprint: 9d81b4628284f78fa98089db8314aad1aa05e2230472c0afe8c1f66e73d16bb8
tested_at:
tested_fingerprint:
updated: 2026-11-10
---

# Try it: Neutralize Provider Switches for Every Profile Folder

<!-- runbook-status -->
> **Status:** drafted · 0 of 9 steps · Mode: light · Drafted at 9594e66 · 2026-11-10
<!-- /runbook-status -->

## What you're testing

Profile folders now block provider switches and keys that a launching tool (Corgi, for example)
exports, for every Claude profile with its own settings file, not only for API-key profiles.
Names the profile supplies through its wrapper are never blanked, and a login profile at share
level `all`, which the folder cannot protect, is reported by `show` and `doctor`.

The journey: build the binary, set up a throwaway home whose default setup turns Vertex on, add a
login profile and watch `sync` neutralize the switch (with a note about the dropped value), check
idempotence, add an `oauth-token` profile and confirm its own token is left alone, add a login
profile at `all` and read its report, then do the one manual check with a real login.

## Test environment

No fixtures. Every automated step runs in a throwaway `HOME` created in Step 2, so your real
setup is never touched. Step 9 uses your own real Claude login and is run by you.

## Steps

### Step 1 of 9: Build the binary (US1)

**Why this step:** every later step runs the branch's `aprofile`, not the installed one.

**Run:**

```
go build -o ./bin/aprofile ./cmd/aprofile && ./bin/aprofile --version
```

**What you should see:** a version line and exit status 0.

**If you see something else:** a compile error means the branch does not build; stop here.

**Result:**

### Step 2 of 9: Create a throwaway home whose default setup turns Vertex on (US1)

**Why this step:** reproduces the upgrade case: a `content` profile copies the default settings once, so a provider switch set there lands in the profile's own file.

**Needs:** Step 1

**Run:**

```
export APH=$(mktemp -d) && mkdir -p "$APH/.claude" && echo '{"env":{"CLAUDE_CODE_USE_VERTEX":"1"}}' > "$APH/.claude/settings.json" && echo "$APH"
```

**What you should see:** one temporary directory path.

**If you see something else:** an empty line means `mktemp` failed; rerun the step.

**Result:**

### Step 3 of 9: Add a login profile and sync (US1)

**Why this step:** the core change: a login profile's folder now neutralizes the auth-clearing list, and a dropped own value is called out on its own line.

**Needs:** Step 2

**Run:**

```
HOME="$APH" ./bin/aprofile add claude work --auth-kind oauth-login --share content && HOME="$APH" ./bin/aprofile sync
```

**What you should see:** `created profile claude/work`, then `sync` lines `claude/work: settings.json: set env ...` for the auth-clearing names, one line `replaced user entry env CLAUDE_CODE_USE_VERTEX`, and a separate `note:` line saying the profile's own env `CLAUDE_CODE_USE_VERTEX` is replaced by an empty entry, with the fix `aprofile edit claude/work --env CLAUDE_CODE_USE_VERTEX=<value>`. No value appears anywhere.

**If you see something else:** no `set env` lines means the neutralizing rule did not apply to login profiles; no `note:` line means the dropped-value warning is missing.

**Result:**

### Step 4 of 9: Read the folder's env block (US1)

**Why this step:** confirms what a folder-only launcher like Corgi will see.

**Needs:** Step 3

**Run:**

```
jq '.env' "$(HOME="$APH" ./bin/aprofile show claude/work --json | jq -r .config_dir)/settings.json"
```

**What you should see:** an object where `CLAUDE_CODE_USE_VERTEX`, `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CODE_OAUTH_TOKEN` and the other Vertex, Bedrock, Foundry and Mantle names all map to `""`.

**If you see something else:** `"1"` for `CLAUDE_CODE_USE_VERTEX` means the switch still leaks into folder launches.

**Result:**

### Step 5 of 9: Sync again and check the status (US1)

**Why this step:** `sync` must be idempotent, and a folder holding only neutralizing entries still counts as self-contained.

**Needs:** Step 3

**Run:**

```
HOME="$APH" ./bin/aprofile sync && HOME="$APH" ./bin/aprofile show claude/work | grep 'folder:'
```

**What you should see:** `already up to date (0 changes)` and `folder:         self-contained`.

**If you see something else:** any change on the second run means the entries flip back and forth; `not self-contained` means the neutralizing entries were counted as wrapper-only settings.

**Result:**

### Step 6 of 9: An oauth-token profile keeps its own token (US2)

**Why this step:** an empty folder entry beats the wrapper's value, so the profile's own token variable must never be blanked.

**Needs:** Step 2

**Run:**

```
HOME="$APH" ./bin/aprofile add claude token --auth-kind oauth-token --auth-key env:MY_TEST_OAUTH_TOKEN --share content && HOME="$APH" ./bin/aprofile sync >/dev/null && jq '.env | has("CLAUDE_CODE_OAUTH_TOKEN"), .CLAUDE_CODE_USE_VERTEX' "$(HOME="$APH" ./bin/aprofile show claude/token --json | jq -r .config_dir)/settings.json"
```

**What you should see:** `false` (no entry for the profile's own token variable), then `""` (the switch is still neutralized).

**If you see something else:** `true` means the folder would wipe the token the wrapper sets.

**Result:**

### Step 7 of 9: A login profile at share level all reports the gap (US3)

**Why this step:** at `all` the settings file is your linked default setup, which aprofile never writes; `show` must say the folder cannot protect this profile.

**Needs:** Step 2

**Run:**

```
HOME="$APH" ./bin/aprofile add claude priv --auth-kind oauth-login --share all && HOME="$APH" ./bin/aprofile sync >/dev/null; HOME="$APH" ./bin/aprofile show claude/priv | grep -A1 'folder:'; jq '.env' "$APH/.claude/settings.json"
```

**What you should see:** `folder:         not self-contained` followed by `wrapper-only: auth-clearing (share level all: settings.json is linked)`, and the default setup's env block unchanged: `{"CLAUDE_CODE_USE_VERTEX": "1"}`.

**If you see something else:** `self-contained` means the gap is not reported; empty entries in the default file mean aprofile wrote through the link.

**Result:**

### Step 8 of 9: Doctor names the gap as information only (US3)

**Why this step:** the primary profile is often a login at `all`; the report must not make `doctor` fail forever.

**Needs:** Step 7

**Run:**

```
HOME="$APH" ./bin/aprofile doctor --json | jq -c '.findings[] | select(.message | test("auth-clearing"))'
```

**What you should see:** exactly one finding for `claude/priv` with `"severity":"info"`, the message `auth-clearing is wrapper-only: share level all: settings.json is linked` and a fix naming the wrapper `claude-priv` and `aprofile edit claude/priv --share content`. No finding for `claude/work` or `claude/token`.

**If you see something else:** `"severity":"problem"` would change doctor's exit status; findings for the `content` profiles mean the check is too broad.

**Result:**

### Step 9 of 9: A real login stays on its subscription when launched from the folder (US1)

**Why this step:** the credential-free research (R1) showed empty entries act as unset; only a real login proves the session uses the subscription (FR-004a, SC-001).

**Needs:** a Claude profile of yours with a real login at share level `content`, synced with this branch's binary

**Do:**

1. Run `./bin/aprofile sync` with your real `HOME` (this places the neutralizing entries in your `content` profiles).
2. Start Claude Code with only the folder set and the switch exported: `CLAUDE_CODE_USE_VERTEX=1 CLAUDE_CONFIG_DIR=<your profile's config dir from aprofile show> claude`.
3. Run `/status` in the session.

**What you should see:** your login's account and the Anthropic API as the provider, not Google Vertex AI.

**If you see something else:** Vertex AI as provider means Claude Code did not apply the empty entry; record the Claude Code version.

**Result:**

## Coverage

- US1 (login folders neutralize exported switches): Steps 3, 4, 5 and 9.
- US2 (wrapper-supplied names are never blanked): Step 6. The wrapper's byte-identical output is covered by unit tests, not by hand.
- US3 (login profile at `all` reports the gap): Steps 7 and 8.
- US4 (Codex): not covered by hand; research R2 showed Codex ignores the exported variables, so there is nothing to try.

## Results
