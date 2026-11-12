package ui

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nxnom/maggot/internal/agentskill"
	"github.com/nxnom/maggot/internal/config"
	"github.com/nxnom/maggot/internal/pack"
	"github.com/nxnom/maggot/internal/skill"
	"github.com/nxnom/maggot/internal/toolbox"
	"github.com/nxnom/maggot/internal/workspace"
)

// Skills are folders in the Agent Skills format kept in <toolbox>/skills.
// The UI shows them read-only: the steps, the files, whether everything they
// need is there, and which agents have them installed. Skill drafts live in
// <drafts>/skills and have refs "draft/<name>", like tool drafts.

type skillRow struct {
	Ref         string    `json:"ref"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     string    `json:"version"`
	Toolbox     string    `json:"toolbox"`
	Ready       bool      `json:"ready"`
	Missing     []string  `json:"missing"`
	Draft       *toolLink `json:"draft,omitempty"`
}

type skillDraft struct {
	Ref         string    `json:"ref"`
	Kind        string    `json:"kind"` // always "draft"
	Path        string    `json:"path"`
	Name        string    `json:"name"`
	Version     string    `json:"version,omitempty"`
	Description string    `json:"description,omitempty"`
	Error       string    `json:"error,omitempty"`
	Of          *toolLink `json:"of,omitempty"`

	skill *toolbox.Skill
}

// isSkillReq reports whether a request is about a skill (kind=skill).
func isSkillReq(r *http.Request) bool { return r.URL.Query().Get("kind") == "skill" }

// skillDrafts lists the skill drafts, loading each SKILL.md fresh.
func (s *Server) skillDrafts() []skillDraft {
	out := []skillDraft{}
	dir, err := config.SkillDraftsDir()
	if err != nil {
		return out
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || !agentskill.Exists(p) {
			continue
		}
		d := skillDraft{Kind: draftHandle, Path: p, Name: e.Name()}
		if sk, err := agentskill.LoadNamed(p); err != nil {
			d.Error = err.Error()
		} else {
			d.Name, d.Version, d.Description = sk.Meta.Name, sk.Version, sk.Meta.Description
			d.skill = &toolbox.Skill{Skill: sk, Toolbox: &toolbox.Toolbox{Handle: draftHandle, Path: dir}}
		}
		d.Ref = draftHandle + "/" + d.Name
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

// installedSkill returns the toolbox skill a draft would replace. With the
// same name in several toolboxes, the default toolbox wins.
func installedSkill(name string, skills []*toolbox.Skill, defaultBox string) *toolbox.Skill {
	var found *toolbox.Skill
	for _, sk := range skills {
		if sk.Meta.Name == name && (found == nil || sk.Toolbox.Handle == defaultBox) {
			found = sk
		}
	}
	return found
}

func linkToSkill(sk *toolbox.Skill) *toolLink {
	return &toolLink{Ref: sk.Ref(), Name: sk.Meta.Name, Version: sk.Version, Toolbox: sk.Toolbox.Handle}
}

// resolveSkill finds a toolbox skill or a skill draft by ref.
func (s *Server) resolveSkill(ref string) (*workspace.Workspace, *toolbox.Skill, *skillDraft, error) {
	ws, err := s.workspace()
	if err != nil {
		return nil, nil, nil, err
	}
	if strings.HasPrefix(ref, draftHandle+"/") {
		for _, d := range s.skillDrafts() {
			if d.Ref != ref {
				continue
			}
			if d.skill == nil {
				return nil, nil, nil, &httpError{code: http.StatusUnprocessableEntity, msg: d.Error}
			}
			return ws, d.skill, &d, nil
		}
		return nil, nil, nil, &httpError{code: http.StatusNotFound, msg: "no skill draft " + ref}
	}
	sk, err := ws.Set.ResolveSkill(ref)
	if err != nil {
		return nil, nil, nil, &httpError{code: http.StatusNotFound, msg: err.Error()}
	}
	return ws, sk, nil, nil
}

// skillState is what /api/state shows about skills.
func (s *Server) skillState(ws *workspace.Workspace) ([]skillRow, []skillDraft) {
	skills, _ := ws.Set.Skills()
	drafts := s.skillDrafts()
	for i := range drafts {
		if drafts[i].skill != nil {
			if sk := installedSkill(drafts[i].Name, skills, ws.Config.Default); sk != nil {
				drafts[i].Of = linkToSkill(sk)
			}
		}
	}
	rows := []skillRow{}
	for _, sk := range skills {
		c := workspace.CheckSkill(ws.Set, sk, s.store, s.checker.LookPath)
		row := skillRow{Ref: sk.Ref(), Name: sk.Meta.Name, Description: sk.Meta.Description, Version: sk.Version,
			Toolbox: sk.Toolbox.Handle, Ready: c.Ready, Missing: c.Missing}
		for _, d := range drafts {
			if d.Of != nil && d.Of.Ref == sk.Ref() {
				row.Draft = &toolLink{Ref: d.Ref, Name: d.Name, Version: d.Version}
			}
		}
		rows = append(rows, row)
	}
	return rows, drafts
}

type agentRow struct {
	Agent string `json:"agent"`
	State string `json:"state"`
	Path  string `json:"path"`
}

func agentRows(sk *toolbox.Skill) []agentRow {
	out := []agentRow{}
	home, err := os.UserHomeDir()
	if err != nil {
		return out
	}
	for _, ag := range skill.AgentsInOrder() {
		if state, p, err := skill.State(home, ag, sk.Meta.Name, sk.Dir); err == nil {
			out = append(out, agentRow{Agent: ag, State: state, Path: p})
		}
	}
	return out
}

// apiSkill returns one skill for its page.
func (s *Server) apiSkill(w http.ResponseWriter, r *http.Request) error {
	ws, sk, draft, err := s.resolveSkill(r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	skills, _ := ws.Set.Skills()
	var draftInfo map[string]any
	var hasDraft *toolLink
	if draft != nil {
		draftInfo = map[string]any{"kind": draftHandle, "path": draft.Path}
		if it := installedSkill(sk.Meta.Name, skills, ws.Config.Default); it != nil {
			draftInfo["of"] = linkToSkill(it)
		}
	} else {
		for _, d := range s.skillDrafts() {
			if d.skill != nil && d.Name == sk.Meta.Name {
				hasDraft = &toolLink{Ref: d.Ref, Name: d.Name, Version: d.Version}
			}
		}
	}
	var agents []agentRow
	if draft == nil {
		agents = agentRows(sk)
	}
	return ok(w, map[string]any{
		"kind": "skill", "ref": sk.Ref(), "toolbox": sk.Toolbox.Handle, "dir": sk.Dir, "meta": sk.Meta,
		"name": sk.Meta.Name, "version": sk.Version, "description": sk.Meta.Description, "body": sk.Body,
		"uses": sk.Uses, "requires": sk.Requires, "env": sk.Env,
		"check":  workspace.CheckSkill(ws.Set, sk, s.store, s.checker.LookPath),
		"agents": agents, "draft": draftInfo, "has_draft": hasDraft,
	})
}

// apiSkillAgents installs a skill into an agent's skills folder (a link),
// or removes it.
func (s *Server) apiSkillAgents(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Ref     string `json:"ref"`
		Agent   string `json:"agent"`
		Install bool   `json:"install"`
		Force   bool   `json:"force"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	_, sk, draft, err := s.resolveSkill(req.Ref)
	if err != nil {
		return err
	}
	if draft != nil {
		return errors.New("import the draft first; agents get the toolbox skill")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if req.Install {
		_, _, err = skill.Link(home, req.Agent, sk.Meta.Name, sk.Dir, false, req.Force, pack.CopyTool)
	} else {
		_, _, err = skill.Unlink(home, req.Agent, sk.Meta.Name, sk.Dir, req.Force)
	}
	if err != nil {
		return err
	}
	return ok(w, map[string]any{"agents": agentRows(sk)})
}

func (s *Server) skillSecret(ref, name string) (*toolbox.Skill, error) {
	_, sk, _, err := s.resolveSkill(ref)
	if err != nil {
		return nil, err
	}
	for _, e := range sk.Env {
		if e == name {
			return sk, nil
		}
	}
	return nil, fmt.Errorf("skill %s has no env %s", sk.Meta.Name, name)
}

func (s *Server) apiSetSkillSecret(w http.ResponseWriter, r *http.Request) error {
	var req struct{ Ref, Name, Value string }
	if err := readJSON(r, &req); err != nil {
		return err
	}
	sk, err := s.skillSecret(req.Ref, req.Name)
	if err != nil {
		return err
	}
	if req.Value == "" {
		return errors.New("enter a value")
	}
	if err := s.store.Set(workspace.SkillSecretKey(sk.Meta.Name), req.Name, req.Value); err != nil {
		return err
	}
	return ok(w, map[string]any{"env": workspace.SkillEnv(s.store, sk)})
}

func (s *Server) apiDeleteSkillSecret(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	sk, err := s.skillSecret(q.Get("ref"), q.Get("name"))
	if err != nil {
		return err
	}
	s.store.Delete(workspace.SkillSecretKey(sk.Meta.Name), q.Get("name"))
	return ok(w, map[string]any{"env": workspace.SkillEnv(s.store, sk)})
}

// folderFor resolves the folder a files, export or open request is about:
// a tool by default, a skill with kind=skill. first is the file to list
// first (tool.json or SKILL.md), entry the tool's entry file.
func (s *Server) folderFor(r *http.Request, ref string, isSkill bool) (dir, first, entry, label string, err error) {
	if isSkill {
		if ref == "" {
			ref = r.URL.Query().Get("ref")
		}
		_, sk, _, err := s.resolveSkill(ref)
		if err != nil {
			return "", "", "", "", err
		}
		return sk.Dir, agentskill.FileName, "", sk.Ref(), nil
	}
	_, t, err := s.resolve(r, ref)
	if err != nil {
		return "", "", "", "", err
	}
	return t.Dir, "tool.json", t.Manifest.Entry.Path, t.Ref(), nil
}

// skillDraftAndInstalled is draftAndInstalled for skills: the draft folder
// and the toolbox skill it changes.
func (s *Server) skillDraftAndInstalled(ref string) (string, *toolLink, string, error) {
	ws, sk, draft, err := s.resolveSkill(ref)
	if err != nil {
		return "", nil, "", err
	}
	if draft == nil {
		return "", nil, "", errors.New("not a draft: " + ref)
	}
	skills, _ := ws.Set.Skills()
	it := installedSkill(sk.Meta.Name, skills, ws.Config.Default)
	if it == nil {
		return "", nil, "", &httpError{code: http.StatusNotFound, msg: "no toolbox skill named " + sk.Meta.Name}
	}
	return draft.Path, linkToSkill(it), it.Dir, nil
}
