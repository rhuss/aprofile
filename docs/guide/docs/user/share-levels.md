# Choose a share level

<!-- Sources:
aprofile:README.md:42-48,50-60,62-109,171-182
aprofile:internal/harness/builtin/claude.yaml:34-40,50-55,83-100
aprofile:internal/harness/builtin/codex.yaml:33-41,64-82
aprofile:internal/harness/builtin/opencode.yaml:42-80
aprofile:internal/harness/builtin/pi.yaml:32-52
claude-code:README.md:7
codex:README.md:1
opencode:README.md:10
pi:README.md:13-17
-->

A share level controls which parts of a harness's default setup a profile uses. Choose one that fits the account you are setting up, then preview the change before applying it.

## Choose a level

Use `all` when the profile should behave like your default setup. Choose `content` when you want shared instructions and skills but separate settings. Choose `none` when the profile should start clean. Which one fits your account?

1. Check the profile's current treatment of each entry.

   ```sh
   ./bin/aprofile show claude/work
   ```

   Check: the output lists entries with their treatment and reason.

2. Preview a level change. Replace `content` with `all` or `none` if that is your choice.

   ```sh
   ./bin/aprofile edit claude/work --share content --dry-run
   ```

   Check: the output shows what the change would do, without applying it.

3. If the preview matches your intent, apply that same change.

   ```sh
   ./bin/aprofile edit claude/work --share content
   ```

   Check: aprofile reports the profile update. Run `aprofile sync` afterward so its wrapper reflects the change.

New profiles use `content` by default, unless `default_share` in `<config dir>/aprofile/settings.yaml` sets another default. You can set the level when adding, editing, copying, or importing a profile, or in the `aprofile ui` form.

## What each level shares

At every level, credentials, runtime state, and harness-owned entries stay private. The table shows what happens to the other entries.

| Entry | `all` | `content` | `none` |
|---|---|---|---|
| Content, such as instructions and skills | Linked to the default setup | Linked to the default setup | Not present |
| Settings, such as permissions and enabled plugins | Linked to the default setup | Own copy, taken once | Own minimal setup |
| Plugin store | Linked to the default setup | Linked to the default setup | Private |
| Hooks | Default setup's hooks | Named hook sets and every-profile hooks | Named hook sets only |

The built-in harnesses are Claude Code, a terminal coding tool; Codex CLI, a coding agent that runs locally; OpenCode, an open source coding agent; and Pi, a minimal, extensible agent harness. Their configuration directories differ: Claude Code uses `CLAUDE_CONFIG_DIR`, Codex uses `CODEX_HOME`, OpenCode uses XDG base directory variables, and Pi uses `PI_CODING_AGENT_DIR` plus a session directory. This affects where aprofile applies the sharing rules, not the meaning of the levels.

There are two exceptions to keep in mind when choosing `none`. Codex and OpenCode read skills from `~/.agents/skills`, and OpenCode also reads `~/.claude/skills`. Those directories are outside their config directories, so share levels cannot isolate them. `aprofile show` and `aprofile doctor` list these paths.

## Limits to know

`all` links the settings file to the default setup. If two profiles change settings at the same time, one change can overwrite the other. At `all` and `content`, the plugin store is linked too, so concurrent plugin installs or updates write to the same store. Use `content` to keep settings separate, or `none` to keep both settings and the plugin store separate.

Changing to `all` replaces the profile's settings file with the shared file. aprofile backs up the profile's file under `<data dir>/backups/` and asks before proceeding. A non-interactive command needs `--yes`; without it, the command exits with code 3. A level change is refused while a harness for that profile is running, with exit code 4. Other level changes add or remove links and create profile copies without deleting profile data.

At `content`, settings are copied from the default setup once and remain profile-specific. Use `aprofile settings pull` to preview a refresh; it merges by default, while `--replace` makes an exact copy. At `none`, the harness starts with its first-run onboarding.

Check: the profile output reflects the selected level and reports whether its wrapper is synced.

```sh
./bin/aprofile show claude/work
```
<!-- expect: contains "claude/work" -->
