---
runbook_version: 1
feature: 002-profile-isolation-levels
status: passed
mode: light
steps_total: 25
step_results: passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed
drafted_at: f242a1d
drafted_fingerprint: 955dcbdc22845dedc5be9112e334bba0729041d978837cd04b9343fca40778c2
tested_at: f242a1d
tested_fingerprint: 94d0cedba5551312339ee1769ce89feca41b9656902a1dc193ef73824b51b882
updated: 2026-11-06
---

# Try it: Profile share levels and hook sets

<!-- runbook-status -->
> **Status:** passed · 25 of 25 steps (25 passed) · Mode: light · Tested at f242a1d · 2026-11-06
<!-- /runbook-status -->



























## What you're testing

Feature 002 gives every aprofile profile a share level (`all`, `content` or `none`) that decides what it borrows from the harness's default setup. Runtime state (history, sessions, databases) is never shared at any level. Hook sets let a profile pick its hooks without copying the whole `settings.json`.

The journey runs entirely inside a throwaway home folder, so your real `~/.claude`, `~/.codex` and aprofile config are never touched:

1. Build the binary and create a sandbox home with a small fake default setup.
2. Create profiles at each level and check that no runtime state is linked (US1).
3. Read the level descriptions and see what one profile shares (US2).
4. Preview and change a level, including the confirmation and backup when profile data would be replaced (US3).
5. Import a hook set, attach and detach it, and see that hand-written hooks survive (US4).
6. Refresh settings from the default setup with `settings pull` (US3).
7. Break a link by hand and let `doctor` and `sync --relink` repair it (US5).
8. Simulate a profile from before share levels and repair its leaking link (US6).
9. Try a level change while the profile is running (US3, FR-017).
10. Against your real `~/.claude`, check the two things the sandbox cannot show (T058): whether `@` imports in a linked `CLAUDE.md` still load (research R7), and whether `/ide` connects from a profile (R8).

## Test environment

No fixtures or services are needed. Every step uses a sandbox home at `specs/002-profile-isolation-levels/.walkthrough/home`, set up in Step 2 and described by `specs/002-profile-isolation-levels/.walkthrough/env.sh`. That folder ignores itself in git.

Run every command from the repository root (the feature worktree). Each command block runs in a subshell `( ... )`, so sourcing `env.sh` never changes `HOME` in your own terminal.

Teardown: `rm -rf specs/002-profile-isolation-levels/.walkthrough/home`

Steps 22 to 25 use a second scratch area, `specs/002-profile-isolation-levels/.walkthrough/real`, described by `.walkthrough/real/env.sh`. It keeps your real `HOME`, so the default setup is your real `~/.claude`, but points every aprofile location (`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`) and the shell startup file (`ZDOTDIR`) into the scratch folder. Your existing profiles, wrappers and `~/.zshrc` are never touched; aprofile only reads `~/.claude` and creates links to it.

Teardown: `rm -rf specs/002-profile-isolation-levels/.walkthrough/real` (the profile's Keychain login entry, if any, can be removed with Keychain Access by searching for `Claude Code`).

## Steps

### Step 1 of 25: Build the binary (setup)

**Why this step:** every later step runs `./bin/aprofile` from this branch.

**Run:**

```
make build && ./bin/aprofile --help | head -1
```

**What you should see:** the build finishes without errors and the help prints `Manage side-by-side profiles for coding harnesses`.

**If you see something else:** a compile error means the worktree is not in the state the review left it in; run `git status` and `make check`.

**Result:** passed (agent). Corrected during the run: the first draft used `./bin/aprofile --version`, which does not exist (`aprofile: unknown flag: --version`); the step now prints the first help line.
  ✔ build succeeds → `go build -o bin/aprofile ./cmd/aprofile`
  ✔ help line → `Manage side-by-side profiles for coding harnesses`

### Step 2 of 25: Create the sandbox home with a fake default setup (setup)

**Why this step:** aprofile borrows from `~/.claude` and `~/.codex`. A fake default setup in a sandbox home shows the behavior without risking your real one.

**Needs:** Step 1.

**Run:**

```
mkdir -p specs/002-profile-isolation-levels/.walkthrough && cat > specs/002-profile-isolation-levels/.walkthrough/env.sh <<'EOF'
W="$(pwd)/specs/002-profile-isolation-levels/.walkthrough"
export HOME="$W/home" XDG_CONFIG_HOME="$W/home/.config" XDG_DATA_HOME="$W/home/.local/share" XDG_STATE_HOME="$W/home/.local/state" ZDOTDIR="$W/home" SHELL=/bin/zsh
A="$(pwd)/bin/aprofile"
P="$XDG_DATA_HOME/aprofile/profiles"
EOF
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  test ! -e "$HOME" || { echo "sandbox already exists: $HOME"; exit 1; }
  mkdir -p "$HOME/.claude/skills/review" "$HOME/.claude/plugins" "$HOME/.claude/hooks" "$HOME/.codex" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_STATE_HOME"
  echo '# default instructions' > "$HOME/.claude/CLAUDE.md"
  echo '# review skill' > "$HOME/.claude/skills/review/SKILL.md"
  echo '{"permissions":{"allow":["Bash(ls:*)"]},"statusLine":{"type":"command","command":"x"},"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"'"$HOME"'/.claude/hooks/rtk.sh"}]}]}}' > "$HOME/.claude/settings.json"
  printf '#!/bin/sh\n' > "$HOME/.claude/hooks/rtk.sh"; chmod +x "$HOME/.claude/hooks/rtk.sh"
  echo '{"display":"default prompt"}' > "$HOME/.claude/history.jsonl"
  echo db > "$HOME/.codex/memories_1.sqlite"; echo wal > "$HOME/.codex/memories_1.sqlite-wal"
  cd "$HOME" && find . -type f | sort )
```

**What you should see:** a sorted list of the seeded files: `./.claude/CLAUDE.md`, `./.claude/history.jsonl`, `./.claude/hooks/rtk.sh`, `./.claude/settings.json`, `./.claude/skills/review/SKILL.md`, `./.codex/memories_1.sqlite` and `./.codex/memories_1.sqlite-wal`.

**If you see something else:** "sandbox already exists" means an earlier run left it behind; remove it with the teardown command and rerun.

**Result:** passed (agent).
  ✔ seeded files → the seven expected paths, from `./.claude/CLAUDE.md` to `./.codex/memories_1.sqlite-wal`

### Step 3 of 25: Create one profile per share level and sync (US1)

**Why this step:** the same default setup is borrowed three different ways, so the difference between levels is visible side by side.

**Needs:** Step 2.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  for lvl in all content none; do "$A" add claude t-$lvl --auth-kind oauth-login --share $lvl || exit 1; done
  "$A" add codex c-content --auth-kind chatgpt-login --share content
  "$A" sync && "$A" list )
```

**What you should see:** four profiles are added, `sync` creates their config dirs and wrappers without errors, and `list` shows a share column with `all`, `content`, `none` for `claude/t-*` and `content` for `codex/c-content`.

**If you see something else:** a prompt or form opening means `add` wanted a missing field; an error naming `share` means the flag is not recognized.

**Result:** passed (agent). Output quoted with the sandbox path shortened to `~sandbox`.
  ✔ four profiles added → `created profile claude/t-all ... share level: all: Uses your whole setup live; ...` (and t-content, t-none, codex/c-content)
  ✔ sync creates dirs and wrappers → `wrapper claude-t-all: written` ... `registry: updated`; the PATH block went to `~sandbox/.zshrc`
  ✔ list shows levels → `t-all  [synced] share: all`, `t-content  [synced] share: content`, `t-none  [synced] share: none`, `c-content  [synced] share: content`

### Step 4 of 25: Check that no runtime state is linked at any level (US1)

**Why this step:** this is the core promise of the feature (SC-001). Linking `history.jsonl` or a SQLite database into the default setup lets two running profiles corrupt each other's state.

**Needs:** Step 3.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  cd "$P" && find . -type l | sort | while read -r l; do echo "$l -> $(readlink "$l")"; done )
```

**What you should see:** links only for borrowable entries. `claude/t-all` links `CLAUDE.md`, `skills`, `plugins` and `settings.json` (possibly a few other content entries such as `hooks`). `claude/t-content` links `CLAUDE.md`, `skills` and `plugins` but not `settings.json`. Nothing under `claude/t-none` is a link. No line names `history.jsonl`, `sessions` or any `*.sqlite*` file.

**If you see something else:** any `history.jsonl` or `memories_1.sqlite*` link is the bug this feature fixes; a link under `t-none` violates the `none` level.

**Result:** passed (agent).
  ✔ t-all links → `CLAUDE.md`, `plugins`, `settings.json`, `skills`
  ✔ t-content links → `CLAUDE.md`, `plugins`, `skills` (no `settings.json`)
  ✔ t-none, c-content → no links
  ✔ no runtime state linked → no `history.jsonl`, `sessions` or `*.sqlite*` line

### Step 5 of 25: An unknown entry is reported, never linked (US1)

**Why this step:** a file the harness definition does not know about must stay untouched and be named, so a new kind of runtime state never leaks silently.

**Needs:** Step 3.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  echo '{"mine":true}' > "$P/claude/t-content/foo.json"
  "$A" sync; echo "---"; cat "$P/claude/t-content/foo.json" )
```

**What you should see:** `sync` reports `foo.json` in `claude/t-content` as unclassified; after the `---` the file still contains `{"mine":true}`.

**If you see something else:** no report means unknown entries go unnoticed; a changed file means sync touched data it should leave alone.

**Result:** passed (agent).
  ✔ reported → `report .../t-content/foo.json (unclassified: left in place, never linked or copied; classify it with: aprofile harness classify claude foo.json --as <category>)`
  ✔ untouched → `{"mine":true}`

### Step 6 of 25: Read the level descriptions (US2)

**Why this step:** a person picks a level from a use case, so the help must explain each level in plain words, plus where the harnesses differ.

**Needs:** Step 1.

**Run:**

```
./bin/aprofile help sharing
```

**What you should see:** a description and a use case for each of `all`, `content` and `none`, a statement that runtime state is never shared, and a section on how the harnesses differ (for example that Codex's `config.toml` stays private).

**If you see something else:** "unknown help topic" means the `sharing` topic is not registered.

**Result:** passed (agent).
  ✔ level descriptions and use cases → UC1 to UC7 table with a level per use case
  ✔ runtime state private → `At every level, logins, history, sessions and memory are always private to the profile`
  ✔ harness differences → `config.toml is private at every level, all included: it holds the MCP servers`

### Step 7 of 25: See what a content profile shares (US2)

**Why this step:** `show` should answer "what does this profile borrow, and what is its own" without reading symlinks by hand.

**Needs:** Step 3.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  "$A" show claude/t-content; echo "---"; jq 'has("hooks")' "$P/claude/t-content/settings.json" )
```

**What you should see:** `share level:    content: ...` with its description, then entries grouped as private, linked to the default setup (`CLAUDE.md`, `skills`, `plugins`) and the profile's own (`settings.json`), with `foo.json` under unclassified. After the `---`, `false`: the profile's copy of `settings.json` has no `hooks` key.

**If you see something else:** `true` means the default setup's hooks were copied into a content profile, which hook sets are meant to replace.

**Result:** passed (agent).
  ✔ level line → `share level:    content: Uses your instructions and skills; keeps its own settings and hooks.`
  ✔ linked → `CLAUDE.md`, `plugins`, `skills`; own → `settings.json`, `settings.local.json`, `.claude.json`; unclassified → `foo.json`
  ✔ no hooks in the copy → `false`
  Observation: entries the default setup lacks (`agents`, `commands`, `keybindings.json`, `output-styles`, `themes`) are listed under "linked to the default setup" with `[missing-link-target]`, although nothing is linked. Also `not borrowed ... hooks (fix: aprofile harness classify claude hooks --as content)`: the built-in claude definition does not classify `~/.claude/hooks/`.

### Step 8 of 25: Preview a move from all to content (US3)

**Why this step:** a level change must be previewable before anything changes (FR-016, SC-006).

**Needs:** Step 3.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  "$A" edit claude/t-all --share content --dry-run; echo "---"; ls -l "$P/claude/t-all/settings.json" )
```

**What you should see:** a `(dry-run)` plan with `unlink settings.json` and `copy-once settings.json`, a list of the default setup's hooks (`PreToolUse (1 group)`) with the hint `aprofile hook-sets import claude`, and a line saying nothing is deleted. After the `---`, `settings.json` is still a symlink (`->` in the listing).

**If you see something else:** `settings.json` no longer being a link means the dry run changed something.

**Result:** passed (agent).
  ✔ plan → `unlink settings.json`, `copy-once settings.json (copied once from the default setup (hooks excluded))`
  ✔ hooks hint → `default setup hooks not copied: PreToolUse (1 group); to keep them, run: aprofile hook-sets import claude --name <set>`
  ✔ nothing deleted → `profile data replaced: no; nothing is deleted`
  ✔ unchanged → `settings.json ⇒ ~sandbox/.claude/settings.json` (still a link)

### Step 9 of 25: Apply the move to content (US3)

**Why this step:** the applied change should do exactly what the dry run listed.

**Needs:** Step 8.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  "$A" edit claude/t-all --share content; echo "---"; ls -l "$P/claude/t-all/settings.json"; jq -c . "$P/claude/t-all/settings.json" )
```

**What you should see:** the same plan lines as Step 8 without `(dry-run)`, no confirmation prompt (nothing is replaced), then `settings.json` as a regular file whose content is the default setup's settings without the `hooks` key.

**If you see something else:** a prompt means the move was wrongly classed as replacing profile data; a `hooks` key in the copy means copy-once kept the hooks.

**Result:** passed (agent).
  ✔ same plan lines as the dry run, no prompt → `updated profile claude/t-all ... share level: all -> content`
  ✔ own file without hooks → `.rw-r--r-- ... settings.json`, `{"permissions":{"allow":["Bash(ls:*)"]},"statusLine":{"type":"command","command":"x"}}`

### Step 10 of 25: Moving back to all needs confirmation and a backup (US3)

**Why this step:** moving to `all` replaces the profile's own `settings.json` with the shared one. That must never happen silently (FR-017, SC-007): off a terminal it exits 3, and with `--yes` it backs up first.

**Needs:** Step 9.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  "$A" edit claude/t-all --share all </dev/null; echo "exit $?"
  "$A" show claude/t-all | grep 'share level'
  "$A" edit claude/t-all --share all --yes; echo "---"
  ls -l "$P/claude/t-all/settings.json"; find "$XDG_DATA_HOME/aprofile/backups" -type f )
```

**What you should see:** the first edit prints the plan with `replace-with-link settings.json` marked as replacing profile data and ends with `exit 3`; `show` still says `content`. The `--yes` run reports `backed up settings.json to ...`. After the `---`, `settings.json` is a symlink again and one backup file `.../backups/claude/t-all/.../settings.json` exists.

**If you see something else:** `exit 0` on the first call means it changed data without confirmation; no backup file means profile data was replaced without one.

**Result:** passed (agent).
  ✔ marked → `replace-with-link settings.json -> ... [replaces profile data]`
  ✔ refused off a terminal → `confirmation needed; re-run with --yes (nothing changed)`, `exit 3`
  ✔ level unchanged → `share level:    content: ...`
  ✔ backup → `backed up settings.json to ~sandbox/.local/share/aprofile/backups/claude/t-all/20261105-082918.726320`
  ✔ link restored → `settings.json ⇒ ~sandbox/.claude/settings.json`; one backup file `.../t-all/20261105-082918.726320/settings.json`
  Note: the `grep 'share level'` also matched entry reasons that contain the words; harmless.

### Step 11 of 25: Import the default setup's hooks as a hook set (US4)

**Why this step:** hook sets let content and none profiles get chosen hooks. Importing turns the default setup's hooks into a reusable set.

**Needs:** Step 2.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  "$A" hook-sets import claude --name base && "$A" hook-sets list && cat "$XDG_CONFIG_HOME/aprofile/hooks.d/base.yaml" )
```

**What you should see:** the import reports a new set `base`, `hook-sets list` shows it with a claude body, and `base.yaml` contains a `PreToolUse` group with matcher `Bash` and the `rtk.sh` command.

**If you see something else:** an error about secrets means the import found something that looks like a token in the hooks.

**Result:** passed (agent).
  ✔ import → `created hook set base from the claude default setup (1 event(s)) in ~sandbox/.config/aprofile/hooks.d/base.yaml`
  ✔ list → `base  [claude]` `used by: no profile`
  ✔ body → `PreToolUse`, `matcher: Bash`, `command: ~sandbox/.claude/hooks/rtk.sh`

### Step 12 of 25: Attach the hook set to a content profile (US4)

**Why this step:** attaching a set should place exactly its groups into the profile's own `settings.json` and record them as managed.

**Needs:** Steps 7 and 11.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  "$A" edit claude/t-content --hook-sets base && "$A" sync
  jq -c '.hooks' "$P/claude/t-content/settings.json"
  cat "$XDG_DATA_HOME/aprofile/managed-hooks/claude/t-content.json" )
```

**What you should see:** `.hooks` holds one `PreToolUse` group with matcher `Bash` running `rtk.sh`, and the managed-hooks record lists that group under set `base` with a `sha256:` fingerprint.

**If you see something else:** no `hooks` key means the set was not placed; two identical groups mean placement is not idempotent.

**Result:** passed (agent).
  ✔ placed → `hooks claude/t-content: place-hook settings.json#hooks (managed hook entries of: base; entries aprofile did not place are kept)`
  ✔ settings → `{"PreToolUse":[{"hooks":[{"command":"~sandbox/.claude/hooks/rtk.sh","type":"command"}],"matcher":"Bash"}]}`
  ✔ record → `"set": "base"`, `"fingerprint": "sha256:abf89f36..."`, `"matcher": "Bash"`

### Step 13 of 25: An all profile refuses a hook set (US4)

**Why this step:** an `all` profile uses the default setup's hooks directly, so attaching a set is invalid for its level (exit 2).

**Needs:** Steps 10 and 11.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  "$A" edit claude/t-all --hook-sets base; echo "exit $?" )
```

**What you should see:** an error saying profiles at share level `all` use the default setup's hooks, then `exit 2`.

**If you see something else:** `exit 0` means the set was attached to a profile whose settings are shared.

**Result:** passed (agent).
  ✔ refused → `profile claude/t-all uses share level all, which uses the hooks of the default setup; hook sets need --share content or --share none`, `exit 2`

### Step 14 of 25: A hand-written hook survives detaching the set (US4)

**Why this step:** aprofile must only remove the entries it placed (SC-008); a group you added yourself stays.

**Needs:** Step 12.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  S="$P/claude/t-content/settings.json"
  jq '.hooks.Stop = [{"matcher":"mine","hooks":[{"type":"command","command":"echo mine"}]}]' "$S" > "$S.new" && mv "$S.new" "$S"
  "$A" edit claude/t-content --hook-sets "" && "$A" sync
  jq -c '.hooks' "$S"; "$A" show claude/t-content | grep 'hook sets' )
```

**What you should see:** `.hooks` contains only the `Stop` group with matcher `mine`; the `PreToolUse` group from `base` is gone. `show` reports `hook sets:      none`.

**If you see something else:** the `mine` group missing means aprofile removed an entry it never placed.

**Result:** passed (agent).
  ✔ only base removed → `remove-hook settings.json#hooks (removed managed hook entries of: base; entries aprofile did not place are kept)`
  ✔ own group kept → `{"Stop":[{"matcher":"mine","hooks":[{"type":"command","command":"echo mine"}]}]}`
  ✔ no sets → `hook sets:      none`

### Step 15 of 25: Refresh settings with settings pull (US3)

**Why this step:** a content profile keeps its own settings; `settings pull` refreshes them from the default setup. A merge keeps profile-only values, `--replace` drops them, and both preview first.

**Needs:** Step 14.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  S="$P/claude/t-content/settings.json"
  jq '.permissions.allow += ["Bash(make test)"]' "$S" > "$S.new" && mv "$S.new" "$S"
  echo "== merge"; "$A" settings pull claude/t-content --dry-run
  echo "== replace"; "$A" settings pull claude/t-content --replace --dry-run
  echo "== file"; jq -c '.permissions' "$S" )
```

**What you should see:** the merge preview does not list `Bash(make test)` as removed (it may say there is nothing to change). The `--replace` preview lists the `permissions` change that drops it and does not touch the `hooks` key. The file still contains `Bash(make test)`, because both were dry runs.

**If you see something else:** the merge preview removing `Bash(make test)` means merge behaves like replace; a changed file means a dry run wrote.

**Result:** passed (agent).
  ✔ merge keeps profile-only values → `nothing to change: the profile's settings already match`
  ✔ replace lists the removal → `removed: permissions.allow["Bash(make test)"]`, `the hook location and owned keys are never touched`
  ✔ dry runs wrote nothing → `{"allow":["Bash(ls:*)","Bash(make test)"]}`

### Step 16 of 25: doctor finds a link replaced by a file (US5)

**Why this step:** if a profile stops sharing something by accident (an editor saved a copy over a link), `doctor` should say so and name the fix.

**Needs:** Step 3.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  rm "$P/claude/t-content/CLAUDE.md" && cp "$HOME/.claude/CLAUDE.md" "$P/claude/t-content/CLAUDE.md"
  "$A" doctor; echo "exit $?" )
```

**What you should see:** a problem for `claude/t-content`: the profile stopped sharing `CLAUDE.md` because the link was replaced by a real file, with a fix naming `aprofile sync --relink`. `foo.json` from Step 5 is also reported. The exit status is non-zero.

**If you see something else:** `exit 0` with no `CLAUDE.md` finding means drift goes unnoticed.

**Result:** passed (agent).
  ✔ problem → `[claude/t-content] the profile stopped sharing CLAUDE.md: the link was replaced by a real file` `fix: aprofile sync --relink (backs the file up and asks first)`
  ✔ foo.json → `[claude/t-content] unclassified config-dir entry "foo.json"`
  ✔ non-zero → `aprofile doctor found 3 problem(s)`, `exit 1` (the third problem is the sandbox bin dir not being on PATH, expected here)
  ✔ information, not problems → `info: [codex/c-content] ~/.agents/skills (skills) is read from $HOME ...`, `info: ... settings differ from the default setup`

### Step 17 of 25: sync --relink repairs it with a backup (US5)

**Why this step:** the repair replaces profile data, so it must back the file up first.

**Needs:** Step 16.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  "$A" sync --relink --yes; echo "---"
  ls -l "$P/claude/t-content/CLAUDE.md"; find "$XDG_DATA_HOME/aprofile/backups/claude/t-content" -type f )
```

**What you should see:** sync reports relinking `CLAUDE.md` and a backup location; `CLAUDE.md` is a symlink to the default setup again and a backup copy of the file exists under `backups/claude/t-content/`.

**If you see something else:** `CLAUDE.md` still a regular file means `--relink` did not run the confirmed step.

**Result:** passed (agent).
  ✔ relink with backup → `replace-with-link CLAUDE.md -> ~sandbox/.claude/CLAUDE.md ... [replaces profile data]`, `backed up to .../backups/claude/t-content/20261105-082945.668961`
  ✔ link restored → `CLAUDE.md ⇒ ~sandbox/.claude/CLAUDE.md`; backup file `.../20261105-082945.668961/CLAUDE.md`

### Step 18 of 25: $HOME escapes are reported as information (US5)

**Why this step:** some harnesses read folders outside their config dir (Codex reads `~/.agents/skills`). aprofile cannot isolate those, so it must say so honestly, as information rather than a problem.

**Needs:** Step 3.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  "$A" show codex/c-content; echo "---"; "$A" harness show codex )
```

**What you should see:** `show` has a `$HOME escapes:` group naming `~/.agents/skills`. `harness show codex` explains what each level shares, states that `config.toml is private at every level`, and lists `~/.agents/skills` as a `$HOME` escape.

**If you see something else:** no escape listed means users would believe a none profile is fully isolated when it is not.

**Result:** passed (agent).
  ✔ show → `$HOME escapes:` `~/.agents/skills             skills read from $HOME: shared with every profile; no share level can separate this`
  ✔ harness show → per-level table (`content link link own`, `settings link copy-once minimal`, ...), `config.toml is private at every level, all included`, `$HOME escapes ... ~/.agents/skills (skills)`
  Observation: `rules` is shown as "the profile's own copy, taken from the default setup once" although the default setup had no `rules` and sync created it as minimal.

### Step 19 of 25: A profile from before share levels keeps working (US6)

**Why this step:** profiles created by slice one have no stored level and may hold a leaking `history.jsonl` link. The upgrade must treat them as `all`, keep their content links, never rewrite their file, and report the leak.

**Needs:** Step 2.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  "$A" add claude legacy --auth-kind oauth-login --share all && "$A" sync >/dev/null
  F="$XDG_CONFIG_HOME/aprofile/profiles/claude/legacy.yaml"
  grep -v '^share:' "$F" > "$F.new" && mv "$F.new" "$F"
  ln -s "$HOME/.claude/history.jsonl" "$P/claude/legacy/history.jsonl"
  "$A" show claude/legacy | head -5; echo "---"
  "$A" sync; echo "---"; grep -c '^share:' "$F" )
```

**What you should see:** `show` reports `share level:    all` followed by the hint that profiles from before share levels are `all` and the command `aprofile edit claude/legacy --share content --dry-run`. `sync` reports `history.jsonl` as a `leaking-link` and names `--fix-links`. The final count is `0`: the profile file was not rewritten.

**If you see something else:** a count of `1` means loading rewrote the file; no leak report means the slice one leak goes unnoticed.

**Result:** passed (agent). Corrected during the run: `head -4` cut off the hint, which is the fifth line; the step now uses `head -5`.
  ✔ implicit all with hint → `share level:    all: ...` then `(no level stored: profiles from before share levels are all; consider: aprofile edit claude/legacy --share content --dry-run)`
  ✔ leak reported → `history.jsonl (leaking-link: state entry is a link -> ~sandbox/.claude/history.jsonl; run: aprofile sync --fix-links)`
  ✔ file not rewritten → `0`

### Step 20 of 25: sync --fix-links repairs the leak without touching the default setup (US6)

**Why this step:** the fix removes the link (never its target) and starts the profile with an empty private history.

**Needs:** Step 19.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  "$A" sync --fix-links; echo "---"
  ls -l "$P/claude/legacy/history.jsonl"; wc -c < "$P/claude/legacy/history.jsonl"; cat "$HOME/.claude/history.jsonl" )
```

**What you should see:** sync says it fixed the leaking link and that earlier data stays in the default setup. `history.jsonl` in the profile is now a regular, empty file (`0` bytes), and the default setup's `history.jsonl` still contains `{"display":"default prompt"}`.

**If you see something else:** an empty default `history.jsonl` means the link target was truncated, which would be data loss.

**Result:** passed (agent).
  ✔ fixed → `fix-link .../legacy/history.jsonl (replaced the leaking link with an empty private entry; the link target is unchanged and earlier data stays in the default setup)`
  ✔ empty private file → `.rw------- ... 0 B ... history.jsonl`, `0`
  ✔ default untouched → `{"display":"default prompt"}`

### Step 21 of 25: A level change is refused while the profile runs (US3)

**Why this step:** changing links under a running harness could corrupt its state, so `edit --share` refuses with exit 4 when a process started for that profile is running (FR-017, research R11). This is the macOS detection path.

**Needs:** Step 3.

**Run:**

```
( . specs/002-profile-isolation-levels/.walkthrough/env.sh
  APROFILE_CONFIG_DIR="$P/claude/t-none" python3 -c 'import time; time.sleep(20)' & pid=$!
  sleep 1; "$A" edit claude/t-none --share content --yes; echo "exit $?"; kill $pid )
```

**What you should see:** an error saying `claude/t-none` is running, naming the Python process id, then `exit 4`.

**If you see something else:** `exit 0` with a plan means the process was not detected. On macOS, Apple platform binaries (such as `/usr/bin/python3`) hide their environment from other processes, so if `python3` resolves to `/usr/bin/python3`, rerun with a Homebrew Python (`/opt/homebrew/bin/python3`).

**Result:** passed (agent). `python3` resolved to `/opt/homebrew/bin/python3`.
  ✔ refused → `profile claude/t-none is in use by a running process (pid 73111 .../Python.app/Contents/MacOS/Python); stop it before changing its share level`
  ✔ exit → `exit 4`
  ✔ nothing changed afterwards → 0 links under `t-none`, profile file still `share: none`

### Step 22 of 25: Create a content profile against your real setup (T058)

**Why this step:** R7 and R8 can only be checked with a real Claude Code session. This creates a throwaway `claude/t058` content profile whose `CLAUDE.md` is a link to your real `~/.claude/CLAUDE.md`, without touching your aprofile setup.

**Needs:** Step 1 (the built binary).

**Run:**

```
( W="$(pwd)/specs/002-profile-isolation-levels/.walkthrough/real"; . "$W/env.sh"
  "$A" add claude t058 --share content --auth-kind oauth-login && "$A" sync >/dev/null 2>&1
  readlink "$D/CLAUDE.md"; ls "$D/RTK.md" 2>&1 | tail -1; rg -n '^@' ~/.claude/CLAUDE.md )
```

**What you should see:** the link target `~/.claude/CLAUDE.md` (absolute), a "No such file" line for `RTK.md` inside the profile (it is not classified, so it is not linked), and the `@RTK.md` import line of your `CLAUDE.md`.

**If you see something else:** an existing `RTK.md` in the profile means a classification rule already covers it, and the R7 question cannot be observed with this file; pick another `@`-imported file.

**Result:** passed (agent, 2026-11-06). The agent ran `add` and `sync` with these settings just before writing this step; the read-only checks were then run again:
  ✔ link target → `/Users/rhuss/.claude/CLAUDE.md`
  ✔ no `RTK.md` in the profile → empty `ls` output (this shell's `ls` is `lsd`, which prints its "No such file" error differently)
  ✔ import line → `232:@RTK.md`
  ✔ `show claude/t058` → `share level:    content`

### Step 23 of 25: Log the profile in (T058)

**Why this step:** credentials are private at every level, so the new profile needs its own login before Claude Code starts a session with it.

**Needs:** Step 22.

**Run:**

```
( W="$(pwd)/specs/002-profile-isolation-levels/.walkthrough/real"; . "$W/env.sh"; "$XDG_DATA_HOME/aprofile/bin/claude-t058" auth login )
```

**What you should see:** Claude Code's own login flow (a browser window), ending with a successful login. Afterwards `claude-t058 auth status` reports `"loggedIn": true`.

**If you see something else:** a "Not logged in" error in Step 24 means the credentials did not reach this config dir; check `auth status`.

**Result:** passed (user, 2026-11-06), after a runbook correction. The step first used `aprofile login claude/t058`, which started a plain `claude` session instead of the login flow, so nothing was stored (`auth status`: `"loggedIn": false`, no Keychain entry for the config dir). This is slice one's known gap FR-039, tracked as 001 task T066, not a 002 issue. The command now calls the wrapper with `auth login`; after it, `auth status` reported `"loggedIn": true`, `"authMethod": "claude.ai"`.

### Step 24 of 25: Does an `@` import in a linked CLAUDE.md load? (R7)

**Why this step:** with the catch-all link gone, `RTK.md` exists only in `~/.claude`, not in the profile. If Claude Code resolves `@RTK.md` next to the link (in the profile), the import silently breaks; if it resolves it next to the link target (`~/.claude`), it keeps working.

**Needs:** Step 23.

**Run:**

```
( W="$(pwd)/specs/002-profile-isolation-levels/.walkthrough/real"; . "$W/env.sh"
  "$XDG_DATA_HOME/aprofile/bin/claude-t058" -p "Without using any tools: is a file named RTK.md part of the instructions you were given? If yes, reply with its first line exactly. If no, reply NO." )
```

**What you should see:** either answer is a finding, recorded in research.md R7:

- `# RTK - Rust Token Killer`: imports resolve against the link target, so linked `CLAUDE.md` imports keep working without any rule.
- `NO`: imports resolve next to the link. Then run `( W="$(pwd)/specs/002-profile-isolation-levels/.walkthrough/real"; . "$W/env.sh"; "$A" harness classify claude RTK.md --as content && "$A" sync >/dev/null 2>&1 )` and repeat the command above; it should now print the heading, which confirms that R7's fix restores the import.

**If you see something else:** an authentication error means Step 23 did not complete.

**Result:** passed (agent, 2026-11-06).
  ✔ answer → `Yes. Its first line is: # RTK - Rust Token Killer`

Finding for R7: Claude Code resolves `@` imports against the link target (`~/.claude`), not next to the link. A linked `CLAUDE.md` keeps its imports without a classification rule for the imported file; `harness classify` stays useful only for files a harness reads directly.

### Step 25 of 25: Does /ide connect from a profile? (R8)

**Why this step:** IDE extensions write lock files into the config dir of the environment the IDE was started from. The README says an IDE started from a shell with the profile's environment connects to the profile, and one started normally connects only to the default setup.

**Needs:** Step 23, Cursor with the Claude Code extension installed.

**Do:**

1. Quit Cursor completely (Cmd+Q), so the next start does not reuse a running instance.
2. In a terminal at the repository root, start Cursor with the profile's config dir: `W="$(pwd)/specs/002-profile-isolation-levels/.walkthrough/real"; . "$W/env.sh"; CLAUDE_CONFIG_DIR="$D" /Applications/Cursor.app/Contents/MacOS/Cursor "$(pwd)" >/dev/null 2>&1 &`
3. In that Cursor window, open the integrated terminal and run `"$XDG_DATA_HOME/aprofile/bin/claude-t058"`, then type `/ide`.
4. In a separate terminal at the repository root, run `ls specs/002-profile-isolation-levels/.walkthrough/real/data/aprofile/profiles/claude/t058/ide`.
5. Afterwards quit this Cursor and start it normally again.

**What you should see:** `/ide` offers Cursor and connects. The profile's `ide/` folder holds a `.lock` file written by the extension.

**If you see something else:** `/ide` finding no IDE while the lock file sits in `~/.claude/ide` instead means the extension ignores `CLAUDE_CONFIG_DIR`, so the README claim is wrong and R8 needs a different decision. No lock file anywhere means the Claude Code extension is not installed in Cursor, or is too old to start.

**Result:** passed (user and agent, 2026-11-06), on the second try.
  First try, inconclusive: Cursor ran with the profile's `CLAUDE_CONFIG_DIR` (checked in its process environment), but `/ide` reported "No available IDEs detected" and no lock file existed in the profile or in `~/.claude/ide`. Cursor had the extension `anthropic.claude-code-1.0.35`, which never started its IDE server.
  Second try, after the user updated the extension to `2.1.318` and opened the repository in a normal editor window of the same Cursor process:
  ✔ lock file in the profile → `ide/31378.lock` (mode 600): `{"pid":19835,"ideName":"Cursor","workspaceFolders":["/Users/rhuss/Development/ai/aprofile"],"transport":"ws"}`; pid 19835 is the Cursor started with the profile's environment
  ✔ nothing in the default setup → `~/.claude/ide` empty
  Observation (not a 002 failure): the session in Cursor reported "Google Vertex AI" as its provider although the profile is logged in with claude.ai. The user's shell exports `CLAUDE_CODE_USE_VERTEX=1`, Cursor inherited it, and the wrapper clears only `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` and `CLAUDE_CODE_OAUTH_TOKEN`, so a provider switch from the shell overrides a profile's login. This belongs to slice one's auth isolation (spec 001).

## Not covered here

- **Running detection on Linux**: this machine is macOS. The `/proc` path was checked by running its tests in a Linux container (research.md, R11 row), not by a step here.
- **The upgrade with the real slice one binary**: Step 19 simulates a slice one profile; building commit `9d6faf8` and syncing with it is left to the quickstart.

## Coverage

- **US1** (runtime state never shared): Steps 3, 4, 5.
- **US2** (pick a level from a use case): Steps 6, 7. The interactive `add` form is not exercised; it needs a terminal UI.
- **US3** (see, preview and refresh): Steps 8, 9, 10, 15, 21.
- **US4** (hook sets): Steps 11 to 14. The files form (OpenCode plugins, pi extensions) is covered by the automated tests only.
- **US5** (keep the setup honest): Steps 16, 17, 18.
- **US6** (upgrade): Steps 19, 20, simulated as described above.
- **T058** (manual checks): Steps 22 to 25 cover R7 and R8 against the real setup; R11 on macOS is Step 21.

## Results

Update 2026-11-06: Steps 22 to 25 were added for the manual checks of T058 and run against the real `~/.claude` (with aprofile's own locations in a scratch folder). 25 steps: 25 passed, 0 failed, 0 skipped. Step 23 needed a runbook correction (`aprofile login` does not run Claude Code's login flow, a known slice one gap), and Step 25 passed on the second try after updating Cursor's Claude Code extension. Findings: `@` imports in a linked `CLAUDE.md` resolve against the link target (R7), and an IDE started with a profile's environment writes its lock file into that profile (R8). Steps 1 to 21 were not rerun after the later 002 fixes; the automated suite covers those changes.

21 steps: 21 passed, 0 failed, 0 skipped. The agent ran every step in a sandbox home on macOS (2026-11-05). Two runbook mistakes were corrected along the way (Step 1 used a nonexistent `--version` flag; Step 19 cut off one line too early); the feature itself behaved as expected in both.

What a reader now knows the feature does:

- No runtime state is linked at any level; only content, settings, the plugin store and the hook location are, and `none` links nothing.
- Level changes print the same plan in the dry run and the real run. A move that replaces profile data exits 3 off a terminal, and with `--yes` it backs the data up first. A running profile is refused with exit 4.
- Hook sets are placed and removed exactly; a hook group you add by hand survives detaching a set. `all` profiles refuse hook sets with exit 2.
- `settings pull` merges by default and only `--replace` drops profile-only values; both preview with `--dry-run`.
- `doctor` reports a link replaced by a file as a problem with its fix, and `$HOME` escapes as information. `sync --relink` and `sync --fix-links` repair without touching the default setup.
- A profile file with no stored level is treated as `all`, is not rewritten, and `show` suggests a move to `content`.

Observations (not failures), all three fixed after the walkthrough and checked again in this sandbox:

- `show` listed entries the default setup lacks (for example `agents`, `commands`, codex `skills`) under "linked to the default setup" with `[missing-link-target]`, although nothing was linked. Fixed: they now appear under "not in the default setup (linked once it is)". `show --json` keeps the documented `linked` / `missing-link-target`.
- The built-in claude definition did not classify `~/.claude/hooks/`. Fixed: `hooks/` is content, so `sync` links it and `doctor` no longer lists it as not borrowed.
- `show` described a copy-once entry with nothing to copy (codex `rules`, claude `settings.local.json`) as "taken from the default setup once". Fixed: it now says "started minimal; the default setup has none to copy".

With the `hooks/` change, Step 4 also lists a `hooks` link for `t-all` and `t-content` (the step already allowed for it).
