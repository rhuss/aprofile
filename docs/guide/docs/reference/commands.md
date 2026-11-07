# Command reference

<!-- Sources:
aprofile:README.md:143-169
aprofile:internal/cli/root.go:52-76
aprofile:internal/cli/add.go:58-70
aprofile:internal/cli/list_show.go:55-67,110-121
aprofile:internal/cli/edit.go:21-34,457-468,546-558
aprofile:internal/cli/sync.go:17-32
aprofile:internal/cli/login.go:18-28
aprofile:internal/cli/copy.go:74-96
aprofile:internal/cli/copy.go:197-222
aprofile:internal/cli/mcp.go:38-49,101-114
aprofile:internal/cli/hooksets.go:150-165,208-225,264-301,304-316
aprofile:internal/cli/doctor.go:16-28
-->

## Profile commands

| Command | Purpose |
|---|---|
| `aprofile add <harness> <name>` | Create a profile. Common options include `--share`, `--hook-sets`, `--model`, `--endpoint`, `--auth-kind`, `--auth-key`, `--env`, `--color`, and `--label`. Use `--adopt` only to reuse an existing config directory. |
| `aprofile list` | List profiles by harness. Add `--json` for machine-readable output. |
| `aprofile show <harness>/<name>` | Show a profile's share level, config entry treatment, hook sets, and whether credentials exist. It does not show credential values. |
| `aprofile edit <harness>/<name>` | Change profile fields. For a share-level change, `--dry-run` lists changes and `--yes` confirms changes that replace profile data. |
| `aprofile rename <harness>/<name> <new-name>` | Rename a profile and move its file and config directory together. |
| `aprofile remove <harness> <name>` | Remove a profile. `--purge` also deletes its config directory after confirmation; `--yes` skips that prompt. |
| `aprofile sync` | Prepare config directories, update wrappers, place hook sets, publish the registry, and manage the PATH block. `--dry-run` lists changes. |
| `aprofile login <harness>/<name>` | Run the harness login flow with the profile's environment. |
| `aprofile copy <harness>/<name> <harness>/<name>` | Copy a profile within or across harnesses. History, sessions, and credentials are not copied. The command does not update wrappers; run `aprofile sync` after. |

Profile changes can leave wrappers stale until `aprofile sync` runs. Only `sync` writes or removes wrappers. Use [Manage profiles](../user/manage-profiles.md) for the procedures.

## Sharing, hooks, and MCP

| Command | Purpose |
|---|---|
| `aprofile harness list` | List effective harness definitions. |
| `aprofile harness show <name>` | Show a harness definition, field and rule origins, and share-level treatment. |
| `aprofile harness classify <harness> <entry> --as <category>` | Add a classification for an entry in `harnesses.d`, merged with built-in rules. |
| `aprofile settings pull <harness>/<name> [key...]` | Refresh selected settings in a `content` profile from the default setup. The default mode merges; `--replace` makes an exact copy. |
| `aprofile help sharing` | Print the share-level guide in the terminal. |
| `aprofile hook-sets list` | List hook sets, supported harnesses, and profiles that use them. |
| `aprofile hook-sets show <name>` | Show one hook set's bodies and the profiles it applies to. |
| `aprofile hook-sets import <harness> --name <set>` | Create a hook set from the harness's default hooks. The import does not modify the default setup. |
| `aprofile hook-sets reapply <harness>/<name>` | Restore the profile's current hook-set entries after backing up diverged entries. `--dry-run` lists changes; `--yes` confirms without prompting. |
| `aprofile mcp list <harness>/<name>` | List a profile's MCP servers. Add `--json` for machine-readable output. |
| `aprofile mcp copy <from-harness>/<name> <to-harness>/<name> [server...]` | Convert selected MCP servers from one profile into another. `--overwrite` replaces servers already in the target. |

Compare [share levels](../user/share-levels.md) before changing profile sharing. See [Manage profiles](../user/manage-profiles.md) for hook sets, MCP servers, and full command procedures.

## Diagnostics and help

| Command | Purpose |
|---|---|
| `aprofile doctor` | Check profiles and config directories for isolation and configuration problems. Add `--json` for machine-readable findings. |
| `aprofile ui` | Browse and edit profiles interactively. The same operations are available as commands. |
| `aprofile --help` | Show top-level command help. Append `--help` to a command for its usage and flags. |

Use [Troubleshoot profiles](../user/troubleshooting.md) to investigate doctor findings. Run `aprofile <command> --help` when you need the available flags for a command.
