# aprofile User Guide style guide

Every page follows these terms and rules alongside its section voice.

## Terms

- "aprofile" means the command-line tool for managing named accounts or configurations of coding harnesses.
- "profile" means one account or configuration of one harness, identified by the pair of harness and name.
- "harness" means a supported coding agent command-line program.
- "wrapper" means the launcher aprofile creates for a profile, such as `claude-work`.
- "share level" means how much of a harness's default setup a profile uses.
- "history group" means a named set of profiles of one harness that share one conversation history.
- "isolated profile" means a profile in no history group; it keeps its history to itself.
- "home" means the built-in history group whose store is the harness's default setup.
- "custom group" means a history group created explicitly, with its history in aprofile's data directory.
- Product names are spelled exactly: aprofile, Claude Code, Codex CLI, OpenCode, and Pi.
- Persona name: "End user".

## Rules

- Do not use em dash or en dash characters.
- Use GitHub Markdown and GitHub alerts for admonitions.
- Generated baseline values use `<!-- guide:attr NAME -->...<!-- /guide:attr -->` regions and stay out of code listings.
- Never put real secret values in examples. Use references such as `env:WORK_TOKEN`.
- Explain that profile changes can leave wrappers stale until `aprofile sync` runs.
- Task pages begin with a short paragraph on why, continue with numbered steps, and end with a `Check:` line that states the expected result.
