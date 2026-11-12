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
		{"don", "donwload", false},
		{"thumbnail", "thumbnails", true},
	} {
		if wordMatch(c.q, c.w) != c.want {
			t.Errorf("wordMatch(%q,%q)", c.q, c.w)
		}
	}
}

func TestClosest(t *testing.T) {
	tools := []*toolbox.Tool{
		tool("media-download", "Media Download", "Download video or audio from YouTube and other sites.", "yt-dlp"),
		tool("pdf-merge", "PDF Merge", "Merge PDF files into one."),
	}
	if hits := Rank(tools, "yotube donwload"); len(hits) != 0 {
		t.Fatalf("typos should not match words: %v", refs(hits))
	}
	got := Closest(tools, "yotube donwload", 3)
	if len(got) == 0 || got[0].Manifest.ID != "media-download" {
		t.Fatalf("closest: %v", got)
	}
	if got := Closest(tools, "zzzz qqqq", 3); len(got) != 0 {
		t.Fatalf("nothing similar: %v", got)
	}
	// A long description must not attract unrelated queries.
	tts := tool("voxcpm-tts", "VoxCPM2 TTS", "Generate speech locally with VoxCPM2 (multilingual, incl. Burmese/English/Chinese): "+
		"voice design from a text description, or voice cloning from a reference WAV (+ transcript for closer match). "+
		"Single line or batch JSON/TXT with named voices, multiple takes, seed, tempo change (e.g. 1.4x). Runs on Apple MPS / CUDA / CPU.",
		"tts", "text-to-speech", "voice", "voice-clone", "speech", "narrator", "voiceover")
	all := append(tools, tts)
	for _, q := range []string{"banana spaceship", "banana-spaceship", "spreadsheet pivot", "zzzz"} {
		if got := Closest(all, q, 3); len(got) != 0 {
			t.Errorf("%q should suggest nothing, got %v", q, got[0].Manifest.ID)
		}
	}
	if got := Closest(all, "voise clonning", 3); len(got) == 0 || got[0].Manifest.ID != "voxcpm-tts" {
		t.Errorf("typos should still find the TTS tool: %v", got)
	}
}
