---
name: maggot
description: Shared local toolbox of tested CLI tools (maggot). Use BEFORE writing any script, utility or CLI for a reusable task — converting, resizing, compressing or renaming files, media/image/PDF/audio/video processing, data extraction, downloads, API helpers — and whenever the user asks to build, create or make a tool, CLI, script or utility: search maggot first, then reuse or improve an existing tool. Also use to create a new reusable tool and save it so every future project and agent can reuse it.
---

# maggot

maggot is the user's shared toolbox of standalone CLI tools. Tools built in one
project are reused in every other project and by every agent. Your memory does
not matter: the toolbox is the memory.

## Rule

Before you write code for a utility that could be reused, search maggot. This
includes requests like "build a tool for X" or "make a CLI that does Y": a tool
for X may already exist. Reuse a
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
   `last_test` shows whether the tool still works. If it failed, or only an
   older version was tested, run `maggot test <tool>` first and fix the tool
   if it is broken.
3. `maggot doctor <tool> --json` — if binaries are missing, show the user the
   install command from the output. Only run `maggot doctor <tool> --install --yes`
   after the user agrees to install software.
4. `maggot run <tool> -- <args>` — everything after `--` goes to the tool
   unchanged (tool flags must come after `--`); the exit code is the tool's. Alternative: `maggot run <tool> --inputs '{"name": value}'`.
   Run from the user's project folder so relative paths work. Input defaults
   from `maggot info` apply only with `--inputs`; with raw args, pass what
   you need yourself (entry.args are always added).

If a tool almost fits, improve it instead of writing a second similar tool:
`maggot draft <tool>` copies it into the drafts folder; change the copy, bump
`version`, and continue with steps 3–5 below.

## Create a tool

1. Read `maggot guide` once: it is the full manifest, run contract and test format.
2. Pick the simplest kind and scaffold a working template:
   - `--lang bin --bin <program>`: an installed program already does the job
     (yt-dlp, ffmpeg, magick). No code: only `tool.json` mapping inputs to the
     program's flags. Never write a script just to wrap another CLI.
   - `--lang sh`: small glue around existing programs.
   - `--lang python` (uv inline deps) or `--lang deno` (npm:/jsr:): only for
     real logic or libraries.

   `maggot new <id> --lang <kind>` creates it in the maggot drafts folder (it
   prints the path). Do not use a temporary or scratchpad folder: the user
   tries the tool later. Never depend on files in the current project.
3. Write the program (none for bin). Fill `tool.json`: clear `description`,
   `tags` with search synonyms, `requires` with install hints, `inputs` for the
   options people use (plus an `args` input for the rest), `outputs`.
4. Write `tests/cases.json` with small local fixtures, including a failure
   case, and `expect.checks` that verify the output content (e.g. ffprobe).
   Tests must be free and offline: mark cases that need the internet with
   `"network": true` and cases that cost money (paid APIs, image/video
   generation, LLM calls) with `"paid": true`; both are skipped unless the user
   asks. Run `maggot test <dir>` until every case passes.
5. Hand it to the user. Do not import it yourself. Tell them the folder path
   and the command to try it in the web UI:

       maggot try <dir>

   They run it there and import it with one click when they are happy. Only
   if the user tells you to import it, run `maggot import <dir> --yes`.

## Leave no copies behind

- Run tools with their output flag pointing straight at where the result is
  needed (for example the project's assets folder). Do not write to a temp
  folder and then copy; if you had to, delete the temp folder afterwards.
- Importing a draft from the drafts folder deletes the draft automatically.
  If the user does not want a draft, ask, then `maggot clean drafts <id> --yes`.
- `maggot clean` shows leftovers (web UI runs, drafts, uploads); delete them
  only when the user agrees.

Secrets: declare them in `env` with `"secret": true`; the user stores them with
`maggot secret set <tool> <NAME>`. Never write secrets into tool files.
