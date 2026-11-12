package ui

import (
	"errors"
	"net/http"

	"github.com/nxnom/maggot/internal/presets"
)

func (s *Server) apiPresets(w http.ResponseWriter, r *http.Request) error {
	_, t, err := s.resolve(r, "")
	if err != nil {
		return err
	}
	list := presets.List(t.Manifest.ID, t.Dir)
	if list == nil {
		list = []presets.Preset{}
	}
	return ok(w, map[string]any{"presets": list})
}

func (s *Server) apiSavePreset(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Ref         string         `json:"ref"`
		Label       string         `json:"label"`
		Description string         `json:"description"`
		Scope       string         `json:"scope"`
		Values      map[string]any `json:"values"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	_, t, err := s.resolve(r, req.Ref)
	if err != nil {
		return err
	}
	name := presets.Slug(req.Label)
	if name == "" {
		return errors.New("give the preset a name")
	}
	known := map[string]bool{}
	for _, in := range t.Manifest.Inputs {
		known[in.Name] = true
	}
	values := map[string]any{}
	for k, v := range req.Values {
		if known[k] {
			values[k] = v
		}
	}
	warnings, err := presets.Save(t.Manifest.ID, t.Dir, presets.Preset{Name: name, Label: req.Label, Description: req.Description, Scope: req.Scope, Values: values})
	if err != nil {
		return err
	}
	if warnings == nil {
		warnings = []string{}
	}
	return ok(w, map[string]any{"use": req.Scope + "/" + name, "warnings": warnings, "presets": presets.List(t.Manifest.ID, t.Dir)})
}

func (s *Server) apiDeletePreset(w http.ResponseWriter, r *http.Request) error {
	_, t, err := s.resolve(r, "")
	if err != nil {
		return err
	}
	if err := presets.Delete(t.Manifest.ID, t.Dir, r.URL.Query().Get("preset")); err != nil {
		return err
	}
	return ok(w, map[string]any{"presets": presets.List(t.Manifest.ID, t.Dir)})
}
