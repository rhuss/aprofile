# aprofile

Give each account and endpoint of your coding agents its own command.

Claude Code, Codex, OpenCode and pi each keep their login, settings and history in a single directory. If you use one of them with more than one account (work and personal, a client, a company gateway, a local model), you probably switch that directory or a few environment variables by hand. Pick the wrong one and you are logged in as the wrong person, or you find a work conversation in your personal history. aprofile gives every account its own copy of that directory and a command to start it, and you decide what the profiles share.

Profiles and groups are named `<harness>/<name>`, for example `claude/work`; `aprofile add` takes the two parts as separate words.

## Two accounts on one machine

```sh
# Create a profile for each account; each one signs in on first start
aprofile add claude work --auth-kind oauth-login
aprofile add claude personal --auth-kind oauth-login --group home   # join the built-in "home" group: share history with plain claude

# Write the launcher commands (claude-work, claude-personal) and put them on your PATH
aprofile sync

claude-work       # work account, with its own conversation history
claude-personal   # personal account; can resume conversations started with plain claude
```

## A company endpoint for your team

Each developer runs the same few commands once:

```sh
# A profile that talks to the company gateway with a token from your environment
aprofile add claude corp --endpoint https://llm.example.com \
  --auth-kind auth-token --auth-key env:CORP_LLM_TOKEN --model team-default

# Optional: a "work" group, so corp and your other work profiles (say, a client
# account) continue each other's conversations, apart from your personal ones
aprofile group create claude/work
aprofile edit claude/corp --group work
aprofile sync

claude-corp       # company endpoint and model; shares history with work profiles only
claude-corp --model other-model   # one run with another model (exported variables never override a profile)
```

Logins always stay inside their profile. Per profile, you choose whether instructions, skills and settings come from your normal setup or stay separate. The guide below explains the details.

<!-- guide:begin -->
<!-- Generated from docs/guide/docs/README.md up to guide:end. Edit that file and
     run make -C docs/guide readme from the repository root. The text above
     guide:begin is hand-written and kept as it is. -->

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

History is separate from share level. A profile in no history group keeps its history to itself, which is the default for new profiles. You can optionally put profiles of the same harness in a history group so they can resume one another's conversations. Group membership selects history; the share level continues to control setup reuse. Logins stay private in a group. See [History groups](docs/guide/docs/user/history-groups.md) for details.

Profile changes can leave wrappers stale. Run `./bin/aprofile sync` after adding, editing, renaming, copying, importing, or removing profiles to bring the launchers up to date.

Curated capability toggles live one layer above the launch settings: `./bin/aprofile features <harness>/<name>` turns harness capabilities such as OTel telemetry or the Auto-mode decision log on or off for one profile, mapping them to whatever mechanism the harness itself uses (no hand-editing of its settings file). Run it without flags to list what the harness supports. Anything outside the catalog is still reachable through the raw `--env` and `--env-literal` escape hatch on `add` and `edit`.

Tools such as Corgi can also start a profile from its folder alone. `sync` writes the launch settings the folder can hold, and for Claude profiles with their own settings it blocks provider switches and keys exported by the launching tool. A Claude login profile at share level `all` gets no such protection; [Register profiles with Corgi](docs/guide/docs/user/corgi.md) explains the details.

## Ways to run it

Choose a share level for how much setup a profile should reuse: `content` reuses instructions and skills, `all` uses the default setup live, and `none` starts clean. Then choose history separately: leave the profile out of a group to keep its history isolated, or join a same-harness group to share its conversations. [Compare share levels](docs/guide/docs/user/share-levels.md) and [learn about history groups](docs/guide/docs/user/history-groups.md) before choosing.

You can manage profiles with `./bin/aprofile` commands or use `./bin/aprofile ui` to browse and edit them. The same operations are available for scripts.

## Status and limits

aprofile runs on Linux and macOS. Building from source requires Go <!-- guide:attr aprofile-go-minimum -->1.24.0<!-- /guide:attr --> or newer. Building from source is the supported installation path; release packaging is not included in this baseline. See [Get started](docs/guide/docs/user/getting-started.md) for build steps.

History groups are supported for Claude Code, Codex CLI, and Pi. OpenCode does not support them. Resuming the same conversation in two profiles at once can interleave its transcript, so use one member at a time. History groups do not convert conversations between harnesses.

## Where to go next

- **End user:** [Get started](docs/guide/docs/user/getting-started.md) to build aprofile and launch your first profile.
- Read [Profile basics](docs/guide/docs/overview/profile-basics.md) for the concepts behind profiles and sharing.
- Compare [share levels](docs/guide/docs/user/share-levels.md), then configure [history groups](docs/guide/docs/user/history-groups.md) if selected profiles should share conversations.
- Manage existing accounts with [Manage profiles](docs/guide/docs/user/manage-profiles.md).
- Diagnose problems with [Troubleshoot profiles](docs/guide/docs/user/troubleshooting.md).
- Look up syntax in the [Command reference](docs/guide/docs/reference/commands.md).
- Use the [table of contents](docs/guide/docs/SUMMARY.md) to follow the guide in order.

This guide describes the sources pinned on <!-- guide:attr baseline-date -->2026-11-08<!-- /guide:attr -->: <!-- guide:attr baseline-sources -->aprofile 4a743a4, claude-code 0ed9469, codex 748ab55, opencode da8c021, pi 6e2222a<!-- /guide:attr -->.

## Project information

[Project details and licensing](docs/README.md).
<!-- guide:end -->
