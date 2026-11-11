// Package doctor checks that the binaries a tool needs are installed in the
// right version, and suggests how to install the ones that are missing.
// It never installs anything by itself.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/nxnom/maggot/internal/manifest"
)

// Status of one requirement.
type Status string

const (
	StatusOK       Status = "ok"
	StatusMissing  Status = "missing"
	StatusOutdated Status = "version_mismatch"
	StatusUnknown  Status = "version_unknown"
)

type Result struct {
	Bin        string   `json:"bin"`
	Status     Status   `json:"status"`
	Path       string   `json:"path,omitempty"`
	Version    string   `json:"version,omitempty"`
	Constraint string   `json:"constraint,omitempty"`
	Error      string   `json:"error,omitempty"`
	Install    *Install `json:"install,omitempty"`
	// Alternatives are install options for package managers not found on this machine.
	Alternatives map[string]string `json:"alternatives,omitempty"`
}

// OK reports whether the requirement is satisfied.
func (r Result) OK() bool { return r.Status == StatusOK }

// Install is a ready-to-run install command (argv, no shell).
type Install struct {
	Manager string   `json:"manager"`
	Package string   `json:"package"`
	Command []string `json:"command"`
}

// String renders the command for humans.
func (i Install) String() string { return strings.Join(i.Command, " ") }

// Checker holds the OS hooks so tests can fake them.
type Checker struct {
	GOOS     string
	LookPath func(string) (string, error)
	// Output runs a command and returns combined stdout+stderr.
	Output  func(ctx context.Context, name string, args ...string) (string, error)
	Timeout time.Duration
}

// New returns a Checker for the real machine.
func New() *Checker {
	return &Checker{
		GOOS:     runtime.GOOS,
		LookPath: exec.LookPath,
		Output: func(ctx context.Context, name string, args ...string) (string, error) {
			out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
			return string(out), err
		},
		Timeout: 10 * time.Second,
	}
}

// Requirements returns the manifest requirements plus an implicit one for the
// program that starts the tool (entry.bin, or the interpreter such as "uv")
// when it is not listed.
func Requirements(m *manifest.Manifest) []manifest.Requirement {
	reqs := append([]manifest.Requirement{}, m.Requires...)
	bin := m.Entry.Bin
	if bin == "" && len(m.Entry.Interpreter) > 0 {
		bin = m.Entry.Interpreter[0]
	}
	if bin == "" || filepath.IsAbs(bin) || strings.ContainsAny(bin, `/\`) {
		return reqs
	}
	for _, r := range reqs {
		if r.Bin == bin {
			return reqs
		}
	}
	return append([]manifest.Requirement{{Bin: bin}}, reqs...)
}

// CheckAll checks every requirement of a manifest.
func (c *Checker) CheckAll(ctx context.Context, m *manifest.Manifest) []Result {
	reqs := Requirements(m)
	out := make([]Result, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, c.Check(ctx, r))
	}
	return out
}

var versionPattern = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// ExtractVersion finds the first version-like string (1.2 or 1.2.3) in s.
func ExtractVersion(s string) (*semver.Version, bool) {
	m := versionPattern.FindString(s)
	if m == "" {
		return nil, false
	}
	v, err := semver.NewVersion(m)
	return v, err == nil
}

// Check checks one requirement.
func (c *Checker) Check(ctx context.Context, r manifest.Requirement) Result {
	res := Result{Bin: r.Bin, Constraint: r.Version}
	path, err := c.LookPath(r.Bin)
	if err != nil {
		res.Status = StatusMissing
		c.addInstall(&res, r)
		return res
	}
	res.Path = path
	if r.Version == "" {
		res.Status = StatusOK
		return res
	}
	constraint, err := semver.NewConstraint(r.Version)
	if err != nil { // already validated by manifest, but stay safe
		res.Status, res.Error = StatusUnknown, err.Error()
		return res
	}
	args := r.Check
	if len(args) == 0 {
		args = []string{"--version"}
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, runErr := c.Output(cctx, path, args...)
	v, ok := ExtractVersion(output)
	if !ok {
		res.Status = StatusUnknown
		res.Error = fmt.Sprintf("could not read a version from `%s %s`", r.Bin, strings.Join(args, " "))
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			res.Error += ": " + runErr.Error()
		}
		return res
	}
	res.Version = v.String()
	if constraint.Check(v) {
		res.Status = StatusOK
		return res
	}
	res.Status = StatusOutdated
	c.addInstall(&res, r)
	return res
}

// managerOrder is the preference order of package managers per OS. Language
// package managers come last so a system package is preferred.
var managerOrder = map[string][]string{
	"darwin":  {"brew", "port", "nix"},
	"linux":   {"apt", "dnf", "yum", "pacman", "apk", "zypper", "nix", "brew"},
	"windows": {"winget", "scoop", "choco"},
}

var languageManagers = []string{"uv", "pipx", "npm", "cargo", "go"}

// managerBinary is the executable that must be on PATH to use a manager.
var managerBinary = map[string]string{"apt": "apt-get"}

func installCommand(manager, pkg string) []string {
	switch manager {
	case "brew":
		return []string{"brew", "install", pkg}
	case "port":
		return []string{"sudo", "port", "install", pkg}
	case "apt":
		return []string{"sudo", "apt-get", "install", "-y", pkg}
	case "dnf", "yum", "zypper":
		return []string{"sudo", manager, "install", "-y", pkg}
	case "pacman":
		return []string{"sudo", "pacman", "-S", "--noconfirm", pkg}
	case "apk":
		return []string{"sudo", "apk", "add", pkg}
	case "nix":
		return []string{"nix", "profile", "install", "nixpkgs#" + pkg}
	case "winget":
		return []string{"winget", "install", "--exact", "--id", pkg}
	case "scoop":
		return []string{"scoop", "install", pkg}
	case "choco":
		return []string{"choco", "install", "-y", pkg}
	case "npm":
		return []string{"npm", "install", "-g", pkg}
	case "pipx":
		return []string{"pipx", "install", pkg}
	case "uv":
		return []string{"uv", "tool", "install", pkg}
	case "cargo":
		return []string{"cargo", "install", pkg}
	case "go":
		return []string{"go", "install", pkg}
	}
	return nil
}

func (c *Checker) addInstall(res *Result, r manifest.Requirement) {
	if len(r.Install) == 0 {
		return
	}
	order := append(append([]string{}, managerOrder[c.GOOS]...), languageManagers...)
	for _, m := range order {
		pkg, ok := r.Install[m]
		if !ok {
			continue
		}
		bin := managerBinary[m]
		if bin == "" {
			bin = m
		}
		if _, err := c.LookPath(bin); err != nil {
			continue
		}
		cmd := installCommand(m, pkg)
		if cmd == nil {
			continue
		}
		if cmd[0] == "sudo" {
			if _, err := c.LookPath("sudo"); err != nil {
				cmd = cmd[1:] // no sudo (for example root in a container)
			}
		}
		res.Install = &Install{Manager: m, Package: pkg, Command: cmd}
		break
	}
	for m, pkg := range r.Install {
		if res.Install != nil && m == res.Install.Manager {
			continue
		}
		if res.Alternatives == nil {
			res.Alternatives = map[string]string{}
		}
		res.Alternatives[m] = strings.Join(installCommand(m, pkg), " ")
	}
}
