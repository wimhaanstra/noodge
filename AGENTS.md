# Authoring `noodge.yaml` (for AI agents)

This file tells an LLM everything it needs to write a **correct, valid**
`noodge.yaml` in one pass. It is the authoring contract; the human-facing tour
is in [README.md](README.md).

`noodge` is a task runner where every command carries its own documentation.
It reads a config from the current project and either opens a TUI browser
(`noodge`) or runs one command with validated parameters (`noodge build`).

**Rule zero:** after writing a config, the user should run `noodge validate`.
It reports every problem with a line and column. Errors block execution
(exit 2); warnings never do. This document marks each rule as **error** or
**warning** so you can get it right before validating.

---

## File location and name

- The file is named `noodge.yaml` **or** `noodge.yml`. Both are accepted; prefer
  `noodge.yaml`.
- It lives at the root of the project. `noodge` walks up from the working
  directory to the nearest one (like git finding `.git`) and stops at a
  repository boundary.
- Commands run in the directory holding the config, from any subdirectory.

## Start every file with the schema modeline

```yaml
# yaml-language-server: $schema=https://wimhaanstra.github.io/noodge/schema/v1/noodge.schema.json
version: 1
name: my-project
```

The modeline gives editors autocomplete and hover docs. `version` is currently
always `1` (omitting it is a **warning**; the file is assumed to be version 1).
`name` is shown in the TUI header.

---

## Minimal valid file

The smallest thing that validates: one command with one step.

```yaml
# yaml-language-server: $schema=https://wimhaanstra.github.io/noodge/schema/v1/noodge.schema.json
version: 1
name: my-project

commands:
  build:
    description: Compiles the project.
    steps:
      - go build ./...
    output: Nothing on success; compiler errors on failure.
```

`description` and `output` are optional (omitting either is only a **warning**)
but they are the entire point of noodge — write them. They are what the browser
shows a human deciding which command to run, and they can run to several
paragraphs. Use a YAML block scalar (`|`) for multi-line prose.

---

## Full field reference

### Top level (`Config`)

| Key | Type | Required | Notes |
|---|---|---|---|
| `version` | int | no | Always `1`. Omitting → **warning**. |
| `name` | string | no | Shown in the TUI header. |
| `shell` | string | no | Interpreter for string steps. Default `cmd /c` on Windows, `sh -c` elsewhere. A command's own `shell` wins. |
| `env` | map<string,string> | no | Applied to every command. A command's `env` is merged over the top. |
| `commands` | map | **yes** | At least one entry, or it's an **error** ("no commands declared"). |

### A command (`commands.<name>`)

The key is the name you type: `noodge build`, `noodge start:local`.

| Key | Type | Required | Notes |
|---|---|---|---|
| `description` | string | no (warn) | Long-form docs, TUI right pane. Multi-paragraph OK. |
| `steps` | list | **yes** | At least one, or **error**. See [Steps](#steps). |
| `output` | string | no (warn) | What the command produces. Documentation only; never verified. |
| `params` | list | no | Parameters, validated and coerced before any step runs. See [Params](#parameters). |
| `env` | map<string,string> | no | Merged over the file-level `env`. |
| `cwd` | string | no | Working dir, **relative** to the config. Absolute → **error**. Missing dir → **warning** (may be created by an earlier step). |
| `aliases` | list<string> | no | Alternative names. Same charset as command names; must be unique. |
| `hidden` | bool | no | Hides from TUI and completion; still runnable by name. For internal/CI commands. |
| `shell` | string | no | Overrides the interpreter for this command's string steps. |

**Command name charset** (`error` if violated): start with a letter or digit,
then letters, digits, and any of `: . _ -`. Colons are idiomatic
(`start:local`, `db:migrate`).

**Reserved names** (`warning`): `completion help init list run schema upgrade
validate version`. Declaring one is allowed but plain `noodge <name>` runs the
built-in — reach yours with `noodge run <name>`, or rename it.

**Duplicate command name** → **error**.

### Parameters (`commands.<name>.params`)

Each entry:

| Key | Type | Required | Notes |
|---|---|---|---|
| `name` | string | **yes** | The template variable. `{{host}}` / `{{flag host}}` refer to the param named `host`. Charset: start with a letter or `_`, then letters, digits, `_`, `-`. |
| `flag` | string | **yes** | How it's typed on the command line, **with two leading dashes**, e.g. `--host`. Single-dash long flags are not expressible. No spaces. |
| `short` | string | no | One-character shorthand with a single dash, e.g. `-c`. |
| `type` | enum | no | `string` (default), `int`, `number`, `bool`, `path`, `enum`. |
| `description` | string | no (warn) | Shown beside the field in the TUI and in help. |
| `required` | bool | no | Refuses to run unless supplied. |
| `default` | any | no | Used when unset. Must match `type`. A param with a default is never unset. |
| `values` | list<string> | for `enum` | Allowed values (also completion candidates). |
| `pattern` | string | no | Regex the value must match. Invalid regex → **error**. |

Parameter validation rules:

- Missing `name` or `flag` → **error**. Duplicate `name`, `flag`, or `short`
  within a command → **error**.
- `flag` not starting with `--` → **error**. `short` not a single dash + single
  char → **error**.
- `type` outside the six allowed → **error**.
- `type: enum` with no `values` → **error**. `values` given without
  `type: enum` → **warning** (ignored).
- `default` whose type doesn't match `type` → **error** (e.g. a non-bool
  default on a `bool`, a non-listed default on an `enum`).
- `required: true` **and** a `default` → **warning** (the default can never
  apply).
- **Every declared param must be used by at least one step**, or **error**.
  Conversely a step referencing an undeclared param → **error**.

### Parameter types

| Type | Value | Notes |
|---|---|---|
| `string` | any text | The default when `type` is omitted. |
| `int` | whole number | |
| `number` | may have a fraction | |
| `bool` | takes **no value** on the command line | Passing the flag sets it true. |
| `path` | filesystem path | Leading `~` expanded; a `required` path is checked to exist before running. |
| `enum` | one of `values` | |

---

## Steps

A `steps:` list runs in order, each step its own process. The command stops at
the first non-zero exit and reports that code. A step takes one of three forms:

**1. String — run through a shell.** Pipes, redirects, and env expansion work.

```yaml
steps:
  - npm run build
  - cat report.txt | grep FAIL
```

**2. List — executed directly, no shell.** The safer form when a value may be
untrusted (no shell parsing of the value at all).

```yaml
steps:
  - ["npm", "pack", "--pack-destination", "./dist"]
```

**3. Parallel group — start several things at once.** Entries are named; the
name labels each line of that entry's output.

```yaml
steps:
  - npm install                # ordinary sequential step first
  - parallel:
      api: node api.js
      worker: node worker.js
      web: npm run dev
    prefix: true               # optional; default true
```

Parallel rules:

- Only the keys `parallel` and `prefix` are allowed in the group → other keys
  are an **error**.
- `parallel` must have at least one entry (**error** if empty). One entry →
  **warning** (same as an ordinary step).
- Entry names: start with a letter or digit, then letters, digits, `. _ -`
  (**no colon**, unlike command names). Duplicate entry name → **error**.
- Groups **cannot nest** — a parallel entry that is itself a parallel group is
  an **error**. Move the inner group into its own command and call it.
- `prefix: false` turns off the per-line labels. Labelling captures output
  through a pipe, which makes many tools disable colour; set `false` when you'd
  rather keep colour than labels.
- A non-zero exit from any entry stops the whole group. A clean exit is left
  alone (so a one-shot task finishing doesn't tear down the servers beside it).

An empty step (no command at all) → **error**.

---

## Placeholders

Steps are plain text; substitute parameter values with `{{...}}`.

| Placeholder | Condition | Expands to |
|---|---|---|
| `{{flag host}}` | set or defaulted | `--host localhost` (flag + value) |
| `{{flag host}}` | optional and unset | **nothing at all** |
| `{{flag verbose}}` | `bool` and true | `--verbose` |
| `{{flag verbose}}` | `bool` and false | **nothing at all** |
| `{{host}}` | set | `localhost` (the bare value) |
| `{{args}}` | — | whatever the user typed after a bare `--` |

Key points:

- **Prefer `{{flag x}}`** for optional params: it disappears entirely when
  unset, so there's no dangling `--host` with nothing after it. Using bare
  `{{x}}` for an optional param with no default → **warning** (expands to
  nothing).
- The flag *you type to noodge* is independent of how it reaches the wrapped
  tool. A step can rewrite it any way the tool needs:

  ```yaml
  steps:
    - node app.js -host {{host}}            # single dash
    - msbuild /p:Configuration={{config}}   # slash
    - java -Xmx{{heap}}m -jar app.jar       # glued together
  ```

- `{{args}}` is substituted where it appears; if no step mentions it, extra
  args after `--` are appended to the last step.
- Values are always shell-quoted when substituted into a string step — they
  reach the tool as literal data, never as shell syntax. `{{args}}` is the one
  deliberate exception. For values that may contain a `%` on Windows, use the
  **list** step form.

---

## A complete example with parameters

```yaml
# yaml-language-server: $schema=https://wimhaanstra.github.io/noodge/schema/v1/noodge.schema.json
version: 1
name: my-api

commands:
  start:
    description: |
      Starts the API server against the shared development database.

      Day-to-day command. Hot-reloads on save.
    steps:
      - node server.js
    output: Streams the server log to stdout on http://localhost:3000.

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
      - name: verbose
        flag: --verbose
        type: bool
        description: Log every request.
    steps:
      - node server.js {{flag host}} {{flag certificate}} {{flag verbose}}
    output: Server log on stdout.

  dev:
    description: Runs the whole stack locally.
    steps:
      - npm install
      - parallel:
          api: noodge start
          worker: node worker.js
          web: npm run dev

  test:
    description: |
      Runs the suite. Extra args pass through:
      `noodge test -- -run TestX -v`.
    steps:
      - go test ./... {{args}}
    output: One line per package, then ok or FAIL.
```

---

## Pre-flight checklist

Before returning a `noodge.yaml`, confirm:

- [ ] First line is the schema modeline; `version: 1` is present.
- [ ] `commands:` has at least one entry; each has at least one step.
- [ ] Every command and parameter has a `description`; commands have an
      `output` — these are the reason noodge exists.
- [ ] No command name collides with a reserved built-in (or you meant it to).
- [ ] Every `flag` starts with `--`; every `short` is `-x` (one char).
- [ ] Every `enum` has `values`; every `default` matches its `type`; no param
      is both `required` and defaulted.
- [ ] Every declared param is referenced by a step, and every `{{name}}` in a
      step is a declared param.
- [ ] Optional params are referenced via `{{flag x}}`, not bare `{{x}}`.
- [ ] Parallel groups have ≥2 named entries, no nesting, colon-free names.
- [ ] `cwd`, if used, is relative.
- [ ] Recommend the user run `noodge validate` to confirm.
```

