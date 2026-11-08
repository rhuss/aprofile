# Manage profiles

<!-- Sources:
aprofile:README.md:165-169,181-190,201-205,224-231
aprofile:internal/cli/login.go:23-39,75-118
aprofile:internal/cli/copy.go:100-125,168-204,221-255,267-289,320-340
aprofile:internal/cli/edit.go:545-600,602-665,668-691,722-800,803-809
aprofile:internal/cli/hooksets.go:22-70,150-205,264-301,304-410
aprofile:internal/cli/mcp.go:35-112,115-165
-->

Profiles are easy to inspect before you change them. Profile records and wrappers are separate, so after a profile change run `aprofile sync` to refresh its launcher. This page covers common profile tasks; for choosing or changing history membership, see [Share conversation history with a group](history-groups.md).

## Inspect profiles

Use the list to find a profile, then show its details before editing. The details include the share level, config directory entries and their treatment, hook sets, and whether credentials exist. Credential values are never shown.

1. List profiles grouped by harness:

   ```sh
   aprofile list
   ```

   Check: the output lists profiles under their harness names.

2. Inspect one profile:

   ```sh
   aprofile show claude/work
   ```

   Check: the output shows the profile's share level, config directory entries, hook sets, and credential presence without credential values.

## Sign in to a profile

Run login for the profile you want the harness to use. aprofile starts the harness's own login flow with that profile's environment, so credentials created by the flow belong to that profile.

1. Start the login flow for your harness:

   ```sh
   aprofile login claude/work
   # Or use the matching profile reference:
   aprofile login codex/team
   aprofile login opencode/work
   aprofile login pi/work
   ```

   Claude Code runs `claude auth login`, Codex runs `codex login`, and OpenCode runs `opencode auth login`. Pi starts a session where you enter `/login`.

   Check: the selected harness login flow runs with the profile's environment.

## Change profiles

Edit a profile when its settings need to change, or rename it when you want a clearer name. These operations update profile data, not wrappers. Run `aprofile sync` after changes so wrapper names and configuration reflect the current profiles.

1. Edit a profile. Preview a share-level change before applying it:

   ```sh
   aprofile edit claude/work --share content --dry-run
   aprofile edit claude/work --share content --yes
   ```

   Check: the dry run shows the proposed changes before the second command applies them.

2. Rename a stopped profile:

   ```sh
   aprofile rename claude/work team
   ```

   The rename is refused while a harness started for the profile is running. If aprofile cannot tell whether it is running, it asks before continuing. `--force` skips both checks. The profile record and config directory move together; imported credential files and references into their directory move too. A rename can also be refused if files already exist at the destination.

   Check: aprofile reports the new profile name. Run `aprofile sync` to replace the old wrapper.

3. Remove a profile record and retain its config directory:

   ```sh
   aprofile remove claude team
   ```

   Check: aprofile reports the profile removed and the retained config directory.

   > [!CAUTION]
   > Use `--purge` only when you intend to delete the profile's config directory, including its credentials and state. Purge also deletes credential files imported for this profile and its set-aside history. It does not delete a history group's store or anything in the default setup. Purge asks for confirmation; `--yes` skips the prompt.

4. Refresh wrappers and config directories after changes:

   ```sh
   aprofile sync
   ```

   Check: aprofile writes one wrapper per profile and prepares each config directory for its share level. A second run with no changes writes zero files.

## Copy profile data

Copy a profile to make a second setup or to move its owned configuration to another harness. Copy carries profile settings and owned data such as MCP servers, converting MCP servers for a different harness. It never carries credentials or runtime state, including source history. A new target starts isolated unless you explicitly choose a history group for the target harness with `--group`.

1. Copy to a new profile, choosing its history group only if you want one:

   ```sh
   aprofile copy claude/work codex/team
   # To place a new Claude Code target in an existing Claude Code group:
   aprofile copy claude/work claude/team --group work
   ```

   `--group` must name a group belonging to the target's harness. Without it, the target has no group, even when the source belongs to one. Copy never carries source history. An existing target keeps its current group; change that membership with `aprofile edit <harness>/<name> --group <group>` before copying. Copying over an existing target requires `--overwrite`; aprofile lists the data it will replace, asks first, and backs it up. Off a terminal, use `--yes` to confirm.

   Check: the report names the target and its group, or says it is isolated. The target's history is not copied from the source.

2. Review MCP servers or copy selected servers between profiles:

   ```sh
   aprofile mcp list claude/work
   aprofile mcp copy claude/work codex/team server-name
   ```

   Check: the first command lists the profile's MCP servers; the second reports the selected server conversion.

## Manage hooks and MCP servers

Hook sets let profiles choose named groups of hooks. Attach a set by editing the profile, then sync to place its managed entries in the profile's hook location. Sync updates or removes only entries it placed itself; it leaves other hook entries alone. If you changed a managed entry by hand, sync reports it and leaves it unchanged.

1. See which hook sets are available:

   ```sh
   aprofile hook-sets list
   ```

   Check: the output lists available hook sets, or says there are none.

2. If no `base` set exists, create it from Claude Code's existing hooks, then list the sets again:

   ```sh
   aprofile hook-sets import claude --name base
   aprofile hook-sets list
   ```

   Check: the list shows `base`. Import creates the set from the default setup's existing hooks; it does not modify that setup.

3. Attach a set that appears in the list, then sync:

   ```sh
   aprofile edit claude/work --hook-sets base
   aprofile sync
   ```

   Check: sync places or updates the managed entries for the profile's hook sets. Use `base` here only if it appears in the list.

4. Use `reapply` if you intentionally want to restore a managed hook entry that you edited by hand:

   ```sh
   aprofile hook-sets reapply claude/work --dry-run
   ```

   Review the plan, then run `aprofile hook-sets reapply claude/work` to apply it. aprofile backs up affected files and asks before writing; `--yes` confirms without asking.

   Check: the dry run lists the hook entries aprofile plans to re-place.

Run `aprofile sync` after add, edit, rename, copy, remove, or import. Until then, list, show, and doctor report affected wrappers as stale.
