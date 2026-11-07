# Get started

<!-- Sources:
aprofile:README.md:3-9,13-40,44-48,199-216
aprofile:Makefile:2-10
aprofile:go.mod:3
aprofile:internal/harness/builtin/claude.yaml:6-12,24-30
aprofile:internal/cli/add.go:85-99
aprofile:internal/cli/list_show.go:110-120,190-208,324-353
aprofile:internal/cli/edit.go:24-25
aprofile:internal/cli/sync.go:65-75
aprofile:internal/wrapper/wrapper.go:143-168,249-255
aprofile:internal/secret/render.go:9-28
claude-code:README.md:7
-->

You'll create a `claude-work` command that starts Claude Code with a work profile. This keeps its credentials and history separate from your other accounts, without choosing a configuration directory each time you launch it.

aprofile is a command-line tool for managing named accounts or configurations of coding tools. Claude Code is a coding assistant that runs in your terminal, understands your codebase, and handles coding tasks through natural language commands. In this guide, a *harness* means a supported coding agent command-line program, such as Claude Code.

A *profile* is one named account or configuration for one harness. A *wrapper* is the launcher aprofile creates for it. Here, `aprofile sync` prepares the profile's configuration and writes `claude-work`; that wrapper starts the installed `claude` program with the profile's environment.

## Build aprofile

Build from an existing aprofile source checkout. Building from source is the supported installation path; release packaging is not available in this baseline. You need Go <!-- guide:attr aprofile-go-minimum -->1.24.0<!-- /guide:attr --> or newer and `make` for the commands below.

1. Open a terminal at the root of your aprofile checkout. Keep this directory for the `./bin/aprofile` commands on this page.

   Check: the checkout contains the build inputs, and the installed Go version is 1.24 or newer, as required by the module.

   ```sh
   test -f go.mod && test -f Makefile && test -d cmd/aprofile
   ```
   <!-- expect: exit 0 -->

   ```sh
   go env GOVERSION | awk -F. '$1 ~ /^go[0-9]+$/ && $2 ~ /^[0-9]+$/ && (substr($1, 3)+0 > 1 || (substr($1, 3)+0 == 1 && $2+0 >= 24)) { exit 0 } { exit 1 }'
   ```
   <!-- expect: exit 0 -->

2. Build the executable.

   ```sh
   make build
   ```

   Check: the executable exists at `./bin/aprofile`. The build does not add it to your shell's command search path, `PATH`.

   ```sh
   test -x ./bin/aprofile
   ```
   <!-- expect: exit 0 -->

## Create a profile

The example stores `env:WORK_TOKEN`, a reference to an environment variable, instead of a token value. The wrapper reads that variable when you launch it. For this example, you need a Claude Code OAuth token available as a nonempty, exported `WORK_TOKEN` in the shell that launches the wrapper.

1. Have Claude Code installed and its `claude` command on `PATH` before you continue.

   Check: your shell finds the installed command.

   ```sh
   command -v claude
   ```
   <!-- expect: exit 0 -->

2. Check: a child shell can read the exported token. This check prints no token value; a successful exit means the variable is present and nonempty.

   ```sh
   sh -c 'test -n "${WORK_TOKEN:-}"'
   ```
   <!-- expect: exit 0 -->

   If it fails, provide and export your token before launching the wrapper. If the reference is missing or empty at launch, the wrapper stops without starting Claude Code.

3. Create the `work` profile. Use an unused profile name; `add` rejects an existing `claude/work` profile.

   ```sh
   ./bin/aprofile add claude work --label "Work" --color blue --icon W \
     --auth-kind oauth-token --auth-key env:WORK_TOKEN
   ```

   Check: the profile shows the name `work` and auth kind `oauth-token`.

   ```sh
   ./bin/aprofile show claude/work
   ```
   <!-- expect: contains "work" and contains "oauth-token" -->

## Sync and launch

Creating a profile records its settings. Run `sync` to prepare the configuration directory and write its wrapper. Profile changes can leave wrappers stale, so run `./bin/aprofile sync` again after editing a profile.

1. Sync your profiles.

   ```sh
   ./bin/aprofile sync
   ```

   Check: the wrapper line ends with `[synced]`.

   ```sh
   ./bin/aprofile show claude/work
   ```
   <!-- expect: contains "[synced]" -->

2. Follow the PATH message from `sync`. When it updates your shell startup file, open a new shell to apply the change. If it prints manual PATH instructions, apply those instructions.

   Check: the new shell finds the wrapper, and the exported token is still available there.

   ```sh
   command -v claude-work
   ```
   <!-- expect: exit 0 -->

   ```sh
   sh -c 'test -n "${WORK_TOKEN:-}"'
   ```
   <!-- expect: exit 0 -->

3. Launch the profile.

   ```sh
   claude-work
   ```

   Check: Claude Code opens in your terminal. This is an interactive check. The wrapper replaces itself with the real `claude` process and forwards any arguments unchanged.

Your work profile has its own credentials and runtime state. To choose how much of your default setup it uses, continue with [Compare share levels](share-levels.md).
