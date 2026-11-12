---
name: maggot
description: "Shared local toolbox of tested CLI tools (maggot). Use BEFORE writing any script, utility or CLI for a reusable task — converting, resizing, compressing or renaming files, media/image/PDF/audio/video processing, data extraction, downloads, API helpers — and whenever the user asks to build, create or make a tool, CLI, script or utility: search maggot first, then reuse or improve an existing tool. Also use to create a new reusable tool and save it so every future project and agent can reuse it."
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

1. **Find:** `maggot search <what the task does> --json`. With no direct
   match it lists the closest tools; check them before building anything.
   `maggot info <tool> --json` gives a short view: inputs (with their flags,
   defaults, choices), readiness, and which test cases passed or were
   skipped. Use `--full` only if you need the README.
2. **Prepare:** `maggot prepare <tool> --inputs '{"name": value}' --out <folder> --json`
   (or `-- <raw args>`). It checks the inputs and requirements, resolves
   defaults, says where output goes, and returns the exact `next` command.
   If `ready` is false, follow each problem's `fix`. Ask the user before
   installing software (`maggot doctor <tool> --install --yes`) or setting a
   secret. If the tool's last test failed, run `maggot test <tool>` first.
3. **Run:** run the `next` command it printed:
   `maggot run <tool> --json --inputs '...' --out <folder>`. The tool's own
   output streams to stderr; stdout is one JSON result with `ok`,
   `category` (ok, invalid_input, missing_requirement, missing_env,
   tool_failed, cancelled), `files` (absolute paths it created), `stdout`
   (for text results), `stderr_tail` and `next` steps. Run from the user's
   project folder so relative paths work.

Raw args: everything after `--` goes to the tool unchanged; input defaults
apply only with `--inputs` (entry.args are always added).

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

## Where results go

Before running a tool that writes files, decide where they go:

1. The user said where → use it.
2. It is clear from the project or from earlier in this conversation → use
   it, and say where in your reply.
3. Otherwise ask once, with concrete options, for example:
   - save to `<a folder in the project you suggest>`
   - save to the current folder (`<cwd>`)
   - another folder (the user names it)
   - review first in the maggot web UI, then move the approved files
     (best for generated images, video, documents)

   Remember the answer for the rest of the conversation and do not ask again
   for the same kind of output.

Pass the folder with the tool's output flag (`outputs.dir_flag` in
`maggot info`). For the review option:

1. `maggot run <tool> --review -- <args>` (leave out the tool's output flag).
   The files are held in a run folder; maggot prints the run id and a web UI
   link where the user can look at them.
2. Ask the user to check them (give the link, or `maggot ui`).
3. Approved: `maggot take <run-id> <destination folder> [file...]` moves the
   files into place and deletes the run, so no copy is left. Rejected:
   `maggot runs rm <run-id>`. The user can also move or delete them in the UI.

## Slow tools

If `maggot info` shows `warm` and you will run the tool several times soon
(or the user asks), run `maggot warm <tool> --for 15m` first, so it loads
once instead of on every run. Tell the user it keeps memory (often GPU memory)
in use, and run `maggot warm stop <tool>` when you are done. Do not warm a
tool for a single run. When building a tool whose runs are mostly loading a
model, use `maggot new <id> --lang python --warm`.

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
