---
runbook_version: 1
feature: 003-history-groups
status: drafted
mode: light
steps_total: 12
step_results: pending,pending,pending,pending,pending,pending,pending,pending,pending,pending,pending,pending
drafted_at: a18edca
drafted_fingerprint: a2879f84c0a677f4cfe3d76c78904f216ccfacb4bbeff3a16801f12a37357317
tested_at:
tested_fingerprint:
updated: 2026-11-07
---

# Try it: History Groups

<!-- runbook-status -->
> **Status:** drafted · 0 of 12 steps · Mode: light · Drafted at a18edca · 2026-11-07
<!-- /runbook-status -->

## What you're testing

Profiles of one harness can share their conversation history through a history group, while
logins and all other runtime state stay private. Every supported harness has a built-in `home`
group backed by your default setup; you can create further groups such as `work`; profiles in no
group stay isolated as before.

The journey runs entirely in a sandbox home under this feature's `.walkthrough/` folder, never in
your real home: build the binary, create a sandbox with a fake default setup, look at the built-in
groups, try a typo, put two profiles into `home` and see one shared session, create a `work`
group, move an existing profile with history into `home` (dry run first), leave a group, try the
delete rules, break a link and let `doctor` and `sync` repair it, and finally check the wrapper and
registry.

## Test environment

No fixtures. Everything runs against a sandbox home at
`specs/003-history-groups/.walkthrough/home`, defined once in
`specs/003-history-groups/.walkthrough/env.sh`. The sandbox paths are absolute and written into
the file before anything is exported, so the XDG variables never point at your real home.
Teardown: `rm -rf specs/003-history-groups/.walkthrough/home`.

## Steps

### Step 1 of 12: Build the binary (setup)

**Why this step:** every later step runs `./bin/aprofile` from this branch.

**Run:**

```
make build && ./bin/aprofile group --help | head -3
```

**What you should see:** the build finishes without errors, and the help names the `group` command with subcommands for listing, showing, creating and deleting history groups.

**If you see something else:** "unknown command group" means the binary is not from this branch; check `git branch --show-current` shows `003-history-groups`.

**Result:**

### Step 2 of 12: Create the sandbox home with a fake default setup (setup)

**Why this step:** history groups link profiles to `~/.claude`. A fake default setup in a sandbox home shows the behavior without touching your real one.

**Needs:** Step 1.

**Run:**

```
mkdir -p specs/003-history-groups/.walkthrough && cat > specs/003-history-groups/.walkthrough/env.sh <<'EOF'
W="$(pwd)/specs/003-history-groups/.walkthrough"
export HOME="$W/home"
export XDG_CONFIG_HOME="$W/home/.config" XDG_DATA_HOME="$W/home/.local/share" XDG_STATE_HOME="$W/home/.local/state" ZDOTDIR="$W/home" SHELL=/bin/zsh
A="$(pwd)/bin/aprofile"
P="$XDG_DATA_HOME/aprofile/profiles"
EOF
( . specs/003-history-groups/.walkthrough/env.sh
  case "$XDG_DATA_HOME" in "$W"/*) ;; *) echo "UNSAFE XDG_DATA_HOME=$XDG_DATA_HOME"; exit 1;; esac
  test ! -e "$HOME" || { echo "sandbox already exists: $HOME"; exit 1; }
  mkdir -p "$HOME/.claude/projects/-tmp-demo" "$HOME/.codex" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_STATE_HOME"
  echo '{"sessionId":"s-default"}' > "$HOME/.claude/projects/-tmp-demo/s-default.jsonl"
  echo '{"display":"default prompt"}' > "$HOME/.claude/history.jsonl"
  cd "$HOME" && find . -type f | sort )
```

**What you should see:** no `UNSAFE` line, and two seeded files: `./.claude/history.jsonl` and `./.claude/projects/-tmp-demo/s-default.jsonl`.

**If you see something else:** "sandbox already exists" means an earlier run left it behind; run the teardown command from the Test environment section and rerun. An `UNSAFE` line means the environment file is wrong; stop and do not run later steps.

**Result:**

### Step 3 of 12: See the built-in groups (US4)

**Why this step:** every harness that supports groups has a `home` group whose store is your default setup; OpenCode is not supported because its session database also holds logins.

**Needs:** Step 2.

**Run:**

```
( . specs/003-history-groups/.walkthrough/env.sh; "$A" group list )
```

**What you should see:** a table with the columns `HARNESS GROUP MEMBERS STORE` and one `home (built in)` row each for `claude`, `codex` and `pi`, with the default setup as member and the sandbox's `.claude`, `.codex` and pi locations as stores. No `opencode` row.

**If you see something else:** an `opencode` row means the OpenCode definition declares history entries, which the spec forbids (FR-003).

**Result:**

### Step 4 of 12: A typo in a group name creates nothing (US2)

**Why this step:** groups are never created implicitly, so a mistyped name must fail cleanly instead of starting a new, empty history.

**Needs:** Step 2.

**Run:**

```
( . specs/003-history-groups/.walkthrough/env.sh; "$A" add claude test --auth-kind oauth-login --group wrok; echo "exit=$?"; ls "$P/claude" 2>&1 )
```

**What you should see:** an error naming the unknown group `wrok` and listing the existing groups (`home`), `exit=2`, and `ls` reporting that the profiles directory does not exist (nothing was created).

**If you see something else:** a profile named `test` in the listing means the group was not validated before creation.

**Result:**

### Step 5 of 12: Two profiles in `home` share one conversation (US1)

**Why this step:** this is the main use case: continue a conversation from another profile without a second copy of it.

**Needs:** Step 2.

**Run:**

```
( . specs/003-history-groups/.walkthrough/env.sh
  "$A" add claude personal --auth-kind oauth-login --group home && "$A" add claude lab --auth-kind oauth-login --group home && "$A" sync >/dev/null
  echo '{"sessionId":"s-lab"}' > "$P/claude/lab/projects/-tmp-demo/s-lab.jsonl"
  readlink "$P/claude/personal/projects"
  ls "$P/claude/personal/projects/-tmp-demo"
  find "$HOME" -name 's-lab.jsonl' | wc -l
  test -L "$P/claude/lab/.credentials.json" && echo "credentials LINKED" || echo "credentials private" )
```

**What you should see:** both `add` commands print the group line `home: shares history with your default setup` and the note that running the same conversation in two profiles at once is not prevented; `readlink` prints the sandbox's `.claude/projects` path; the listing shows `s-default.jsonl` and `s-lab.jsonl`; the count is `1` (one copy on disk); and `credentials private`.

**If you see something else:** a count of `2` means the session was copied instead of shared; `credentials LINKED` would be a serious isolation bug.

**Result:**

### Step 6 of 12: `show` explains what is shared (US1)

**Why this step:** you should be able to see, per profile, which entries are shared with the group and which stay private.

**Needs:** Step 5.

**Run:**

```
( . specs/003-history-groups/.walkthrough/env.sh; "$A" show claude/lab | grep -E 'group|projects/|history.jsonl|sessions/' )
```

**What you should see:** a line saying the profile shares history with your default setup and the other members of history group `home`; `projects/` and `history.jsonl` listed as linked with the reason `history group home`; `sessions/` listed as private.

**If you see something else:** `projects/` shown as a leaking link means the treatment code does not know about the group.

**Result:**

### Step 7 of 12: A separate `work` group stays separate (US2)

**Why this step:** work profiles share among themselves and never with personal ones.

**Needs:** Step 5.

**Run:**

```
( . specs/003-history-groups/.walkthrough/env.sh
  "$A" group create claude/work && "$A" add claude acme --auth-kind oauth-login --group work && "$A" add claude corp --auth-kind oauth-login --group work && "$A" sync >/dev/null
  mkdir -p "$P/claude/acme/projects/-tmp-work" && echo '{"sessionId":"s-work"}' > "$P/claude/acme/projects/-tmp-work/s-work.jsonl"
  ls "$P/claude/corp/projects/-tmp-work"
  ls "$HOME/.claude/projects" )
```

**What you should see:** `created history group claude/work` with a store under the sandbox's `.local/share/aprofile/groups/claude/work`; `corp` lists `s-work.jsonl`; the default setup's `projects` lists only `-tmp-demo` (no `-tmp-work`).

**If you see something else:** `-tmp-work` in the default setup means the `work` members are linked to the wrong store.

**Result:**

### Step 8 of 12: Move an existing profile with history into `home`, dry run first (US3)

**Why this step:** most real profiles already have history; joining must show exactly what it will do and lose nothing.

**Needs:** Step 5.

**Run:**

```
( . specs/003-history-groups/.walkthrough/env.sh
  "$A" add claude solo --auth-kind oauth-login && "$A" sync >/dev/null
  mkdir -p "$P/claude/solo/projects/-tmp-demo" && echo '{"sessionId":"s-solo"}' > "$P/claude/solo/projects/-tmp-demo/s-solo.jsonl"
  echo '{"display":"solo prompt"}' > "$P/claude/solo/history.jsonl"
  "$A" edit claude/solo --group home --dry-run
  echo "---- real run ----"
  "$A" edit claude/solo --group home --yes
  ls "$HOME/.claude/projects/-tmp-demo"
  find "$XDG_DATA_HOME/aprofile/set-aside/claude/solo" -type f | sed "s|$XDG_DATA_HOME/||" )
```

**What you should see:** the dry run lists a `move-to-store` step for `projects/-tmp-demo/s-solo.jsonl`, a `set-aside` step for `history.jsonl` (not mergeable) and `link-history` steps, and says where set-aside history goes. The real run prints `joined history group home`. The default setup's `-tmp-demo` now lists `s-default.jsonl`, `s-lab.jsonl` and `s-solo.jsonl`; the set-aside folder holds `history.jsonl` and `manifest.json` under a timestamped directory.

**If you see something else:** exit 3 means `--yes` was not passed; a missing `history.jsonl` in the set-aside folder would mean data was lost.

**Result:**

### Step 9 of 12: Leaving a group keeps the shared history (US3)

**Why this step:** leaving must give the profile fresh private history and never touch the group's store.

**Needs:** Step 8.

**Run:**

```
( . specs/003-history-groups/.walkthrough/env.sh
  "$A" edit claude/solo --no-group --yes
  test -L "$P/claude/solo/projects" && echo "still linked" || echo "projects is a real directory"
  ls "$P/claude/solo/projects" | wc -l
  ls "$HOME/.claude/projects/-tmp-demo" )
```

**What you should see:** `left history group home; the profile's history is its own again`, `projects is a real directory`, a count of `0`, and the default setup still listing all three sessions including `s-solo.jsonl`.

**If you see something else:** a missing `s-solo.jsonl` in the default setup means leaving changed the store, which the spec forbids (FR-015).

**Result:**

### Step 10 of 12: Group delete rules (US4)

**Why this step:** a group with members cannot be deleted, `home` can never be deleted, and a deleted group's history is kept unless you purge it.

**Needs:** Step 7.

**Run:**

```
( . specs/003-history-groups/.walkthrough/env.sh
  "$A" group delete claude/work; echo "exit=$?"
  "$A" group delete claude/home; echo "exit=$?"
  "$A" edit claude/acme --no-group --yes >/dev/null && "$A" edit claude/corp --no-group --yes >/dev/null
  "$A" group delete claude/work; echo "exit=$?"
  "$A" group create claude/work; echo "exit=$?" )
```

**What you should see:** the first delete fails with `exit=2` and lists `acme` and `corp`; deleting `home` fails with `exit=2`; after both leave, the delete prints `deleted history group claude/work; its store is kept at ...` and `exit=0`; creating `work` again fails with `exit=2` and names the kept store path.

**If you see something else:** a successful delete of `home`, or a new `work` silently adopting the old store, would contradict FR-005 and FR-006.

**Result:**

### Step 11 of 12: Drift is detected and repaired (US5)

**Why this step:** a harness can replace a link with a real file; aprofile must notice and only repair it on request.

**Needs:** Step 5.

**Run:**

```
( . specs/003-history-groups/.walkthrough/env.sh
  rm "$P/claude/lab/projects" && mkdir "$P/claude/lab/projects"
  "$A" doctor 2>&1 | grep -i 'lab' | head -3
  "$A" sync 2>&1 | grep -i 'lab' | head -3
  test -L "$P/claude/lab/projects" && echo "linked after sync" || echo "still real after sync"
  "$A" sync --relink --yes >/dev/null 2>&1
  test -L "$P/claude/lab/projects" && echo "linked after relink" || echo "still real after relink" )
```

**What you should see:** `doctor` reports a problem for `claude/lab` naming `projects/` and the `sync --relink` fix; plain `sync` reports it and names `aprofile edit claude/lab --group home`; `still real after sync`; then `linked after relink`.

**If you see something else:** `linked after sync` means plain `sync` replaced a real entry, which it must never do (FR-022).

**Result:**

### Step 12 of 12: Wrapper and registry publish the group (US5)

**Why this step:** other tools (for example a session browser) read the group from the wrapper environment and the registry.

**Needs:** Step 5.

**Run:**

```
( . specs/003-history-groups/.walkthrough/env.sh
  grep APROFILE_HISTORY_GROUP "$XDG_DATA_HOME/aprofile/bin/claude-lab" "$XDG_DATA_HOME/aprofile/bin/claude-solo"
  "$A" registry export | jq -r '.. | objects | select(has("history_group")) | "\(.harness)/\(.name) \(.history_group)"' )
```

**What you should see:** the `claude-lab` wrapper sets `APROFILE_HISTORY_GROUP` to `home` and `claude-solo` sets it empty; the registry lists `claude/lab home`, `claude/personal home`, and `claude/solo`, `claude/acme`, `claude/corp` with an empty group.

**If you see something else:** a missing variable means the wrapper was not re-rendered; run `"$A" sync` inside the sandbox and retry.

**Result:**

## Not covered here

- Resuming a real conversation with a real harness under a second account (research R3). It needs real Claude Code logins and your real home, which this sandbox journey deliberately avoids. Try it by hand with two of your own profiles in `home`: start a conversation with one wrapper and resume it with the other using `--resume <id>`.
- The TUI group select (FR-027): interactive forms are covered by the form-parity tests rather than this journey.
- Codex and pi joins: covered by the integration tests; the journey uses Claude only to keep it short.

## Coverage

- US1: Steps 5, 6.
- US2: Steps 4, 7.
- US3: Steps 8, 9.
- US4: Steps 3, 10.
- US5: Steps 11, 12.

All five user stories are covered; the cross-account resume and the TUI are listed under Not covered here.

## Results
