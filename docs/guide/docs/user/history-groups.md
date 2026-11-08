# Share conversation history with a group

<!-- Sources:
aprofile:README.md:42-48,67-73,112-142,203
aprofile:internal/cli/group.go:54-63,91-119,138-175,194-245,247-298
aprofile:internal/cli/history_groups.go:17-22,84-95,115-124,194-231,292-305
aprofile:internal/history/group.go:92-101,170-202,250-346
aprofile:internal/history/join.go:23-37,84-90,117-133,239-270,284-328
aprofile:internal/harness/builtin/claude.yaml:232-249
aprofile:internal/harness/builtin/codex.yaml:50-63
aprofile:internal/harness/builtin/pi.yaml:39-53
aprofile:internal/harness/builtin/opencode.yaml:51-66
-->

Use a history group when two profiles of the same harness should continue the same conversations. Membership controls history only. A profile's share level still controls which settings, content, and plugin behavior it borrows from the default setup, so a profile at share level `none` can join `home` and share only history. See [Compare share levels](share-levels.md) for that choice.

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
   aprofile group list
   ```
   <!-- expect: exit 0 -->

You can also check an existing profile with `aprofile show <harness>/<name>`. Replace `<harness>` and `<name>` with its harness and profile name. Its history line tells you whether it is isolated or which group it belongs to.

## Create and join a group

Joining an existing profile makes its harness history entries point to the selected group's store. Use an existing profile for this walkthrough. Replace `<profile>` with its name, and replace `claude` with `codex` or `pi` when that is the harness you use.

1. Create a custom group if the list does not already contain it.

   ```sh
   aprofile group create claude/work
   ```

   The built-in `home` group already exists. Do not try to create it.

   Check: `aprofile group show` finds the new group.
   ```sh
   aprofile group show claude/work
   ```
   <!-- expect: contains "claude/work" -->

2. Preview how an existing profile would join. This command does not change membership or move history.

   Check: the plan lists history entries it would move or set aside, if any.

   ```sh
   aprofile edit claude/<profile> --group work --dry-run
   ```
   <!-- expect: exit 0 -->

3. Join the group after reviewing the plan.

   Check: the command reports that the profile joined `work`.

   ```sh
   aprofile edit claude/<profile> --group work
   ```
   <!-- expect: contains "joined history group work" -->

   If existing history needs to move or be set aside, aprofile asks once before changing it. After the edit, run `aprofile sync` to update the profile's wrapper and registry.

   Check: the profile details name `work` as its history group.
   ```sh
   aprofile show claude/<profile>
   ```
   <!-- expect: exit 0 -->

To join the default setup instead, use `--group home`. Joining `home` is allowed only when the default location can serve as that harness's history store.

## Resume in another member

Once two profiles of one harness share a group, use that harness's normal resume command from either member. For example, a Claude Code conversation can be resumed through the other Claude Code profile with its native `--resume <id>` option. The conversation uses the member profile's login, endpoint, and model. This shares same-harness history; it does not convert transcripts or mirror history between different harnesses.

1. Start or resume a conversation using one member's wrapper.
2. Resume that conversation with another member of the same group, using the harness's own resume command and the conversation ID.

   Check: Claude Code opens the conversation through its native resume flow.
   ```sh
   claude-lab --resume <id>
   ```
   <!-- expect: exit 0 -->

Run `aprofile sync` after profile edits because wrappers stay stale until sync updates them.

## Leave a group

Leaving separates a profile's future history from the group's store. The group and its shared history remain available to the other members.

1. Leave the current group and return the profile to isolated history.

   Check: aprofile reports that the profile left its group.

   ```sh
   aprofile edit claude/<profile> --no-group
   ```
   <!-- expect: contains "left history group" -->

   Check: `aprofile show` reports the profile as isolated.
   ```sh
   aprofile show claude/<profile>
   ```
   <!-- expect: exit 0 -->

   aprofile removes the profile's history links and creates fresh, empty private history entries. The group store is unchanged. Run `aprofile sync` to refresh the wrapper and registry.

2. Delete an empty custom group only when you no longer need its record. Run the deletion only after all members have left.

   ```sh
   aprofile group delete claude/work
   ```

   Without `--purge`, aprofile removes the group record and keeps its store. A later group with the same name will not adopt that kept store.

   Check: aprofile reports that the group does not exist.
   ```sh
   aprofile group show claude/work
   ```
   <!-- expect: exit 2 -->

## Limits and recovery

Review the join plan before approving it. A running profile cannot change groups, and running the same conversation in two profiles at once is not prevented. Two writers can interleave a transcript, so keep each conversation active in one profile at a time.

When a join finds existing history, its result depends on the harness's merge mode. A `per-file` directory entry moves regular files whose relative paths are missing from the group store. A path already present in the store is set aside, and the store file is not overwritten. A `none` entry is set aside as a whole. Other unmergeable entries are also set aside, so a join does not promise that every history file merges.

Set-aside data is kept under aprofile's data directory, with a `manifest.json` alongside each join's set-aside items. The manifest records the profile, group, creation time, and each item's original path and reason. `aprofile doctor` reports set-aside history as information. If a join stops partway through, `doctor` and `sync` report the membership drift and the command to complete the join. `sync` recreates missing links; `sync --relink` backs up a replaced link and restores it.

Shared transcripts can show which account wrote them because they carry account identifiers. Group membership controls history, while share level continues to control setup sharing. The `home` group cannot be created, renamed, or deleted. A custom group cannot be deleted while it has members; move those profiles elsewhere or use `--no-group` first.

Use `--purge` only when you want to delete the empty custom group's stored history too. For example, `aprofile group delete claude/work --purge` deletes the group record and store after confirmation. Off a terminal, add `--yes` to confirm. Do not use `--purge` if you want to keep that history.

For OpenCode, history groups are unavailable because its session database also contains login state. Choose another supported harness if you need group-based history sharing.
