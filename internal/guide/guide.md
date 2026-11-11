# maggot tool authoring guide

A maggot tool is a normal CLI program in a folder with a `tool.json` manifest.
maggot only reads the manifest. It never changes how the program works, and the
program must run without maggot.

## Workflow (for agents)

1. Search first: `maggot search <what you need>`. Reuse a tool when one fits.
   If a tool almost fits, improve it (and bump `version`) instead of adding a
   second similar tool.
2. Pick the simplest kind (see "Choosing the kind of tool") and scaffold:
   `maggot new <id> --lang bin|sh|python|deno [--bin <program>] --dir <scratch dir>`.
   The template already works and passes its tests. Change it into your tool.
3. Write the program (none for `bin`), update `tool.json`, `README.md` and
   `tests/cases.json`.
4. Test until it passes: `maggot test <dir>`.
5. Install it: `maggot import <dir>` (the user confirms, or pass `--yes` after
   the user approved). Bump `version` for updates.

## Choosing the kind of tool

Use the simplest kind that does the job:

| kind | when | what you write |
|---|---|---|
| `bin` | an installed program already does it (yt-dlp, ffmpeg, magick, pandoc) | only `tool.json`: `entry.bin`, fixed `entry.args`, and `inputs` mapped to the program's own flags |
| `sh` | small glue: a few commands, fixed flags, a loop over files | a short POSIX shell script |
| `python` | real logic or libraries (Pillow, OpenCV, an API client) | a uv script with inline dependencies |
| `deno` | real logic or libraries in TypeScript | a Deno script with `npm:`/`jsr:` imports |

Do not write a script only to rename another program's flags; that is what a
`bin` tool's `inputs` are for. Do not add a runtime (uv, deno) the task does
not need.

## Folder layout

    <id>/
      tool.json            required
      run.py | run.sh | …  the entry program (any language, any files)
      README.md            optional, shown by `maggot info`
      tests/cases.json     tests for `maggot test`
      tests/fixtures/…     small sample inputs for tests
      .maggotignore        optional, globs to leave out of archives

The folder name must equal the tool `id`. A tool can contain anything it needs
(vendored code, binaries, model files). Keep secrets out of the folder.

## tool.json

```json
{
  "schema": 1,
  "id": "image-resize",
  "version": "1.0.0",
  "name": "Image Resize",
  "description": "Resize images to a width and format.",
  "categories": ["Images"],
  "tags": ["thumbnail", "scale", "webp"],
  "entry": {"path": "run.py", "interpreter": ["uv", "run", "--script"]},
  "requires": [
    {"bin": "magick", "version": ">=7", "check": ["-version"],
     "install": {"brew": "imagemagick", "apt": "imagemagick", "winget": "ImageMagick.ImageMagick"}}
  ],
  "env": [{"name": "REMOVE_BG_KEY", "secret": true, "required": false, "description": "API key"}],
  "inputs": [
    {"name": "width", "type": "integer", "min": 1, "max": 8000, "default": 800, "arg": {"flag": "--width"}},
    {"name": "format", "type": "enum", "options": ["png", "jpeg", "webp"], "arg": {"flag": "--format"}},
    {"name": "overwrite", "type": "boolean", "arg": {"flag": "--overwrite"}},
    {"name": "files", "type": "files", "required": true, "accept": ["image/*"], "arg": {"positional": true}}
  ],
  "outputs": {"kind": "files", "dir_flag": "--out"},
  "examples": ["maggot run image-resize -- --width 400 --out thumbs a.png b.png"]
}
```

Unknown fields are errors. The fields are:

| field | required | notes |
|---|---|---|
| `schema` | yes | always `1` |
| `id` | yes | lowercase kebab-case, max 64 chars, same as the folder name |
| `version` | yes | semver `MAJOR.MINOR.PATCH`; bump it on every change you import |
| `name`, `description` | yes | the description is the most important search text |
| `categories` | no | up to 5, used for grouping in the UI |
| `tags` | no | extra search words: synonyms, formats, library names |
| `entry.path` | one of path/bin | entry file, relative, forward slashes |
| `entry.bin` | one of path/bin | a program on PATH, for manifest-only tools (checked like a requirement) |
| `entry.interpreter` | no | with `path` only: argv prefix, e.g. `["uv","run","--script"]`, `["deno","run","-A"]`, `["sh"]`. Without it, `entry.path` must be executable. Set it even when the file has a shebang so the tool also runs on Windows. |
| `entry.args` | no | fixed args placed before the user's args, e.g. `["--newline", "-o", "%(title)s.%(ext)s"]`. Many programs let a later flag win, so these work as overridable defaults. |
| `requires[]` | no | binaries needed on PATH. `version` is a semver constraint (`>=7`, `^1.2`, `~3.1`). `check` are the args that print the version (default `["--version"]`). `install` maps a package manager to a package name. |
| `env[]` | no | env vars the tool reads. `secret: true` values are stored by `maggot secret set` outside the toolbox and passed at run time. `required: true` blocks `maggot run` when the value is missing. Names starting with `MAGGOT_` are reserved. |
| `inputs[]` | no | describe args so the web UI can build a form and `--inputs` JSON works. Tools without inputs still run with raw args. |
| `outputs` | no | what the tool produces, see below |
| `examples` | no | command lines shown by `maggot info` |

The entry interpreter's first word (for example `uv`) is checked like a
requirement automatically.

Package managers for `install`: brew, port, apt, dnf, yum, pacman, apk,
zypper, nix, winget, scoop, choco, npm, pipx, uv, cargo, go.

### inputs

| type | value | widget |
|---|---|---|
| `string` | string | one-line text |
| `text` | string | multi-line text |
| `integer`, `number` | number, `min`/`max` | number field |
| `boolean` | true/false | checkbox; sent only when true, as `arg.flag` or as the list `arg.args` |
| `enum` | one of `options` (strings or `{"value","label","args"}`) | select |
| `file`, `directory` | path | file / folder picker; `accept` takes MIME types or extensions |
| `files` | list of paths | multi-file picker |
| `strings` | list of strings | one value per line (URLs, names) |
| `args` | string, split like a shell command line | free-form extra args, no `arg` needed |

How a value becomes argv (`arg`):

- `{"flag": "--width"}` → `--width 800`
- `{"positional": true}` → `800`
- `{"flag": "-S", "template": "res:{value}"}` → `-S res:1080`. `template`
  also works with positional inputs.
- boolean `{"args": ["--write-subs", "--embed-subs"]}` → both args when true.
- list inputs (`files`, `strings`): positional, or a flag with
  `"repeat": true` (`--in a --in b`). Only the last positional input may be a list.
- enum options with `args` choose a whole set of args, and the input has no
  `arg`. Either every option has `args` or none does; `"args": []` sends nothing:

      {"name": "mode", "type": "enum", "default": "video", "options": [
        {"value": "video", "label": "Video", "args": []},
        {"value": "mp3", "label": "Audio MP3", "args": ["-x", "--audio-format", "mp3"]}
      ]}

- `args` inputs are split into words (quotes work, no variables or globs) and
  passed as they are. Use one as an "Extra args" escape hatch for programs
  with many options.

maggot builds argv in this order: `entry.args`, flags and option/boolean args
(manifest order), `outputs.dir_flag`, `args` inputs, then positionals. A
missing value uses `default`; when there is no default, nothing is sent.

Optional per input: `label`, `description`, `required`, `default`, and form
hints that do not change argv: `placeholder`, `group` (section title, e.g.
`"Extras"`), `advanced: true` (hidden until the user opens advanced options).
Map the options people really use; leave the rest to an `args` input.

### outputs

`outputs` describes the result. It does not restrict the tool.

| kind | meaning | extra fields |
|---|---|---|
| `files` | writes new files | `dir_flag`: the flag that takes the output folder, e.g. `--out` |
| `stdout` | the result is what it prints | `format`: `text` (default) or `json` |
| `inplace` | changes the input files | |
| `none` | side effects only (send mail, call an API) | |

`progress` (any kind) reads progress from output lines for programs that do
not write `MAGGOT_EVENTS`: `{"regex": "\\[download\\]\\s+([\\d.]+)%", "max": 100}`.
The first regex group is the number; `max` is the value that means done
(default 100).

For `files` tools, always support an output folder flag and declare it in
`dir_flag`. The web UI gives every run its own folder through that flag and
previews the files it finds there. A tool without `dir_flag` can still write
files wherever it wants; it just has no managed output folder.

## Run contract

- Input is argv. maggot runs `<interpreter...> <abs entry path> <entry.args...> <args...>`,
  or `<bin> <entry.args...> <args...>` for bin tools.
- The working directory is the caller's directory, so relative paths work.
- Exit code 0 means success. Print errors to stderr.
- Write results to stdout and/or files, and declare which in `outputs`. When
  the tool writes new files, accept an output folder flag (`outputs.dir_flag`).
- Do not prompt on stdin unless the tool's purpose needs it; agents run tools
  without a terminal.
- maggot sets `MAGGOT_TOOL_DIR`, `MAGGOT_TOOL_ID`, `MAGGOT_TOOL_VERSION`. Use
  `MAGGOT_TOOL_DIR` (or the script's own path) to find files next to the
  entry. Do not rely on these vars for correctness; the tool must also work
  when started directly.

### Events (optional)

When `MAGGOT_EVENTS` is set it is a file path. The tool may append one JSON
object per line. The web UI uses these to show progress and results.

    {"type": "progress", "value": 0.4, "label": "Resizing"}     value 0..1
    {"type": "log", "level": "info", "message": "Read 3 files"} level: info, warn, error
    {"type": "output", "path": "/abs/out/a.webp", "label": "a.webp"}

Ignore the variable when it is not set. Never fail because of it.

## Dependencies

Keep the tool standalone, and add no runtime it does not need:

- Python: `uv` with inline script metadata (`# /// script` block). uv installs
  the packages into a cached env on first run. No venv folder in the tool.
- Deno/TypeScript: import with `npm:` / `jsr:` specifiers. No node_modules.
- Shell: wrap system binaries and list them in `requires`.
- Anything else (Go/Rust binary, Node app with vendored node_modules, Docker)
  is fine; list what must be installed in `requires`.

Pin dependency versions so the tool keeps working.

## tests/cases.json

```json
{
  "cases": [
    {
      "name": "resizes to width",
      "args": ["--width", "10", "--out", "{work}/out", "{tool}/tests/fixtures/a.png"],
      "expect": {"exit": 0, "stdout_contains": ["a.png"], "files": ["out/a.png"]}
    },
    {
      "name": "same through inputs",
      "inputs": {"width": 10, "files": ["{tool}/tests/fixtures/a.png"]},
      "out": "{work}/out",
      "expect": {"files": ["out/a.png"]}
    }
  ]
}
```

- Each case runs in a new empty temp folder, which is also the working directory.
- `{tool}` is the tool folder, `{work}` is the temp folder.
- Use `args` (raw argv) or `inputs` (values turned into argv through the
  manifest, the same path the web UI uses), not both. `out` goes with `inputs`.
- Optional: `stdin`, `env` (object), `timeout_seconds` (default 60).
- `expect.exit` defaults to 0. `expect.files` are relative to `{work}` and must
  be non-empty.
- Test failure cases too (bad input → non-zero exit and a clear stderr message).
- Keep fixtures small. Tests must not need network access when possible.

`maggot test <dir>` validates the manifest, checks requirements, then runs the
cases. It exits 1 when anything fails.
