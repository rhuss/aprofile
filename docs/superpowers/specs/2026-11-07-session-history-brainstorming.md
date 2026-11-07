# aprofile: session history and conversation portability

Status: brainstorming handoff, not an approved design or implementation plan.
Prepared: 2026-11-07.
Repository inspected at `49c0610d7df5109a792062b9a5994ad79e9da9d7` on `docs/profile-guide`.

## Start the next session here

Suggested opening prompt:

> Continue brainstorming aprofile's session-history and conversation-portability features from this brief. First confirm the intended user experience and unresolved decisions. Recheck the relevant code and external projects before choosing an approach. Produce a design for review before implementing product changes.

The author considers both individual-conversation switching and broader access to a harness's history valuable. Do not make them choose one as if it excludes the other. They have not yet selected conversion software, storage policy, commands, or a first release scope.

## What the author wants

- Explain clearly how each profile mode treats history and sessions, especially what happens when users try to resume a conversation under another profile.
- Continue a particular conversation using another profile, for example when changing an account or model.
- Ideally continue a conversation in another harness, including Claude Code to Codex and Codex to Claude Code.
- Explore full-history mirroring and merged history for profiles of a given harness.
- Use the next session to discuss the design. This session performed quick source research; it did not implement or install session-transfer software.

A model change and a profile change are separate operations. Establish whether a native model switch already solves a particular use case before introducing profile transfer for it.

## Current aprofile behavior

All three share levels, `all`, `content`, and `none`, keep credentials and runtime state private. Runtime state includes history, sessions, memory, logs, caches, databases, and locks. Even `all` does not expose another profile's conversation history.

Creating a profile starts it with its own state. `aprofile copy` carries settings and owned data as supported, but does not copy the source profile's history or sessions. An existing destination's state is retained rather than replaced during overwrite.

The wrapper selects the profile's configuration environment and forwards arguments to the harness. Passing a native resume command through the wrapper does not itself transfer another profile's session into the destination store.

Relevant implementation and documentation:

- `internal/harness/builtin/claude.yaml`: redirects Claude's configuration and classifies projects, history, session files, file history, and other state as private.
- `internal/harness/builtin/codex.yaml`: redirects `CODEX_HOME`; classifies sessions, history, indexes, SQLite files and companions, and supporting runtime directories as private.
- `internal/harness/builtin/opencode.yaml`: redirects XDG directories; session databases and supporting data are private.
- `internal/harness/builtin/pi.yaml`: redirects the agent directory and explicitly pins the session directory under the profile.
- `internal/layer/treatment.go`: reports runtime state as always private.
- `internal/cli/copy.go`, especially the target preparation and same-harness copy paths: creates private state without transferring source state.
- `specs/002-profile-isolation-levels/spec.md`: private runtime state is a deliberate requirement, not an incidental implementation detail. Any new sharing policy needs an explicit opt-in exception and updated specification.
- Guide: `docs/guide/docs/overview/profile-basics.md` and `docs/guide/docs/user/share-levels.md`.

Source locations should be rechecked against the checkout used in the next session.

## Separate the capabilities before choosing storage

| Capability | Intended outcome | Decision still needed |
|---|---|---|
| Same-harness session handoff | Continue a conversation using a different profile's configuration and credentials | Continue one logical thread or create an independent continuation? |
| Cross-harness session conversion | Create a resumable destination conversation from another harness's transcript | Which information must transfer, and what losses are acceptable? |
| Full-history mirror | Make a collection of past conversations available in another profile | Snapshot or ongoing refresh; one-way or bidirectional; how to handle changes and deletion? |
| Merged history catalog | Browse conversations from several profiles of one harness together | Does selecting a session resume its original profile, or offer transfer to another profile? |

A merged catalog can reference isolated stores without sharing the files those stores write. It does not necessarily merge the contents of conversations. A history mirror and a continuously shared session store are also different features.

Define what “history” includes: prompt-input history, user/assistant transcripts, tool calls and results, session discovery indexes, compacted context, memory, and file checkpoints are separate data. Do not equate a copied JSONL file with a complete, usable session or promise recovery of context absent from the source transcript.

## Quick research: existing projects

The following findings came from project READMEs and selected implementation files. Live resume behavior and compatibility with aprofile profiles have not been independently tested. Version numbers and round-trip demonstrations below are authors' reports, not guarantees for current installations.

### Official OpenAI Codex plugin for Claude Code

- Repository: https://github.com/openai/codex-plugin-cc
- Inspected revision: `db52e28f4d9ded852ab3942cea316258ae4ef346`.
- Its `/codex:transfer` command creates a persistent Codex thread from a Claude Code transcript and prints a `codex resume` command.
- Uses Codex's external-agent session importer, producing visible turns that can be continued in the App or TUI.
- The documented transfer direction is Claude Code to Codex. This research did not establish a reverse operation in the plugin.
- Significant aprofile limitation: `plugins/codex/scripts/lib/claude-session-transfer.mjs` constructs the allowed source root from the actual home directory plus `.claude/projects`, and rejects sources outside it. That conflicts with aprofile's isolated Claude configuration directories unless support changes or an appropriate integration is developed.
- Older Codex versions without external session import are unsupported by the transfer command.

Primary sources:

- [Transfer documentation](https://github.com/openai/codex-plugin-cc/blob/db52e28f4d9ded852ab3942cea316258ae4ef346/README.md#codextransfer)
- [Source-path validation](https://github.com/openai/codex-plugin-cc/blob/db52e28f4d9ded852ab3942cea316258ae4ef346/plugins/codex/scripts/lib/claude-session-transfer.mjs)

### ai-session-bridge

- Repository: https://github.com/bakhtiersizhaev/ai-session-bridge
- Inspected revision: `e7f1868bb9ae1bc213307a86e16465887b78acd9`.
- Implements both `claude2codex` and `codex2claude`.
- Converts conversation messages and tool calls, maps tool names, and registers converted sessions so the destination can discover and continue them.
- Documents optional trimming to the last N user turns and explicit format losses, including Codex turn context and Claude file-history snapshots.
- Author reports live round-trip recall and subsequent session writes with Codex CLI 0.126 and Claude Code 2.1.
- Particularly relevant to aprofile: `src/discover.ts` respects both `CODEX_HOME` and `CLAUDE_CONFIG_DIR`. This is evidence of configurable stores, not proof that all commands work with arbitrary aprofile source/destination combinations.
- Codex registration can include `session_index.jsonl` and `state_5.sqlite`, depending on the environment. Producing a transcript alone is not the whole operation.

Primary sources:

- [README and limitations](https://github.com/bakhtiersizhaev/ai-session-bridge/blob/e7f1868bb9ae1bc213307a86e16465887b78acd9/README.md)
- [Directory resolution](https://github.com/bakhtiersizhaev/ai-session-bridge/blob/e7f1868bb9ae1bc213307a86e16465887b78acd9/src/discover.ts)

### agent-session-bridge

- Repository: https://github.com/connectwithprakash/agent-session-bridge
- Inspected revision: `0e23f6c94306d11867a5b8913c62c6fceebbaa02`.
- Supports conversion among Claude Code, Codex, and Hermes through an intermediate representation with messages, tools, metadata, and pending state.
- Reports conversion asymmetries and supplies a continuation preamble. It can explicitly represent interrupted tool calls.
- Documents losses in branching, provider-bound reasoning signatures, instructions, permissions, and other harness-specific information.
- Provides Codex session-store registration with backup and schema checks. `src/session_bridge/cli.py` exposes an explicit `--codex-home` option.
- The author's compatibility table names Claude Code 2.1.212 and Codex CLI 0.146.0 with `state_5.sqlite`; newer formats require verification.
- Claude placement and custom profile discovery also need investigation; the Codex home option alone does not establish complete aprofile compatibility.

Primary sources:

- [README, compatibility, and conversion losses](https://github.com/connectwithprakash/agent-session-bridge/blob/0e23f6c94306d11867a5b8913c62c6fceebbaa02/README.md)
- [CLI and destination-home option](https://github.com/connectwithprakash/agent-session-bridge/blob/0e23f6c94306d11867a5b8913c62c6fceebbaa02/src/session_bridge/cli.py)

### Claude / Codex Bridge

- Repository: https://github.com/nerln/claude-codex-bridge
- README inspected on 2026-11-07; a repository commit was not recorded in this quick research.
- Creates native destination sessions in both directions, tracks transfer lineage, and combines visible conversation history with project context.
- Explicitly describes native conversion as experimental and version-gated.
- Author reports an August 2026 macOS round trip using Claude Code 2.1.219/2.1.220 and Codex 0.146.0-alpha.9.2.
- Default behavior compacts tool output and bounds transcript size. Its automatic mode can fall back to project context without the transcript; required-transcript mode refuses that fallback.
- Useful design reference for independent conversation lanes, retry identity, and preserving source sessions. Custom profile-home support was not verified.

Primary source: [README](https://github.com/nerln/claude-codex-bridge).

These projects establish practical examples of cross-harness continuation. They do not establish a lossless universal session format, continuous history synchronization, or compatibility with every current harness version. OpenCode and Pi conversion support was not researched here.

## Initial recommendations to discuss, not accepted decisions

- Make history policy independent of `all`/`content`/`none`. Preserve existing isolation behavior unless users explicitly opt into a history operation.
- Consider an existing converter behind an aprofile integration rather than maintaining every vendor's session format ourselves.
- Prefer vendor-supported import mechanisms where available; compare their restrictions with third-party conversion approaches.
- Consider a first experiment that creates a new destination session from one selected source conversation, preserves the original, and records lineage and conversion losses.
- Treat a merged history catalog as a separate capability that can initially read isolated stores.
- Avoid starting with shared writable session directories or automatic merging of databases, locks, caches, and credentials. Those would change isolation guarantees substantially.

No external dependency or CLI command syntax has been selected.

## Questions for the next brainstorming conversation

Start with the expected experience when switching one conversation:

1. After switching, should the original and destination remain independent branches, or should switching back automatically pick up all intervening work as one logical conversation?
2. Is a complete visible transcript required, or is an explicitly labeled partial transcript or summary acceptable when native conversion cannot preserve it?
3. What does “all history available” mean: a searchable catalog, importable snapshots, automatic copying, or simultaneous access to the same writable sessions?
4. Which transfers belong in the first release: same-harness profile switching, Claude/Codex conversion, or both? Which behavior must also work for OpenCode and Pi?
5. Should users select the destination profile each time, or define explicit groups allowed to exchange history? What must remain separate for work, personal, and client profiles?
6. Should aprofile delegate to a separately installed converter, bundle a dependency, or provide its own adapters? Evaluate licenses and maintenance before deciding to embed code.
7. How should switching work from inside a running agent versus from the terminal? What should happen if a source turn or tool call is still active?

Further design must settle project/worktree matching, destination credentials and model selection, session identity and lineage, index registration, interrupted calls, version compatibility, collision handling, retries, rollback, and mirror deletion/conflict rules where applicable.

## Suggested experiment after agreeing its scope

Use disposable profile stores and a small dedicated test project. Do not run conversion against real work or personal histories as the first experiment.

- Establish a same-harness transfer baseline between two profiles with different authentication/model settings.
- Try Claude profile A to Codex profile B to Claude profile C using supported installed versions and a candidate backend.
- Put distinct facts in user and assistant turns, include completed tool calls, and verify the destination can recall the transferred content and append new turns.
- Confirm the destination appears in the harness's native session picker and still resumes after restarting the harness.
- Verify the chosen profile's credentials, settings, and model remain the destination's; confirm source session bytes and unrelated destination history remain intact.
- Record what is lost: branches, checkpoints, reasoning metadata, compacted history, instructions, and tool semantics.
- Exercise unsupported schemas and incomplete source turns. A refusal or fallback should be explicit rather than presented as a complete transfer.

The experiment's result should determine the supported compatibility matrix and first release scope before a product implementation plan is written.

## Documentation follow-up

Regardless of feature scope, strengthen the user guide's explanation of history and sessions. Add an explicit per-mode table and explain the current limits of profile copy and cross-profile resume. Describe future transfer capabilities only after they exist and have verified recipes.

The repository root README is generated from `docs/guide/docs/README.md`; refresh it with `make -C docs/guide readme` after editing that introduction. The guide's pinned source baseline should be updated deliberately when product behavior changes.

## Working-tree context

The guide and repository-entry changes are committed locally. No Git remote was configured when this brief was written, so the earlier requested push remains unresolved until a remote URL is supplied. `.specify/spex.json` had a pre-existing user modification; preserve it and do not include it in unrelated commits.
