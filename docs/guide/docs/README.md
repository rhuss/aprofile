# aprofile User Guide

<!-- Sources:
aprofile:README.md:3-9,13-23,25-48,54-72,76-87,165-182,197-207,240-242
aprofile:internal/harness/builtin/claude.yaml:6-12
aprofile:internal/harness/builtin/codex.yaml:6-12
aprofile:internal/harness/builtin/opencode.yaml:11-19
aprofile:internal/harness/builtin/pi.yaml:11-17
aprofile:internal/cli/edit.go:21-25
aprofile:go.mod:1-3
claude-code:README.md:7
codex:README.md:1
opencode:README.md:10
pi:README.md:13-21
-->

## What you get

Keep your work, personal, and client accounts separate while using them on the same computer. With aprofile, you'll create a named profile and launch your coding tool with a short command such as `claude-work`. You won't have to point the tool at a different configuration directory by hand each time, which can mix up conversations, memory, or billing.

## What the parts are

aprofile is a command-line tool that manages separate accounts or configurations for coding tools. A **profile** is one account or configuration of one tool, identified by the tool and a name, such as `claude/work`.

The supported tools help you work on code:

| Tool | What it does for you |
|---|---|
| Claude Code | Runs in your terminal, understands your codebase, and carries out coding tasks you describe in natural language. |
| Codex CLI | OpenAI's coding agent, a program that works on coding tasks and runs locally on your computer. |
| OpenCode | An open source AI coding agent that helps you work on code. |
| Pi | A minimal coding agent you can customize to fit your workflow. |

This guide calls a supported coding agent command-line program a **harness**. A **wrapper** is the launcher aprofile creates for a profile, such as `claude-work`.

## How they fit

aprofile knows how to select a separate configuration for each supported tool. You create a profile, then run `aprofile sync` to prepare that configuration and write its wrapper. Running the wrapper starts the coding tool with the profile's environment. The wrapper forwards your arguments unchanged, so the coding tool still handles your work.

A **share level** decides how much of the tool's default setup a profile uses. You can reuse instructions while keeping settings separate, reuse more of the default setup, or start clean. Credentials and runtime state, such as conversations, sessions, and memory, stay private to each profile at every share level.

Profile changes can leave wrappers stale. Run `aprofile sync` after adding, editing, renaming, copying, importing, or removing profiles to bring the launchers up to date.

## Ways to run it

Choose the share level that fits the account:

- `content` reuses instructions and skills while keeping the profile's own settings. It's the default for new profiles unless you change the default setting.
- `all` reuses the default setup live, so changes to shared settings also affect the profile.
- `none` starts clean without borrowing the default setup's content or settings.

[Compare share levels](user/share-levels.md) before choosing how much to share. You can manage profiles with commands or use `aprofile ui` to browse and edit them interactively; the same operations are available for scripts.

## Status and limits

aprofile runs on Linux and macOS. Building from source requires the Go compiler, version <!-- guide:attr aprofile-go-minimum -->1.24.0<!-- /guide:attr --> or newer. It's the supported installation path; release packaging isn't included in this baseline. See [Get started](user/getting-started.md) for the build steps.

Profile separation has limits: Codex CLI and OpenCode also read skills from locations outside the profile. No share level isolates those locations. Shared settings and plugin storage can also receive concurrent writes from different profiles. The [share-level comparison](user/share-levels.md) explains these limits before you choose a setup.

## Where to go next

- **End user:** [Get started](user/getting-started.md) to build aprofile and launch your first profile.
- Read [Profile basics](overview/profile-basics.md) for the concepts behind profiles and sharing.
- Use the [table of contents](SUMMARY.md) to follow the guide in order.

This guide describes the sources pinned on <!-- guide:attr baseline-date -->2026-11-06<!-- /guide:attr -->: <!-- guide:attr baseline-sources -->aprofile 483957d, claude-code 0ed9469, codex 6abffd4, opencode cd47971, pi 6bf2ab9<!-- /guide:attr -->.
