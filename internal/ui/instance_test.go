package ui

import (
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestRunningAndTryRemote(t *testing.T) {
	t.Setenv("MAGGOT_CONFIG_DIR", t.TempDir())
	t.Setenv("MAGGOT_DATA_DIR", t.TempDir())
	t.Setenv("MAGGOT_SECRETS", "file")
	if _, _, ok := Running(); ok {
		t.Fatal("nothing should be running")
	}
	s, err := New("test", "")
	if err != nil {
		t.Fatal(err)
	}
	ln, link, err := s.Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	go http.Serve(ln, s.Handler())
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if !strings.Contains(link, "token=") {
		t.Fatal(link)
	}
	MarkRunning(port)
	base, token, ok := Running()
	if !ok || token != s.Token {
		t.Fatalf("running: %v %v", base, ok)
	}
	dir := writeShellTool(t, t.TempDir(), "remote", "0.1.0")
	ref, err := TryRemote(base, token, dir)
	if err != nil || ref != "try/remote" {
		t.Fatalf("try: %q %v", ref, err)
	}
	if got := ToolURL(base, token, ref); !strings.HasSuffix(got, "#/tool/try%2Fremote") {
		t.Fatalf("url: %s", got)
	}
	if _, err := TryRemote(base, token, t.TempDir()); err == nil {
		t.Fatal("folder without tool.json should fail")
	}
	MarkStopped()
	if _, _, ok := Running(); ok {
		t.Fatal("should be stopped")
	}
}
