# Share conversation history with a group

<!-- Sources:
aprofile:README.md:42-48,67-73,112-142,203
aprofile:internal/cli/group.go:54-63,91-119,138-175,194-245,247-298
aprofile:internal/cli/history_groups.go:17-22,84-95,115-124,194-231,292-305
aprofile:internal/cli/add.go:48-50,55,64-75,79-102,190-219
aprofile:internal/history/group.go:92-101,170-202,250-346
aprofile:internal/history/join.go:23-37,84-90,117-133,239-270,284-328
aprofile:internal/harness/builtin/claude.yaml:26-38,232-249
aprofile:internal/harness/builtin/codex.yaml:50-63
aprofile:internal/harness/builtin/pi.yaml:39-53
aprofile:internal/harness/builtin/opencode.yaml:51-66
-->

Use a history group when two profiles of the same harness should continue the same conversations. Membership controls history only. A profile's share level still controls which settings, content, and plugin behavior it borrows from the default setup, so a profile at share level `none` can join `home` and share only history. The commands below assume you are in the aprofile checkout root and use `./bin/aprofile` to run the manager. See [Compare share levels](share-levels.md) for the separate setup-sharing choice.

## Choose who shares history

Pick a group based on which profiles should see the same conversations. New profiles start isolated, and group membership never crosses harnesses.

| Choice | Who shares history | Where the history lives |
| --- | --- | --- |
| No group | Only that profile | In the profile's own store |
| `home` | Members of `home` and the harness's default setup | In the default setup |
| Custom group | Profiles of that harness that join the named group | In aprofile's data directory, apart from the default setup |

Claude Code, Codex CLI, and Pi support history groups. OpenCode does not. The built-in `home` group is available for supported harnesses; create a custom group for conversations you want to keep separate from your default setup.

To see the groups that already exist, run:

1. List groups before choosing a destination.

   Check: supported harnesses appear with their groups, including `home`.

   ```sh
   ./bin/aprofile group list
   ```
   <!-- expect: exit 0 -->

You can also check an existing profile with `./bin/aprofile show <harness>/<name>`. Replace `<harness>` and `<name>` with its harness and profile name. Its history line tells you whether it is isolated or which group it belongs to.

## Create and join a group

This walkthrough assumes you completed [Get started](getting-started.md), exported `WORK_TOKEN`, and have a stopped `claude/work` profile. It creates a second profile, `claude/lab`, so you can confirm both members share a group. Keep both wrappers stopped while changing group membership. The commands below use `./bin/aprofile` from the repository checkout root.

Joining an existing profile makes its harness history entries point to the selected group's store. The examples use the group name `shared`; choose a different name if that one already exists and replace the group name and `--group` value below.

1. Create a custom group if the list does not already contain it.

   ```sh
   ./bin/aprofile group create claude/shared
   ```

   The built-in `home` group already exists. Do not try to create it.

   Check: `./bin/aprofile group show` finds the new group.
   ```sh
   ./bin/aprofile group show claude/shared
   ```
   <!-- expect: contains "claude/shared" -->

2. Preview how `claude/work` would join. This command does not change membership or move history.

   Check: the plan lists history entries it would move or set aside, if any.

   ```sh
   ./bin/aprofile edit claude/work --group shared --dry-run
   ```
   <!-- expect: exit 0 -->

3. Join `claude/work` after reviewing the plan.

   Check: the command reports that the profile joined `shared`.

   ```sh
   ./bin/aprofile edit claude/work --group shared
   ```
   <!-- expect: contains "joined history group shared" -->

   If existing history needs to move or be set aside, aprofile asks once before changing it. After the edit, run `./bin/aprofile sync` to update the profile's wrapper and registry.

   Check: the profile details name `shared` as its history group.
   ```sh
   ./bin/aprofile show claude/work
   ```
   <!-- expect: exit 0 -->

4. Add `claude/lab` to the `shared` group using the token reference from Get started.

   ```sh
   ./bin/aprofile add claude lab --label "Lab" --auth-kind oauth-token \
     --auth-key env:WORK_TOKEN --group shared
   ./bin/aprofile sync
   ```

   Check: both profiles appear as members of the group.
   ```sh
   ./bin/aprofile group show claude/shared
   ```
   <!-- expect: contains "claude/work" and contains "claude/lab" -->

To join the default setup instead, use `--group home`. Joining `home` is allowed only when the default location can serve as that harness's history store.

## Resume in another member

Once two profiles of one harness share a group, use that harness's normal resume command from either member. For example, a Claude Code conversation can be resumed through the other Claude Code profile with its native `--resume <id>` option. The conversation uses the member profile's login, endpoint, and model. This shares same-harness history; it does not convert transcripts or mirror history between different harnesses.

1. Start or resume a conversation using one member's wrapper.
2. Resume that conversation with `claude-lab`, using Claude Code's own resume command and the conversation ID.

   Check: Claude Code opens the conversation through its native resume flow. This is an interactive check; verify the transcript, then exit Claude Code.
   ```sh
   claude-lab --resume <id>
   ```

Run `./bin/aprofile sync` after profile edits because wrappers stay stale until sync updates them.

## Leave a group

Leaving separates a profile's future history from the group's store. The group and its shared history remain available to the other members.

1. Leave the group with `claude/lab` to return that profile to isolated history. The group and its store remain for `claude/work`.

   Check: aprofile reports that the profile left `shared`.

   ```sh
   ./bin/aprofile edit claude/lab --no-group
   ```
   <!-- expect: contains "left history group shared" -->

   Check: `./bin/aprofile show` reports the profile as isolated.
   ```sh
   ./bin/aprofile show claude/lab
   ```
   <!-- expect: contains "claude/lab" -->

   aprofile removes the profile's history links and creates fresh, empty private history entries. The group store is unchanged. Run `./bin/aprofile sync` to refresh the wrapper and registry.

2. If you no longer need the shared group, leave it with `claude/work` too, then delete the now-empty group:

   ```sh
   ./bin/aprofile edit claude/work --no-group
   ./bin/aprofile sync
   ./bin/aprofile group delete claude/shared
   ```
   <!-- expect: exit 0 -->

   Without `--purge`, aprofile removes the group record and keeps its store. A later group with the same name will not adopt that kept store.

   Check: aprofile reports that the group does not exist.
   ```sh
   ./bin/aprofile group show claude/shared
   ```
   <!-- expect: exit 2 -->

## Limits and recovery

Review the join plan before approving it. A running profile cannot change groups, and running the same conversation in two profiles at once is not prevented. Two writers can interleave a transcript, so keep each conversation active in one profile at a time.

When a join finds existing history, its result depends on the harness's merge mode. A `per-file` directory entry moves regular files whose relative paths are missing from the group store. If the store already has a different file at that relative path, the profile file is set aside; the store file is never overwritten. A `none` entry is set aside as a whole. Other unmergeable entries are also set aside, so a join does not promise that every history file merges.

Set-aside data is kept under aprofile's data directory, with a `manifest.json` alongside each join's set-aside items. The manifest records the profile, group, creation time, and each item's original path and reason. `./bin/aprofile doctor` reports set-aside history as information. If a join stops partway through, `doctor` and `sync` report the membership drift and the command to complete the join. `sync` recreates missing links; `sync --relink` backs up a replaced link and restores it.

Shared transcripts can show which account wrote them because they carry account identifiers. Group membership controls history, while share level continues to control setup sharing. The `home` group cannot be created, renamed, or deleted. A custom group cannot be deleted while it has members; move those profiles elsewhere or use `--no-group` first.

Use `--purge` only when you want to delete the empty custom group's stored history too. For example, `./bin/aprofile group delete claude/shared --purge` deletes the group record and store after confirmation. Off a terminal, add `--yes` to confirm. Do not use `--purge` if you want to keep that history.

For OpenCode, history groups are unavailable because its session database also contains login state. Choose another supported harness if you need group-based history sharing.

Continue with [Manage profiles](manage-profiles.md), then [Troubleshoot profiles](troubleshooting.md) or the [command reference](../reference/commands.md).
