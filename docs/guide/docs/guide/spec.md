# Guide spec: aprofile User Guide

<!-- Sources:
aprofile:README.md:3-5,7-23,25-48,112-142,233-245,284-286
aprofile:internal/cli/group.go:55-62
aprofile:internal/history/store.go:13-55
aprofile:internal/harness/builtin/claude.yaml
aprofile:internal/harness/builtin/codex.yaml
aprofile:internal/harness/builtin/opencode.yaml
aprofile:internal/harness/builtin/pi.yaml
claude-code:README.md:7
codex:README.md:1
opencode:README.md:10
pi:README.md:13-21
url:https://github.com/rhuss/aprofile
-->

## Why

### Problem

Managing work, personal, and client accounts by manually switching configuration directories can mix conversations, memory, or billing. Some related accounts also need to continue the same conversations while other accounts must stay separate. (Source: `aprofile:README.md:7-9,112-127`.)

### Outcome

The reader can launch each account with a named wrapper, choose how much setup it reuses, and keep history isolated or explicitly share it with profiles of the same supported harness. The selected wrapper uses that profile's login, endpoint, and model. (Source: `aprofile:README.md:25-48,112-140`.)

### Components

- aprofile manages named accounts or configurations of coding tools. (Source: `aprofile:README.md:3-5`.)
- Claude Code is a terminal coding tool that works on a codebase through natural language. (Source: `claude-code:README.md:7`.)
- Codex CLI is OpenAI's locally running coding agent. (Source: `codex:README.md:1`.)
- OpenCode is an open source AI coding agent. (Source: `opencode:README.md:10`.)
- Pi is a minimal, extensible agent harness. (Source: `pi:README.md:13-17`.)

### Fit

aprofile prepares each profile's configuration and launcher on sync. Share levels govern reused setup. A separate, optional history-group membership links designated history entries to one store for that harness. The built-in home group uses the default setup's history; custom groups use an aprofile-managed store. (Source: `aprofile:README.md:32-48,112-120`; `aprofile:internal/cli/group.go:55-62`; `aprofile:internal/history/store.go:13-55`.)

### Why this combination

Users can keep unrelated accounts separate while letting selected profiles continue one another's conversations, without making their logins shared. History-group membership is independent of the share level. (Source: `aprofile:README.md:112-120`.)

### Status and limits

aprofile runs on Linux and macOS and builds from source with Go <!-- guide:attr aprofile-go-minimum -->1.24.0<!-- /guide:attr --> or newer. History groups are supported for Claude Code, Codex, and Pi; OpenCode does not support them. Running one conversation simultaneously through multiple members can interleave its transcript. This feature does not convert conversations between harnesses. (Source: `aprofile:README.md:11-23,112-142,284-286`; `aprofile:internal/harness/builtin/claude.yaml`; `aprofile:internal/harness/builtin/codex.yaml`; `aprofile:internal/harness/builtin/opencode.yaml`; `aprofile:internal/harness/builtin/pi.yaml`.)

## The guide

### Purpose

Help end users build aprofile, create and launch profiles, choose share levels and history groups, manage profiles, diagnose common problems, and find command syntax.

### Persona and journey

- **End user**: learns what a profile separates, builds aprofile from source, creates and syncs a profile, launches its wrapper, chooses a share level and history group, manages profiles and hook sets, and uses `show` or `doctor` to inspect problems.

### Sections and pages

#### Overview

- `docs/README.md` is the start page. It carries the six-part Why card, defines aprofile and each named coding tool from its own pinned source, explains how the wrapper and profile fit together, and links readers to setup and reference pages.
- `docs/overview/profile-basics.md` is the overview section page. It explains the terms profile, harness, default setup, wrapper, and share level, then introduces the three share levels and the data that remains private at each level.

#### End user guide

- `docs/user/getting-started.md` is the End user entry page. It starts with the outcome for this persona, then shows the supported build-from-source path, profile creation, `sync`, and launching a generated wrapper.
- `docs/user/share-levels.md` helps the reader choose `all`, `content`, or `none`. It explains what each level shares, what always stays private, and the documented differences and limits across supported tools.
- `docs/user/history-groups.md` explains how profiles of one harness can share conversation history through `home` or a custom group, or keep history isolated without group membership. It covers the relationship between history membership and share levels, supported harnesses, and the known limits.
- `docs/user/manage-profiles.md` covers listing and inspecting profiles, editing and renaming, removal and its `--purge` confirmation, copying profiles, login, MCP server copy, hook sets, and running `sync` after changes.
- `docs/user/troubleshooting.md` uses `show` and `doctor` to inspect stale wrappers, PATH problems, missing harness binaries, unresolved references, and unclassified entries. It gives only fixes supported by the pinned aprofile source.

#### Reference

- `docs/reference/commands.md` is a concise command index for the end user. It groups profile, sharing, history-group, hook, MCP, and diagnostic commands and points to the harness-specific documentation where appropriate.

### Diagrams

No diagrams are planned. The profile setup path is short enough to explain with numbered steps, and the share-level and history-group differences fit in tables.

### Experimental content

No upstream change requests were supplied, so the guide has no experimental sections.

### Internal part

There is no internal part. The guide contains no confidential input claims.

### Sources

The guide pins this checkout and the four upstream sources that define the supported coding tools. The local source field remains `../..`; the public repository is [github.com/rhuss/aprofile](https://github.com/rhuss/aprofile).

| ID | Repository | Commit |
|---|---|---|
| `aprofile` | local checkout `../..` | <!-- guide:attr aprofile-commit -->483957da3f5eadd28e72661f5fd53875efda88d4<!-- /guide:attr --> |
| `claude-code` | `anthropics/claude-code` | <!-- guide:attr claude-code-commit -->0ed94698aa0aeee471465fc9c867c2e8434c4d68<!-- /guide:attr --> |
| `codex` | `openai/codex` | <!-- guide:attr codex-commit -->6abffd442d6865a72056b305a31a604b83d087d8<!-- /guide:attr --> |
| `opencode` | `anomalyco/opencode` | <!-- guide:attr opencode-commit -->cd47971fd60f9a46dab2600087a691064c7f46fa<!-- /guide:attr --> |
| `pi` | `earendil-works/pi` | <!-- guide:attr pi-commit -->6bf2ab9bbb64fd14793e506a5404078441219b95<!-- /guide:attr --> |

No additional inputs were registered.

### Open questions

- The weekly drift workflow is committed at the parent repository's `.github/workflows/guide-drift.yml`, because GitHub discovers workflows only at the repository root.
