# maggot

A shared toolbox of standalone CLI tools for you and your AI coding agents.

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
maggot test <that folder>                # validate + run tests/cases.json
maggot try <that folder>                 # try it in the web UI, import from there

maggot search upper case text
maggot run text-upper -- --upper --out out notes.txt
```

## Commands

| command | what it does |
|---|---|
| `toolbox init/add/list/remove/default` | create and register toolbox folders |
| `list`, `search <words>` | find tools (`--json` for agents) |
| `info <tool>` | short view: inputs, readiness, tested cases, examples (`--full` for the manifest and README) |
| `prepare <tool> --inputs '{…}' --out <dir>` | check a run without running it: inputs, requirements, output folder, exact command |
| `doctor [tool]` | check required binaries, versions and env vars; `--install` asks before installing |
| `run <tool> -- <args>` | run a tool in the current folder; `--inputs '{json}'` builds args from the manifest; `--json` prints one structured result (status, files, errors) |
| `new <id> --lang bin\|sh\|python\|deno` | scaffold a working tool with tests, in the drafts folder |
| `draft <tool>` | copy an installed tool into the drafts folder to change it |
| `try <folder>` | open a tool folder in the web UI to try it before importing |
| `run <tool> --review -- …`, `take <run> <folder>` | hold output for review in the web UI, then move (not copy) the approved files into place |
| `runs`, `runs rm <run>` | list or delete saved runs |
| `open <tool>` | open a tool's folder in the file manager |
| `warm <tool> --for 30m`, `warm list`, `warm stop` | keep a slow-loading tool (e.g. a model) in memory so the next runs start fast |
| `clean [runs\|drafts\|uploads\|all]` | show or delete leftovers in the data folder (never toolboxes or projects) |
| `test <dir\|tool>`, `test --all` | validate and run `tests/cases.json`; network and paid cases only run with `--network` / `--paid`. Results show in `list`, `info` and the UI |
| `import <file\|dir>`, `export <tool>`, `pack <dir>`, `remove <tool>` | share tools as `.maggot` archives (zip files) |
| `secret set/rm/list` | store secret env values in the OS keychain, outside the toolbox |
| `config [edit\|path\|show\|validate]` | maggot's own settings |
| `skill install/uninstall/show` | the agent skill |
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
```

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
