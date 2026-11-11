package manifest

import (
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	binPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	envPattern      = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	inputPattern    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	flagPattern     = regexp.MustCompile(`^--?[A-Za-z0-9][A-Za-z0-9-]*$`)
	PackageManagers = []string{"brew", "port", "apt", "dnf", "yum", "pacman", "apk", "zypper", "nix", "winget", "scoop", "choco", "npm", "pipx", "uv", "cargo", "go"}
	previews        = map[string]bool{"": true, "none": true, "file": true, "image": true, "text": true, "json": true}
)

// Validate checks every rule and returns all problems joined together.
func (m *Manifest) Validate() error {
	v := &validator{}
	if m.Schema != SchemaVersion {
		v.add("schema", "must be %d (got %d)", SchemaVersion, m.Schema)
	}
	if !idPattern.MatchString(m.ID) || len(m.ID) > 64 {
		v.add("id", "must be lowercase kebab-case, max 64 chars (got %q)", m.ID)
	}
	if _, err := semver.StrictNewVersion(m.Version); err != nil {
		v.add("version", "must be semver like 1.2.3 (got %q)", m.Version)
	}
	if strings.TrimSpace(m.Name) == "" {
		v.add("name", "is required")
	}
	if strings.TrimSpace(m.Description) == "" {
		v.add("description", "is required")
	}
	if len(m.Categories) > 5 {
		v.add("categories", "max 5 categories")
	}
	seen := map[string]bool{}
	for i, c := range m.Categories {
		k := strings.ToLower(strings.TrimSpace(c))
		if k == "" {
			v.add(fmt.Sprintf("categories[%d]", i), "is empty")
		} else if seen[k] {
			v.add(fmt.Sprintf("categories[%d]", i), "duplicate %q", c)
		}
		seen[k] = true
	}
	v.entry(m.Entry)
	v.requires(m.Requires)
	v.env(m.Env)
	v.inputs(m.Inputs, m.Outputs)
	if m.Outputs != nil {
		if !previews[m.Outputs.Preview] {
			v.add("outputs.preview", "must be one of none, file, image, text, json (got %q)", m.Outputs.Preview)
		}
		if m.Outputs.DirFlag != "" && !flagPattern.MatchString(m.Outputs.DirFlag) {
			v.add("outputs.dir_flag", "invalid flag %q", m.Outputs.DirFlag)
		}
	}
	return v.err()
}

type validator struct{ problems []string }

func (v *validator) add(field, format string, args ...any) {
	v.problems = append(v.problems, field+": "+fmt.Sprintf(format, args...))
}

func (v *validator) err() error {
	if len(v.problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: v.problems}
}

// ValidationError lists every problem found in a manifest.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string {
	return "invalid tool.json:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// IsValidationError reports whether err is (or wraps) a ValidationError.
func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

func (v *validator) entry(e Entry) {
	switch {
	case e.Path == "":
		v.add("entry.path", "is required")
	case strings.Contains(e.Path, `\`):
		v.add("entry.path", "use forward slashes (got %q)", e.Path)
	case path.IsAbs(e.Path) || path.Clean(e.Path) != e.Path || e.Path == "." || strings.HasPrefix(e.Path, "../") || e.Path == "..":
		v.add("entry.path", "must be a clean relative path inside the tool folder (got %q)", e.Path)
	case e.Path == FileName:
		v.add("entry.path", "cannot be %s", FileName)
	}
	for i, s := range e.Interpreter {
		if strings.TrimSpace(s) == "" {
			v.add(fmt.Sprintf("entry.interpreter[%d]", i), "is empty")
		}
	}
}

func (v *validator) requires(reqs []Requirement) {
	seen := map[string]bool{}
	known := map[string]bool{}
	for _, pm := range PackageManagers {
		known[pm] = true
	}
	for i, r := range reqs {
		f := fmt.Sprintf("requires[%d]", i)
		if !binPattern.MatchString(r.Bin) {
			v.add(f+".bin", "invalid binary name %q", r.Bin)
		}
		if seen[r.Bin] {
			v.add(f+".bin", "duplicate %q", r.Bin)
		}
		seen[r.Bin] = true
		if r.Version != "" {
			if _, err := semver.NewConstraint(r.Version); err != nil {
				v.add(f+".version", "invalid constraint %q: %v", r.Version, err)
			}
		}
		for pm, pkg := range r.Install {
			if !known[pm] {
				v.add(f+".install", "unknown package manager %q (known: %s)", pm, strings.Join(PackageManagers, ", "))
			}
			if strings.TrimSpace(pkg) == "" || strings.ContainsAny(pkg, ";&|`$<>\n") {
				v.add(f+".install."+pm, "invalid package name %q", pkg)
			}
		}
	}
}

func (v *validator) env(vars []EnvVar) {
	seen := map[string]bool{}
	for i, e := range vars {
		f := fmt.Sprintf("env[%d].name", i)
		switch {
		case !envPattern.MatchString(e.Name):
			v.add(f, "must be UPPER_SNAKE_CASE (got %q)", e.Name)
		case strings.HasPrefix(e.Name, "MAGGOT_"):
			v.add(f, "the MAGGOT_ prefix is reserved (got %q)", e.Name)
		case seen[e.Name]:
			v.add(f, "duplicate %q", e.Name)
		}
		seen[e.Name] = true
	}
}

func (v *validator) inputs(inputs []Input, out *Outputs) {
	names := map[string]bool{}
	flags := map[string]bool{}
	if out != nil && out.DirFlag != "" {
		flags[out.DirFlag] = true
	}
	variadicAt := -1
	for i, in := range inputs {
		f := fmt.Sprintf("inputs[%d]", i)
		if !inputPattern.MatchString(in.Name) {
			v.add(f+".name", "must be lower_snake_case (got %q)", in.Name)
		} else if names[in.Name] {
			v.add(f+".name", "duplicate %q", in.Name)
		}
		names[in.Name] = true
		if !inputTypes[in.Type] {
			v.add(f+".type", "unknown type %q", in.Type)
			continue
		}
		if in.Type == TypeEnum {
			if len(in.Options) == 0 {
				v.add(f+".options", "enum needs at least one option")
			}
			seenOpt := map[string]bool{}
			for j, o := range in.Options {
				if o.Value == "" || seenOpt[o.Value] {
					v.add(fmt.Sprintf("%s.options[%d]", f, j), "empty or duplicate value %q", o.Value)
				}
				seenOpt[o.Value] = true
			}
		} else if len(in.Options) > 0 {
			v.add(f+".options", "only allowed for enum")
		}
		numeric := in.Type == TypeInteger || in.Type == TypeNumber
		if (in.Min != nil || in.Max != nil) && !numeric {
			v.add(f, "min/max only allowed for integer and number")
		}
		if in.Min != nil && in.Max != nil && *in.Min > *in.Max {
			v.add(f, "min is greater than max")
		}
		if len(in.Accept) > 0 && in.Type != TypeFile && in.Type != TypeFiles {
			v.add(f+".accept", "only allowed for file and files")
		}
		if in.Default != nil {
			if err := in.CheckValue(in.Default); err != nil {
				v.add(f+".default", "%v", err)
			}
		}
		// argv mapping
		a := in.Arg
		switch {
		case a.Flag != "" && a.Positional:
			v.add(f+".arg", "use either flag or positional, not both")
		case a.Flag == "" && !a.Positional:
			v.add(f+".arg", "needs a flag or positional: true")
		case a.Flag != "":
			if !flagPattern.MatchString(a.Flag) {
				v.add(f+".arg.flag", "invalid flag %q", a.Flag)
			} else if flags[a.Flag] {
				v.add(f+".arg.flag", "duplicate flag %q", a.Flag)
			}
			flags[a.Flag] = true
		case a.Positional:
			if in.Type == TypeBoolean {
				v.add(f+".arg", "boolean inputs must use a flag")
			}
			if variadicAt >= 0 {
				v.add(f+".arg", "no positional input may follow the variadic input %q", inputs[variadicAt].Name)
			}
			if in.Type == TypeFiles {
				variadicAt = i
			}
		}
		if a.Repeat && (in.Type != TypeFiles || a.Flag == "") {
			v.add(f+".arg.repeat", "only allowed for files inputs with a flag")
		}
		if in.Type == TypeFiles && a.Flag != "" && !a.Repeat {
			v.add(f+".arg", "files inputs with a flag need repeat: true (or use positional)")
		}
	}
}

// CheckValue validates a value (as decoded from JSON) against the input type.
func (in Input) CheckValue(val any) error {
	switch in.Type {
	case TypeString, TypeText, TypeFile, TypeDirectory:
		if _, ok := val.(string); !ok {
			return fmt.Errorf("must be a string")
		}
	case TypeEnum:
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("must be a string")
		}
		for _, o := range in.Options {
			if o.Value == s {
				return nil
			}
		}
		return fmt.Errorf("%q is not one of the options", s)
	case TypeBoolean:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("must be true or false")
		}
	case TypeInteger, TypeNumber:
		n, ok := toFloat(val)
		if !ok {
			return fmt.Errorf("must be a number")
		}
		if in.Type == TypeInteger && n != math.Trunc(n) {
			return fmt.Errorf("must be a whole number")
		}
		if in.Min != nil && n < *in.Min {
			return fmt.Errorf("must be >= %v", *in.Min)
		}
		if in.Max != nil && n > *in.Max {
			return fmt.Errorf("must be <= %v", *in.Max)
		}
	case TypeFiles:
		list, ok := val.([]any)
		if !ok {
			if ss, ok2 := val.([]string); ok2 {
				if len(ss) == 0 && in.Required {
					return fmt.Errorf("needs at least one file")
				}
				return nil
			}
			return fmt.Errorf("must be a list of paths")
		}
		for _, item := range list {
			if _, ok := item.(string); !ok {
				return fmt.Errorf("must be a list of paths")
			}
		}
	}
	return nil
}

func toFloat(val any) (float64, bool) {
	switch n := val.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
