# Manage profiles

<!-- Sources:
aprofile:README.md:81,165-169,181-190,201-205,224-231
aprofile:internal/cli/login.go:23-39,75-118
aprofile:internal/cli/copy.go:100-125,168-204,221-255,267-289,320-340,403-414
aprofile:internal/harness/builtin/codex.yaml:26-29
aprofile:internal/cli/edit.go:545-600,602-665,668-691,722-800,803-809
aprofile:internal/cli/hooksets.go:22-70,150-205,208-212,264-301,304-410
aprofile:internal/cli/mcp.go:35-112,115-165
-->

Profiles are easy to inspect before you change them. The commands below assume `claude/work` exists and that you are in the repository checkout root; run `./bin/aprofile` to use the manager. Profile records and wrappers are separate, so after a profile change run `./bin/aprofile sync` to refresh its launcher. This page covers common profile tasks; for choosing or changing history membership, see [Share conversation history with a group](history-groups.md).

## Inspect profiles

Use the list to find a profile, then show its details before editing. The details include the share level, config directory entries and their treatment, hook sets, and whether credentials exist. Credential values are never shown.

1. List profiles grouped by harness:

   ```sh
   ./bin/aprofile list
   ```

   Check: the command lists profiles grouped by harness.
   <!-- expect: exit 0 -->

2. Inspect one profile:

   ```sh
   ./bin/aprofile show claude/work
   ```

   Check: the output shows the profile's share level, config directory entries, hook sets, and credential presence without credential values.
   <!-- expect: contains "claude/work" -->

## Sign in to a profile

Run login for the profile you want the harness to use. Choose one command for a profile you have created. aprofile starts the harness's own login flow with that profile's environment, so credentials created by the flow belong to that profile.

1. Start the login flow for your harness:

   ```sh
   ./bin/aprofile login <harness>/<name>
   ```

   Claude Code runs `claude auth login`, Codex runs `codex login`, and OpenCode runs `opencode auth login`. Pi starts a session where you enter `/login`.

   Check: the selected harness login flow runs with the profile's environment.
   This is an interactive check; complete the harness login flow in the terminal.

## Change profiles

Edit a profile when its settings need to change, or rename it when you want a clearer name. These operations update profile data, not wrappers. Run `./bin/aprofile sync` after changes so wrapper names and configuration reflect the current profiles.

This sequence assumes `claude/work` exists and is stopped, and `claude/team` is not already in use. It renames the work profile to `claude/team` and keeps it for the copy and hook examples below. Substitute an existing profile you intend to remove for `claude/old`.

1. Edit a profile. Preview a share-level change before applying it:

   ```sh
   ./bin/aprofile edit claude/work --share content --dry-run
   ./bin/aprofile edit claude/work --share content --yes
   ```

   Check: the dry run shows the proposed changes before the second command applies them.
   <!-- expect: exit 0 -->

2. Rename a stopped profile:

   ```sh
   ./bin/aprofile rename claude/work team
   ```

   The rename is refused while a harness started for the profile is running. If aprofile cannot tell whether it is running, it asks before continuing. `--force` skips both checks. The profile record and config directory move together; imported credential files and references into their directory move too. A rename can also be refused if files already exist at the destination.

   Check: aprofile reports the new profile name. Run `./bin/aprofile sync` to replace the old wrapper.
   <!-- expect: contains "renamed claude/work to claude/team" -->

3. Remove a profile record and retain its config directory:

   ```sh
   ./bin/aprofile remove claude old
   ```

   Check: aprofile reports `claude/old` removed and its config directory retained.
   <!-- expect: contains "removed profile claude/old" -->

   > [!CAUTION]
   > Use `--purge` only when you intend to delete the profile's config directory, including its credentials and state. Purge also deletes credential files imported for this profile and its set-aside history. It does not delete a history group's store or anything in the default setup. Purge asks for confirmation; `--yes` skips the prompt.

4. Refresh wrappers and config directories after changes:

   ```sh
   ./bin/aprofile sync
   ```

   Check: aprofile writes one wrapper per profile and prepares each config directory for its share level. A second run with no changes writes zero files.
   <!-- expect: exit 0 -->

## Copy profile data

Copy a profile to make a second setup or to move its owned configuration to another harness. Copy carries profile settings and owned data such as MCP servers, converting MCP servers for a different harness. It never carries credentials or runtime state, including source history. A new target starts isolated unless you explicitly choose a history group for the target harness with `--group`.

1. Copy to a new profile whose name is not already in use, choosing its history group only if you want one:

   ```sh
   ./bin/aprofile copy claude/team codex/copied-work
   # To place a new Claude Code target in an existing Claude Code group:
   ./bin/aprofile copy claude/team claude/lab-copy --group shared
   ```

   The cross-harness example creates a Codex profile with Codex's default login kind. Run `./bin/aprofile login codex/copied-work` if you want to sign in to it.

   `--group` must name a group belonging to the target's harness. Without it, the target has no group, even when the source belongs to one. Copy never carries source history. An existing target keeps its current group; change that membership with `./bin/aprofile edit <harness>/<name> --group <group>` before copying. Copying over an existing target requires `--overwrite`; aprofile lists the data it will replace, asks first, and backs it up. Off a terminal, use `--yes` to confirm.

   Check: the report names the target and its group, or says it is isolated. The target's history is not copied from the source.
   <!-- expect: contains "codex/copied-work" -->

2. Review MCP servers or copy selected servers between profiles:

   ```sh
   ./bin/aprofile mcp list claude/team
   # Replace server-name with a name shown by the list command.
   ./bin/aprofile mcp copy claude/team codex/copied-work server-name
   ```

   Check: the first command lists the profile's MCP servers; the second reports the selected server conversion.
   <!-- expect: exit 0 -->

## Manage hooks and MCP servers

Hook sets let profiles choose named groups of hooks. Attach a set by editing the profile, then sync to place its managed entries in the profile's hook location. Sync updates or removes only entries it placed itself; it leaves other hook entries alone. If you changed a managed entry by hand, sync reports it and leaves it unchanged.

1. See which hook sets are available:

   ```sh
   ./bin/aprofile hook-sets list
   ```

   Check: the output lists available hook sets, or says there are none.
   <!-- expect: exit 0 -->

2. If no `base` set exists, create it from Claude Code's existing hooks, then list the sets again:

   ```sh
   ./bin/aprofile hook-sets import claude --name base
   ./bin/aprofile hook-sets list
   ```

   Check: the list shows `base`. Import creates the set from the default setup's existing hooks; it does not modify that setup.
   <!-- expect: contains "base" -->

3. Attach a set that appears in the list, then sync:

   ```sh
   ./bin/aprofile edit claude/team --hook-sets base
   ./bin/aprofile sync
   ```

   Check: sync places or updates the managed entries for the profile's hook sets. Use `base` here only if it appears in the list.
   <!-- expect: exit 0 -->

4. Use `reapply` if you intentionally want to restore a managed hook entry that you edited by hand:

   ```sh
   ./bin/aprofile hook-sets reapply claude/team --dry-run
   ```

   Review the plan, then run `./bin/aprofile hook-sets reapply claude/team` to apply it. aprofile backs up affected files and asks before writing; `--yes` confirms without asking.

   Check: the dry run lists the hook entries aprofile plans to re-place.
   <!-- expect: exit 0 -->

Run `./bin/aprofile sync` after add, edit, rename, copy, remove, or import. Until then, list, show, and doctor report affected wrappers as stale. Continue with [Troubleshoot profiles](troubleshooting.md), then use the [command reference](../reference/commands.md) as needed.
