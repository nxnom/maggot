// Package pack exports a tool or skill folder to a .maggot archive (a zip
// file) and imports archives or folders into a toolbox.
package pack

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/nxnom/maggot/internal/agentskill"
	"github.com/nxnom/maggot/internal/manifest"
)

// Ext is the archive extension. Archives are plain zip files.
const Ext = ".maggot"

// IgnoreFile lists extra paths to leave out of an archive (one glob per line).
const IgnoreFile = ".maggotignore"

// Limits for archives being imported.
var (
	MaxFiles            = 20000
	MaxTotalBytes int64 = 2 << 30 // 2 GiB uncompressed
)

var alwaysIgnored = []string{".git", ".DS_Store", "__pycache__", IgnoreFile}

// fixed mod time keeps archives reproducible.
var zipTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// FileName is the default archive name for a manifest.
func FileName(m *manifest.Manifest) string { return m.ID + "-" + m.Version + Ext }

// SkillFileName is the default archive name for a skill.
func SkillFileName(sk *agentskill.Skill) string {
	if sk.Version == "" {
		return sk.Meta.Name + Ext
	}
	return sk.Meta.Name + "-" + sk.Version + Ext
}

// Pack writes the tool in dir to out. The manifest must be valid.
func Pack(dir, out string) (*manifest.Manifest, error) {
	m, err := manifest.Load(dir)
	if err != nil {
		return nil, err
	}
	return m, writeZip(dir, out)
}

// PackSkill writes the skill in dir to out. SKILL.md must be valid.
func PackSkill(dir, out string) (*agentskill.Skill, error) {
	sk, err := agentskill.Load(dir)
	if err != nil {
		return nil, err
	}
	return sk, writeZip(dir, out)
}

func writeZip(dir, out string) error {
	files, err := collect(dir)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), ".pack-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	zw := zip.NewWriter(tmp)
	for _, rel := range files {
		if err := addFile(zw, dir, rel); err != nil {
			zw.Close()
			tmp.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), out)
}

func loadIgnore(dir string) []string {
	patterns := append([]string{}, alwaysIgnored...)
	f, err := os.Open(filepath.Join(dir, IgnoreFile))
	if err != nil {
		return patterns
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, strings.TrimSuffix(line, "/"))
		}
	}
	return patterns
}

// ignored matches a pattern against the full relative path or any single
// path segment, so "node_modules" and "build/*.o" both work.
func ignored(rel string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, rel); ok {
			return true
		}
		if !strings.Contains(p, "/") {
			for _, seg := range strings.Split(rel, "/") {
				if ok, _ := path.Match(p, seg); ok {
					return true
				}
			}
		}
	}
	return false
}

// collect lists files (slash paths, sorted) to put in the archive.
func collect(dir string) ([]string, error) {
	patterns := loadIgnore(dir)
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if ignored(rel, patterns) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink; symlinks are not supported in archives (add it to %s)", rel, IgnoreFile)
		}
		if d.Type().IsRegular() {
			files = append(files, rel)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func addFile(zw *zip.Writer, dir, rel string) error {
	full := filepath.Join(dir, filepath.FromSlash(rel))
	info, err := os.Stat(full)
	if err != nil {
		return err
	}
	h, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	h.Name = rel
	h.Method = zip.Deflate
	h.Modified = zipTime
	// Keep only the permission bits we care about: everything is 0644 or 0755.
	mode := fs.FileMode(0o644)
	if info.Mode()&0o111 != 0 {
		mode = 0o755
	}
	h.SetMode(mode)
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	f, err := os.Open(full)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// Kinds of things an archive or folder can hold.
const (
	KindTool  = "tool"
	KindSkill = "skill"
)

// Preview describes what an import would do, for the confirmation prompt.
type Preview struct {
	// Kind is "tool" (Manifest is set) or "skill" (Skill is set).
	Kind     string             `json:"kind"`
	ID       string             `json:"id"`
	Version  string             `json:"version"`
	Manifest *manifest.Manifest `json:"manifest,omitempty"`
	Skill    *agentskill.Skill  `json:"skill,omitempty"`
	Files    int                `json:"files"`
	Bytes    int64              `json:"bytes"`
	Target   string             `json:"target"`
	// Existing is the version already installed, or "" for a new tool.
	Existing string `json:"existing,omitempty"`
	// Action is "install", "upgrade", "downgrade" or "reinstall".
	Action string `json:"action"`
}

// Staged is an import that is extracted and validated but not yet in place.
// Call Commit to install it or Discard to clean up.
type Staged struct {
	Preview
	tmpDir string
}

// Stage extracts an archive (.maggot / .zip) or copies a folder into a
// hidden temp folder inside toolsDir and validates it. A tool goes to
// toolsDir; a skill goes to the skills folder next to it.
func Stage(src, toolsDir string) (*Staged, error) {
	return StageIn(src, toolsDir, filepath.Join(filepath.Dir(toolsDir), "skills"))
}

// KindOf says whether dir holds a tool (tool.json) or a skill (SKILL.md),
// or "" for neither. A folder with both is a tool.
func KindOf(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
		return KindTool
	}
	if agentskill.Exists(dir) {
		return KindSkill
	}
	return ""
}

// StageIn is Stage with an explicit skills folder.
func StageIn(src, toolsDir, skillsDir string) (*Staged, error) {
	info, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(toolsDir, ".import-")
	if err != nil {
		return nil, err
	}
	s := &Staged{tmpDir: tmp}
	fail := func(err error) (*Staged, error) {
		os.RemoveAll(tmp)
		return nil, err
	}
	if info.IsDir() {
		err = copyDir(src, tmp, &s.Preview)
	} else {
		err = extract(src, tmp, &s.Preview)
	}
	if err != nil {
		return fail(err)
	}
	s.Action = "install"
	if KindOf(tmp) == KindSkill {
		sk, err := agentskill.Load(tmp)
		if err != nil {
			return fail(err)
		}
		if err := os.MkdirAll(skillsDir, 0o755); err != nil {
			return fail(err)
		}
		s.Kind, s.Skill, s.ID, s.Version = KindSkill, sk, sk.Meta.Name, sk.Version
		s.Target = filepath.Join(skillsDir, sk.Meta.Name)
		if old, err := agentskill.Load(s.Target); err == nil {
			s.Existing = old.Version
			if s.Existing == "" {
				s.Existing = "unknown"
			}
			s.Action = compare(sk.Version, old.Version)
		} else if _, statErr := os.Stat(s.Target); statErr == nil {
			s.Existing = "unknown"
			s.Action = "reinstall"
		}
		return s, nil
	}
	m, err := manifest.Load(tmp)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			err = fmt.Errorf("%s has no %s (tool) or %s (skill)", src, manifest.FileName, agentskill.FileName)
		}
		return fail(err)
	}
	s.Kind, s.Manifest, s.ID, s.Version = KindTool, m, m.ID, m.Version
	s.Target = filepath.Join(toolsDir, m.ID)
	if old, err := manifest.Load(s.Target); err == nil {
		s.Existing = old.Version
		s.Action = compare(m.Version, old.Version)
	} else if _, statErr := os.Stat(s.Target); statErr == nil {
		s.Existing = "unknown"
		s.Action = "reinstall"
	}
	return s, nil
}

func compare(newV, oldV string) string {
	n, err1 := semver.NewVersion(newV)
	o, err2 := semver.NewVersion(oldV)
	if err1 != nil || err2 != nil {
		return "reinstall"
	}
	switch n.Compare(o) {
	case 1:
		return "upgrade"
	case -1:
		return "downgrade"
	}
	return "reinstall"
}

// NeedsForce reports whether the import would not move to a newer version.
func (s *Staged) NeedsForce() bool { return s.Action == "downgrade" || s.Action == "reinstall" }

// Commit moves the staged tool into place, replacing any existing folder.
func (s *Staged) Commit() error {
	// The temp folder is private (0700); the installed folder is not.
	if err := os.Chmod(s.tmpDir, 0o755); err != nil {
		return err
	}
	backup := ""
	if _, err := os.Stat(s.Target); err == nil {
		backup = s.tmpDir + ".old"
		if err := os.Rename(s.Target, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(s.tmpDir, s.Target); err != nil {
		if backup != "" {
			os.Rename(backup, s.Target)
		}
		return err
	}
	if backup != "" {
		return os.RemoveAll(backup)
	}
	return nil
}

// Discard removes the staged files.
func (s *Staged) Discard() error { return os.RemoveAll(s.tmpDir) }

// safeRel checks an archive entry name and returns a clean slash path.
func safeRel(name string) (string, error) {
	if strings.Contains(name, `\`) {
		return "", fmt.Errorf("archive entry %q uses backslashes", name)
	}
	clean := path.Clean(name)
	if path.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, "../") || clean == "." || strings.Contains(name, ":") {
		return "", fmt.Errorf("archive entry %q points outside the tool folder", name)
	}
	return clean, nil
}

func extract(src, dst string, p *Preview) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("%s is not a valid archive: %w", src, err)
	}
	defer zr.Close()

	// Accept archives made by zipping the folder itself: one top folder that
	// holds tool.json or SKILL.md.
	prefix := ""
	hasRootManifest := false
	tops := map[string]bool{}
	for _, f := range zr.File {
		if f.Name == manifest.FileName || f.Name == agentskill.FileName {
			hasRootManifest = true
		}
		if strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		tops[strings.SplitN(f.Name, "/", 2)[0]] = true
	}
	if !hasRootManifest && len(tops) == 1 {
		for top := range tops {
			prefix = top + "/"
		}
	}

	var total int64
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		name := strings.TrimPrefix(f.Name, prefix)
		if name == "" || strings.HasSuffix(name, "/.DS_Store") || name == ".DS_Store" {
			continue
		}
		rel, err := safeRel(name)
		if err != nil {
			return err
		}
		mode := f.Mode()
		if mode&fs.ModeSymlink != 0 {
			return fmt.Errorf("archive entry %q is a symlink; not allowed", f.Name)
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if mode.IsDir() || strings.HasSuffix(f.Name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if !mode.IsRegular() {
			return fmt.Errorf("archive entry %q is not a regular file", f.Name)
		}
		p.Files++
		if p.Files > MaxFiles {
			return fmt.Errorf("archive has more than %d files", MaxFiles)
		}
		n, err := writeEntry(f, target, mode, MaxTotalBytes-total)
		if err != nil {
			return err
		}
		total += n
	}
	p.Bytes = total
	return nil
}

func writeEntry(f *zip.File, target string, mode fs.FileMode, budget int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err
	}
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	perm := fs.FileMode(0o644)
	if mode&0o111 != 0 {
		perm = 0o755
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return 0, fmt.Errorf("archive entry %q: %w", f.Name, err)
	}
	// Read one byte past the budget so oversize archives are detected even
	// when the header lies about the size.
	n, err := io.Copy(out, io.LimitReader(rc, budget+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, err
	}
	if n > budget {
		return n, errors.New("archive is too large when extracted")
	}
	return n, nil
}

// CopyTool copies a tool or skill folder to dst (which must not exist),
// leaving out the same files as archives do.
func CopyTool(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if err := copyDir(src, dst, &Preview{}); err != nil {
		os.RemoveAll(dst)
		return err
	}
	return nil
}

func copyDir(src, dst string, p *Preview) error {
	files, err := collect(src)
	if err != nil {
		return err
	}
	for _, rel := range files {
		from := filepath.Join(src, filepath.FromSlash(rel))
		to := filepath.Join(dst, filepath.FromSlash(rel))
		info, err := os.Stat(from)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := copyFile(from, to, info.Mode().Perm()); err != nil {
			return err
		}
		p.Files++
		p.Bytes += info.Size()
	}
	return nil
}

func copyFile(from, to string, perm fs.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
