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

> **Status: early.** The config format, validation, the JSON Schema, running
> commands, the browser and shell completion all work. Installers and
> self-update are still being built.

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
specific file, and `--dry-run` prints the exact command lines without running
anything.

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

## Licence

MIT. See [LICENSE](LICENSE).
