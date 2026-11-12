package search

import (
	"reflect"
	"testing"

	"github.com/nxnom/maggot/internal/manifest"
	"github.com/nxnom/maggot/internal/toolbox"
)

func tool(id, name, desc string, tags ...string) *toolbox.Tool {
	return &toolbox.Tool{
		Toolbox:  &toolbox.Toolbox{Handle: "p"},
		Manifest: &manifest.Manifest{ID: id, Name: name, Description: desc, Tags: tags},
	}
}

func refs(hits []Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, h.Tool.Manifest.ID)
	}
	return out
}

func TestWords(t *testing.T) {
	got := Words("Remove the image-background, to PNG!")
	want := []string{"remove", "image", "background", "png"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestRank(t *testing.T) {
	tools := []*toolbox.Tool{
		tool("background-remover", "Background Remover", "Cut the subject out of a photo.", "rembg", "transparent"),
		tool("image-resize", "Image Resize", "Resize images to a target size.", "thumbnail"),
		tool("video-convert", "Video Convert", "Convert videos with ffmpeg.", "mp4", "webm"),
		tool("pdf-merge", "PDF Merge", "Merge PDF files into one."),
	}
	cases := map[string][]string{
		"remove image background": {"background-remover", "image-resize"},
		"make thumbnails":         {"image-resize"},
		"convert mp4 to webm":     {"video-convert"},
		"rembg":                   {"background-remover"},
		"combine pdfs":            {"pdf-merge"},
		"the a to":                nil,
		"spreadsheet":             nil,
	}
	for q, want := range cases {
		if got := refs(Rank(tools, q)); !reflect.DeepEqual(got, want) {
			t.Errorf("Rank(%q) = %v, want %v", q, got, want)
		}
	}
}

func TestWordMatch(t *testing.T) {
	for _, c := range []struct {
		q, w string
		want bool
	}{
		{"remove", "remover", true},
		{"pdfs", "pdf", true},
		{"im", "image", false},
		{"video", "audio", false},
	} {
		if wordMatch(c.q, c.w) != c.want {
			t.Errorf("wordMatch(%q,%q)", c.q, c.w)
		}
	}
}

func TestClosest(t *testing.T) {
	tools := []*toolbox.Tool{
		tool("youtube-download", "YouTube Download", "Download video or audio.", "yt-dlp"),
		tool("pdf-merge", "PDF Merge", "Merge PDF files into one."),
	}
	if hits := Rank(tools, "yotube donwload"); len(hits) != 0 {
		t.Fatalf("typos should not match words: %v", refs(hits))
	}
	got := Closest(tools, "yotube donwload", 3)
	if len(got) == 0 || got[0].Manifest.ID != "youtube-download" {
		t.Fatalf("closest: %v", got)
	}
	if got := Closest(tools, "zzzz qqqq", 3); len(got) != 0 {
		t.Fatalf("nothing similar: %v", got)
	}
}
