# Get started

<!-- Sources:
aprofile:README.md:3-9,13-40,114-120,203,249-254
url:https://github.com/rhuss/aprofile
aprofile:go.mod:1-4
aprofile:Makefile:1-10
aprofile:internal/harness/builtin/claude.yaml:26-38
aprofile:internal/cli/add.go:48-50,64-75,79-102,190-219
aprofile:internal/cli/list_show.go:548-576,603-619
aprofile:internal/cli/sync.go:138-150
aprofile:internal/cli/login.go:23-40,88-118
aprofile:internal/secret/secret.go:84-106,142-145
aprofile:internal/secret/render.go:8-41
aprofile:internal/wrapper/wrapper.go:124-160,213-226
claude-code:README.md:7
-->

aprofile is a command-line tool for managing profiles of coding agents. Claude Code is a coding tool that runs in your terminal and works on your codebase. A profile keeps one account's configuration separate; a wrapper is the named launcher aprofile creates for it. This walkthrough builds aprofile, creates the `claude/work` profile, and launches its `claude-work` wrapper. That keeps you from switching configuration directories by hand. The profile's history stays private while it belongs to no history group.

The commands below assume you are in the aprofile repository checkout root. Run manager commands as `./bin/aprofile`; launch profile wrappers such as `claude-work` directly.

## Build aprofile

Clone the repository and build aprofile from source. The build requires Go <!-- guide:attr aprofile-go-minimum -->1.24.0<!-- /guide:attr --> or newer and `make`.

1. Clone the repository and enter its directory.

   ```sh
   git clone https://github.com/rhuss/aprofile.git
   cd aprofile
   ```

   Check: the checkout contains the Go module, build file, and command source.

   ```sh
   test -f go.mod && test -f Makefile && test -d cmd/aprofile
   ```
   <!-- expect: exit 0 -->

2. Check that Go meets the module minimum, then build aprofile.

   ```sh
   go version
   make build
   ```

   Check: the build creates `./bin/aprofile`.

   ```sh
   test -x ./bin/aprofile
   ```
   <!-- expect: exit 0 -->

## Create a profile

Claude Code defines four authentication kinds: `api-key`, `auth-token`, and `oauth-token` use secret references; `oauth-login` runs Claude Code's interactive login flow. This example uses `oauth-token` with an environment reference. Keep the secret value out of the command and profile settings.

1. Install Claude Code and make its `claude` command available on `PATH`.

   Check: your shell can find Claude Code.

   ```sh
   command -v claude
   ```
   <!-- expect: exit 0 -->

2. Export the token in the shell that will run the wrapper. The value stays in your shell environment; do not paste it into the aprofile command.

   ```sh
   export WORK_TOKEN='your-token'
   ```

   Check: a child shell can read a nonempty value without printing it.

   ```sh
   sh -c 'test -n "${WORK_TOKEN:-}"'
   ```
   <!-- expect: exit 0 -->

3. Create the `work` profile with `env:WORK_TOKEN`, a reference to that variable.

   ```sh
   ./bin/aprofile add claude work --label "Work" --color blue --icon W \
     --auth-kind oauth-token --auth-key env:WORK_TOKEN
   ```

   aprofile accepts `env:NAME` and `file:/absolute/path` references for authentication. It rejects plain auth values and values that look like secrets. At launch, the wrapper checks that the referenced environment variable is set and nonempty; if not, it stops before starting Claude Code.

   Check: the displayed auth reference resolves. The output shows the reference, not its secret value.

   ```sh
   ./bin/aprofile show claude/work
   ```
   <!-- expect: contains "work" and contains "resolves" -->

   If you prefer Claude Code's interactive login, create the profile with `--auth-kind oauth-login` and no `--auth-key`, then run `./bin/aprofile login claude/work`. Claude Code's login flow uses the profile environment.

## Sync and launch

Run `sync` to prepare the profile configuration and write its wrapper. Changes to a profile can leave wrappers stale until you run `./bin/aprofile sync` again.

1. Sync the profile.

   ```sh
   ./bin/aprofile sync
   ```

   If sync adds aprofile's `bin` directory to a shell startup file, open a new shell to apply the PATH change. If it prints manual PATH instructions, follow them.

   Check: your shell finds the profile wrapper.

   ```sh
   command -v claude-work
   ```
   <!-- expect: exit 0 -->

2. Confirm the exported token remains available in the shell where you launch the wrapper.

   ```sh
   sh -c 'test -n "${WORK_TOKEN:-}"'
   ```
   <!-- expect: exit 0 -->

3. Start Claude Code through the profile wrapper.

   ```sh
   claude-work
   ```

   Check: Claude Code opens in your terminal. This is an interactive check. The wrapper replaces itself with Claude Code and forwards the arguments you pass to it.

After the first launch works, continue with [Compare share levels](share-levels.md), then [Share conversation history with a group](history-groups.md). You can later [manage profiles](manage-profiles.md), [troubleshoot problems](troubleshooting.md), or look up syntax in the [command reference](../reference/commands.md). A new profile starts isolated, with its history to itself.
