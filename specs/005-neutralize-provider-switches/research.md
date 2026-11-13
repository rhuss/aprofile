# Research: Neutralize Provider Switches for Every Profile Folder

Verified on 2026-11-10 against Claude Code 2.1.319 and Codex 0.167.0 with a local logging stub
server, fresh config folders and an empty environment (`env -i` with a temporary `HOME`), never
with real keys or logins. Fake tokens only.

## R1. Empty entries leave a Claude login profile on its login (FR-004a)

- Setup: two fresh config folders, no login in either. Exported in both runs:
  `ANTHROPIC_API_KEY=sk-ant-test-exported`, `CLAUDE_CODE_USE_VERTEX=1`,
  `CLAUDE_CODE_SKIP_VERTEX_AUTH=1`, `ANTHROPIC_VERTEX_PROJECT_ID`, `CLOUD_ML_REGION`,
  `ANTHROPIC_VERTEX_BASE_URL` and `ANTHROPIC_BASE_URL` pointing at the stub. Folder A's
  `settings.json` holds empty `env` entries for `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`,
  `CLAUDE_CODE_OAUTH_TOKEN` and the Vertex names; folder B's holds none. `claude -p hi`.
- Folder B (today's login profile): `POST /vertex/projects/proj/locations/us-east5/.../
  claude-opus-5-5:streamRawPredict` with `x-api-key: sk-ant-test-exported`. The exported switch
  and key took over.
- Folder A: no request at all; Claude printed "Not logged in · Please run /login". Empty entries
  are treated as unset: no empty key is sent, Vertex is off, and Claude falls back to the login
  path, which with a real login uses the login against the default provider.
- Decision: neutralize the whole auth-clearing list for every own settings file; the empty-entry
  semantics hold for login profiles. The one remaining manual check, a real Claude Max login
  launched from a folder with the entries, is a RUNBOOK step (SC-001), not a blocker.
- Alternatives: neutralizing only the provider switches (not the auth variables) would leave an
  exported `ANTHROPIC_API_KEY` beating the login, which Folder B shows Claude would use.

## R2. Codex ignores exported OpenAI variables for login profiles (FR-008)

- Setup: two fresh `CODEX_HOME` folders with an empty `config.toml`. Exported in both runs:
  `OPENAI_API_KEY=sk-test-exported`, `OPENAI_BASE_URL` pointing at the stub.
  `codex exec --skip-git-repo-check hi`.
- No login: Codex sent `POST https://api.openai.com/v1/responses` without any auth header ("Missing
  bearer or basic authentication"). It ignored both the exported key and the exported base URL;
  the stub got no request.
- Fake ChatGPT login (`auth.json` with unsigned fake tokens): Codex went to
  `https://chatgpt.com/backend-api/codex/...` and failed on the fake token. Both exported variables
  were ignored; the stub got no request.
- Decision: no Codex change (FR-009's "unchanged" branch). Record the result in a comment of the
  built-in Codex definition. `OPENAI_BASE_URL` stays in `launch_clear` for wrapper launches, which
  costs nothing.
- Note: these runs reached the real OpenAI and ChatGPT endpoints with fake tokens only.

## R3. Where the neutralizing decision lives (FR-001, FR-002, FR-004)

- Decision: `desiredState` in `internal/launch/plan.go` places the neutralizing entries whenever
  the folder can carry environment variables (the existing `envCarrier` rule returns no reason:
  the definition declares `folder_launch.env` and the env file is not linked), instead of only
  when a key setting is folder-carried.
- The supplied-name exclusion grows by one source: the variable of every key setting that stays
  wrapper-carried (`oauth-token`, an `api-key` key with reason `auth-kind`, `cleared-source` and
  so on). A folder-carried key's own variable keeps being blanked, since the key command serves it
  (spec 004 R2).
- Rationale: one rule, reusing the same linked-file test that already decides the share-level-`all`
  reason for variables; at `all` the desired set stays empty, so the planner's unplaced report
  (`unplaced`, spec 004 FR-020b) keeps flagging hand-added auth-clearing entries in the linked
  default file exactly as today.
- Alternatives: gating on auth kind (login only) was rejected in the brainstorm; gating on
  `p.ShareLevel()` would duplicate the linked-file logic that `envCarrier` already owns.

## R4. Reporting the share-level-`all` gap (FR-005, FR-006, FR-005a)

- Decision: the gap is a reporting fact, not a launch setting. `launch.Status` computes it and
  `StatusReport.Folder()` appends a wrapper-only item `{setting: "auth-clearing", variable: "",
  reason: "share-all"}` when: the definition declares `folder_launch.env`, the env file is linked
  (`envCarrier` returns `share-all`), the profile's auth kind sets no variables, and
  `UnsetAuthVars()` is not empty.
- `DecideWith` stays unchanged, because the wrapper renders from it (`internal/wrapper/env.go`)
  and the planner places from it; a pseudo setting there would leak into both.
- `doctor` reuses its wrapper-only finding with a special fix for this item: "start the profile
  through its wrapper <name>, or give it its own settings: aprofile edit <id> --share content".
- Alternatives: a separate `warnings` array in `show --json` would be a new field and a second
  place to look; the wrapper-only list already says "the folder alone does not do this".
