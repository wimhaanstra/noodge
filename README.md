# noodge

A task runner where every command carries its own documentation.

npm scripts, Makefiles and half-remembered `dotnet run` incantations tell you
*that* a command exists. They never tell you what it does, what arguments it
takes, or what it produces. New joiners read the raw command line and guess;
everyone else keeps the knowledge in their head.

`noodge` reads a `noodge.yaml` from the project you are in and gives you two
things:

```
noodge                             # browse the commands, with their docs
noodge start:local --host foo      # run one, with its parameters validated
```

> **Status: early**, but everything described here works.

## Installing

**Windows**

```powershell
irm https://raw.githubusercontent.com/wimhaanstra/noodge/main/scripts/install.ps1 | iex
```

**macOS and Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/wimhaanstra/noodge/main/scripts/install.sh | sh
```

Both verify the download's SHA-256 against the published checksums before
installing anything, and neither needs administrator rights. A script piped
into a shell cannot take arguments, so they are configured with environment
variables — `NOODGE_VERSION`, `NOODGE_INSTALL_DIR`, and `NOODGE_NO_PATH` on
Windows.

**Scoop**

```powershell
scoop bucket add wimhaanstra https://github.com/wimhaanstra/scoop-bucket
scoop install noodge
```

Or download an archive from [Releases](https://github.com/wimhaanstra/noodge/releases)
and put the binary on your PATH. With Go installed, `go install
github.com/wimhaanstra/noodge/cmd/noodge@latest` also works.

## The browser

Run `noodge` with no arguments and you get commands on the left, their full
documentation on the right:

```
noodge  D:\dev\noodge\noodge.yaml

                    |
> build             | build
  test              |
  test:race         | Compiles every package.
  lint              |
  install           | The fastest check that a change has not broken anything
                    | structurally. Does not produce a binary; use install for
                    | that.
                    |
                    | Output
                    | Nothing on success. Compiler errors on failure.
                    |
                    | Steps
                    | 1. go build ./...

up/down move   enter run   / filter   q quit
```

`/` filters on names *and* descriptions, so a word you half-remember from the
docs finds the command.

If the command takes parameters, Enter opens a form — required ones first,
defaults pre-filled, each with its description beside it. Confirming prints
the equivalent command line before running it, so the browser teaches the
command line rather than replacing it:

```
noodge start:local --certificate dev.pfx --host localhost --verbose
```

The browser never runs anything itself. It hands back a command name and its
arguments, and those go through exactly the same path a typed invocation does
— so the command inherits the real terminal, keeps its colours, can prompt for
input, and reports its own exit code.

In a pipe, in CI, or anywhere without a terminal, `noodge` lists instead. That
is the correct answer rather than a fallback.

## What a noodge.yaml looks like

```yaml
# yaml-language-server: $schema=https://wimhaanstra.github.io/noodge/schema/v1/noodge.schema.json
version: 1
name: my-api

commands:
  start:
    description: |
      Starts the API server against the shared development database.

      Day-to-day command. Hot-reloads on save. Nothing is written to disk,
      so it is safe to kill at any time.
    steps:
      - node myscript.ts
    output: |
      Streams the server log to stdout. Listens on http://localhost:3000.

  start:local:
    description: Starts the API behind a local HTTPS listener.
    params:
      - name: host
        flag: --host
        type: string
        default: localhost
        description: Hostname the server binds to.
      - name: certificate
        flag: --certificate
        short: -c
        type: path
        required: true
        description: Path to the .pfx used for the local HTTPS listener.
    steps:
      - node myscript.ts {{flag host}} {{flag certificate}}
    output: Server log on stdout.
```

`description` and `output` are the point of the whole thing. They are what the
browser shows you when you are trying to remember which command to run.

> **Writing one with an AI agent?** [AGENTS.md](AGENTS.md) is a compact,
> field-by-field authoring contract written for an LLM — every key, every
> validation rule, and a pre-flight checklist. Point your agent at it.

### Placeholders

A parameter has two separate spellings: `name` is the template variable, and
`flag` is what you type on the command line.

| Placeholder | When the parameter is | Expands to |
|---|---|---|
| `{{flag host}}` | set, or defaulted | `--host localhost` |
| `{{flag host}}` | optional and unset | **nothing at all** |
| `{{flag verbose}}` | a `bool`, and true | `--verbose` |
| `{{flag verbose}}` | a `bool`, and false | **nothing at all** |
| `{{host}}` | set | `localhost` — the value alone |
| `{{args}}` | — | whatever you typed after `--` |

`{{flag host}}` disappearing entirely when unset is what stops an optional
parameter leaving a dangling `--host` with nothing after it.

The flag spelling you type to noodge is independent of how it reaches the
wrapped tool. noodge's own flags follow the usual `--long` convention, but a
step is plain text, so it can say whatever the tool needs:

```yaml
steps:
  - node app.js -host {{host}}              # single dash
  - msbuild /p:Configuration={{config}}     # slash
  - java -Xmx{{heap}}m -jar app.jar         # glued together
```

### Steps

A step written as a **string** runs through a shell, so pipes and redirects
work. A step written as a **list** is executed directly with no shell at all,
which is the safer form when a parameter value is untrusted:

```yaml
steps:
  - npm run build
  - ["npm", "pack", "--pack-destination", "./dist"]
```

Each step is its own process, so "stop at the first failure" is real rather
than inherited from shell semantics — which matters on Windows, where
PowerShell 5.1 has no `&&` at all.

### Running things at the same time

Steps are sequential, so a **parallel group** is how you start a set of
services together. Entries are named, and the name labels their output:

```yaml
dev:
  description: Runs the whole stack locally.
  steps:
    - npm install          # sequential, as usual
    - parallel:
        api: noodge api
        worker: noodge worker
        web: npm run dev
```

```
api    | listening on :3000
worker | polling for jobs
web    | vite ready in 412 ms
```

Entries are ordinary steps, so `noodge api` works and each service keeps its
own documentation and parameters.

**When one exits.** A non-zero exit stops the whole group and noodge reports
that exit code — a crashed API with two healthy logs still scrolling is not a
useful state. A clean exit is left alone, so a watcher or a one-shot task that
finishes does not tear down the servers beside it.

**Nothing is left behind.** Everything a group starts goes into one process
tree, so stopping it also stops what those processes started. This is the part
that is easy to get wrong on Windows, which has no process groups: killing
`npm` there leaves `node` holding the port, invisible to whoever is wondering
why the next run cannot bind. noodge uses a Job Object with kill-on-close, so
the whole tree goes.

**Prefixes cost colour.** Labelling output means capturing it through a pipe,
and a program handed a pipe concludes it is not talking to a terminal and turns
its colours off. noodge sets `FORCE_COLOR` and `CLICOLOR_FORCE`, which many
tools honour, but anything that only checks for a terminal will go monochrome.
When you would rather have the colours than the labels:

```yaml
    - parallel:
        web: npm run dev
      prefix: false
```

Entries get no stdin: several processes reading one terminal is a race with no
useful outcome.

### Asking before a destructive command

Some commands you want to be sure about — dropping a database, deleting build
artifacts, deploying to production. `confirm` makes noodge ask first:

```yaml
db:reset:
  description: Drops and recreates the local database.
  confirm: true                        # a default prompt
  steps:
    - ./scripts/reset-db.sh

deploy:prod:
  description: Deploys to production.
  confirm: This deploys to PRODUCTION. Continue?   # your own prompt
  steps:
    - ./scripts/deploy.sh --env prod
```

`confirm: true` asks with a default question; a string asks with that string.
Omitting it, or `confirm: false`, runs without asking.

The prompt defaults to **no**, so a stray Enter cancels rather than fires.
Declining runs nothing and exits 2. It behaves the same whether you type the
command or pick it in the browser, because the browser runs your choice through
the same path.

`--yes` answers the prompt for you, which is how a confirm command runs in CI
or a script:

```bash
noodge deploy:prod --yes
```

Without a terminal to ask at and without `--yes`, a confirm command refuses to
run rather than guess an answer — a destructive command should never fire
unattended by accident. `--dry-run` never asks; it only prints what would run.

## Commands

| | |
|---|---|
| `noodge` | Open the browser, or list when there is no terminal |
| `noodge <command>` | Run a command from `noodge.yaml` |
| `noodge run <command>` | The same, for when the name collides with one below |
| `noodge list [--json]` | List commands, optionally machine-readable |
| `noodge validate` | Check `noodge.yaml` and report problems with line numbers |
| `noodge init` | Write a starter `noodge.yaml` |
| `noodge completion <shell>` | Print the completion script |
| `noodge completion install <shell>` | Set completion up for you |
| `noodge schema` | Print the JSON Schema |
| `noodge version` | Print the version |

`-C <dir>` runs against a project elsewhere, `NOODGE_CONFIG` points at a
specific file, `--dry-run` prints the exact command lines without running
anything, and `--yes` answers any confirmation prompt (see below) with yes.

Built-in names win, so a command you call `list` is reached with
`noodge run list`. `noodge validate` warns when that happens rather than
leaving you to find out later.

### Passing extra arguments

Anything after a bare `--` goes to the wrapped tool:

```bash
noodge test -- -run TestDiscover -v
```

It is substituted where `{{args}}` appears, or appended to the last step when
no step mentions it. `noodge <command> --help` says which, for that command,
and `--dry-run` shows exactly where it landed.

### Exit codes

A step's exit code passes through untouched, so `noodge build` is a drop-in
replacement for the command it wraps in a script or a CI job. A command stops
at the first step that fails. noodge's own refusals — a bad config, an unknown
command, a missing required parameter — exit **2**, and every one of them
happens before any process starts.

## Tab completion

Completion is **per directory**. The generated script calls back into `noodge`
on every TAB, running wherever you are, so it offers the commands *this*
project declares — including their descriptions.

```bash
noodge completion install pwsh
```

Targets are `pwsh`, `windows-powershell`, `bash`, `zsh` and `fish`. It writes
the script to its own file, adds one line to your profile, shows you the change
first, takes a backup, and does nothing the second time you run it. Add
`--print-only` to see the plan without writing anything.

The two PowerShell editions are named separately because they do not share a
profile — PowerShell 7 uses `Documents\PowerShell`, Windows PowerShell 5.1 uses
`Documents\WindowsPowerShell`. Installing for the wrong one leaves you with no
completion and no clue why, so when both are present `noodge` asks which you
mean rather than guessing.

To do it by hand instead, `noodge completion <shell>` prints the script.

On Windows, the default Tab binding cycles matches inline one at a time and
**cannot show descriptions** — which for this tool is most of what completion
is worth. One line fixes it:

```powershell
Set-PSReadLineKeyHandler -Key Tab -Function MenuComplete
```

`Ctrl+Space` already shows the menu without rebinding anything.

A `noodge.yaml` you are halfway through editing still completes the commands
it does contain, and a directory with no config completes the built-ins. The
completion path never writes to stderr and never exits non-zero, because the
generated PowerShell script reads the last line of output as an integer — one
stray message there breaks TAB in a way that is very hard to diagnose.

If completion seems to do nothing in VS Code's integrated terminal, that
terminal has its own suggestion widget bound to Tab. Turn it off with
`"terminal.integrated.suggest.enabled": false`.

## Editor support

Put the modeline at the top of your `noodge.yaml` and any editor with the YAML
language server gets completion and hover text:

```yaml
# yaml-language-server: $schema=https://wimhaanstra.github.io/noodge/schema/v1/noodge.schema.json
```

The descriptions come from the Go doc comments in `internal/config`, so the
schema and the implementation cannot drift apart. CI checks that they haven't.

## Configuration is discovered, not configured

`noodge` walks up from your working directory to the nearest `noodge.yaml`,
the same way git finds `.git`. It stops at a repository boundary, so a stray
config in your home directory is never picked up by an unrelated project.

Commands run in the directory holding the config, not wherever you happened to
be standing, so `noodge build` means the same thing from any subdirectory.

## Errors point at the line

```
noodge.yaml:8:15: error: flag "-host" must start with two dashes
  hint: write it as --host. This is only how you type it to noodge; a step is
        still free to write -host {{host}} to pass it on with a single dash
```

Missing descriptions are warnings, never errors. A half-written config is
exactly when you least want an argument with your tooling.

## Parameter values are always quoted

Values arrive from terminals and from CI, and a string step goes through a
shell, so every value is quoted as it is substituted. There is no per-parameter
opt-out:

```bash
noodge deploy --host 'a && shutdown /s'
```

reaches the wrapped tool as one literal argument. It is data, and it stays
data. `{{args}}` is the single deliberate exception, because those are words
the user typed at their own prompt for that one invocation.

Getting this right on Windows takes two passes, because two parsers disagree
about it: `cmd.exe` escapes an embedded quote as `""`, while the C runtime in
the program being launched expects `\"`. Satisfying either alone breaks the
other. noodge quotes for the C runtime, then caret-escapes every character
`cmd` acts on — the quotes included — so `cmd` never enters its quoted state,
and what survives its pass is exactly what the program expects.

One gap is documented rather than fixed: `cmd` expands `%VAR%` before caret
processing, and nothing on a command line prevents that. Expansion produces
text rather than a command, so it cannot run anything, but it is a surprise.
Use the list step form for values that may contain a percent sign.

## No telemetry

`noodge` collects nothing and phones home to nobody. The only network request
it will ever make is an update check, which is cached, never blocks a command,
and is switched off entirely by `NOODGE_NO_UPDATE_CHECK=1`.

## Building

```bash
go build ./...
go test ./...
go run ./tools/gen-schema   # after changing anything in internal/config
```

The repository has its own `noodge.yaml`, so once you have noodge installed
you can use it on itself.

## Updating

```bash
noodge upgrade
```

Downloads the latest release, checks it against the published checksum, and
replaces the running binary.

noodge tells you when a newer version exists, but never updates itself. The
check happens at most once a day, runs alongside your command rather than
before it, and is never waited for — so it costs your command nothing, and the
notice you see comes from what a previous run already learned. It goes to
stderr, and is skipped entirely in CI, when output is redirected, on a
development build, and whenever `NOODGE_NO_UPDATE_CHECK` is set.

If a package manager installed noodge, `upgrade` refuses and tells you what to
run instead:

```
noodge: this noodge was installed by Scoop, which keeps its own record of the
installed version. Upgrading in place would leave Scoop believing the old
version is still installed, and its next update would put it back.

  scoop update noodge
```

`noodge upgrade --check` reports without changing anything, exiting **10** when
an upgrade is available so a script can branch on it.

## Releasing

Ordinary commits run CI and publish nothing. A release happens when a tag is
pushed:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

That builds Windows, macOS and Linux for amd64 and arm64, writes checksums,
and publishes a GitHub release with a changelog. To see what a release would
produce without publishing anything:

```bash
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
```

## Licence

MIT. See [LICENSE](LICENSE).
