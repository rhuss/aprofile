# SC-004 walkthrough: can people pick the right share level from the in-tool help?

**Success criterion (spec SC-004)**: in a walkthrough with at least three people who have not read
the README, each picks the recommended share level for at least six of the seven use cases, using
only the in-tool help. This is a manual check; it is not part of the automated test suite.

## Setup (maintainer)

1. Build the binary: `make build`.
2. Pick participants who use a coding harness but have not read the aprofile README or the spec.
3. Give each participant a terminal with `./bin/aprofile` on `PATH` and nothing else open.
4. Do not explain share levels. Answer only questions about the task itself ("what do I type?"),
   never about which level to pick.

## Instructions (read to each participant)

> aprofile lets you run several profiles of a coding tool side by side. Each profile has a share
> level. Use only `aprofile help sharing` and `aprofile add --help` to decide which share level you
> would choose for each situation below. Say your choice out loud and, briefly, why.

## The seven situations

Read them in this order. Do not show the use-case IDs or the recommended level.

1. You have a work account and a personal account of Claude Code and often run both at the same
   time. You want the same instructions in both, but a permission you grant at work should not show
   up in your personal account.
2. You do consulting for a client. Their account must never see your personal memory, history,
   instructions or plugins.
3. You want to try out a new plugin, hook or model endpoint without risking your normal setup, and
   throw the experiment away afterwards.
4. You use Claude Code and Codex together and want the same set of hooks in both.
5. You run a local model gateway and want a profile that behaves exactly like your normal setup,
   except for credentials and the model.
6. You are recording a demo and nothing personal may appear on screen.
7. You set up a profile for CI that should run only hooks you chose explicitly.

## Expected answers (maintainer only)

| Situation | Use case | Recommended |
|-----------|----------|-------------|
| 1 | UC1 | `content` |
| 2 | UC2 | `none` |
| 3 | UC3 | `content` |
| 4 | UC4 | `content` (plus the same hook sets) |
| 5 | UC5 | `all` |
| 6 | UC6 | `none` |
| 7 | UC7 | `none` (with named hook sets) |

## Results

Fill in one row per participant. A participant passes with at least six correct answers.

| Participant | 1 | 2 | 3 | 4 | 5 | 6 | 7 | Correct | Pass | Notes (what confused them) |
|-------------|---|---|---|---|---|---|---|---------|------|----------------------------|
| P1 | | | | | | | | | | |
| P2 | | | | | | | | | | |
| P3 | | | | | | | | | | |

**Outcome**: not yet run (prepared 2026-11-04). SC-004 passes when every participant (at least
three) passes. Record wording problems the participants hit; they feed back into
`internal/cli/levels.go`, the single source of the level text.
