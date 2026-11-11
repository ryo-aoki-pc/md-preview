package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-preview/internal/resolve"
	"github.com/ryo-aoki-pc/md-preview/internal/session"
)

func TestEditFixture(t *testing.T) {
	for _, r := range []request{{Edit: "file"}, {Edit: "buffer", Live: true}} {
		a := &application{}
		if err := a.start(r, &output{encoder: json.NewEncoder(io.Discard)}); err == nil {
			a.close("exit")
			t.Fatal("未対応の指定を許した")
		}
	}
	root, err := resolve.RealPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "main.md")
	if err := os.WriteFile(path, []byte("本文"), 0644); err != nil {
		t.Fatal(err)
	}
	a := &application{}
	defer a.close("exit")
	if err := a.start(request{Edit: "file", Live: true, Root: root}, &output{encoder: json.NewEncoder(io.Discard)}); err != nil {
		t.Fatal(err)
	}
	if err := a.live.SetDocument(session.Document{Path: path, Text: "本文", Rev: 1}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		src, ok := a.live.Source(nil)
		if ok && src.Render > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("原文が無い")
		}
		time.Sleep(5 * time.Millisecond)
	}
	src, _ := a.live.Source(nil)
	body, _ := json.Marshal(map[string]any{"render": src.Render, "range": "1:1-1:6", "text": "変更"})
	req, err := http.NewRequest("POST", a.server.URL+"_/edit", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", strings.TrimSuffix(strings.SplitN(a.server.URL, "/"+a.server.Token, 2)[0], "/"))
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status, string(b))
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "変更" {
		t.Fatal(string(got), err)
	}
}
