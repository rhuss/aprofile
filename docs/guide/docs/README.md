# aprofile User Guide

<!-- Sources:
aprofile:README.md:3-9,13-23,25-48,81,112-120,140,199-203,286
aprofile:internal/cli/group.go:54-62
claude-code:README.md:7
codex:README.md:1
opencode:README.md:10
pi:README.md:13-17
aprofile:go.mod:1-3
-->

## What you get

With aprofile, you can launch work, personal, and client accounts through named commands such as `claude-work`. Each profile uses its own login and can keep its conversations separate by default, while selected profiles can share history when they need to continue one another's work. This avoids switching configuration directories by hand, which can mix conversations, memory, or billing.

## What the parts are

This guide calls a supported coding agent command-line program a **harness**.

aprofile is a command-line tool for managing named accounts or configurations of coding tools. A **profile** is one account or configuration of one harness, identified by the harness and a name, such as `claude/work`.

| Tool | What it does |
|---|---|
| Claude Code | A coding tool in your terminal that understands your codebase and carries out tasks through natural language commands. |
| Codex CLI | OpenAI's coding agent that runs locally on your computer. |
| OpenCode | An open source AI coding agent. |
| Pi | A minimal, extensible agent harness that you can customize with extensions, skills, prompt templates, and themes. |

A **wrapper** is the launcher aprofile creates for a profile, such as `claude-work`.

## How they fit

You create profiles, then run `./bin/aprofile sync` from the repository checkout root to prepare their configuration and write wrappers. A wrapper starts its harness with the profile's login, endpoint, and model, where configured. Each profile's **share level** controls how much of the harness's default setup it reuses.

History is separate from share level. A profile in no history group keeps its history to itself, which is the default for new profiles. You can optionally put profiles of the same harness in a history group so they can resume one another's conversations. Group membership selects history; the share level continues to control setup reuse. Logins stay private in a group. See [History groups](user/history-groups.md) for details.

Profile changes can leave wrappers stale. Run `./bin/aprofile sync` after adding, editing, renaming, copying, importing, or removing profiles to bring the launchers up to date.

## Ways to run it

Choose a share level for how much setup a profile should reuse: `content` reuses instructions and skills, `all` uses the default setup live, and `none` starts clean. Then choose history separately: leave the profile out of a group to keep its history isolated, or join a same-harness group to share its conversations. [Compare share levels](user/share-levels.md) and [learn about history groups](user/history-groups.md) before choosing.

You can manage profiles with `./bin/aprofile` commands or use `./bin/aprofile ui` to browse and edit them. The same operations are available for scripts.

## Status and limits

aprofile runs on Linux and macOS. Building from source requires Go <!-- guide:attr aprofile-go-minimum -->1.24.0<!-- /guide:attr --> or newer. Building from source is the supported installation path; release packaging is not included in this baseline. See [Get started](user/getting-started.md) for build steps.

History groups are supported for Claude Code, Codex CLI, and Pi. OpenCode does not support them. Resuming the same conversation in two profiles at once can interleave its transcript, so use one member at a time. History groups do not convert conversations between harnesses.

## Where to go next

- **End user:** [Get started](user/getting-started.md) to build aprofile and launch your first profile.
- Read [Profile basics](overview/profile-basics.md) for the concepts behind profiles and sharing.
- Compare [share levels](user/share-levels.md), then configure [history groups](user/history-groups.md) if selected profiles should share conversations.
- Manage existing accounts with [Manage profiles](user/manage-profiles.md).
- Diagnose problems with [Troubleshoot profiles](user/troubleshooting.md).
- Look up syntax in the [Command reference](reference/commands.md).
- Use the [table of contents](SUMMARY.md) to follow the guide in order.

This guide describes the sources pinned on <!-- guide:attr baseline-date -->2026-11-08<!-- /guide:attr -->: <!-- guide:attr baseline-sources -->aprofile 4a743a4, claude-code 0ed9469, codex 748ab55, opencode da8c021, pi 6e2222a<!-- /guide:attr -->.
