# maggot

Go CLI (one binary, no server, no MCP) that keeps a shared toolbox of CLI
tools and Agent Skills for AI coding agents. It also has a local web UI.
See `README.md` for what it does and how users install it.

## Commands

- `make build` builds `bin/maggot`.
- `make install` builds with the git version and installs to `$(go env GOPATH)/bin`.
  Run it after changes, because agents call the installed `maggot`.
- `make test` runs `go test ./...`. `make vet` runs `go vet`.

## Layout

- `cmd/maggot`: entry point. `internal/cli`: all commands (cobra).
- `internal/manifest`: `tool.json` parsing and validation.
- `internal/runner`, `internal/warm`: run tools, warm (kept-alive) Python tools.
- `internal/testkit`, `internal/testlog`: `maggot test` and its results.
- `internal/scaffold`: `maggot new` templates (`templates/` is embedded).
- `internal/ui`: web UI server. `internal/ui/web` is plain JS/CSS, embedded.
- `internal/guide/guide.md`: the full authoring guide (`maggot guide`). Embedded.
- `internal/skill/SKILL.md`: the agent skill text. Embedded.

## Rules to keep

- **Two copies of the skill.** `internal/skill/SKILL.md` is the source.
  `skills/maggot/SKILL.md` must be an exact copy. After you edit the source,
  run `cp internal/skill/SKILL.md skills/maggot/SKILL.md`.
  `TestRepoCopyMatchesEmbedded` fails if they differ.
- **Skill frontmatter** is parsed with a strict YAML parser. Quote the
  `description` if it has `: `.
- **Guide and skill are embedded.** After you change `guide.md` or `SKILL.md`,
  run `make install`, then `maggot skill install` to update installed agents.
- **Tool authoring rules live in the guide, not here.** When you change how
  tools are made (manifest fields, inputs, tests), update `guide.md` and
  `SKILL.md` step 4 so they stay the one source of truth.
- `tools/` in this repo is empty. `examples/toolbox` is only a sample. Real
  tools live in the user's toolbox (`maggot toolbox list`).
