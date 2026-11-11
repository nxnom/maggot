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

maggot new text-upper --lang python --dir /tmp/work
maggot test /tmp/work/text-upper         # validate + run tests/cases.json
maggot import /tmp/work/text-upper       # asks first; --yes to skip

maggot search upper case text
maggot run text-upper -- --upper --out out notes.txt
```

## Commands

| command | what it does |
|---|---|
| `toolbox init/add/list/remove/default` | create and register toolbox folders |
| `list`, `search <words>` | find tools (`--json` for agents) |
| `info <tool>` | manifest, README, and the direct command without maggot |
| `doctor [tool]` | check required binaries, versions and env vars; `--install` asks before installing |
| `run <tool> -- <args>` | run a tool in the current folder; `--inputs '{json}'` builds args from the manifest |
| `new <id> --lang python\|sh\|deno` | scaffold a working tool with tests |
| `test <dir\|tool>` | validate the manifest and run `tests/cases.json` |
| `import <file\|dir>`, `export <tool>`, `pack <dir>`, `remove <tool>` | share tools as `.maggot` archives (zip files) |
| `secret set/rm/list` | store secret env values in the OS keychain, outside the toolbox |
| `config [edit\|path\|show\|validate]` | maggot's own settings |
| `skill install/uninstall/show` | the agent skill |
| `guide` | full tool authoring reference |

Use a tool from a specific toolbox with `toolbox/id` (for example
`team/image-resize`), or pick one toolbox with `--toolbox <name|path>`.
A toolbox around the current folder is found automatically, even if it isn't
registered.

## Files

```
~/.config/maggot/config.json      registered toolboxes, default toolbox
<toolbox>/maggot.json             toolbox name (marks the folder as a toolbox)
<toolbox>/tools/<id>/tool.json    one tool: entry, requires, env, inputs, outputs
```

The config folder is `$MAGGOT_CONFIG_DIR`, then `$XDG_CONFIG_HOME/maggot`, then
`~/.config/maggot` (`%AppData%\maggot` on Windows).

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

- v0.1 (this): toolbox discovery, search, manifest validation, run, doctor,
  import/export, tests, one agent skill.
- v0.2: `maggot ui`, a local web UI with forms generated from `inputs`, and a
  run folder per execution for outputs and logs.
- v0.3: release binaries and install script, better sharing workflows.

## Development

```sh
make test    # go test ./...
make vet
make build
```
