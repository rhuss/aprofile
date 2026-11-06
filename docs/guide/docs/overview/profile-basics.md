# Profile basics

<!-- Sources:
aprofile:README.md:3-9,25-48,54-58,64-80,101,169,173-180,184-199
claude-code:README.md:7
codex:README.md:1
opencode:README.md:10
pi:README.md:13-17
-->

## Why this matters

You can run work, personal, or client accounts side by side and start each with a short command. aprofile is the command-line tool that manages those named accounts or configurations. It keeps each account's credentials and runtime state separate while letting you choose how much setup to share.

Without that help, you have to point each account at the right configuration yourself. A mistake can mix transcripts, memory, or billing across accounts. With aprofile, a launcher such as `claude-work` starts the coding tool with the work profile already selected.

## What a profile is

A *harness* is a coding agent command-line program: a tool you ask to help with coding work. aprofile includes support for four:

- Claude Code runs in your terminal and handles coding tasks, code explanations, and Git workflows through natural language commands.
- Codex CLI is a coding agent from OpenAI that runs locally on your computer.
- OpenCode is an open source AI coding agent.
- Pi is a minimal, customizable agent harness that you can extend with prompt templates, themes, and other additions.

A *profile* is one account or configuration of one harness. Its identity combines the harness and a name, such as `claude/work`. The name tells you which configuration you're choosing; the harness tells aprofile which program to start.

Your *default setup* is the harness configuration you already use outside an aprofile profile. A profile can reuse parts of that setup, such as instructions, while keeping its account separate.

A *wrapper* is the launcher aprofile creates for a profile. For example, `claude-work` starts Claude Code with the profile's environment and configuration. The wrapper forwards your arguments to the coding tool, so you can keep using its usual command options.

`aprofile sync` prepares the profile configuration and writes its wrapper. Changes to profiles can leave wrappers stale: after adding, editing, renaming, copying, removing, or importing a profile, run `aprofile sync` to update them.

## What stays private

A profile keeps several kinds of data of its own. These categories help you understand what sharing will and won't change:

- *Credentials* are the profile's login information. aprofile never links or copies entries in this category.
- *Runtime state* includes history, sessions, memory, logs, and caches. Each profile has its own state; state entries start empty and aren't linked or copied.
- *Owned data* includes profile-specific choices such as which model the coding tool uses. These settings stay private at every share level, although an explicit profile copy carries owned data over.

Shared content is a different category. Instructions, prompts, and commands can come from your default setup without sharing your login or conversation history. That distinction lets you use the same instructions for work and personal accounts while keeping their sessions separate.

## What a share level changes

A *share level* controls how much of the default setup a profile uses. Choose it per profile, according to how closely you want that profile to follow your usual configuration:

| Share level | What it means for you |
| --- | --- |
| `all` | Uses the default setup live, including its settings. Credentials, runtime state, and owned data remain private. |
| `content` | Reuses instructions and other content. The profile gets its own settings, copied from the default setup once. |
| `none` | Starts clean without borrowing the default setup's content or settings. |

New profiles use `content` unless you've changed aprofile's `default_share` setting. It suits work and personal accounts that need common instructions but separate permissions. `all` follows changes to your default setup; `none` keeps personal instructions and plugins out of a client profile.

Share levels have limits. Codex CLI and OpenCode also read skills from shared locations outside the profile configuration, so even `none` can't isolate those locations. `aprofile show` and `aprofile doctor` report them.

Continue with [Get started](../user/getting-started.md) to create your first profile, or [Compare share levels](../user/share-levels.md) for the settings, plugin, and hook details.
