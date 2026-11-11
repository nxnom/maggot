package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/nxnom/maggot/internal/config"
)

// DefaultPort is the preferred UI port. 6244 spells "magg" on a phone
// keypad, and 26244 is below the ports the OS hands out for outgoing
// connections, so it is rarely taken.
const DefaultPort = 26244

// instanceFile records the running UI so `maggot try` can reuse it.
type instanceFile struct {
	Port int `json:"port"`
	PID  int `json:"pid"`
}

func instancePath() (string, error) {
	d, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "ui.json"), nil
}

// MarkRunning records this process as the running UI on port.
func MarkRunning(port int) error {
	p, err := instancePath()
	if err != nil {
		return err
	}
	data, _ := json.Marshal(instanceFile{Port: port, PID: os.Getpid()})
	os.MkdirAll(filepath.Dir(p), 0o755)
	return config.WriteFileAtomic(p, data, 0o644)
}

// MarkStopped removes the record if it belongs to this process.
func MarkStopped() {
	p, err := instancePath()
	if err != nil {
		return
	}
	var in instanceFile
	if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &in) == nil && in.PID == os.Getpid() {
		os.Remove(p)
	}
}

// Running returns the base URL and token of a UI that answers, if any.
func Running() (base, token string, ok bool) {
	p, err := instancePath()
	if err != nil {
		return "", "", false
	}
	var in instanceFile
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &in) != nil || in.Port == 0 {
		return "", "", false
	}
	d, _ := config.DataDir()
	tb, err := os.ReadFile(filepath.Join(d, "ui-token"))
	if err != nil {
		return "", "", false
	}
	token = string(bytes.TrimSpace(tb))
	base = fmt.Sprintf("http://127.0.0.1:%d", in.Port)
	req, _ := http.NewRequest("GET", base+"/api/state", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	resp, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		return "", "", false
	}
	resp.Body.Close()
	return base, token, resp.StatusCode == http.StatusOK
}

// TryRemote asks a running UI to add a folder to try and returns its ref.
func TryRemote(base, token, path string) (string, error) {
	body, _ := json.Marshal(map[string]string{"path": path})
	req, _ := http.NewRequest("POST", base+"/api/drafts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var res struct{ Ref, Error string }
	json.NewDecoder(resp.Body).Decode(&res)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s", res.Error)
	}
	return res.Ref, nil
}

// ToolURL is the link that signs in and opens a tool page.
func ToolURL(base, token, ref string) string {
	u := base + "/?token=" + token
	if ref != "" {
		u += "#/tool/" + url.PathEscape(ref)
	}
	return u
}
