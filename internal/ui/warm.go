package ui

import (
	"errors"
	"net/http"
	"time"

	"github.com/nxnom/maggot/internal/warm"
	"github.com/nxnom/maggot/internal/workspace"
)

// warmInfo is the warm state of a tool for the tool page.
func warmInfo(id string, supported bool) map[string]any {
	info := map[string]any{"supported": supported}
	if st, _ := warm.Get(id); st != nil {
		info["status"] = st
	}
	return info
}

func (s *Server) apiWarmStatus(w http.ResponseWriter, r *http.Request) error {
	_, t, err := s.resolve(r, "")
	if err != nil {
		return err
	}
	return ok(w, warmInfo(t.Manifest.ID, t.Manifest.Warm != nil))
}

// apiWarmStart loads the tool in the background and returns when it is ready.
func (s *Server) apiWarmStart(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Ref string `json:"ref"`
		For string `json:"for"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	_, t, err := s.resolve(r, req.Ref)
	if err != nil {
		return err
	}
	idle := warm.DefaultIdle
	if req.For != "" {
		if idle, err = time.ParseDuration(req.For); err != nil || idle < time.Minute {
			return errors.New("choose at least 1 minute")
		}
	}
	if missing := workspace.Missing(t, s.store, s.checker.LookPath); len(missing) > 0 {
		return errors.New("install what the tool needs first")
	}
	env, err := workspace.SecretEnv(s.store, t)
	if err != nil {
		return err
	}
	if _, err := warm.Start(t, idle, 5*time.Minute, env); err != nil {
		return err
	}
	return ok(w, warmInfo(t.Manifest.ID, true))
}

func (s *Server) apiWarmStop(w http.ResponseWriter, r *http.Request) error {
	_, t, err := s.resolve(r, "")
	if err != nil {
		return err
	}
	if err := warm.Stop(t.Manifest.ID); err != nil {
		return err
	}
	return ok(w, warmInfo(t.Manifest.ID, t.Manifest.Warm != nil))
}
