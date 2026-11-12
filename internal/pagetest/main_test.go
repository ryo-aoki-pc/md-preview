package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ryo-aoki-pc/md-tools/internal/resolve"
	"github.com/ryo-aoki-pc/md-tools/internal/session"
)

func TestProtocolAndEOF(t *testing.T) {
	input, commands := io.Pipe()
	responses, stdout := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- run(input, stdout)
		_ = stdout.Close()
	}()
	defer commands.Close()
	defer responses.Close()
	encoder := json.NewEncoder(commands)
	decoder := json.NewDecoder(responses)
	request := func(value any) map[string]any {
		t.Helper()
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
		var response map[string]any
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	ready := request(map[string]any{"id": 1, "op": "start", "render": map[string]any{"html": "<p>初回</p>", "rev": 1}})
	if ready["type"] != "ready" || ready["id"] != float64(1) {
		t.Fatal(ready)
	}
	url, ok := ready["url"].(string)
	if !ok || !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.HasSuffix(url, "/"+ready["token"].(string)+"/") {
		t.Fatal(ready)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	page, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(page.Body)
	_ = page.Body.Close()
	if err != nil || page.StatusCode != 200 || !strings.Contains(string(body), "gp-content") {
		t.Fatalf("ページの応答: status=%d, error=%v", page.StatusCode, err)
	}
	events, err := client.Get(url + "_/events")
	if err != nil {
		t.Fatal(err)
	}
	defer events.Body.Close()
	if events.StatusCode != 200 || !strings.HasPrefix(events.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal(events.Status, events.Header)
	}
	reader := bufio.NewScanner(events.Body)
	next := func(name string) map[string]any {
		t.Helper()
		for reader.Scan() {
			if reader.Text() != "event: "+name {
				continue
			}
			if !reader.Scan() {
				break
			}
			var data map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(reader.Text(), "data: ")), &data); err != nil {
				t.Fatal(err)
			}
			return data
		}
		t.Fatalf("SSE の %s が届きません: %v", name, reader.Err())
		return nil
	}
	if data := next("render"); data["html"] != "<p>初回</p>" {
		t.Fatal(data)
	}
	response := request(map[string]any{"id": 2, "op": "broadcast", "name": "render", "data": map[string]any{"html": "<p>更新</p>", "rev": 2, "extra": "<>&日本語"}})
	if response["type"] != "ok" || response["id"] != float64(2) {
		t.Fatal(response)
	}
	if data := next("render"); data["html"] != "<p>更新</p>" || data["rev"] != float64(2) || data["extra"] != "<>&日本語" {
		t.Fatal(data)
	}
	if err := commands.Close(); err != nil {
		t.Fatal(err)
	}
	if data := next("stop"); data["reason"] != "exit" {
		t.Fatal(data)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("標準入力が閉じてもサーバーが終了しません")
	}
	if response, err := client.Get(url); err == nil {
		_ = response.Body.Close()
		t.Fatal("終了後もサーバーが待ち受けています")
	}
}

func TestFixtureDiagram(t *testing.T) {
	diagram := &fixtureDiagram{done: make(chan struct{})}
	defer diagram.Close()
	for _, source := range []string{"日本語", "multiple", "error200"} {
		svg, err := diagram.Render(context.Background(), source)
		if err != nil || string(svg) != diagramSVG {
			t.Fatalf("成功する原文 %q: %v", source, err)
		}
	}
	for _, source := range []string{"plain", "png", "invalid-utf8", "oversize", "empty"} {
		if _, err := diagram.Render(context.Background(), source); err == nil {
			t.Fatalf("失敗する原文 %q が成功しました", source)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := diagram.Render(ctx, "slow"); err == nil {
		t.Fatal("遅い応答の取り消しが届きません")
	}
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelWait()
	if _, err := diagram.Render(waitCtx, "hang"); err == nil {
		t.Fatal("応答待ちの取り消しが届きません")
	}
}

func TestReviewLaunchFixture(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "成功"
		if fail {
			name = "失敗"
		}
		t.Run(name, func(t *testing.T) {
			root, err := resolve.RealPath(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "main.md")
			if err := os.WriteFile(file, []byte("本文\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var buffer bytes.Buffer
			out := &output{encoder: json.NewEncoder(&buffer)}
			app := &application{}
			if err := app.start(request{Root: root, Live: true, Review: true, LaunchFail: fail}, out); err != nil {
				t.Fatal(err)
			}
			defer app.close("exit")
			if err := app.live.SetDocument(session.Document{Path: file, Text: "本文\n", Rev: 1}); err != nil {
				t.Fatal(err)
			}
			var render uint64
			until := time.Now().Add(5 * time.Second)
			for time.Now().Before(until) {
				if source, ok := app.live.Source(nil); ok {
					render = source.Render
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if render == 0 {
				t.Fatal("コメントの試験用の描画が届きません")
			}
			client := &http.Client{Timeout: 5 * time.Second}
			post := func(route, body string) (int, map[string]any) {
				t.Helper()
				r, err := http.NewRequest("POST", app.server.URL+route, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", "http://"+r.URL.Host)
				response, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				var data map[string]any
				if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
					t.Fatal(err)
				}
				return response.StatusCode, data
			}
			status, data := post("_/review/prepare", fmt.Sprintf(`{"render":%d,"comments":[{"target":{"kind":"block","tag":"p","range":"1:1-1:6"},"text":"直してください"}]}`, render))
			if status != 200 {
				t.Fatal(status, data)
			}
			id := data["id"]
			for i := 0; i < 2; i++ {
				status, data = post("_/review/launch", fmt.Sprintf(`{"id":%q}`, id))
				if fail {
					if status != 503 || data["error"].(map[string]any)["code"] != "notfound" {
						t.Fatal(status, data)
					}
				} else if status != 200 || data["launched"] != true || i == 1 && data["again"] != true {
					t.Fatal(status, data)
				}
			}
			out.mu.Lock()
			records := buffer.String()
			out.mu.Unlock()
			want := 1
			if fail {
				want = 2
			}
			lines := strings.Split(strings.TrimSpace(records), "\n")
			if len(lines) != want {
				t.Fatal("偽の起動の記録数が違います", records)
			}
			for _, line := range lines {
				var record map[string]any
				if json.Unmarshal([]byte(line), &record) != nil || record["type"] != "launch" || record["root"] != root {
					t.Fatal("偽の起動へ渡るルートが違います", line)
				}
			}
		})
	}
}
