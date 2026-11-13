# Command reference

<!-- Sources:
aprofile:README.md:177-203
aprofile:internal/cli/group.go:54-119,138-175,194-298
aprofile:internal/cli/add.go:45-76
aprofile:internal/cli/edit.go:22-37,545-556,722-735
aprofile:internal/cli/copy.go:100-126
aprofile:internal/cli/login.go:23-38
aprofile:internal/cli/import.go:16-53
aprofile:internal/cli/registry.go:11-48
aprofile:internal/cli/hooksets.go:22-70,150-205,208-212,264-301,304-315
aprofile:internal/cli/mcp.go:100-111
-->

The commands below show their syntax. From a source checkout, run them as `./bin/aprofile <command>` from the repository root.

## Profile commands

| Command | Purpose |
|---|---|
| `aprofile add <harness> <name>` | Create a profile. Common options include `--share`, `--group`, `--hook-sets`, `--exclude-hook-sets`, `--model`, `--endpoint`, `--auth-kind`, `--auth-key`, `--env`, `--env-literal`, `--color`, `--icon`, `--label`, `--primary`, `--herdr-agent`, and `--adopt`. `--model` works for every harness that can carry a model: Claude Code (through `ANTHROPIC_MODEL`) and Codex (passed as `-c model="..."`). |
| `aprofile list` | List profiles by harness. Add `--json` for machine-readable output. |
| `aprofile show <harness>/<name>` | Show share level, config entry treatment, hook sets, and credential status without credential values. |
| `aprofile edit <harness>/<name>` | Change profile fields. `--group <group>` joins or moves history membership; `--no-group` leaves it. They cannot be combined. Use `--dry-run` to preview and `--yes` to confirm data changes without prompting. |
| `aprofile rename <harness>/<name> <new-name>` | Rename a profile, moving its file, config directory, and imported credential files together. `--force` skips the running-process check or confirmation when aprofile cannot check. |
| `aprofile remove <harness> <name>` | Remove a profile. `--purge` also deletes its config directory and imported credential files after confirmation; `--yes` skips the prompt. |
| `aprofile sync` | Prepare config directories, update wrappers, place hook sets, publish the registry, and manage the PATH block. `--dry-run` previews changes; `--fix-links` repairs leaking links; `--relink` restores links a harness replaced with a real file; `--json` emits machine-readable output. |
| `aprofile login <harness>/<name>` | Run the harness login flow with the profile's environment, using its declared login command or login hook. |
| `aprofile copy <harness>/<name> <harness>/<name>` | Copy a profile within or across harnesses. `--group <group>` puts a new target in a group of its harness; otherwise it is isolated. History is never copied. |
| `aprofile import cc-deck` | Import profiles without modifying cc-deck files. Use `--dry-run` to preview, `--source` or `--source-data` to select source directories, `--share` to set the imported share level, `--overwrite` to replace profile settings and owned data, and `--overwrite-credentials` to replace credentials too. |

Profile changes can leave wrappers stale until `aprofile sync` runs. Only `sync` writes or removes wrappers. See [Manage profiles](../user/manage-profiles.md) for procedures.

## Sharing, hooks, and MCP

| Command | Purpose |
|---|---|
| `aprofile group list` | List supported history groups. Use `--harness <name>` to filter or `--json` for machine-readable output. |
| `aprofile group show <harness>/<group>` | Show the group's store, members, and history entries. Add `--json` for machine-readable output. |
| `aprofile group create <harness>/<group>` | Create a group with an empty store. Add `--json` for machine-readable output. |
| `aprofile group delete <harness>/<group>` | Delete a group with no members. Its store is kept by default; `--purge` also deletes it after confirmation, and `--yes` confirms without a terminal prompt. `--json` emits machine-readable output. |
| `aprofile harness list` | List effective harness definitions. |
| `aprofile harness show <name>` | Show a harness definition, field and rule origins, and share-level treatment. |
| `aprofile harness classify <harness> <entry> --as <category>` | Add a classification for an entry in `harnesses.d`, merged with built-in rules. |
| `aprofile settings pull <harness>/<name> [key...]` | Refresh selected settings in a `content` profile from the default setup. The default mode merges; `--replace` makes an exact copy. |
| `aprofile help sharing` | Print the share-level guide in the terminal. |
| `aprofile hook-sets list` / `show <name>` | List hook sets and show one set's bodies and profiles. |
| `aprofile hook-sets import <harness> --name <set>` | Create a hook set from the harness's default hooks without changing the default setup. |
| `aprofile hook-sets reapply <harness>/<name>` | Restore the profile's current hook-set entries. `--dry-run` previews; `--yes` confirms without prompting. |
| `aprofile mcp list <harness>/<name>` | List a profile's MCP servers. Add `--json` for machine-readable output. |
| `aprofile mcp copy <from-harness>/<name> <to-harness>/<name> [server...]` | Convert selected MCP servers from one profile into another. `--overwrite` replaces servers already in the target. |
| `aprofile registry path` | Print the registry file location. |

Groups cannot be renamed; the hidden `aprofile group rename` command only explains this limitation. Create a new group, move members with `edit --group`, then delete the old group. History groups select history within one harness. See [History groups](../user/history-groups.md) for full procedures. Compare [share levels](../user/share-levels.md) before changing profile sharing. See [Manage profiles](../user/manage-profiles.md) for hook sets, MCP servers, and command procedures.

## Diagnostics and help

| Command | Purpose |
|---|---|
| `aprofile doctor` | Check profiles and config directories for isolation and configuration problems. Add `--json` for machine-readable findings. |
| `aprofile ui` | Browse and edit profiles interactively. The same operations are available as commands. |
| `aprofile --help` | Show top-level command help. Append `--help` to a command for its usage and flags. |

Use [Troubleshoot profiles](../user/troubleshooting.md) to investigate doctor findings. Run `aprofile <command> --help` when you need available flags.
