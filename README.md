<p align="center">
  <img src="assets/brand/maggot-icon-256.png" alt="maggot" width="120" height="120">
</p>

<h1 align="center">maggot</h1>

<p align="center">
  A shared toolbox of CLI tools and skills for you and your AI coding agents.<br>
  Build a tool once. Every project and every agent can find it, run it and improve it.
</p>

<p align="center">
  <a href="#install">Install</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#commands">Commands</a> ·
  <a href="#skills">Skills</a> ·
  <a href="#web-ui">Web UI</a> ·
  <a href="#writing-tools">Writing tools</a>
</p>

---

When an agent builds a useful utility in one project (resize images, convert
video, merge PDFs, call some API), it usually disappears with that project.
The next project, or the next agent, writes it again. maggot keeps these tools
in one folder, so any agent in any project can find them, check their
dependencies, run them, and add new ones. Agent memory doesn't matter: the
toolbox is the memory.

- **Tools are plain CLI programs.** Any language, any dependencies. Each tool is
  a folder with a `tool.json` manifest. Tools run without maggot.
- **One binary, no server, no MCP.** Agents call `maggot` through the shell.
- **One skill** teaches agents to search the toolbox before writing code.
- **Skills for workflows.** A toolbox can also hold
  [Agent Skills](https://agentskills.io) (`skills/<name>/SKILL.md`):
  step-by-step workflows that combine tools, scripts, programs and APIs.
  Agents follow them; any tool that reads Agent Skills can install them
  straight from the toolbox repo.
- **Toolboxes are plain folders**, so you can keep them in git.

## Install

From a local checkout (no download):

```sh
make install          # builds with the git version and installs to $(go env GOPATH)/bin
maggot --version
```

`make install` uses `go env GOBIN`, or `$(go env GOPATH)/bin` (usually `~/go/bin`).
Make sure that folder is on your PATH. Run `make install` again after pulling
changes, then `maggot skill install` to update the agent skill.

From GitHub: `go install github.com/nxnom/maggot/cmd/maggot@latest`.

## Quick start

```sh
maggot toolbox init ~/toolbox            # create and register a toolbox
maggot skill install                     # SKILL.md for Claude Code and Codex

maggot new text-upper --lang python      # → ~/.local/share/maggot/drafts/text-upper
maggot drafts                            # list drafts that are not imported yet
maggot test text-upper                   # validate + run tests/cases.json (a draft works by name)
maggot try text-upper                    # try it in the web UI, import from there

maggot search upper case text
maggot run text-upper -- --upper --out out notes.txt
```

### Install the skill with npx

If you already use the `skills` installer, you can add maggot's skill to your
agents (Claude Code, Codex, Cursor and others) straight from this repo:

```sh
npx skills add nxnom/maggot
```

This only installs the skill, which teaches agents to call `maggot`. You still
need the `maggot` binary (see [Install](#install)). Prefer `maggot skill
install` when you can: it writes the skill text that matches your binary, so
they never get out of sync. After `maggot` upgrades, run it again (or
`npx skills update`).

## Commands

| command | what it does |
|---|---|
| `toolbox init/add/list/remove/default` | create and register toolbox folders |
| `list`, `search <words>` | find tools and skills (`--json` for agents) |
| `info <tool>` | short view: inputs, readiness, tested cases, examples (`--full` for the manifest and README) |
| `prepare <tool> --inputs '{…}' --out <dir>` | check a run without running it: inputs, requirements, output folder, exact command |
| `doctor [tool]` | check required binaries, versions and env vars; `--install` asks before installing |
| `run <tool> -- <args>` | run a tool in the current folder; `--inputs '{json}'` builds args from the manifest; `--json` prints one structured result (status, files, errors) |
| `new <id> --lang bin\|sh\|python\|deno` | scaffold a working tool with tests, in the drafts folder |
| `draft <tool>` | copy an installed tool into the drafts folder to change it |
| `drafts` | list tool and skill drafts that are not imported yet (`--json` for agents). Drafts work by name in `test`, `try`, `import` and `skill show/run/path` |
| `try <folder>` | open a tool folder in the web UI to try it before importing |
| `run <tool> --review -- …`, `take <run> <folder>` | hold output for review in the web UI, then move (not copy) the approved files into place |
| `runs`, `runs rm <run>` | list or delete saved runs |
| `open <tool>` | open a tool's folder in the file manager |
| `presets <tool>`, `presets save/rm`, `run --preset <name>` | saved input values per tool, personal or shared in the toolbox |
| `warm <tool> --for 30m`, `warm list`, `warm stop` | keep a slow-loading tool (e.g. a model) in memory so the next runs start fast |
| `clean [runs\|drafts\|uploads\|all]` | show or delete leftovers in the data folder (never toolboxes or projects) |
| `test <dir\|tool>`, `test --all` | validate and run `tests/cases.json`; network and paid cases only run with `--network` / `--paid`. Results show in `list`, `info` and the UI |
| `import <file\|dir>`, `export <tool>`, `pack <dir>`, `remove <tool>` | share tools and skills as `.maggot` archives (zip files) |
| `archive <tool\|skill>`, `restore <name>`, `archived` | hide an old tool or skill from search, list and agents without deleting it (moved to `<toolbox>/.archive/`) |
| `secret set/rm/list` | store secret env values in the OS keychain, outside the toolbox (`--skill` for skills) |
| `config [edit\|path\|show\|validate]` | maggot's own settings |
| `skill install/uninstall/show` | maggot's own agent skill (no names) |
| `skill list`, `skill show <name>` | toolbox skills, with a readiness check (tools, programs, secrets) |
| `skill new <name>`, `skill draft <name>` | create a skill (Agent Skills format) or copy one to change it, in the drafts folder |
| `skill install <name>` / `uninstall <name>` | link a toolbox skill into Claude Code / Codex skills folders (`--copy` to copy) |
| `skill run <name> <script> [args]` | run a script from the skill folder: no path needed, the interpreter is picked from the file type, secrets are added, runs in the current folder |
| `skill path <name> [file]` | print the skill folder, or the full path of a file in it (`references/`, `assets/`) |
| `skill exec <name> -- <cmd>` | run any other command with the skill's secrets in the environment |
| `ui` | local web UI: a form for every tool, live runs, output previews |
| `guide` | full tool authoring reference |

Use a tool from a specific toolbox with `toolbox/id` (for example
`team/image-resize`), or pick one toolbox with `--toolbox <name|path>`.
A toolbox around the current folder is found automatically, even if it isn't
registered.

## Files

```
~/.config/maggot/config.json      registered toolboxes, default toolbox, runs_keep
<toolbox>/maggot.json             toolbox name (marks the folder as a toolbox)
<toolbox>/tools/<id>/tool.json    one tool: entry, requires, env, inputs, outputs
<toolbox>/skills/<name>/SKILL.md  one skill (Agent Skills format) + scripts/, references/, assets/
```

### Skills

A skill is a folder in the [Agent Skills](https://agentskills.io) format, so
it works with any agent that reads skills. maggot keeps its own fields under
the standard `metadata` map:

```yaml
---
name: video-short
description: Make a 60 s horror short from stills and cloned voices. Use when ...
metadata:
  version: 1.0.0
  maggot-uses: voxcpm-tts depth-map      # maggot tools the steps call
  maggot-requires: ffmpeg node           # programs needed on PATH
  maggot-env: STABILITY_API_KEY          # secrets (maggot secret set --skill)
---
```

Steps in a skill call its scripts by name, so they work from any project
folder, with no path to build:

```sh
maggot skill run video-short sfx.py rain.wav        # runs scripts/sfx.py
cat "$(maggot skill path video-short references/prompts.md)"
```

Because skills live in `skills/` at the toolbox root, a toolbox pushed to
GitHub can also be installed with other skill installers (for example
`npx skills add <owner>/<repo>`). See `examples/toolbox/skills/audio-clip`.

The config folder is `$MAGGOT_CONFIG_DIR`, then `$XDG_CONFIG_HOME/maggot`, then
`~/.config/maggot` (`%AppData%\maggot` on Windows).

## Web UI

```sh
maggot ui            # opens http://127.0.0.1:26244 in your browser
```

- Pick a toolbox folder (or create one), then pick a tool. The form is built
  from the tool's `inputs`; the exact command it will run is shown under it.
- Each run gets its own folder under the data dir (`$MAGGOT_DATA_DIR`,
  `$XDG_DATA_HOME/maggot` or `~/.local/share/maggot`). The tool runs inside
  `<run>/out`, which is also passed as its `outputs.dir_flag`. Output files
  are previewed (images, video, audio) and can be opened, downloaded or shown
  in the file manager. The newest `runs_keep` runs per tool are kept (default 30).
- Missing requirements can be installed from the tool page (after you confirm).
- Tools that support it have "Keep in memory between runs for N min". When it
  is checked, the next run loads the tool once and keeps it; the minutes count
  from the end of each run. Unchecking it frees the memory at once.
- "About this tool and its code" shows the README and every file (read-only),
  with a button to open the tool folder.
- Skills have their own page: the instructions, the files, what they need
  (tools, programs, secrets) and buttons to install them into Claude Code or
  Codex. "Copy prompt" copies a prompt that tells your agent to use the skill.
- Import `.maggot` files by dropping them on the page.
- The server listens on 127.0.0.1 only and needs the token from the first
  link it prints (kept in the data dir, so bookmarks keep working).

## Writing tools

Run `maggot guide` for the full reference. In short:

- `tool.json` describes how to start the tool (`entry`), what it needs
  (`requires` with install hints per package manager, `env`), its arguments
  (`inputs`, used for `--inputs` and the web UI form), and what it produces
  (`outputs`).
- Prefer runtimes that keep a tool self-contained: Python with `uv` inline
  dependencies, or Deno with `npm:`/`jsr:` imports.
- `tests/cases.json` holds test cases. Each one runs in a temp folder.

## Safety

- Registering a toolbox means you trust its tools.
- Importing a tool shows what will be installed and asks first.
- `doctor --install` asks before each install command.
- Without a terminal, all of these need `--yes`, so an agent has to get the
  user's approval first.
- Archives are checked for path traversal, symlinks and size before anything
  is written into the toolbox.

## Roadmap

- v0.1: toolbox discovery, search, manifest validation, run, doctor,
  import/export, tests, one agent skill.
- v0.2 (this): `maggot ui`, manifest-only `bin` tools, richer inputs.
- v0.3: release binaries and install script, better sharing workflows.

## Development

```sh
make test    # go test ./...
make vet
make build
```

## License

[MIT](LICENSE)
