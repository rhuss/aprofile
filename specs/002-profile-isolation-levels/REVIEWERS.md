# Review Guide: Profile Share Levels and Hook Sets

**Generated**: 2026-11-04 | **Spec**: [spec.md](spec.md)

## Why This Change

Slice one promises that each profile keeps its own history, memory and logins. For Claude Code and
Codex that promise does not hold: their definitions end in a catch-all rule that links every
entry nobody classified, and on a real machine that linked prompt history, session data, Codex's
memory database and live lock directories into the default setup. Two profiles running at the
same time mix accounts. The shared settings file also means a permission granted at work shows up
in the personal profile, and there is no way to give one profile different hooks than another.

## What Changes

Each profile gets a share level, chosen with `--share`: `all` uses the default setup live,
`content` borrows instructions and skills but keeps its own settings and hooks, `none` borrows
nothing. Logins, history, sessions and memory are private at every level. The catch-all rules are
gone; unknown entries are reported instead of linked. Hook sets add a central hook library that
profiles opt into by name. `show`, `doctor`, `harness show` and a new `help sharing` topic explain
what a profile shares and why, and every level change can be previewed. Breaking behavior: new
profiles default to `content`, and user files in `~/.claude` that no definition knows (for example
`RTK.md`) are no longer linked into new profiles; existing profiles keep working as `all` and get
reports instead of silent changes.

## How It Works

- Harness definitions replace the `shared` layer with four categories (content, settings, plugin
  store, hook location) plus a per-level policy table, all data (Constitution I).
- One planner in `internal/layer/plan.go` turns definition, profile and level into steps (link,
  unlink, copy once, replace with link, and so on). `sync`, `edit --share`, `settings pull`, dry
  runs and `show` all use it, so a preview cannot disagree with what happens. Steps that replace
  profile data are marked; `sync` skips them and names the command that completes them.
- A new `internal/hooksets` package loads `hooks.d/*.yaml`, places hook groups (Claude and Codex
  share the `event-groups` JSON shape) or links files (OpenCode plugins, pi extensions), and
  records a fingerprint of everything it placed, so user and third-party hooks are never touched.
- Wrappers export `APROFILE_SHARE`; registry entries gain `share` at `format_version` 1.
- macOS gets a real running-profile check (`kern.procargs2`); slice one's check always returned
  false there.

## When It Applies

**Applies when**:
- creating, copying, importing or editing any profile of the four built-in harnesses;
- running `sync` on existing slice one profiles (leaks are reported, not silently changed);
- a user wants different hooks per profile, or wants to see exactly what a profile borrows.

**Does not apply when**:
- a harness reads files relative to `$HOME` (for example `~/.agents/skills`): no level can isolate
  them; they are reported, and repointing `$HOME` is out of scope;
- hooks need to move between harnesses: bodies are written per harness, no translation;
- instructions should be shared between Claude Code and Codex: each harness links its own
  default setup.

## Key Decisions

1. **Three levels named by what they share (`all`, `content`, `none`).** Considered
   `shared`/`curated`/`isolated` and `linked`/`seeded`/`isolated`; the `--share` wording answers
   the user's actual question. Because "all" could imply logins are shared, every description says
   they are not.
2. **`content` copies settings once, then they belong to the profile.** Considered re-syncing
   selected keys on every `sync` and launch-time injection; both need merge rules users must learn
   and would overwrite permissions the harness writes at runtime. `settings pull` refreshes on
   request, merging by default.
3. **Skills are content.** Slice one made them per profile and empty, which left `all` and
   `content` profiles without the user's skills.
4. **The plugin store is linked at `all` and `content`.** Private stores would mean reinstalling
   plugins per profile; this is the one shared entry the harness writes at `content`, documented in
   SC-002.
5. **Managed hooks are identified by fingerprint, recorded outside the harness dir.** Hook formats
   have no id field and harnesses may reject unknown keys; matching by command string breaks when
   two sets share a script.
6. **No catch-all in built-ins, with `harness classify` as the escape hatch.** Users' own files
   are reported with a one-line fix instead of being shared silently.

## Areas Needing Attention

- **Upgrade behavior for user files.** Files like `RTK.md` that slice one linked through the
  catch-all stay linked in existing profiles but are reported; new profiles do not get them until
  the user runs `harness classify`. Whether Claude Code resolves `@` imports against the link or
  its target is still unverified (research R7, task T058).
- **`ide/` as state.** An IDE started outside a profile wrapper connects to the default setup only.
  This is the correct isolation but may surprise users; verification is task T058.
- **The `shared` alias.** User `harnesses.d` files using `layer: shared` load as `content` with a
  doctor warning. A user catch-all override would therefore still link everything; doctor reports
  it, but the behavior is allowed.
- **Concurrency at `all`.** Two `all` profiles still share one live settings file; documented, not
  prevented.
- **Size.** 62 tasks across six stories; US1 plus US2 form the MVP and can ship first.

## Open Questions

- The two manual verifications above (R7 `@` imports, R8 `/ide`) may change the README guidance but
  not the design.
- Removing `aprofile import cc-deck` is tracked in `brainstorm/idea-inbox.md`, outside this spec.

## Review Checklist

- [ ] Key decisions are justified
- [ ] Breaking changes are documented with migration guidance
- [ ] Scope matches the stated boundaries
- [ ] Success criteria are achievable
- [ ] No unstated assumptions
- [ ] Constitution 1.1.0 amendment (Principles I to IV) matches what the spec needs
- [ ] The R1 classification tables in research.md match what the harnesses write today

---

<!-- Code phase sections are appended below this line by the phase-manager command -->
