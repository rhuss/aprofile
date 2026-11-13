# Choose a share level

<!-- Sources:
aprofile:README.md:3-9,42-48,62-73,81,83-102,112-120,138-142,189
aprofile:internal/cli/settings.go:35-48,110-126
aprofile:internal/cli/copy.go:231-255
aprofile:internal/harness/builtin/claude.yaml:10-14,232-249
aprofile:internal/harness/builtin/codex.yaml:10-14,53-61
aprofile:internal/harness/builtin/opencode.yaml:15-21,51-61
aprofile:internal/harness/builtin/pi.yaml:15-19,39-51
claude-code:README.md:7
codex:README.md:1
opencode:README.md:10
pi:README.md:15-17
-->

A share level controls which parts of a harness's default setup a profile uses. Choose one that fits the account you are setting up, then preview the change before applying it. Run the commands from the repository checkout root with `./bin/aprofile`.

## Choose a level

Use `all` when the profile should behave like your default setup. Choose `content` when you want shared instructions and skills but separate settings. Choose `none` when the profile should start clean. Which one fits your account?

1. Check the profile's current treatment of each entry.

   ```sh
   ./bin/aprofile show claude/work
   ```

   Check: the output lists entries with their treatment and reason.
   <!-- expect: contains "claude/work" -->

2. Preview a level change. Replace `content` with `all` or `none` if that is your choice.

   ```sh
   ./bin/aprofile edit claude/work --share content --dry-run
   ```

   Check: the output shows what the change would do, without applying it.
   <!-- expect: exit 0 -->

3. If the preview matches your intent, apply that same change.

   ```sh
   ./bin/aprofile edit claude/work --share content
   ```

   Check: aprofile reports the profile update. Run `./bin/aprofile sync` afterward so its wrapper reflects the change.
   <!-- expect: exit 0 -->

New profiles use `content` by default, unless `default_share` in `<config dir>/aprofile/settings.yaml` sets another default. You can set the level when adding, editing, copying, or importing a profile, or in the `./bin/aprofile ui` form.

## Launch settings at each level

At `content` and `none` the profile has its own `settings.json`, so `sync` places the profile's launch settings there and the folder launches on its own. At `all` the settings file is linked to the default setup, which aprofile never writes: the launch settings stay with the wrapper, and `edit --share all --dry-run` says so. Codex keeps its key command in `config.toml`, which is the profile's own at every level.

## What each level shares

A share level and history-group membership answer different questions. The share level controls setup entries; a history group selects which history entries profiles of the same harness share. Membership does not change how settings, content, credentials, or plugins follow the selected share level.

| Entry | `all` | `content` | `none` |
|---|---|---|---|
| Credentials | Private | Private | Private |
| Conversation history entries, when ungrouped | Private to this profile | Private to this profile | Private to this profile |
| Conversation history entries, when grouped and supported | Selected history entries shared with group members | Selected history entries shared with group members | Selected history entries shared with group members |
| Other runtime state, such as logs and caches | Private | Private | Private |
| Owned data, such as MCP servers and model settings | Private | Private | Private |
| Content, such as instructions and skills | Linked to the default setup | Linked to the default setup | Not present |
| Settings, such as permissions and enabled plugins | Linked to the default setup | Own copy, taken once | Own minimal setup |
| Plugin store | Linked to the default setup | Linked to the default setup | Private |
| Hooks | Default setup's hooks | Named hook sets and every-profile hooks | Named hook sets only |

Credentials stay private regardless of group membership. Each harness marks the history entries it can share; those entries may live in a sessions directory, while process state, logs, and caches stay private. Settings continue to follow the table.

The built-in harnesses are Claude Code, a terminal coding tool; Codex CLI, a coding agent that runs locally; OpenCode, an open source coding agent; and Pi, a minimal, extensible agent harness. Their configuration directories differ: Claude Code uses `CLAUDE_CONFIG_DIR`, Codex uses `CODEX_HOME`, OpenCode uses XDG base directory variables, and Pi uses `PI_CODING_AGENT_DIR` plus a session directory. This affects where aprofile applies the sharing rules, not the meaning of the levels.

There are two exceptions to keep in mind when choosing `none`. Codex and OpenCode read skills from `~/.agents/skills`, and OpenCode also reads `~/.claude/skills`. Those directories are outside their config directories, so share levels cannot isolate them. `./bin/aprofile show` and `./bin/aprofile doctor` list these paths.

### History and sessions

An ungrouped profile keeps its history to itself, regardless of whether its share level is `all`, `content`, or `none`. A grouped profile shares the history entries supported by that harness with members of its group, also regardless of share level. For example, a profile at `none` can still join a history group; its settings and credentials remain governed by the share rules above.

History groups select entries among profiles of one harness. Claude Code, Codex CLI, and Pi support history groups. OpenCode does not: its sessions and logins are stored in one database. See [Share history with a group](history-groups.md) for choosing membership and managing groups.

Copying a profile does not copy history, whether the destination uses the same harness or another harness. Cross-harness copy converts supported configuration such as MCP servers; it does not convert transcripts into another harness's format. To share selected history, use a supported group within one harness.

If a session seems to have disappeared after changing profiles, follow [Check which profile owns the session](troubleshooting.md#check-which-profile-owns-the-session).

## Limits to know

`all` links the settings file to the default setup. If two profiles change settings at the same time, one change can overwrite the other. At `all` and `content`, the plugin store is linked too, so concurrent plugin installs or updates write to the same store. Use `content` to keep settings separate, or `none` to keep both settings and the plugin store separate.

Changing to `all` replaces the profile's settings file with the shared file. aprofile backs up the profile's file under `<data dir>/backups/` and asks before proceeding. A non-interactive command needs `--yes`; without it, the command exits with code 3. A level change is refused while a harness for that profile is running, with exit code 4. Other level changes add or remove links and create profile copies without deleting profile data.

At `content`, settings are copied from the default setup once and remain profile-specific. Preview a refresh for `claude/work` with `./bin/aprofile settings pull claude/work --dry-run`; it merges by default, while `--replace` makes an exact copy. At `none`, the harness starts with its first-run onboarding.

Check: the profile output reflects the selected level and reports whether its wrapper is synced.

```sh
./bin/aprofile show claude/work
```
<!-- expect: contains "claude/work" -->

Continue with [Share conversation history with a group](history-groups.md), then [Manage profiles](manage-profiles.md). The [troubleshooting guide](troubleshooting.md) and [command reference](../reference/commands.md) cover common fixes and syntax.
