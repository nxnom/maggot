---
name: maggot
description: "Shared local toolbox of tested CLI tools (maggot). Use BEFORE writing any script, utility or CLI for a reusable task — converting, resizing, compressing or renaming files, media/image/PDF/audio/video processing, data extraction, downloads, API helpers — and whenever the user asks to build, create or make a tool, CLI, script or utility: search maggot first, then reuse or improve an existing tool. Also use to create a new reusable tool and save it so every future project and agent can reuse it. maggot also keeps the user's skills: step-by-step workflows (SKILL.md) that combine tools, scripts, programs and APIs — check for one before a multi-step task, and save a workflow the user wants to repeat as a skill."
---

# maggot

maggot is the user's shared toolbox of standalone CLI tools. Tools built in one
project are reused in every other project and by every agent. Your memory does
not matter: the toolbox is the memory.

A toolbox holds two kinds of things:
- **Tools** (`tools/<id>/tool.json`): one program with inputs, tests and a
  form in the web UI. maggot runs them (`maggot run`).
- **Skills** (`skills/<name>/SKILL.md`, the Agent Skills format): steps for
  you to follow, often using several maggot tools plus the skill's own
  scripts, other programs or APIs. You follow them; maggot does not run them.

## Rule

Before you write code for a utility that could be reused, or start a task with
several steps (make a video, publish a release, process a batch), search
maggot: `maggot search` returns matching tools and skills. This
includes requests like "build a tool for X" or "make a CLI that does Y": a tool
for X may already exist. Reuse a
fitting tool. Write a new tool only when nothing fits, and only after the user
approved your plan for it (see "Create a tool"); then save it to the toolbox
so it is never written again. One-off code that is specific to the
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
   `ready` is true only when nothing must be fixed or decided:
   - `problems`: follow each `fix`. Ask the user before installing software
     (`maggot doctor <tool> --install --yes`) or setting a secret.
   - `decisions` (e.g. `output_folder`): decide from the task, or ask the
     user (see "Where results go"), then prepare again. Never run a `next`
     command that still contains `<choose a folder>`.
   If the tool's last test failed, run `maggot test <tool>` first.
3. **Run:** run the `next` command it printed:
   `maggot run <tool> --json --inputs '...' --out <folder>`. The tool's own
   output streams to stderr; stdout is one JSON result with `ok`,
   `category` (ok, invalid_input, missing_requirement, missing_env,
   tool_failed, cancelled), `files` (absolute paths it created), `stdout`
   (for text results), `stderr_tail` and `next` steps. Run from the user's
   project folder so relative paths work.

Presets: `info` lists the user's saved presets (`presets`, each with a `use`
value). When the user names one ("my narrator voice") or one clearly fits,
add `--preset <use>` to prepare and run; `--inputs` then only needs what
differs (like the text). Save a preset only when the user asks:
`maggot presets save <tool> "<name>" --inputs '{...}'` (personal), or with
`--toolbox` to share it through the toolbox.

Raw args: everything after `--` goes to the tool unchanged; input defaults
apply only with `--inputs` (entry.args are always added).

If a tool almost fits, improve it instead of writing a second similar tool.
Propose the change first and wait for approval (step 1 below). Then
`maggot draft <tool>` copies it into the drafts folder; change the copy, bump
`version`, and continue with steps 4–6 below. Keep the same `id`: that is
how maggot connects the draft to the installed tool. In the web UI the user
then sees "New version of <tool>" and a Changes tab with a diff against the
installed version; point them to it when you hand over.

## Use a skill

1. `maggot search <the task> --json` lists matching skills under `skills`.
2. `maggot skill show <name>` prints the folder, a readiness check (the
   maggot tools it uses, programs on PATH, secrets) and the SKILL.md steps.
   Fix what is missing first: a missing tool means search for it or propose
   building it; ask the user before installing programs or setting secrets
   (`maggot secret set --skill <name> <NAME>`).
3. Follow the steps. Run maggot tools as in "Use a tool". Run the skill's
   own scripts from its folder by absolute path (`<folder>/scripts/x.py`);
   never copy them into the project. When a script needs the skill's
   secrets, run it as `maggot skill exec <name> -- <command>`.
4. If a step did not work or the user corrected you, propose a fix to the
   skill (see "Create or change a skill").

## Create a tool

1. **Propose first, then wait.** Do not scaffold or write any file yet. Search
   first (see above), then reply with a short plan and stop until the user
   approves it:
   - what the tool does, in one or two sentences, and its id and name
   - existing tools you found and why they don't fit (or that you will
     improve one instead)
   - the kind (bin, sh, python, deno) and why; required programs and versions
   - the inputs (options in the form) with defaults and choices
   - the outputs: what files or text it produces, and where
   - how it will be tested (offline fixtures; what is marked network or paid)
   - the decisions below that apply to this tool, each with your
     recommendation and the reason, so the user only has to say yes or
     change one
   - open questions, each with your suggested answer

   Decisions to cover when they apply (skip the ones that don't):
   - **Speed and memory:** how long one run takes, and how long loading takes
     (models, big libraries). If loading is slow: should it support warm
     (`--warm`), keep it warm by default (`warm.default_on`), and for how many
     minutes (`warm.minutes`)? Say how much memory or GPU memory it holds
     while warm, and recommend minutes from how the user will use it (one
     run now and then: no warm; batches: 10–15 min).
   - **Cost and limits:** paid APIs, credits, rate limits, size or length
     limits, and what a typical run costs.
   - **Secrets:** which API keys or tokens (`env` with `"secret": true`) and
     where the user gets them.
   - **Requirements:** programs and version ranges, how to install them
     (brew, apt, uv, npm), big downloads (models) and where they are stored.
   - **Defaults:** quality, format, size, language, voice, and which options
     stay in the main form and which go under advanced.
   - **Input shapes:** single file or many files or a folder, text or a text
     file (`one_of`), overwrite in place or write new files.
   - **Output:** file names and formats, the output folder flag, progress
     reporting for long runs.
   - **Presets:** settings the user will reuse (a voice, a style) that should
     be saved as presets, personal or shared in the toolbox.
   - **Platform:** macOS only, or Linux and Windows too; GPU (CUDA, Apple MPS)
     or CPU.

   Change the plan from the user's feedback and ask again if it changed a lot.
   Build only after a clear yes ("ok", "go ahead", "build it"). Skip the
   proposal only when the user explicitly said to build without asking, or
   already gave a full spec and said to build it.
2. Read `maggot guide` once: it is the full manifest, run contract and test format.
3. Pick the simplest kind and scaffold a working template:
   - `--lang bin --bin <program>`: an installed program already does the job
     (yt-dlp, ffmpeg, magick). No code: only `tool.json` mapping inputs to the
     program's flags. Never write a script just to wrap another CLI.
   - `--lang sh`: small glue around existing programs.
   - `--lang python` (uv inline deps) or `--lang deno` (npm:/jsr:): only for
     real logic or libraries.

   `maggot new <id> --lang <kind>` creates it in the maggot drafts folder (it
   prints the path). Do not use a temporary or scratchpad folder: the user
   tries the tool later. Never depend on files in the current project.
4. Write the program (none for bin). Fill `tool.json`: clear `description`,
   `tags` with search synonyms, `requires` with install hints, `inputs` for the
   options people use (plus an `args` input for the rest), `outputs`. Give
   every `file`/`files` input an `accept` list (`[".json", ".txt"]`,
   `["audio/*"]`): the web UI file picker then shows only those files.
   For every input that takes a structured file (JSON, JSON lines, CSV), put
   one short valid example, the keys (required or not), the path rule (relative
   to the file or to the working directory) and any override rule in the input
   `description`. `maggot info` is all an agent reads, so it must be enough
   without the README.
   If the tool writes files into its own folder (a model cache, downloads, a
   build or render folder), list them in a `.gitignore` inside the tool folder.
   Common junk (`node_modules/`, `__pycache__/`, `.venv/`, `.DS_Store`) is
   already ignored by the toolbox root; do not repeat it. Assets the tool ships
   stay in git, so keep them small: compress them (e.g. sounds as `.ogg`) and
   keep test fixtures small.
5. Write `tests/cases.json` with small local fixtures, including a failure
   case, and `expect.checks` that verify the output content (e.g. ffprobe).
   Tests must be free and offline: mark cases that need the internet with
   `"network": true` and cases that cost money (paid APIs, image/video
   generation, LLM calls) with `"paid": true`; both are skipped unless the user
   asks. Run `maggot test <dir>` until every case passes.
6. Hand it to the user. Do not import it yourself. Tell them the folder path
   and the command to try it in the web UI:

       maggot try <dir>

   They run it there and import it with one click when they are happy. Only
   if the user tells you to import it, run `maggot import <dir> --yes`.

## Create or change a skill

Save a workflow as a skill when the user wants to repeat it ("do this again
next time", "save this as a skill", "remember how we made this").

1. **Propose first, then wait**, like a tool. The plan:
   - name (lowercase, hyphens) and a description that says what it does
     and when to use it ("Use when ...")
   - the steps, in short
   - for each step, where the work happens: an existing maggot tool; a new
     maggot tool (when the step is useful on its own: it then gets a form,
     tests and search, and you propose it separately); a script inside the
     skill (only useful inside this workflow); or a program or API called
     directly
   - programs, secrets and paid services it needs, and rough cost or time
   - the rules learned so far (what works, what to avoid)
2. After a clear yes: `maggot skill new <name> --description "..."` creates
   `SKILL.md` in the drafts folder (it prints the path). Fill in:
   - `metadata`: `version`, `maggot-uses` (tool ids), `maggot-requires`
     (programs), `maggot-env` (secret names), each a space-separated string
   - the body: steps with exact commands, then the rules. Keep it under
     ~500 lines; put long docs in `references/`, code in `scripts/`,
     templates and images in `assets/`. Paths are relative to the folder.
3. Run every script once on a small example. Check with
   `maggot skill show <folder>` until it is ready (or only secrets the user
   must set are missing).
4. Hand it over: tell the user the folder and that they can read it in the
   web UI (`maggot ui`, under Drafts) and import it there. Import it yourself
   (`maggot import <folder> --yes`) only when the user says so. Installing is
   optional: agents find skills with `maggot search` anyway. Offer
   `maggot skill install <name>` (links it into Claude Code and Codex so they
   load it on their own) only for skills the user runs often; each installed
   skill adds to every session's context. Ask first.

To change a skill: propose the change, then `maggot skill draft <name>`,
edit the copy, bump `metadata.version`, and hand it over the same way. The
user sees a Changes tab with a diff in the web UI.

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

Secrets: declare them in `env` with `"secret": true` (tools) or in
`metadata.maggot-env` (skills); the user stores them with
`maggot secret set <tool> <NAME>` or `maggot secret set --skill <skill> <NAME>`.
Never write secrets into tool or skill files.
