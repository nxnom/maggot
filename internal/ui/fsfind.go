package ui

import (
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Media types the system table may not know; used for accept patterns
// like "audio/*".
var extraTypes = map[string]string{
	".wav": "audio/wav", ".m4a": "audio/mp4", ".flac": "audio/flac", ".ogg": "audio/ogg", ".opus": "audio/opus",
	".mp3": "audio/mpeg", ".aac": "audio/aac", ".aiff": "audio/aiff", ".mkv": "video/x-matroska", ".mov": "video/quicktime",
	".mp4": "video/mp4", ".webm": "video/webm", ".avi": "video/x-msvideo", ".heic": "image/heic", ".webp": "image/webp",
	".avif": "image/avif", ".json": "application/json", ".md": "text/markdown", ".srt": "text/plain", ".vtt": "text/vtt",
}

func typeOf(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if t, found := extraTypes[ext]; found {
		return t
	}
	t, _, _ := strings.Cut(mime.TypeByExtension(ext), ";")
	return t
}

// parseAccept splits "?accept=.json,audio/*" into patterns.
func parseAccept(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// accepts reports whether a file name matches an input's accept list:
// extensions (".json") or media types ("audio/*", "application/pdf").
func accepts(name string, accept []string) bool {
	if len(accept) == 0 {
		return true
	}
	lower := strings.ToLower(name)
	typ := ""
	for _, a := range accept {
		if strings.HasPrefix(a, ".") {
			if strings.HasSuffix(lower, a) {
				return true
			}
			continue
		}
		if typ == "" {
			typ = typeOf(name)
		}
		if typ == "" {
			continue
		}
		if prefix, found := strings.CutSuffix(a, "/*"); found {
			if strings.HasPrefix(typ, prefix+"/") {
				return true
			}
		} else if typ == a {
			return true
		}
	}
	return false
}

const (
	findMaxResults = 200
	findMaxVisited = 50_000
	findMaxDepth   = 10
	findTimeout    = 3 * time.Second
)

// apiFSFind searches a folder and its subfolders for names that contain
// every word of q. It stops at a limit so a search from $HOME stays fast.
func (s *Server) apiFSFind(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	root, err := s.fsPath(q.Get("path"))
	if err != nil {
		return err
	}
	words := strings.Fields(strings.ToLower(q.Get("q")))
	if len(words) == 0 {
		return ok(w, map[string]any{"entries": []fsEntry{}})
	}
	files := q.Get("files") == "1"
	hidden := q.Get("hidden") == "1"
	accept := parseAccept(q.Get("accept"))
	deadline := time.Now().Add(findTimeout)
	out := []fsEntry{}
	visited := 0
	partial := false
	// Breadth first, so nearby matches come before deep ones.
	queue := []string{root}
	for depth := 0; len(queue) > 0 && depth <= findMaxDepth; depth++ {
		var next []string
		for _, dir := range queue {
			if len(out) >= findMaxResults || visited >= findMaxVisited || time.Now().After(deadline) {
				partial = true
				break
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				visited++
				name := e.Name()
				if !hidden && strings.HasPrefix(name, ".") {
					continue
				}
				full := filepath.Join(dir, name)
				isDir := e.IsDir() // symlinked folders are not followed
				if isDir && !skipDirs[name] && e.Type()&fs.ModeSymlink == 0 {
					next = append(next, full)
				}
				if !isDir && (!files || !accepts(name, accept)) {
					continue
				}
				if !matchesAll(strings.ToLower(name), words) {
					continue
				}
				en := fsEntry{Name: name, Path: full, Dir: isDir}
				if rel, err := filepath.Rel(root, full); err == nil {
					en.Rel = filepath.ToSlash(rel)
				}
				if !isDir {
					if info, err := e.Info(); err == nil {
						en.Size = info.Size()
					}
				}
				out = append(out, en)
				if len(out) >= findMaxResults {
					break
				}
			}
		}
		queue = next
	}
	if len(queue) > 0 && !partial && len(out) < findMaxResults {
		partial = true // stopped at the depth limit
	}
	return ok(w, map[string]any{"path": root, "entries": out, "partial": partial})
}

func matchesAll(name string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(name, w) {
			return false
		}
	}
	return true
}
