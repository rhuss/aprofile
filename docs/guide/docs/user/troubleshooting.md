# Troubleshoot profiles

<!-- Sources:
aprofile:README.md:138-142
aprofile:README.md:182-188,203
aprofile:internal/doctor/doctor.go:95-123,200-235,282-305,309-355,370-392,464-481
aprofile:internal/cli/doctor.go:16-27,30-73
aprofile:internal/cli/group.go:54-64,138-175,194-220
aprofile:internal/cli/list_show.go:603-621
aprofile:internal/cli/list_show.go:275-323
aprofile:internal/history/join.go:284-327,338-360
aprofile:internal/config/config.go:268-274
aprofile:internal/layer/plan.go:54-65
aprofile:internal/doctor/doctor.go:620-632
-->

When a profile behaves differently than expected, inspect its setup and history group first. Then run `doctor` and follow the fix named beside each finding.

## Inspect the profile

`aprofile show` identifies the profile, shows each configuration entry and its treatment, and reports credential status without exposing credential values. It also shows the `auth-status` result where the harness supports that check.

1. Inspect the profile that is having trouble:

   ```sh
   aprofile show claude/work
   ```

   Check: the report identifies `claude/work`, shows its entry treatments, and does not print secret values.
   <!-- expect: contains "claude/work" -->

## Run doctor

`doctor` checks profile configuration, wrappers, the harness binary, references, authentication status, and history-group links. Findings include a severity and a suggested fix. Only problems affect the command's exit status.

1. Run the check:

   ```sh
   aprofile doctor
   ```

   Check: the output lists findings and their fixes, or reports `no problems found`.

## Follow a reported fix

Start with the fix printed beside the finding. These common cases explain what the report means and what to do next.

1. If aprofile's bin directory is missing from `PATH`, or a wrapper is missing or stale, run sync. If another executable shadows a wrapper, remove or move the executable named by the finding so the aprofile wrapper can be found first.

   ```sh
   aprofile sync
   ```

   Check: sync updates wrappers and its managed `PATH` block where needed.

2. If `doctor` reports that the harness binary is missing, install that harness. If an `auth-status` finding says credentials are missing, run `aprofile login <harness>/<name>`. If the status is unknown, verify that profile's login. For a failed auth-status hook, use the command in the finding to inspect the hook.

3. If `doctor` reports an unclassified entry, decide how the profile should treat it, then classify it in the harness definition. For example:

   ```sh
   aprofile harness classify claude RTK.md --as content
   ```

   Choose the category that fits the entry. `show` and `doctor` report unclassified entries because aprofile does not know how to treat them.

4. If a reference cannot be resolved, set the named environment variable, create the referenced file, or correct the reference. The finding names the reference.

5. If a linked file was replaced by a real file, review the proposed backup and relink. `sync` leaves a real replacement in place unless you request relinking:

   ```sh
   aprofile sync --relink
   ```

   Check: aprofile backs up the replacement and asks before restoring the link.

6. If `doctor` reports an orphan wrapper, it is an aprofile-marked wrapper left by a removed or renamed profile. Run `aprofile sync` to remove it. Sync removes temporary `.aprofile-tmp-*` and `.aprofile-new-*` entries left by interrupted writes after they are older than a minute. It restores a directory-swap temporary named `.<entry>.aprofile-new-*-old` by renaming it back. If a temporary entry cannot be removed or restored, sync reports a problem instead of claiming it is up to date.

## Check which profile owns the session

If a conversation is missing after you switch profiles, inspect the original profile and its history-group membership. An isolated profile keeps its history to itself. Members of a group use that group's shared history store. Leaving a group removes the profile's history links and gives it fresh, empty history. A share level or profile copy does not transfer history.

1. Inspect the profile you originally used. For a conversation started through `claude-work`:

   ```sh
   aprofile show claude/work
   ```

   Check: the report identifies the profile and shows its history entries and their status.
   <!-- expect: contains "claude/work" -->

2. Inspect the named group to see its members and history entries:

   ```sh
   aprofile group show claude/team
   ```

   Check: the report shows the group members, store, and history entries.
   <!-- expect: contains "history group claude/team" -->

3. If the profile names a group that does not exist, correct the membership to an existing group or leave the group:

   ```sh
   aprofile edit claude/work --group team
   ```

   Or use `aprofile edit claude/work --no-group` to make its history isolated. A wrong history link is repaired by `aprofile sync`; a missing history link is recreated by sync. Sync does not move data. If a real file replaced the link, use `aprofile sync --relink` to back it up and restore the link, or use `aprofile edit <harness>/<name> --group <group>` to merge eligible history into that group.

4. To find set-aside entries from a group change, use `aprofile show <harness>/<name>` to list the set-aside run directories, or inspect `<data dir>/aprofile/set-aside/<harness>/<name>/<timestamp>/manifest.json`. The manifest records each profile-relative path and why it was set aside. aprofile does not automatically restore these entries; `doctor` suggests moving items back by hand if you need them. Use the manifest to decide what to recover.

   Check: the manifest identifies each set-aside entry and its reason.

See [Share conversation history with a group](history-groups.md) for how group membership and leaving affect history.
