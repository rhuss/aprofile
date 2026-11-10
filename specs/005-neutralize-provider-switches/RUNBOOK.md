# Runbook: Neutralize Provider Switches

## Manual check before merge (FR-004a, SC-001, quickstart step 4)

This check needs a real Claude Max login, so it is not automated. Research R1 covers the
credential-free part.

1. Pick a Claude profile with a real login at share level `content` (or create one and log in
   with `./bin/aprofile login claude/<name>`), then run `./bin/aprofile sync`.
2. Confirm the folder holds the neutralizing entries:

   ```sh
   jq '.env.CLAUDE_CODE_USE_VERTEX' "$(./bin/aprofile show claude/<name> --json | jq -r .config_dir)/settings.json"
   ```

   Expected: `""`.
3. Start Claude Code from the folder alone with a provider switch exported:

   ```sh
   env CLAUDE_CODE_USE_VERTEX=1 CLAUDE_CONFIG_DIR="$(./bin/aprofile show claude/<name> --json | jq -r .config_dir)" claude
   ```

4. Run `/status` in the session.

   Expected: the login's account and the Anthropic API, not Google Vertex AI.
