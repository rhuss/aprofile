# Profile basics

<!-- Sources:
aprofile:README.md:3-9,13,32-48,64-73,81,112-120,124-126,140,203,224-229,286
aprofile:go.mod:1-3
aprofile:internal/cli/group.go:57-61
aprofile:internal/harness/builtin/claude.yaml:92-104
aprofile:internal/harness/builtin/codex.yaml:44-61
aprofile:internal/harness/builtin/opencode.yaml:54-63
aprofile:internal/harness/builtin/pi.yaml:45-59
claude-code:README.md:7
codex:README.md:1
opencode:README.md:10
pi:README.md:13-19
-->

## Why this matters

You can run work, personal, or client accounts side by side and start each with a short command. aprofile manages named accounts or configurations of coding tools, so you do not have to switch configuration directories by hand. That matters when separate accounts need separate conversations, or when selected accounts should be able to continue the same conversation.

Without separate profiles, a manual configuration switch can mix transcripts, memory, or billing. aprofile prepares a wrapper for each profile, such as `claude-work`, so the short command starts the intended account. A profile uses its own login, endpoint, and model.

## What a profile is

A *harness* is a coding agent command-line program. aprofile supports four:

- Claude Code is an agentic coding tool in your terminal that understands your codebase and helps with coding, explanations, and Git workflows through natural language.
- Codex CLI is a coding agent from OpenAI that runs locally on your computer.
- OpenCode is an open source AI coding agent.
- Pi is a minimal, extensible agent harness that you can customize with extensions, skills, prompt templates, and themes.

A *profile* is one account or configuration of one harness, identified by the harness and a name, such as `claude/work`. A *wrapper* is the launcher aprofile creates for a profile. `claude-work`, for example, starts Claude Code with that profile's environment and configuration.

The *default setup* is the harness configuration you use outside an aprofile profile. aprofile can reuse parts of that setup, and the built-in `home` history group uses its history store. A custom group keeps its history in aprofile's data directory.

Run `./bin/aprofile sync` from the repository checkout root after changing profiles so their wrappers reflect those changes.

Profiles have two separate choices: a share level and history-group membership. The share level controls how much of the harness's default setup the profile uses. A *history group* is a named set of profiles of one harness that share one conversation history. A profile in no group keeps its history to itself. Group membership does not change the share level.

For example, put `claude/work` and `claude/personal` in different history groups, or leave both out of a group, to keep their conversations apart. If two Claude Code accounts should continue each other's conversations, put both profiles in the same group. Each account still uses its own login, endpoint, and model.

## What stays private

History is private to each profile unless you choose to share it through a history group. Group members share the history entries that the harness marks for sharing; credentials and other runtime state, such as process state, logs, and caches, remain per profile. Settings continue to follow the profile's share level. Copying a profile does not copy its history.

This separation means two profiles can share setup without sharing conversations. It also means two profiles can share history while keeping their account details separate. For the membership choices and commands, see [History groups](../user/history-groups.md).

## What a share level changes

A *share level* controls how much of the harness's default setup a profile uses. Choose it per profile, according to how closely you want that profile to follow your usual configuration:

| Share level | What it means for you |
| --- | --- |
| `all` | Uses the default setup live, including its settings. Credentials, non-history runtime state, and owned data remain private. |
| `content` | Reuses instructions and other content. The profile gets its own settings, copied from the default setup once. |
| `none` | Starts clean without borrowing the default setup's content or settings. |

New profiles use `content` unless you have changed aprofile's `default_share` setting. Share level decides what setup a profile reuses; history-group membership separately decides which profiles share designated history. Codex CLI and OpenCode also read skills from shared locations outside the profile configuration, so even `none` cannot isolate those locations. `./bin/aprofile show` and `./bin/aprofile doctor` report them.

History groups work with Claude Code, Codex CLI, and Pi. OpenCode does not support them. Running the same conversation through two group members at once can interleave its transcript, and a history group does not convert conversations between different harnesses. aprofile runs on Linux and macOS and builds from source with the Go version recorded in its `go.mod` file.

Continue with [Get started](../user/getting-started.md) to create your first profile, or [Compare share levels](../user/share-levels.md) for the settings, plugin, and hook details.
