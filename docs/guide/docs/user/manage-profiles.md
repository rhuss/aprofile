# Manage profiles

<!-- Sources:
aprofile:README.md:111-140
aprofile:internal/cli/hooksets.go:269-270
aprofile:README.md:143-169
aprofile:README.md:188-195
aprofile:internal/cli/copy.go:184-222
aprofile:internal/cli/edit.go:457-467,546-605
-->

Profiles are easy to inspect before you change them. The main thing to keep in mind is that profile records and wrappers are separate: after a profile change, run `aprofile sync` so its launcher catches up.

## Inspect profiles

Use the list to find a profile, then show its details before editing. The details include the share level, how each config directory entry is treated, hook sets, and whether credentials exist. Credential values are never shown.

1. List profiles grouped by harness:

   ```sh
   aprofile list
   ```

   Check: the output lists profiles under their harness names.

2. Inspect one profile:

   ```sh
   aprofile show claude/work
   ```

   Check: the output shows its share level, config directory entries and treatment, hook sets, and credential presence without credential values.

3. Start the harness's login flow for a profile when you need to sign in:

   ```sh
   aprofile login claude/work
   ```

   Check: the harness login flow runs with that profile's environment.

## Change profiles

Edit a profile when its settings need to change, or rename it when you want a clearer name. These operations update profile data, not the launchers. Run `sync` after changes so the wrapper names and configuration reflect the current profiles.

1. Edit a profile. For a share-level change, inspect the proposed changes with `--dry-run` before applying them with `--yes`:

   ```sh
   aprofile edit claude/work --share content --dry-run
   aprofile edit claude/work --share content --yes
   ```

   Check: the dry run shows the proposed level change before the second command applies it.

2. Rename a profile:

   ```sh
   aprofile rename claude/work team
   ```

   Check: aprofile moves the profile file and config directory together.

3. Remove a profile:

   ```sh
   aprofile remove claude team
   ```

   Check: aprofile removes the profile record and reports that its config directory is retained.

   > [!CAUTION]
   > Add `--purge` only when you also want to delete the profile's config directory, including the history and sessions stored there. Without `--purge`, aprofile retains that directory.

   A share-level change to `all` can replace files after backup and confirmation. `aprofile settings pull --replace` also replaces settings with the default setup, so inspect the profile first.

4. Refresh wrappers and config directories after changes:

   ```sh
   aprofile sync
   ```

   Check: aprofile writes one wrapper per profile and prepares each config directory for its share level. A second run with no changes writes zero files.

## Copy profile data

Copy a profile when you want a second setup or want to move its owned configuration to another harness. The copy command converts MCP servers for the destination harness. Credentials and runtime state are not copied.

A new destination starts with its own empty history and sessions. Copying `claude/work` to another Claude Code profile or to `codex/team` does not make work conversations resumable there. If you copy over an existing destination, its own credentials and runtime state remain in place; the source's conversations are not merged into them. See [History and sessions](share-levels.md#history-and-sessions).

1. Copy a profile within or across harnesses:

   ```sh
   aprofile copy claude/work codex/team
   ```

   Check: the destination profile is created with copied profile data and converted MCP servers.

2. Review MCP servers separately, or copy selected servers between profiles:

   ```sh
   aprofile mcp list claude/work
   aprofile mcp copy claude/work codex/team server-name
   ```

   Check: the first command lists the profile's MCP servers; the second copies the named server.

   > [!CAUTION]
   > `aprofile copy --overwrite` replaces the destination profile data after backing it up and asking first. Check the destination before using this option.

## Manage hooks and MCP servers

Hook sets let profiles choose named groups of hooks. Attach a set by editing the profile, then sync to place its managed entries in the profile's hook location. Sync only updates or removes entries it placed itself; it leaves other hook entries alone. If you changed a managed entry by hand, sync reports it and leaves it unchanged.

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

3. Attach the set you confirmed is available, then sync:

   ```sh
   aprofile edit claude/work --hook-sets base
   aprofile sync
   ```

   Check: sync places, updates, or removes the managed entries for the profile's hook sets. Use `base` here only if it appears in the list; for other hook sets, use only names that the list shows.

4. Use `reapply` if you intentionally want to restore a managed hook entry that you edited by hand:

   ```sh
   aprofile hook-sets reapply claude/work
   ```

   Check: aprofile shows a dry run, backs up the file, and asks before writing the set's version.

Run `aprofile sync` after add, edit, rename, copy, remove, or import. Until then, list, show, and doctor report affected wrappers as stale.
