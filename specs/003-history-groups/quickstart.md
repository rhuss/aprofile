# Quickstart: History Groups

Validation guide for feature 003. Automated checks run against a temporary home
(`testutil.HermeticHome`); the manual steps use a throwaway `HOME` so no real harness state is
touched.

## Prerequisites

- `go build -o /tmp/ap/aprofile ./cmd/aprofile`
- A throwaway home, set in two steps. A single `export HOME=$(mktemp -d) XDG_CONFIG_HOME=$HOME/.config`
  expands `$HOME` before it is assigned and points the XDG paths at the real home:
  `T=$(mktemp -d); export HOME=$T` and then
  `export XDG_CONFIG_HOME=$T/.config XDG_DATA_HOME=$T/.local/share XDG_STATE_HOME=$T/.local/state`.
  Check with `echo $XDG_DATA_HOME` before running anything.
- Seed a fake default setup: `mkdir -p $HOME/.claude/projects/-tmp-demo` and write a file
  `$HOME/.claude/projects/-tmp-demo/s-default.jsonl`.

## Automated

```bash
go test ./...                                   # unit and integration
go test ./tests/integration -run 'HistoryGroup' # feature scenarios
```

Covered: US1 to US5 acceptance scenarios, SC-001 to SC-005, FR-019 purge safety, and isolated
profiles unchanged after upgrade (SC-002).

## Manual walkthrough

1. **Groups exist.** `aprofile group list` shows `claude/home`, `codex/home`, `pi/home` with the
   default setup as member; no `opencode` row.
2. **Unknown group is harmless.** `aprofile add claude test --group wrok` exits 2, lists `home`,
   and `ls $XDG_DATA_HOME/aprofile/profiles/claude` shows no `test`.
3. **Join `home` at creation.** `aprofile add claude personal --group home`; then
   `ls -l $XDG_DATA_HOME/aprofile/profiles/claude/personal/projects` is a link to
   `$HOME/.claude/projects`, and `s-default.jsonl` is visible through it. `aprofile show
   claude/personal` lists `projects/` as `linked (history group home)`.
4. **Own group.** `aprofile group create claude/work`, `aprofile add claude acme --group work`,
   `aprofile add claude corp --group work`; a file written into
   `.../acme/projects/x/s.jsonl` is visible under `.../corp/projects/x/` and not under
   `$HOME/.claude/projects`.
5. **Join with existing history.** `aprofile add claude solo`; write
   `.../solo/projects/-tmp-demo/s-solo.jsonl` and `.../solo/history.jsonl`; run
   `aprofile edit claude/solo --group home --dry-run` (plan lists one `move-to-store`, one
   `set-aside`), then without `--dry-run` and `--yes`. `s-solo.jsonl` now lives in
   `$HOME/.claude/projects/-tmp-demo/`; `history.jsonl` is in
   `$XDG_DATA_HOME/aprofile/set-aside/claude/solo/<ts>/` with a manifest.
6. **Resume under another profile (real harness, optional).** With real Claude Code and two
   profiles in `home` that use different endpoints or accounts: start a conversation with one
   wrapper, resume it with the other (`claude-<name> --resume <id>`). Expected: the conversation
   continues with the second profile's model. Record the result for research R3; if Claude
   refuses because the session belongs to another account, document that limitation in
   `help sharing`.
7. **Leave.** `aprofile edit claude/solo --no-group`; `projects/` is a real empty directory again
   and `$HOME/.claude/projects/-tmp-demo/s-solo.jsonl` still exists.
8. **Delete rules.** `aprofile group delete claude/work` exits 2 and lists `acme`, `corp`; after
   `aprofile edit claude/acme --no-group --yes` and the same for `corp`, delete succeeds and
   prints the kept store path; `aprofile group create claude/work` then exits 2 naming that path.
9. **Drift.** Replace `.../corp/projects` (after rejoining a group) with a real directory;
   `aprofile doctor` reports it with the `sync --relink` fix; `aprofile sync` leaves it;
   `aprofile sync --relink --yes` restores the link after a backup.
10. **Wrapper and registry.** `grep APROFILE_HISTORY_GROUP ~/.local/bin/claude-personal` shows
    `home`; `aprofile registry export | jq '.[].history_group'` shows the groups.

## Results

Recorded 2026-11-07 on macOS with the branch build.

- **Automated**: `go vet ./...`, `gofmt` and `go test ./...` pass, including
  `tests/integration/history_groups_test.go` (US1 to US5, SC-001 to SC-005, FR-019) and the
  performance checks in `tests/integration/perf_test.go` (a join of 2,000 session files took about
  0.6 s against the 5 s budget; sync of 20 profiles with 10 members about 40 ms against 1 s; a
  member's wrapper overhead about 25 ms against 50 ms).
- **Manual walkthrough, steps 1 to 5 and 7 to 10**: run as a script against a throwaway home; every
  step showed the expected result (the three `home` groups and no OpenCode row; `--group wrok`
  exits 2 and creates nothing; `projects` linked to the default location with `s-default.jsonl`
  visible; `work` sessions visible between members and not in the default setup; the dry run
  lists one `move-to-store` and one `set-aside`, and the real run moves `s-solo.jsonl` and writes
  the set-aside manifest; leaving gives a real `projects` directory and keeps the moved session;
  delete refuses with members, keeps the store and `group create` then names it; doctor reports
  the drift, sync names `edit --group`, `sync --relink --yes` restores the link; the wrapper
  exports `APROFILE_HISTORY_GROUP='home'` and the registry carries `history_group`). The scripted
  run takes under two seconds; the SC-006 five-minute check by a person reading only
  `aprofile help sharing` and command help is still open.
- **Step 6 (research R3, resume under another account)**: not run. It needs a real Claude Code
  installation with two accounts, and the walkthrough must not touch the real home. The
  limitation stays documented in research R3; `help sharing` says that shared conversations show
  which account wrote them.
- **Incident during this check**: the first scripted run used the single-`export` form above and
  wrote into the real `~/.config/aprofile` and `~/.local/share/aprofile` (four test profiles, their
  wrappers, group, set-aside and backup directories, two empty state directories in an existing
  profile, and a rewritten registry and wrapper of that profile). The real `~/.claude` was not
  touched because `HOME` itself pointed at the temporary directory. The created entries were
  removed again; the existing profile's wrapper and registry entry remain as rendered by the branch
  build (they only add the empty `APROFILE_HISTORY_GROUP` and `history_group`), and one
  `aprofile sync` with the installed version renders them as before. The prerequisites above now
  show the safe two-step form.
