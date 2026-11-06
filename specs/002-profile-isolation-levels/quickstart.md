# Quickstart: validating share levels and hook sets

Runs against a throwaway home so nothing touches your real setup. Commands and flags are defined
in [contracts/cli.md](contracts/cli.md); entry lists in [research.md](research.md) R1.

## Prerequisites

```sh
make build                                  # ./bin/aprofile
export HOME=$(mktemp -d) XDG_CONFIG_HOME=$HOME/.config XDG_DATA_HOME=$HOME/.local/share
# A small fake default setup (the full R1 entry lists are the test fixtures in
# internal/testutil/defaults.go, used by the automated tests).
mkdir -p "$HOME/.claude/skills/review" "$HOME/.claude/plugins" "$HOME/.claude/hooks"
echo '# default instructions' > "$HOME/.claude/CLAUDE.md"
echo '# review skill' > "$HOME/.claude/skills/review/SKILL.md"
echo '{"permissions":{"allow":["Bash(ls:*)"]},"statusLine":{"type":"command","command":"x"},"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"'"$HOME"'/.claude/hooks/rtk.sh"}]}]}}' > "$HOME/.claude/settings.json"
printf '#!/bin/sh\n' > "$HOME/.claude/hooks/rtk.sh"; chmod +x "$HOME/.claude/hooks/rtk.sh"
echo '{"display":"default prompt"}' > "$HOME/.claude/history.jsonl"
mkdir -p "$HOME/.codex"; echo db > "$HOME/.codex/memories_1.sqlite"; echo wal > "$HOME/.codex/memories_1.sqlite-wal"
A=./bin/aprofile
```

## 1. Runtime state is private at every level (US1, SC-001)

```sh
for lvl in all content none; do $A add claude t-$lvl --share $lvl --auth-key env:T; done
$A add codex c-content --share content
$A sync
find "$XDG_DATA_HOME/aprofile/profiles" -type l -name 'history.jsonl' -o -type l -name '*.sqlite*'
```

Expected: no output. `$A show claude/t-all` lists `projects` and every other state entry the
profile holds as private ("runtime state: always private"); `history.jsonl` appears once the
harness writes it.

## 2. Levels do what the descriptions say (US2)

```sh
$A help sharing
$A show claude/t-content --json | jq '.entries[] | select(.entry=="settings.json" or .entry=="CLAUDE.md") | {entry,treatment}'
```

Expected: `CLAUDE.md` linked, `settings.json` own-copy at `content`; at `none` neither linked;
`settings.json` has no `hooks` key in `t-content`.

## 3. Preview and change a level (US3, SC-006)

```sh
$A edit claude/t-all --share content --dry-run     # lists unlink + copy-once, "nothing deleted"
$A edit claude/t-all --share content
$A edit claude/t-all --share all --dry-run         # lists replace-with-link (confirm, backup)
$A edit claude/t-all --share all </dev/null; echo $?   # 3: needs --yes off a terminal; nothing changed
```

## 4. Hook sets (US4, SC-008)

```sh
mkdir -p "$XDG_CONFIG_HOME/aprofile/hooks.d"
$A hook-sets import claude --name base             # from the fake default setup's hooks
$A edit claude/t-content --hook-sets base && $A sync
jq '.hooks' "$XDG_DATA_HOME/aprofile/profiles/claude/t-content/settings.json"
$A edit claude/t-all --hook-sets base              # exit 2: all profiles use the default hooks
```

Then add a hook group by hand to `t-content`'s `settings.json`, detach `base`, `sync`, and check
that only `base`'s groups are gone.

## 5. Settings pull merges (US3 scenario 6)

```sh
S=$XDG_DATA_HOME/aprofile/profiles/claude/t-content/settings.json
jq '.permissions.allow += ["Bash(make test)"]' "$S" > "$S.new" && mv "$S.new" "$S"
$A settings pull claude/t-content --dry-run        # does not list the profile-only permission as removed
$A settings pull claude/t-content --replace --dry-run   # lists it as removed
```

## 6. Drift and honesty (US5)

```sh
P=$XDG_DATA_HOME/aprofile/profiles/claude/t-content
rm "$P/CLAUDE.md" && cp "$HOME/.claude/CLAUDE.md" "$P/CLAUDE.md"
$A doctor                                          # problem: stopped sharing CLAUDE.md; fix --relink
$A sync --relink --yes                             # backs the file up, relinks
$A show codex/c-content; $A doctor                 # info: $HOME escapes (~/.agents/skills)
```

## 7. Upgrade of slice one profiles (US6, SC-009)

Create profiles with the slice one binary (`git worktree add /tmp/s1 9d6faf8 && make -C /tmp/s1 build`), run
`sync`, switch to this build, run `sync`: settings and content links unchanged, state links reported
as leaking, nothing deleted; `show` reports `share: all` with a hint.

## Also verify by hand (research open items)

- Claude Code resolves `@` imports in a linked `CLAUDE.md` (R7).
- `/ide` connects from a profile started via its wrapper (R8).
- `edit --share` refuses while `claude-t-content` runs, on macOS and Linux (R11).
