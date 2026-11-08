# aprofile User Guide Implementation Plan

<!-- Sources:
aprofile:README.md:3-242
aprofile:internal/cli/root.go:96-104
aprofile:specs/003-history-groups/spec.md:1-50
-->

> **Historical create plan:** This plan records the original isolated-profile scope. For the evolved guide scope, see the [evolved guide specification](spec.md) and the [2026-11-08 guide evolution plan](plans/2026-11-08-evolve.md). The source entries below were refreshed against the target source revision; line hints elsewhere in this preserved plan describe its original research and are not current verified recipes.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan page by page. Follow the task procedure and use checkbox syntax to track work.

**Goal:** Give end users a source-verified path to build aprofile, create and run profiles, choose a share level, manage profiles, and find command syntax.

**Architecture:** Write seven GFM pages in the adapter's existing `docs/` tree and navigation order. Each page cites the pinned local aprofile checkout and, where it names a supported coding tool, that tool's own pinned repository. The weekly drift workflow is installed at the parent repository root because the guide is nested under `docs/guide`.

**Tech Stack:** GitHub Markdown, GFM adapter, pinned Git sources, `make check`.

**Spec:** `docs/guide/spec.md`

## Global Constraints

- Guide: "aprofile User Guide", format `gfm`, rigor `fast`, cc-prose present (present: write with the section voice profiles and run the prose check; absent: follow `style-rules.md`).
- Baseline (captured 2026-11-06); every statement is verified at these commits, never at a branch head:

  | Source | Repository | Commit | Status |
  |---|---|---|---|
  | `aprofile` | `../..` | <!-- guide:attr aprofile-commit -->4a743a4b404f049f8ea6f0c22fc3cbdef8e85c9f<!-- /guide:attr --> | verified |
  | `claude-code` | `anthropics/claude-code` | <!-- guide:attr claude-code-commit -->0ed94698aa0aeee471465fc9c867c2e8434c4d68<!-- /guide:attr --> | verified |
  | `codex` | `openai/codex` | <!-- guide:attr codex-commit -->748ab55ca7de0b647d9a7a9d9a8e70e00f28666d<!-- /guide:attr --> | verified |
  | `opencode` | `anomalyco/opencode` | <!-- guide:attr opencode-commit -->da8c0211f7c00afbf1dd96e629801fe84d205541<!-- /guide:attr --> | verified |
  | `pi` | `earendil-works/pi` | <!-- guide:attr pi-commit -->6e2222a8dda06e3d69fad46d630f152f5873567a<!-- /guide:attr --> | verified |

- Baseline values available to pages (never type versions or commits): `aprofile-repo`, `aprofile-commit`, `aprofile-commit-short`, `aprofile-blob`, `aprofile-tree`; `claude-code-repo`, `claude-code-commit`, `claude-code-commit-short`, `claude-code-blob`, `claude-code-tree`; `codex-repo`, `codex-commit`, `codex-commit-short`, `codex-blob`, `codex-tree`; `opencode-repo`, `opencode-commit`, `opencode-commit-short`, `opencode-blob`, `opencode-tree`; `pi-repo`, `pi-commit`, `pi-commit-short`, `pi-blob`, `pi-tree`; `baseline-date`, `baseline-sources`.
- Change requests documented as experimental (label only through the adapter snippet): none.
- Personas and their entry pages: `user` (End user), `user/getting-started`.
- Sections and voices: Overview (`conversational`); End user guide (`tutorial`); Reference (`reference`).
- Every page starts with a source block after the title; entries are `<source-id>:<path>[:<lines>]`, `url:<url>`, or (internal pages only) `input:<input-id>`. Facts without a pinned source stay out of the page.
- Inputs are hints, never proof. Registered inputs: none. Content derived from a confidential input (none) appears only on internal pages.
- Shareable pages never contain: no confidential terms, no text matching confidential patterns, or links into the internal part (`internal/`, disabled).
- No em dash or en dash characters in any file.
- GitHub Markdown; source block and expectations as HTML comments; baseline values as generated regions `<!-- guide:attr <name> -->...<!-- /guide:attr -->` (never inside listings), rewritten by `make site`; GitHub alerts for admonitions; experimental labels as IMPORTANT alerts with the fixed first line after an `<a id>` anchor; diagrams linked to their full-size SVG; pages under `docs/` with the order in `docs/SUMMARY.md`, internal pages under `internal/`.
- Admonitions: only NOTE, TIP, IMPORTANT, WARNING, CAUTION, with their fixed meanings (see `page-procedure.md`).
- Task pages: a why-paragraph, numbered steps with copy-paste commands, `Check:` lines with expectations.
- The start page and the section overview pages follow `overview-contract.md`: why before how, the spec's `## Why` card as their content, every product defined from its own pinned source, and the cold-reader test as their review gate.
- Diagrams follow the diagrams skill. Every label, arrow, and frame is a claim with a source in the diagram's claims file (`diagrams/<name>.claims.yaml`); where a product runs its parts comes from its own architecture documentation at the pinned version. Palette: platform gray (`#e9ecef`/`#495057`), governance orange (`#ffe8cc`/`#e8590c`), admin red (`#ffe3e3`/`#c92a2a`), user blue (`#d0ebff`/`#1971c2`); text and legends `#495057`.
- Run every command from the guide project root with path arguments; never `cd` into `.guide/cache/`.
- Containers: `podman`, never `docker`. Python packages: `uv`, never pip.
- Commit messages end with the trailer `Assisted-By: 🤖 Claude Code`. Only the controller pushes, and only when the author opted in (local commits only).
- The aprofile source is a local checkout with no verified public web URL. Do not use generated `aprofile-repo`, `aprofile-blob`, or `aprofile-tree` values as public links. Use pinned source IDs and paths for citations. Use a clone command only after the author supplies a fetchable repository URL.

## Review Focus

- The README clone URL may be unavailable. The getting-started page uses only a URL the author confirms, or gives build steps from an existing source checkout.
- Authentication examples use secret references such as `env:WORK_TOKEN`; they never contain a secret value.
- `aprofile sync` is required after profile changes that leave wrappers stale.
- Credentials and non-history runtime state remain private at every share level. Designated history entries follow group membership; content, settings, hooks, and plugin behavior follow the selected share level.
- Codex and OpenCode read some skills from paths under `$HOME`, outside the profile configuration directory; explain the documented limit when describing share levels.

---

### Task 1: Start page

**Page path:** `docs/README.md`

**Outline:** Follows `overview-contract.md`. Carry the Why card into the start page shape: what readers get, what aprofile and each supported coding tool do, how the wrapper and profile fit together, where to go next, and status and limits. Keep repository layout and implementation detail out of this page.

**Voice:** `conversational`.

**Facts to verify with source hints:**
- Profile problem and outcome: `aprofile:README.md:3-9,25-48`.
- Definitions for every named product: `aprofile:README.md:3-5`; `claude-code:README.md:7`; `codex:README.md:1`; `opencode:README.md:10`; `pi:README.md:13-17`.
- Fit and launcher behavior: `aprofile:README.md:32-40,171-182,197-207`; the four files under `aprofile:internal/harness/builtin/`.
- Why the share levels matter: `aprofile:README.md:42-58,62-72`.
- Platforms and build limits: `aprofile:README.md:11-23,240-242`.

**Content rules to add to `guide-rules.yaml`:**
```yaml
- glob: docs/README.md
  require: ["^## What you get$", "^## What the parts are$", "^## How they fit$", "^## Ways to run it$", "^## Status and limits$", "^## Where to go next$"]
  forbid: ["\\u2013|\\u2014"]
  require_order: ["^## What you get$", "^## What the parts are$", "^## How they fit$", "^## Ways to run it$", "^## Status and limits$", "^## Where to go next$"]
```

**Persona:** End user, with the start page readable before the reader knows any named product.

**Internal:** no.

**Procedure:** Follow `page-procedure.md`; the cold-reader test is part of this page's review gate.

### Task 2: Profile basics

**Page path:** `docs/overview/profile-basics.md`

**Outline:** Follows `overview-contract.md` as the Overview section page. Open with what the reader gains and why, then define a profile, harness, default setup, wrapper, and share level. Introduce credentials, runtime state, owned data, and shared content without listing implementation paths.

**Voice:** `conversational`.

**Facts to verify with source hints:**
- aprofile and profile definitions: `aprofile:README.md:3-5`.
- Definitions for every harness named: `claude-code:README.md:7`; `codex:README.md:1`; `opencode:README.md:10`; `pi:README.md:13-17`.
- Separation purpose and share-level summary: `aprofile:README.md:7-9,42-48`.
- Layer meanings and private state: `aprofile:README.md:62-72,184-195`.

**Content rules to add to `guide-rules.yaml`:**
```yaml
- glob: docs/overview/profile-basics.md
  require: ["^## Why this matters$", "^## What a profile is$", "^## What stays private$", "^## What a share level changes$"]
  forbid: ["\\u2013|\\u2014"]
  require_order: ["^## Why this matters$", "^## What a profile is$", "^## What stays private$", "^## What a share level changes$"]
```

**Persona:** End user.

**Internal:** no.

**Procedure:** Follow `page-procedure.md` and the Overview cold-reader test in `overview-contract.md`.

### Task 3: Get started

**Page path:** `docs/user/getting-started.md`

**Outline:** Follows `overview-contract.md` as the End user entry page. Start with the outcome, name and define every coding tool used in the example, then show build from an existing source checkout, create one profile with a secret reference, run `sync`, and launch the generated wrapper. Include a copy-paste check after each observable step.

**Voice:** `tutorial`.

**Facts to verify with source hints:**
- Product definitions for the example: `claude-code:README.md:7`; `aprofile:README.md:3-5`.
- Go version, build command, and source-build limit: `aprofile:README.md:11-23`; `aprofile:Makefile:1-10`.
- Profile creation, `sync`, PATH update, and launcher behavior: `aprofile:README.md:25-40`.
- Secret references and fail-closed behavior: `aprofile:README.md:209-216`.
- Clone URL remains an open question. Use an existing checkout until the author supplies and the controller fetches a valid URL.

**Content rules to add to `guide-rules.yaml`:**
```yaml
- glob: docs/user/getting-started.md
  require: ["^## Build aprofile$", "^## Create a profile$", "^## Sync and launch$"]
  forbid: ["\\u2013|\\u2014", "sk-[A-Za-z0-9]{8,}", "ghp_[A-Za-z0-9]{8,}"]
  require_order: ["^## Build aprofile$", "^## Create a profile$", "^## Sync and launch$"]
```

**Persona:** user, End user entry page.

**Internal:** no.

**Procedure:** Follow `page-procedure.md` and the Overview contract for the opening paragraph and product definitions.

### Task 4: Choose a share level

**Page path:** `docs/user/share-levels.md`

**Outline:** Explain how the reader chooses `all`, `content`, or `none`, what each level shares, what remains private, how to preview a level change, and the documented safety limits. Include harness-specific differences only where needed to make the choice clear.

**Voice:** `tutorial`.

**Facts to verify with source hints:**
- Share levels and defaults: `aprofile:README.md:42-60`.
- Treatment of credentials, runtime state, content, settings, plugin store, and hooks: `aprofile:README.md:62-109`.
- Harness-specific configuration directories: `aprofile:README.md:171-182`; `aprofile:internal/harness/builtin/claude.yaml`; `aprofile:internal/harness/builtin/codex.yaml`; `aprofile:internal/harness/builtin/opencode.yaml`; `aprofile:internal/harness/builtin/pi.yaml`.
- Product definitions for every harness named: `claude-code:README.md:7`; `codex:README.md:1`; `opencode:README.md:10`; `pi:README.md:13-17`.

**Content rules to add to `guide-rules.yaml`:**
```yaml
- glob: docs/user/share-levels.md
  require: ["`all`", "`content`", "`none`", "credentials", "runtime state"]
  forbid: ["\\u2013|\\u2014"]
  require_order: ["^## Choose a level$", "^## What each level shares$", "^## Limits to know$"]
```

**Persona:** user, End user.

**Internal:** no.

**Procedure:** Follow `page-procedure.md`.

### Task 5: Manage profiles

**Page path:** `docs/user/manage-profiles.md`

**Outline:** Cover common list, show, login, edit, rename, remove, copy, MCP copy, and hook-set operations. Explain which actions change wrappers and when the reader runs `sync`. Warn before profile removal with `--purge` or changes that replace settings.

**Voice:** `tutorial`.

**Facts to verify with source hints:**
- Core commands and stale-wrapper behavior: `aprofile:README.md:143-169`.
- Hook-set commands and how `sync` manages their entries: `aprofile:README.md:111-140,193-195`.
- MCP copy and profile copy behavior: `aprofile:README.md:143-169`.

**Content rules to add to `guide-rules.yaml`:**
```yaml
- glob: docs/user/manage-profiles.md
  require: ["aprofile list", "aprofile show", "aprofile edit", "aprofile copy", "aprofile sync", "hook-sets", "mcp copy"]
  forbid: ["\\u2013|\\u2014"]
  require_order: ["^## Inspect profiles$", "^## Change profiles$", "^## Copy profile data$", "^## Manage hooks and MCP servers$"]
```

**Persona:** user, End user.

**Internal:** no.

**Procedure:** Follow `page-procedure.md`.

### Task 6: Troubleshoot profiles

**Page path:** `docs/user/troubleshooting.md`

**Outline:** Show how to inspect a profile and run `doctor`, then map documented findings to supported next steps. Cover PATH and stale-wrapper reports, a missing harness binary, unclassified entries, unresolved references, and linked files a harness replaced.

**Voice:** `tutorial`.

**Facts to verify with source hints:**
- What `show` reports and how stale wrappers are surfaced: `aprofile:README.md:143-169`.
- `doctor` checks, findings, and supported fixes: `aprofile:README.md:236-238`.
- Repairing a replaced link: `aprofile:README.md:91-109`.

**Content rules to add to `guide-rules.yaml`:**
```yaml
- glob: docs/user/troubleshooting.md
  require: ["aprofile show", "aprofile doctor", "PATH", "unclassified"]
  forbid: ["\\u2013|\\u2014"]
  require_order: ["^## Inspect the profile$", "^## Run doctor$", "^## Follow a reported fix$"]
```

**Persona:** user, End user.

**Internal:** no.

**Procedure:** Follow `page-procedure.md`.

### Task 7: Command reference

**Page path:** `docs/reference/commands.md`

**Outline:** Build a quick lookup table grouped by profile, sharing and harness, hooks and MCP, and diagnostics. Include exact command forms and safe-use notes; link to the relevant how-to page for full procedures.

**Voice:** `reference`.

**Facts to verify with source hints:**
- User-facing command list: `aprofile:README.md:143-169`.
- Exact command forms and summaries: `aprofile:internal/cli/root.go`, `aprofile:internal/cli/add.go`, `aprofile:internal/cli/list_show.go`, `aprofile:internal/cli/edit.go`, `aprofile:internal/cli/sync.go`, `aprofile:internal/cli/login.go`, `aprofile:internal/cli/copy.go`, `aprofile:internal/cli/mcp.go`, `aprofile:internal/cli/hooksets.go`, `aprofile:internal/cli/doctor.go`.

**Content rules to add to `guide-rules.yaml`:**
```yaml
- glob: docs/reference/commands.md
  require: ["aprofile add", "aprofile list", "aprofile show", "aprofile edit", "aprofile sync", "aprofile doctor", "--help"]
  forbid: ["\\u2013|\\u2014"]
  require_order: ["^## Profile commands$", "^## Sharing, hooks, and MCP$", "^## Diagnostics and help$"]
```

**Persona:** user, End user.

**Internal:** no.

**Procedure:** Follow `page-procedure.md`.
