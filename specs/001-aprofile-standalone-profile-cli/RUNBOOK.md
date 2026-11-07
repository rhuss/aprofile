---
runbook_version: 1
feature: 001-aprofile-standalone-profile-cli
status: passed
mode: light
steps_total: 13
step_results: passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed,passed
drafted_at: 483957d
drafted_fingerprint: c1c5479df760f2a1df8a94e6f1283fec614b481aca2c63793fe33d941fb83c3a
tested_at: 483957d
tested_fingerprint: c1c5479df760f2a1df8a94e6f1283fec614b481aca2c63793fe33d941fb83c3a
updated: 2026-11-06
---

# Try it: aprofile standalone profile CLI (slice one gap fixes)

<!-- runbook-status -->
> **Status:** passed · 13 of 13 steps (13 passed) · Mode: light · Tested at 483957d · 2026-11-06
<!-- /runbook-status -->














## What you're testing

Slice one gives every coding harness (Claude Code, Codex, OpenCode, pi) any number of side-by-side profiles, each with its own config dir and a wrapper command. This journey tries the slice one behaviour that the Phase 11 gap fixes and the deep review changed: what a new profile inherits from the default setup, what the wrapper and `aprofile login` put into the environment, how MCP servers are converted between harnesses without leaking secrets, how `copy` treats an existing target, and how `sync`, `list` and `doctor` report broken or leftover profiles. It ends with a dry-run import of your real cc-deck profiles.

The journey runs entirely inside a throwaway home folder, except for the last step, which only reads your cc-deck config:

1. Build aprofile and a stub harness, create a sandbox home with a small default setup.
2. Create a Claude profile and see what it inherits (US1, US2).
3. Start it through its wrapper and log it in (US1, US2).
4. Copy it to Codex and pi and inspect the converted MCP servers (US4).
5. Overwrite an existing copy and copy onto a leftover dir (US4).
6. Break a profile by hand and remove another, then let `doctor`, `list` and `sync` report and repair (US1, US5).
7. Read the machine-readable output (US3).
8. Dry-run an import of your real cc-deck profiles (US6).

## Test environment

No services are needed. Every step uses the sandbox home `specs/001-aprofile-standalone-profile-cli/.walkthrough/home`, set up in Step 2 and described by `specs/001-aprofile-standalone-profile-cli/.walkthrough/env.sh` (that folder ignores itself in git). Harness binaries are a stub that prints its arguments and environment (`tests/fixtures/stub`), so nothing real starts.

Run every command from the repository root (the 001 worktree). Each command block runs in a subshell `( ... )`, so sourcing `env.sh` never changes your own terminal.

Teardown: `rm -rf specs/001-aprofile-standalone-profile-cli/.walkthrough`

## Steps

### Step 1 of 13: Build aprofile and the stub harnesses (setup)

**Why this step:** every later step runs this build of aprofile, and stub binaries named `claude`, `codex` and `pi` stand in for the real harnesses so nothing real is launched.

**Needs:** Go 1.24.

**Run:**

```
( W=specs/001-aprofile-standalone-profile-cli/.walkthrough; mkdir -p $W/bin && make build >/dev/null &&
  go build -o $W/bin/claude ./tests/fixtures/stub && cp $W/bin/claude $W/bin/codex && cp $W/bin/claude $W/bin/pi &&
  ls $W/bin && ./bin/aprofile --help | head -1 )
```

**What you should see:** `claude codex pi` listed, then aprofile's one-line description ("Manage side-by-side profiles for coding harnesses").

**If you see something else:** a build error means the tree does not compile; stop here.

**Result:** passed (agent, 2026-11-06).
  ✔ stubs listed → `claude`, `codex`, `pi`
  ✔ aprofile builds → `Manage side-by-side profiles for coding harnesses`

### Step 2 of 13: Create the sandbox home with a default setup (setup)

**Why this step:** aprofile borrows from the "default setup" (`~/.claude`, `~/.claude.json`). The sandbox gives it a small one that contains an account, an API key, counters, a preference and three MCP servers: one with a header built from an env variable, one with a long path and a token argument after `--access`, and one whose env value starts with `!`. The fake tokens are assembled at run time.

**Needs:** Step 1.

**Run:**

```
( W="$(pwd)/specs/001-aprofile-standalone-profile-cli/.walkthrough"; H="$W/home"; rm -rf "$H"; mkdir -p "$H/.claude"
  cat > "$W/env.sh" <<EOF
export HOME="$H" XDG_CONFIG_HOME="$H/.config" XDG_DATA_HOME="$H/.local/share" XDG_STATE_HOME="$H/.local/state" ZDOTDIR="$H" SHELL=/bin/zsh
export PATH="$W/bin:/usr/bin:/bin"
A="$(pwd)/bin/aprofile"
P="$H/.local/share/aprofile/profiles"
EOF
  TOK="ghp_$(printf 'Ab1Cd2Ef3Gh4%.0s' 1 2 3)"
  printf '{"theme":"dark"}\n' > "$H/.claude/settings.json"; printf '# my instructions\n' > "$H/.claude/CLAUDE.md"
  cat > "$H/.claude.json" <<EOF
{"oauthAccount":{"emailAddress":"me@example.com"},"primaryApiKey":"$TOK","numStartups":42,"autoUpdates":false,
 "mcpServers":{
  "github":{"type":"http","url":"https://api.example.com/mcp","headers":{"Authorization":"Bearer \${GH_TOKEN}"}},
  "files":{"command":"node","args":["/Users/someone/projects/mcp/server/dist/index.js","--root=/Users/someone/projects/very/long/path/root","--access","$TOK"]},
  "danger":{"command":"run-it","env":{"X":"!echo pwned"}}}}
EOF
  chmod 600 "$H/.claude.json"; ls -A "$H" "$H/.claude" )
```

**What you should see:** the sandbox home lists `.claude` and `.claude.json`; `.claude` holds `CLAUDE.md` and `settings.json`. No output mentions a token.

**If you see something else:** a heredoc error usually means the block was pasted partly; rerun it as a whole.

**Result:** passed (agent, 2026-11-06).
  ✔ sandbox home → `.claude`, `.claude.json`; `.claude` holds `CLAUDE.md`, `settings.json`
  ✔ no token in the output

### Step 3 of 13: Create a Claude profile and see what it inherits (US1, US2)

**Why this step:** a new profile is seeded once from the default setup's `.claude.json` (now read from `~/.claude.json`, where Claude Code keeps it). It must get the MCP servers and preferences, but never the account, the API key or counters, and the file must stay private (0600).

**Needs:** Step 2.

**Run:**

```
( . specs/001-aprofile-standalone-profile-cli/.walkthrough/env.sh
  "$A" add claude work --auth-kind oauth-login && "$A" sync >/dev/null
  jq -r 'keys|join(" ")' "$P/claude/work/.claude.json"; stat -f '%Lp' "$P/claude/work/.claude.json" )
```

**What you should see:** a key list containing `autoUpdates` and `mcpServers`, and not containing `oauthAccount`, `primaryApiKey` or `numStartups`; then `600`.

**If you see something else:** `oauthAccount` or `primaryApiKey` in the list means credential keys are seeded, a serious leak; an empty object `{}` means the seed source was not found.

**Result:** passed (agent, 2026-11-06).
  ✔ seeded keys → `autoUpdates mcpServers` (no `oauthAccount`, `primaryApiKey`, `numStartups`)
  ✔ file mode → `600`

### Step 4 of 13: Start the profile through its wrapper (US1, US2)

**Why this step:** the wrapper points the harness at the profile's config dir and clears any provider switch exported in your shell, so `CLAUDE_CODE_USE_VERTEX=1` cannot override the profile's own login.

**Needs:** Step 3.

**Run:**

```
( . specs/001-aprofile-standalone-profile-cli/.walkthrough/env.sh
  CLAUDE_CODE_USE_VERTEX=1 CLOUD_ML_REGION=us-east5 "$XDG_DATA_HOME/aprofile/bin/claude-work" --version |
  grep -E 'STUB_ARGS|CLAUDE_CONFIG_DIR|APROFILE_NAME|CLAUDE_CODE_USE_VERTEX|CLOUD_ML_REGION' )
```

**What you should see:** `STUB_ARGS: [--version]`, `CLAUDE_CONFIG_DIR=` pointing at `profiles/claude/work`, `APROFILE_NAME=work`, and no `CLAUDE_CODE_USE_VERTEX` or `CLOUD_ML_REGION` line.

**If you see something else:** a `CLAUDE_CODE_USE_VERTEX=1` line means the shell's provider switch leaks into the profile.

**Result:** passed (agent, 2026-11-06).
  ✔ `STUB_ARGS: [--version]`, `APROFILE_NAME=work`, `CLAUDE_CONFIG_DIR=.../profiles/claude/work`
  ✔ no `CLAUDE_CODE_USE_VERTEX` or `CLOUD_ML_REGION` line

### Step 5 of 13: Log the profile in (US2)

**Why this step:** `aprofile login` must run the harness's login flow (`claude auth login`), not a plain session, with the same environment as the wrapper.

**Needs:** Step 3.

**Run:**

```
( . specs/001-aprofile-standalone-profile-cli/.walkthrough/env.sh
  CLAUDE_CODE_USE_VERTEX=1 "$A" login claude/work 2>&1 | grep -E 'STUB_ARGS|CLAUDE_CONFIG_DIR|CLAUDE_CODE_USE_VERTEX' )
```

**What you should see:** `STUB_ARGS: [auth login]`, the profile's `CLAUDE_CONFIG_DIR`, and no `CLAUDE_CODE_USE_VERTEX` line.

**If you see something else:** `STUB_ARGS: []` means login starts a bare session (the old bug).

**Result:** passed (agent, 2026-11-06).
  ✔ `STUB_ARGS: [auth login]` with the profile's `CLAUDE_CONFIG_DIR`
  ✔ no `CLAUDE_CODE_USE_VERTEX` line

### Step 6 of 13: Copy the profile to Codex and read the conversion report (US4)

**Why this step:** a cross-harness copy converts MCP servers. The header built from `${GH_TOKEN}` must become Codex's `bearer_token_env_var`; the long path and `--root=/long/path` arguments must survive; the server whose argument holds a literal token must be skipped (dropping one argument would shift the others); the `!` value is plain text for Codex.

**Needs:** Step 3.

**Run:**

```
( . specs/001-aprofile-standalone-profile-cli/.walkthrough/env.sh
  "$A" copy claude/work codex/work --auth chatgpt-login 2>&1 | grep -iE 'mcp|skip|drop|literal|convert'
  echo ---; grep -vE '^\s*$' "$P/codex/work/config.toml" )
```

**What you should see:** the report names `github` and `danger` as converted and `files` as skipped because an argument holding a literal secret was dropped (`args[3]`). `config.toml` has `[mcp_servers.github]` with `bearer_token_env_var = 'GH_TOKEN'` and no `files` server; no `ghp_` text anywhere.

**If you see something else:** a `files` entry containing the fake token, or `Bearer ${GH_TOKEN}` as a literal header, means a secret or a dead reference was written.

**Result:** passed (agent, 2026-11-06), after a runbook correction: the first run printed `config.toml` with `cat`, which the agent's shell aliases to another tool; the command now uses `grep` alone.
  ✔ report → `MCP servers converted: danger, github`; `MCP server skipped: files (args[3] dropped; writing the rest would shift the arguments after it)`; `MCP dropped: files args[3] (literal-secret)`
  ✔ `config.toml` → `[mcp_servers.github]` with `bearer_token_env_var = 'GH_TOKEN'`, no `files` server, `X = '!echo pwned'` as plain text for Codex
  ✔ no fake token → `grep -c ghp_` = 0

### Step 7 of 13: Copy the profile to pi and check the `!` value (US4)

**Why this step:** pi runs any value that starts with `!` as a shell command. A plain text value from Claude must be written escaped (`$!echo pwned`), never as a command.

**Needs:** Step 3.

**Run:**

```
( . specs/001-aprofile-standalone-profile-cli/.walkthrough/env.sh
  "$A" copy claude/work pi/work --auth login >/dev/null 2>&1; jq '.mcpServers.danger.env' "$P/pi/work/agent/mcp.json" )
```

**What you should see:** `{ "X": "$!echo pwned" }`.

**If you see something else:** `"!echo pwned"` without the `$` means pi would execute the value.

**Result:** passed (agent, 2026-11-06).
  ✔ pi env → `{ "X": "$!echo pwned" }`

### Step 8 of 13: Overwrite an existing copy safely (US4)

**Why this step:** `copy --overwrite` replaces the target's data, so it lists what it replaces, asks first (off a terminal it needs `--yes`, exit 3 without) and makes a backup.

**Needs:** Step 6.

**Run:**

```
( . specs/001-aprofile-standalone-profile-cli/.walkthrough/env.sh
  "$A" copy claude/work codex/work --auth chatgpt-login --overwrite </dev/null >/dev/null 2>&1; echo "exit $?"
  "$A" copy claude/work codex/work --auth chatgpt-login --overwrite --yes 2>&1 | grep -iE 'replaces|backed up' )
```

**What you should see:** `exit 3`, then a "copy onto existing profile codex/work replaces:" header and a "backed up the replaced data of codex/work to ..." line.

**If you see something else:** `exit 0` on the first command means data was replaced without confirmation.

**Result:** passed (agent, 2026-11-06).
  ✔ without `--yes` → `exit 3`
  ✔ with `--yes` → `copy onto existing profile codex/work replaces:` and `backed up the replaced data of codex/work to .../backups/codex/work/<timestamp>`

### Step 9 of 13: Copy onto a leftover config dir (US4)

**Why this step:** a config dir without a profile (left by `remove` without `--purge`) is never silently reused; `copy` names it and requires `--adopt`.

**Needs:** Step 3.

**Run:**

```
( . specs/001-aprofile-standalone-profile-cli/.walkthrough/env.sh
  mkdir -p "$P/claude/old"; "$A" copy claude/work claude/old 2>&1 | tail -1
  "$A" copy claude/work claude/old --adopt 2>&1 | grep -E 'share level|warning' )
```

**What you should see:** an error that names `profiles/claude/old` and mentions `--adopt`; then, with `--adopt`, the copy report (`share level: content`) and a warning that the secret-looking `files args[3]` value was carried verbatim (a same-harness copy keeps owned values as they are and warns, FR-033a).

**If you see something else:** success on the first command means a leftover dir was taken over silently.

**Result:** passed (agent, 2026-11-06), after a runbook correction: the first version echoed `$?` after a pipe (it showed `tail`'s exit code) and expected a different final line.
  ✔ without `--adopt` → `a config dir already exists at .../profiles/claude/old without a profile; pass --adopt to reuse it`
  ✔ with `--adopt` → profile `claude/old` created (`old.yaml` in the store); warning `owned value files args[3] looks like a secret and was duplicated verbatim`
  Note: the agent also made one probe copy (`claude/old2`) to see the success output, then removed it with `remove --purge --yes` and `sync`.

### Step 10 of 13: Break a profile by hand and let sync and doctor report it (US1, US5)

**Why this step:** a hand-edited `auth_kind` that the harness does not declare must not produce a wrapper that silently clears all credentials: sync fails that profile and keeps its old wrapper, doctor names the valid kinds.

**Needs:** Step 9.

**Run:**

```
( . specs/001-aprofile-standalone-profile-cli/.walkthrough/env.sh
  F="$XDG_CONFIG_HOME/aprofile/profiles/claude/old.yaml"; sed -i '' 's/^auth_kind:.*/auth_kind: nope/' "$F"
  "$A" sync >/dev/null 2>&1; echo "sync exit $?"; "$A" doctor 2>&1 | grep -A1 'auth kind "nope"' )
```

**What you should see:** `sync exit 1`, then a doctor problem `auth kind "nope" is not declared by harness claude (valid kinds: ...)` with the fix `aprofile edit claude/old --auth-kind <kind>`.

**If you see something else:** `sync exit 0` means the broken profile was accepted. If `sed` fails, the store path differs; check it with `ls "$XDG_CONFIG_HOME/aprofile/profiles/claude"`.

**Result:** passed (agent, 2026-11-06).
  ✔ `sync exit 1`
  ✔ doctor → `[claude/old] auth kind "nope" is not declared by harness claude (valid kinds: api-key, auth-token, oauth-token, oauth-login)` with `fix: aprofile edit claude/old --auth-kind <kind>`

### Step 11 of 13: Remove a profile and clean up its wrapper (US1)

**Why this step:** `remove` does not touch wrappers (only `sync` does), so `list` and `doctor` must report the leftover wrapper until the next `sync` removes it.

**Needs:** Step 10.

**Run:**

```
( . specs/001-aprofile-standalone-profile-cli/.walkthrough/env.sh
  "$A" remove claude old --yes 2>&1 | tail -1; "$A" list 2>/dev/null | grep -i leftover
  "$A" sync >/dev/null 2>&1; /bin/ls "$XDG_DATA_HOME/aprofile/bin" )
```

**What you should see:** a remove message saying `claude-old` is removed by the next `aprofile sync`, a `1 leftover wrapper from a removed or renamed profile: claude-old` line, and after sync a bin dir with `claude-work`, `codex-work` and `pi-work` only.

**If you see something else:** no leftover line means stale wrappers go unreported.

**Result:** passed (agent, 2026-11-06), after a runbook correction: `ls` resolved to a shell alias in the agent's shell; the command now calls `/bin/ls`.
  ✔ remove → `removed profile claude/old; config dir retained at ...; its wrapper claude-old is removed by the next aprofile sync (run: aprofile sync)`
  ✔ list → `1 leftover wrapper from a removed or renamed profile: claude-old (run: aprofile sync)`
  ✔ bin dir after sync → `claude-work`, `codex-work`, `pi-work`

### Step 12 of 13: Read the machine-readable output (US3)

**Why this step:** other tools read aprofile through stable JSON with a `format_version`; `sync --dry-run --json` lists planned changes without writing anything, and `show --json` reports where credentials come from without values.

**Needs:** Step 11.

**Run:**

```
( . specs/001-aprofile-standalone-profile-cli/.walkthrough/env.sh
  "$A" sync --dry-run --json | jq -c '{format_version, profiles: (.profiles|length)}'
  "$A" show claude/work --json | jq -c '{format_version, harness, name, has_credentials, problems}' )
```

**What you should see:** `{"format_version":1,"profiles":3}` and a show line with `"format_version":1`, `"harness":"claude","name":"work"`, `"has_credentials":false` (the stub never logged in) and `"problems":[]`.

**If you see something else:** a missing `format_version`, or any token text, is a contract break.

**Result:** passed (agent, 2026-11-06), after a runbook correction: `show --json` has no `id` field (it has `harness` and `name`); the first run printed `"id":null`.
  ✔ `{"format_version":1,"profiles":3}`
  ✔ show → `format_version` 1, `harness` claude, `name` work, `has_credentials` false, `problems` []

### Step 13 of 13: Dry-run an import of your real cc-deck profiles (US6)

**Why this step:** SC-010 asks that existing cc-deck profiles import with no manual edits. The dry run only reads your cc-deck config and data dirs and writes nothing; aprofile's own dirs point at an empty folder in the sandbox, so no existing profile conflicts with the import.

**Needs:** Step 1, cc-deck config at `~/.config/cc-deck`.

**Run:**

```
( R="$HOME"; . specs/001-aprofile-standalone-profile-cli/.walkthrough/env.sh; I="$(pwd)/specs/001-aprofile-standalone-profile-cli/.walkthrough/import"
  rm -rf "$I"; XDG_CONFIG_HOME="$I/config" XDG_DATA_HOME="$I/data" XDG_STATE_HOME="$I/state" \
  "$A" import cc-deck --source "$R/.config/cc-deck" --source-data "$R/.local/share/cc-deck" --dry-run 2>&1 | grep -E '^\s+(import|skip|failed)' )
```

**What you should see:** one `import <harness>/<name>` line per cc-deck profile and no `skip` or `failed` line.

**If you see something else:** a `skip` line names the reason (for example an unmapped auth form); `failed` means an unreadable cc-deck file.

**Result:** passed (agent, 2026-11-06), after a runbook correction: the first run imported into the sandbox's aprofile dirs, where Step 3's `claude/work` already exists, so the import correctly skipped it (`profile already exists (use --overwrite or --overwrite-credentials)`). The step now imports into an empty folder.
  ✔ `import claude/enmaas`, `import claude/private`, `import claude/work`, `import codex/private`; no skip or failed line

## Not covered here

- **A real harness session** (logging in with a real account, MCP servers actually starting): the stub harness only prints its arguments and environment. The 002 walkthrough ran a real Claude Code login and `/ide` session.
- **The interactive UI** (`aprofile ui`): it needs a terminal UI session; its parity with the commands is covered by `tests/integration/ui_parity_test.go`.
- **Exec hooks** (write-config, auth-status, timeouts, process-group kill): no built-in harness declares a hook; covered by `internal/harness/hook_*_test.go`.
- **The shell PATH block in a real rc file and `--require-path`**: covered by `internal/sync/path_test.go` and `tests/integration/sync_phase11_test.go`, kept out of this journey to leave your shell files alone.
- **Linux**: this machine is macOS; the suite ran on Linux in a podman container during verification.

## Coverage

- **US1** (create and launch): Steps 3, 4, 10, 11.
- **US2** (keep accounts apart): Steps 3, 4, 5.
- **US3** (other tools read the profile): Step 12.
- **US4** (copy with MCP servers): Steps 6, 7, 8, 9.
- **US5** (new or changed harness): Step 10 covers the definition check on profiles; adding a harness is covered by `tests/integration/harness_def_test.go`.
- **US6** (cc-deck import): Step 13 (dry run).
- **US7** (interactive UI): not covered, see above.

## Results

13 steps: 13 passed, 0 failed, 0 skipped. The agent ran every step in a sandbox home on macOS (2026-11-06) against the uncommitted Phase 11 and deep-review fixes; Step 13 read the real cc-deck config in a dry run. Five runbook mistakes were corrected along the way (two shell aliases for `cat` and `ls`, a `$?` after a pipe, a wrong JSON field name, and an import that collided with the sandbox's own profile); the feature behaved as expected in each case.

What a reader now knows the feature does:

- A new profile is seeded from `~/.claude.json` with MCP servers and preferences only; account, API key and counters never reach it, and the file stays 0600.
- The wrapper and `aprofile login` clear provider switches like `CLAUDE_CODE_USE_VERTEX` from the caller's shell, and `login` runs `claude auth login`.
- MCP conversion turns a `Bearer ${VAR}` header into Codex's `bearer_token_env_var`, keeps long path arguments, skips a server whose literal-token argument had to be dropped, and writes a `!` value escaped for pi.
- `copy --overwrite` exits 3 off a terminal without `--yes` and backs up what it replaces; a leftover config dir needs `--adopt`.
- An undeclared auth kind fails that profile in `sync` and is named by `doctor`; a removed profile's wrapper is reported by `list` until `sync` removes it.
- JSON output carries `format_version`; all four of your cc-deck profiles would import without skips.
