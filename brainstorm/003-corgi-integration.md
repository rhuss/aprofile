# Brainstorm: Corgi Integration

**Date:** 2026-11-09
**Status:** active

Companion brainstorm on the Corgi side: `corgi-specs/brainstorm/11-aprofile-registry.md`
(Corgi reads aprofile's registry). This document covers the aprofile side.

## Problem Framing

The author wants Corgi's agent picker to show exactly the aprofile profiles in daily use, each one
launching correctly, with its model visible and, for the subscription profiles, its budget (usage
and reset countdown). The color of a profile should mark its agents in Corgi.

Profiles in daily use:

| Scope | Profiles | Auth |
|---|---|---|
| private | `claude/private`, `codex/private` | subscription logins (Claude Max, ChatGPT) |
| private | `claude/deepseek`, `claude/glm`, `codex/deepseek`, `codex/glm` | API key, free models over EnMaaS |
| work | `claude/work` (Opus over EnMaaS), `claude/work-deepseek`, `claude/work-glm` | API key |
| work | `codex/work-deepseek`, `codex/work-glm` (occasional) | API key |

`claude/vertex-old` and `codex/work` are not in daily use. Claude is the main harness.

The strategy has two sides, each useful alone:

1. **aprofile side (this document).** Every profile folder carries its own launch settings, so
   stock Corgi, which starts an account by setting only `CLAUDE_CONFIG_DIR` or `CODEX_HOME`,
   launches it correctly. The registry gains the profile's model.
2. **Corgi side (companion document).** A patch makes Corgi read `registry.json`, launch through
   the wrapper and show model, label and color. It is offered upstream to Tal Shiri; if it is not
   accepted, the author carries it in a custom Corgi build.

Success: Corgi's picker lists the daily profiles and nothing else; each launches against the right
endpoint with the right key; every launch starts from the profile's model; the subscription
profiles show their budget.

Anti-goals:

- No Omarchy file formats or integrations (removed on 2026-11-09, FR-031 withdrawn).
- No leftover rows in Corgi's picker the author did not choose (old cc-deck folders, duplicates).
- No literal secrets in any file aprofile writes (Principle IV).
- aprofile does not drive or write into Corgi (Principle V stays as it is).

## Findings

**How Corgi models accounts.** An account is a settings folder. Corgi launches it by setting only
the config-folder variable (`corgi/docs/agent-launcher.md`, "Several accounts"). It finds folders
it created, folders added with `corgi account add <agent> NAME --dir FOLDER`, Omarchy's accounts
(read from `~/.local/state/omarchy/agents/accounts/<provider>.json`, `src/accounts.ts:177`), and
folders an agent was seen running in. The model shown for Claude is the `model` key of the
folder's `settings.json`, then project settings, then the daemon's own `ANTHROPIC_MODEL`. Saved
custom commands launch through a shell but are capped at six, so they do not fit ten profiles.

**The gap.** API-key profiles keep the key reference, endpoint, model and aliases in the wrapper
script only. Registered as a Corgi account, such a folder launches without a key against the wrong
endpoint. Subscription profiles have no gap: their login lives in the folder.

**Model precedence, Claude Code 2.1.319.** `ANTHROPIC_MODEL` beats the `model` key of
`settings.json`. `/model` writes that key, so in a profile whose wrapper pins a model (for example
`claude-deepseek`) a `/model` change lasts one session. `claude-private` and `claude-work` pin no
model, so `/model` sticks; in `claude-private` it lands in `~/.claude/settings.json`, because that
profile links its settings at share level `all`. The `model` keys found on disk differ per profile
(`claude/glm` holds its real model, `claude/deepseek` holds `opus`): Claude Code wrote them, not
aprofile.

**Codex, 0.167.0.**
- A provider block can fetch its key with a command:
  `[model_providers.<name>.auth]` with `command`, `args`, `timeout_ms`, `refresh_interval_ms`,
  `cwd`. Verified against a local dummy server with `OPENAI_API_KEY` unset: Codex sent the file
  content as `Bearer` on both `GET /v1/models` and `POST /v1/responses`. `config.toml` holds only
  the command; the key stays in its file.
- `/model` saves into `config.toml` by default. Verified in the interactive interface (tmux, dummy
  catalog): the picker offers "enter default · s session"; Enter rewrote `model` and added
  `model_reasoning_effort`. `s` keeps the choice for the session only.
- `-m` and `-c model=…` beat `config.toml`. The Codex wrappers set no model today.
- A top-level `-m` from the wrapper plus a top-level `-m` from the caller fails: "the argument
  '--model <MODEL>' cannot be used multiple times". `-c model=<profile model>` does not conflict: a
  caller's `-m` (top level or after `exec`) or a later `-c model=…` wins over it.

**Claude `settings.json` `env` block, verified on 2.1.319** (dummy server, fresh
`CLAUDE_CONFIG_DIR`, empty environment):
- Applied before gateway model discovery: `GET /v1/models` went to the `env` block's
  `ANTHROPIC_BASE_URL`, carrying the `apiKeyHelper` key as both `x-api-key` and `Bearer`.
- Model aliases from the block apply: `--model haiku` sent `ANTHROPIC_DEFAULT_HAIKU_MODEL`'s value.
- The `env` block beats the caller's environment: with `ANTHROPIC_MODEL` and `ANTHROPIC_BASE_URL`
  also set by the caller, requests still used the block's model and endpoint.
- The block's `ANTHROPIC_MODEL` beats the `model` key that `/model` writes: with `"model":
  "picked-model"` in the same file, requests used the block's model. So a Claude folder alone
  already starts every launch from the profile's model; only stock Corgi's display (which reads
  the `model` key) can show a stale pick.

**Corgi color.** Tal's draft PR [#74](https://github.com/tals/corgi/pull/74) ("tricks: add
programmable UI customizations", implements [#44](https://github.com/tals/corgi/issues/44))
exposes `agent.account` with a `directory` field and draws an account-colored marker on the
sidebar rail. A Trick can match `directory` to the registry's `config_dir`.

## Approaches Considered

### A: Self-contained folders for every profile with launch settings (chosen)
- `aprofile sync` writes the wrapper's launch settings into the folder as aprofile-owned keys:
  Claude gets entries in the `settings.json` `env` block plus `apiKeyHelper`; Codex gets a
  `[model_providers.<name>.auth]` command block. Everything else in those files stays untouched.
- The Codex wrapper passes the profile's model at launch; `registry.json` gains `model`.
- Profiles are registered with Corgi by hand from documented commands.
- Pros: stock Corgi works for every profile; the folders also work with any other tool that only
  sets the config-folder variable; no Corgi-specific code in aprofile.
- Cons: needs a constitution amendment (Principle II); after a `/model` change, a launch from the
  folder alone follows the last pick.

### B: Same as A, opt-in per profile
- Pros: folders change only where asked.
- Cons: one more flag; the amendment is needed anyway; it would be on for all ten daily profiles.

### Dropped
- **Corgi patch only, no folder changes.** The author wants stock Corgi to work too.
- **`aprofile corgi sync` driving `corgi account add`.** Writes state into Corgi; Principle V forbids
  it.
- **Literal key in the `env` block.** Violates Principle IV.
- **A Corgi-defined account file that aprofile writes.** The Omarchy pattern, an anti-goal.
- **aprofile rewriting the `model` key on every `sync`** to avoid drift. Fights the harness.

## Decision

Approach A. Principle II is amended so aprofile may own the launch keys (model, endpoint, model
aliases, key helper) inside a harness's settings file. The drift for launches from the folder alone
is accepted. After verification it is smaller than expected: Claude folders start every launch
from the profile's model even without the wrapper (only stock Corgi's displayed model can be
stale); Codex folders launched without the wrapper follow the last `/model` pick saved as default.

## Key Requirements

- **Every launch starts from the profile's model** when started through the wrapper. Stated by the
  author.
- **Claude folders:** the wrapper's non-auth variables (`ANTHROPIC_MODEL`, `ANTHROPIC_BASE_URL`,
  `ANTHROPIC_DEFAULT_*_MODEL`, `CLAUDE_CODE_SUBAGENT_MODEL` and the profile's other extra
  variables) become entries in the `settings.json` `env` block, and the key reference becomes an
  `apiKeyHelper` command that reads the key file. aprofile owns only those entries and
  `apiKeyHelper`; the author's own `env` entries (MLflow and others) stay.
- **Codex folders:** a `[model_providers.<name>.auth]` block with a command that reads the key
  file. The Codex wrapper passes the profile's model with `-c model=…`, not `-m`, so a caller's
  `-m` still works.
- **Registry:** `model` field added, the profile's configured model, never the last `/model` pick.
  Additive, `format_version` stays 1, with a note (Principle V: public interface).
- **No literal secrets** in `settings.json` or `config.toml`; only commands that read the key file.
- **Profiles without a configured model** (`claude-private`, `claude-work`) keep following the
  harness. Claude's proposal, so the subscription profile is not forced onto a model.
- **The wrapper stays** the way to start a profile from a shell and keeps its never-overridable
  auth clearing.
- **Registration is manual:** documented `corgi account add <agent> <name> --dir <config_dir>`
  commands, plus the one-time cleanup in Corgi (remove the cc-deck folders and `private-max` with
  `corgi account remove`, delete the four saved commands, hide default rows that duplicate a named
  profile).
- **Constitution:** Principle II gains the owned launch keys in the settings file; Principle I
  (harness knowledge is data) gets the key locations into the harness definitions. Claude's
  proposal: settings files become mixed files with declared owned keys, like `.claude.json`.

## Open Questions

- Caller overrides for Claude: the `env` block beats the caller's environment, so
  `ANTHROPIC_MODEL=x claude-work-deepseek` and `ANTHROPIC_BASE_URL=…` overrides stop working once
  the variables live in the block (Principle V promises them). `--model` still overrides the model
  for one run; the endpoint has no flag. Accept and amend Principle V (overrides through flags),
  keep some variables out of the block, or another way?
- Codex: does the `auth` block conflict with `env_key` in the same provider block? The wrapper
  still sets `OPENAI_API_KEY`; decide which one the profile keeps.
- A profile at share level `all` links `settings.json` to the default setup. If such a profile ever
  has launch settings, aprofile must not write them into the default setup's file. Refuse, warn, or
  require `content`?
- What happens to the owned keys when a profile's launch settings are removed or its auth kind
  changes (clean removal of `env` entries aprofile placed)?
- Default accounts in Corgi: the default Claude account is the daemon's `CLAUDE_CONFIG_DIR`
  (currently `claude/private`), the default Codex account is `~/.codex`. Hide or keep?
