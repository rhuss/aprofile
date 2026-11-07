# Troubleshoot profiles

<!-- Sources:
aprofile:README.md:91-107
aprofile:README.md:143-169
aprofile:README.md:236-238
aprofile:internal/doctor/doctor.go:90-100,125-145,155-170,231-246,255-290,294-322,370-382
aprofile:README.md:42-48,62-72,184-207
aprofile:internal/cli/copy.go:197-222
aprofile:internal/wrapper/wrapper.go:99-106,254-255
-->

When a profile behaves differently than you expect, inspect its recorded setup first, then ask `doctor` to check for drift. The report names each finding and gives a next step, so you can fix the specific cause instead of changing the share level blindly.

## Inspect the profile

`aprofile show` lays out the profile's share level and each config-directory entry with its treatment and reason. It also reports hook sets and whether credentials exist, without showing credential values. After profile changes, it can flag a wrapper as stale until `aprofile sync` updates it.

1. Inspect the profile that is having trouble:

   ```sh
   aprofile show claude/work
   ```

   Check: the report shows the share level, entry treatments, hook sets, and credential presence without secret values.

## Run doctor

Use `doctor` to check the installation and all profiles together. It checks whether aprofile's bin directory is on `PATH`, whether another executable shadows a wrapper, whether wrappers are stale or missing, and whether a harness binary is missing. It also checks config entries, linked files replaced by a harness, hook sets, definitions, and references that cannot be resolved. Findings include a severity and a suggested fix; only problems affect the exit status.

1. Run the check:

   ```sh
   aprofile doctor
   ```

   Check: the output lists any findings with their severity and fix, or reports no problems.

## Follow a reported fix

Start with the fix printed beside the finding. These common cases have direct remedies. A command that replaces a file or changes links may ask before it proceeds, so read its prompt.

1. If the bin directory is missing from `PATH`, or wrappers are stale or missing, run sync. It manages the PATH block and writes wrappers. If another executable shadows a wrapper, remove that executable or put aprofile's bin directory first on `PATH`.

   ```sh
   aprofile sync
   ```

   Check: sync completes and updates the managed wrappers and PATH block where needed.

2. If a harness binary is missing, install that harness. The profile wrapper cannot start it until the executable is available.

3. If an entry is unclassified, decide how the profile should treat it, then classify it in the harness definition. For an entry that belongs with shared content, for example:

   ```sh
   aprofile harness classify claude RTK.md --as content
   ```

   Use the category that fits the entry; `content` is only an example. `show` and `doctor` report unclassified entries because aprofile does not know how to treat them.

4. If a reference cannot be resolved, set the named environment variable or create the referenced file, or correct the reference. `doctor` identifies the reference and suggests these options.

5. If a harness replaced a linked file with a real file, `sync` leaves that file in place. To restore the link, run the explicit relink operation after reviewing the replacement. It backs up the file and asks first; use `--yes` only when you have reviewed and accepted the change.

   ```sh
   aprofile sync --relink
   ```

   Check: aprofile asks before replacing the file, then reports the backup and relink.

## Check which profile owns the session

If you cannot find a conversation after switching profiles, check which profile you originally used. History and sessions remain private at `all`, `content`, and `none`. Sharing more setup or copying a profile does not transfer them.

1. Inspect the original profile. For a conversation started through `claude-work`:

   ```sh
   aprofile show claude/work
   ```

   Check: the output identifies `claude/work` and shows its configuration entries and their treatment. This inspects the profile's setup, not the contents of a conversation.
   <!-- expect: contains "claude/work" -->

2. Start the original profile's wrapper:

   ```sh
   claude-work
   ```

   Check: Claude Code starts with the work profile's configuration. Use its session tools to look for the conversation there. The available sessions and resume behavior are handled by Claude Code.

Starting another profile selects that profile's configuration. aprofile does not automatically make the original conversation available in it. See [History and sessions](share-levels.md#history-and-sessions) for the current limits.
