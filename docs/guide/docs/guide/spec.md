# Guide spec: aprofile User Guide

## Why

### Problem

Managing work, personal, or client accounts for one coding tool by hand means pointing each account at a separate configuration directory, which is fiddly and can mix transcripts, memory, or billing. (Source: `aprofile:README.md:7-9`.)

### Outcome

The reader can create a named profile and use a short launcher such as `claude-work` to start the selected coding tool with that profile's configuration and private credentials and runtime state. (Source: `aprofile:README.md:3-5,25-40,42-48`.)

### Components

- aprofile is a command-line tool for running accounts or configurations of coding tools side by side, each with its own credentials, history, memory, and settings. (Source: `aprofile:README.md:3-5`.)
- Claude Code is an agentic coding tool that runs in a terminal, understands the codebase, and handles coding work through natural language commands. (Source: `claude-code:README.md:7`.)
- Codex CLI is a coding agent from OpenAI that runs locally on the user's computer. (Source: `codex:README.md:1`.)
- OpenCode is an open source AI coding agent. (Source: `opencode:README.md:10`.)
- Pi is a minimal, extensible agent harness that users can customize. (Source: `pi:README.md:13-17`.)

### Fit

aprofile has a built-in definition for each supported coding tool, including the environment variable that selects its configuration directory. (Source: `aprofile:README.md:171-182`; `aprofile:internal/harness/builtin/claude.yaml`; `aprofile:internal/harness/builtin/codex.yaml`; `aprofile:internal/harness/builtin/opencode.yaml`; `aprofile:internal/harness/builtin/pi.yaml`.)

When the reader runs `aprofile sync`, aprofile prepares the profile configuration and writes a launcher that starts the selected tool with the profile's environment. (Source: `aprofile:README.md:32-40,197-207`.)

### Why this combination

Separate profiles let users run work, personal, or client accounts side by side while choosing how much default setup each profile shares, with credentials and runtime state kept private to each profile. (Source: `aprofile:README.md:9,42-58,62-72`.)

### Status and limits

aprofile runs on Linux and macOS, builds from source with Go 1.24 or newer, and does not yet include release packaging in this slice. (Source: `aprofile:README.md:11-23,240-242`.)

## The guide

### Purpose

Help end users build aprofile, create and launch their first profile, choose a share level, manage profiles, diagnose common problems, and find command syntax.

### Persona and journey

- **End user**: learns what a profile separates, builds aprofile from an available source checkout, creates and syncs a profile, launches its wrapper, chooses a share level, manages profiles and hook sets, and uses `show` or `doctor` to inspect problems.

### Sections and pages

#### Overview

- `docs/README.md` is the start page. It carries the six-part Why card, defines aprofile and each named coding tool from its own pinned source, explains how the wrapper and profile fit together, and links readers to setup and reference pages.
- `docs/overview/profile-basics.md` is the overview section page. It explains the terms profile, harness, default setup, wrapper, and share level, then introduces the three share levels and the data that remains private at each level.

#### End user guide

- `docs/user/getting-started.md` is the End user entry page. It starts with the outcome for this persona, then shows the supported build-from-source path, profile creation, `sync`, and launching a generated wrapper. The draft uses an existing source checkout and omits a clone command while the repository URL is unresolved.
- `docs/user/share-levels.md` helps the reader choose `all`, `content`, or `none`. It explains what each level shares, what always stays private, and the documented differences and limits across supported tools.
- `docs/user/manage-profiles.md` covers listing and inspecting profiles, editing and renaming, removal and its `--purge` confirmation, copying profiles, login, MCP server copy, hook sets, and running `sync` after changes.
- `docs/user/troubleshooting.md` uses `show` and `doctor` to inspect stale wrappers, PATH problems, missing harness binaries, unresolved references, and unclassified entries. It gives only fixes supported by the pinned aprofile source.

#### Reference

- `docs/reference/commands.md` is a concise command index for the end user. It groups profile, sharing, hook, MCP, and diagnostic commands and points to the harness-specific documentation where appropriate.

### Diagrams

No diagrams are planned. The profile setup path is short enough to explain with numbered steps, and the share-level differences fit in a table.

### Experimental content

No upstream change requests were supplied, so the guide has no experimental sections.

### Internal part

There is no internal part. The guide contains no confidential input claims.

### Sources

The guide pins this checkout and the four upstream sources that define the supported coding tools.

| ID | Repository | Commit |
|---|---|---|
| `aprofile` | local checkout `../..` | `483957da3f5eadd28e72661f5fd53875efda88d4` |
| `claude-code` | `anthropics/claude-code` | `0ed94698aa0aeee471465fc9c867c2e8434c4d68` |
| `codex` | `openai/codex` | `6abffd442d6865a72056b305a31a604b83d087d8` |
| `opencode` | `anomalyco/opencode` | `cd47971fd60f9a46dab2600087a691064c7f46fa` |
| `pi` | `earendil-works/pi` | `6bf2ab9bbb64fd14793e506a5404078441219b95` |

No additional inputs were registered.

### Open questions

- The pinned aprofile README gives `https://github.com/cc-deck/aprofile` as the clone URL, but a source lookup during setup returned `Repository not found`. Should the install page start from an existing checkout and omit the clone command, or will the author supply another repository URL that can be fetched?
- The weekly drift workflow is committed at the parent repository's `.github/workflows/guide-drift.yml`, because GitHub discovers workflows only at the repository root. This checkout has no configured GitHub remote, so the schedule will run only after the repository is hosted on GitHub.
