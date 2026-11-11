// Package runner builds the command line for a tool and starts it.
//
// maggot run is only a convenience wrapper: the command it builds is exactly
// what a user could type by hand (interpreter + entry + args).
package runner

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/nxnom/maggot/internal/manifest"
)

// Env vars maggot sets for the tool. Tools may ignore all of them.
const (
	EnvToolDir     = "MAGGOT_TOOL_DIR"
	EnvToolID      = "MAGGOT_TOOL_ID"
	EnvToolVersion = "MAGGOT_TOOL_VERSION"
	// EnvEvents is a file path. When set, a tool may append NDJSON events
	// (progress, log, output) to it. See `maggot guide`.
	EnvEvents = "MAGGOT_EVENTS"
)

// BuildArgs turns input values (as decoded from JSON) into argv using each
// input's arg mapping. Missing values use the manifest default. Order: flags
// in manifest order, then the output dir flag, then positionals.
func BuildArgs(m *manifest.Manifest, values map[string]any, outDir string) ([]string, error) {
	known := map[string]bool{}
	for _, in := range m.Inputs {
		known[in.Name] = true
	}
	var unknown []string
	for k := range values {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown input(s): %s", strings.Join(unknown, ", "))
	}

	var flags, positionals []string
	var problems []string
	for _, in := range m.Inputs {
		val, ok := values[in.Name]
		if !ok || val == nil {
			val, ok = in.Default, in.Default != nil
		}
		if s, isStr := val.(string); ok && isStr && s == "" && in.Type != manifest.TypeString && in.Type != manifest.TypeText {
			ok = false // empty form field
		}
		if list, isList := val.([]any); ok && isList && len(list) == 0 {
			ok = false
		}
		if !ok {
			if in.Required {
				problems = append(problems, in.Name+": is required")
			}
			continue
		}
		if err := in.CheckValue(val); err != nil {
			problems = append(problems, in.Name+": "+err.Error())
			continue
		}
		strs := render(in, val)
		switch {
		case in.Type == manifest.TypeBoolean:
			if val.(bool) {
				flags = append(flags, in.Arg.Flag)
			}
		case in.Arg.Positional:
			positionals = append(positionals, strs...)
		case in.Arg.Repeat:
			for _, s := range strs {
				flags = append(flags, in.Arg.Flag, s)
			}
		default:
			flags = append(flags, in.Arg.Flag, strs[0])
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid inputs:\n  - %s", strings.Join(problems, "\n  - "))
	}
	if outDir != "" {
		if m.Outputs == nil || m.Outputs.DirFlag == "" {
			return nil, fmt.Errorf("%s has no outputs.dir_flag, so an output dir cannot be passed", m.ID)
		}
		flags = append(flags, m.Outputs.DirFlag, outDir)
	}
	return append(flags, positionals...), nil
}

func render(in manifest.Input, val any) []string {
	switch v := val.(type) {
	case string:
		return []string{v}
	case bool:
		return []string{strconv.FormatBool(v)}
	case []any:
		out := make([]string, len(v))
		for i, x := range v {
			out[i] = x.(string)
		}
		return out
	case []string:
		return v
	}
	f, _ := toFloat(val)
	if in.Type == manifest.TypeInteger {
		return []string{strconv.FormatInt(int64(f), 10)}
	}
	return []string{strconv.FormatFloat(f, 'f', -1, 64)}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// Options for Command.
type Options struct {
	// Dir is the working directory. Empty means the caller's directory.
	Dir string
	// ExtraEnv is added on top of the current environment (NAME=value).
	ExtraEnv []string
	// EventsPath sets MAGGOT_EVENTS when not empty.
	EventsPath string
}

// Argv returns the full command line for a tool in toolDir.
func Argv(toolDir string, m *manifest.Manifest, args []string) ([]string, error) {
	entry, err := m.EntryPath(toolDir)
	if err != nil {
		return nil, err
	}
	argv := append(append([]string{}, m.Entry.Interpreter...), entry)
	return append(argv, args...), nil
}

// Command builds an *exec.Cmd ready to start. Stdio is left to the caller.
func Command(toolDir string, m *manifest.Manifest, args []string, opts Options) (*exec.Cmd, error) {
	argv, err := Argv(toolDir, m, args)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = opts.Dir
	cmd.Env = append(os.Environ(),
		EnvToolDir+"="+toolDir,
		EnvToolID+"="+m.ID,
		EnvToolVersion+"="+m.Version,
	)
	if opts.EventsPath != "" {
		cmd.Env = append(cmd.Env, EnvEvents+"="+opts.EventsPath)
	}
	cmd.Env = append(cmd.Env, opts.ExtraEnv...)
	return cmd, nil
}
