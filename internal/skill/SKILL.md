---
name: maggot
description: Shared local toolbox of tested CLI tools (maggot). Use BEFORE writing any script or utility for a reusable task — converting, resizing, compressing or renaming files, media/image/PDF/audio/video processing, data extraction, downloads, API helpers — to find and run an existing tool. Also use to create a new reusable tool and save it so every future project and agent can reuse it.
---

# maggot

maggot is the user's shared toolbox of standalone CLI tools. Tools built in one
project are reused in every other project and by every agent. Your memory does
not matter: the toolbox is the memory.

## Rule

Before you write code for a utility that could be reused, search maggot. Reuse a
fitting tool. Write a new tool only when nothing fits, and then save it to the
toolbox so it is never written again. One-off code that is specific to the
current project does not belong in maggot.

## Setup check

Run `maggot --version`. If the command is missing, tell the user and stop:
install from https://github.com/nxnom/maggot (`go install github.com/nxnom/maggot/cmd/maggot@latest`).
If `maggot list` says there are no toolboxes, ask the user where the toolbox
folder is (`maggot toolbox add <path>`) or create one (`maggot toolbox init <path>`).

## Use a tool

1. `maggot search <words describing the task> --json` (also `maggot list --json`).
2. `maggot info <tool> --json` — read the manifest, README, inputs and examples.
3. `maggot doctor <tool> --json` — if binaries are missing, show the user the
   install command from the output. Only run `maggot doctor <tool> --install --yes`
   after the user agrees to install software.
4. `maggot run <tool> -- <args>` — everything after `--` goes to the tool
   unchanged (tool flags must come after `--`); the exit code is the tool's. Alternative: `maggot run <tool> --inputs '{"name": value}'`.
   Run from the user's project folder so relative paths work.

If a tool almost fits, prefer improving it (new flag, bump version) over writing
a second similar tool.

## Create a tool

1. Read `maggot guide` once: it is the full manifest, run contract and test format.
2. Pick the simplest kind and scaffold a working template in a scratch folder:
   - `--lang bin --bin <program>`: an installed program already does the job
     (yt-dlp, ffmpeg, magick). No code: only `tool.json` mapping inputs to the
     program's flags. Never write a script just to wrap another CLI.
   - `--lang sh`: small glue around existing programs.
   - `--lang python` (uv inline deps) or `--lang deno` (npm:/jsr:): only for
     real logic or libraries.

   `maggot new <id> --lang <kind> --dir <scratch folder>`. Never depend on
   files in the current project.
3. Write the program (none for bin). Fill `tool.json`: clear `description`,
   `tags` with search synonyms, `requires` with install hints, `inputs` for the
   options people use (plus an `args` input for the rest), `outputs`.
4. Write `tests/cases.json` with real fixtures, including a failure case.
   Run `maggot test <dir>` until every case passes.
5. Ask the user, then `maggot import <dir> --yes`. For an update, bump
   `version` first. Then use it with `maggot run`.

Secrets: declare them in `env` with `"secret": true`; the user stores them with
`maggot secret set <tool> <NAME>`. Never write secrets into tool files.
