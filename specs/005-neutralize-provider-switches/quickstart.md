# Quickstart: Validate Neutralizing Entries

All steps run in a throwaway `HOME` (tests never touch the real home, Constitution Principle VII).

## 1. Automated checks

```bash
go test ./internal/launch/... ./internal/cli/... ./internal/doctor/... ./tests/integration/...
```

Expected: new tests cover login, `oauth-token` and wrapper-key profiles at `content`/`none` (empty
entries for every non-supplied auth-clearing name), share level `all` (no entries, `auth-clearing`
wrapper-only for login kinds only), idempotent second `sync`, and Codex unchanged.

## 2. File-level check (SC-001, automated part)

```bash
export HOME=$(mktemp -d)
aprofile add claude work-login --auth-kind oauth-login --share content
aprofile sync
jq '.env' "$(aprofile show claude/work-login --json | jq -r .config_dir)/settings.json"
```

Expected: an empty string for `CLAUDE_CODE_USE_VERTEX`, `ANTHROPIC_API_KEY` and the rest of the
auth-clearing list. A second `aprofile sync` reports no changes.

## 3. Share level `all` report (SC-004)

```bash
aprofile add claude private-all --auth-kind oauth-login --share all
aprofile show claude/private-all          # folder: not self-contained, wrapper-only: auth-clearing
aprofile doctor; echo "exit=$?"           # info finding, exit status unchanged by it
```

## 4. Manual check with a real login (SC-001, FR-004a)

With a real Claude Max login profile at `content` after `sync`, start Claude with only
`CLAUDE_CONFIG_DIR` set to the profile folder and `CLAUDE_CODE_USE_VERTEX=1` exported. `/status`
shows the login's account and the Anthropic API, not Vertex AI. Evidence for the credential-free
part is in [research.md](research.md) R1.
